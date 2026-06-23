package player

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

type hlsSession struct {
	id         string
	input      string
	videoCodec string
	tracks     []AudioTrackInfo
	duration   float64
	dir        string
	cmd        *exec.Cmd
	running    bool // true while ffmpeg is actively writing segments
	startSeg   int
	lastUsed   time.Time
	mu         sync.Mutex
	restarting bool
	errCh      chan struct{} // closed when ffmpeg exits with a non-kill error
}

// browserSafeAudioCodecs can be stream-copied into HLS without re-encoding.
var browserSafeAudioCodecs = map[string]bool{
	"aac": true, "mp3": true, "opus": true, "vorbis": true, "flac": true,
}

func newSessionID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// startFfmpeg kills any running process for this session and starts a new one
// exactly at the requested segment offset.
func (s *hlsSession) startFfmpeg(startSeg int) error {
	s.mu.Lock()
	if s.restarting {
		log.Printf("[HLS:%s] startFfmpeg(%d): suppressed — already restarting", s.id, startSeg)
		s.mu.Unlock()
		return nil
	}
	s.restarting = true

	// Kill old process while holding the lock
	oldCmd := s.cmd
	s.running = false
	s.startSeg = startSeg
	s.cmd = nil
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		s.restarting = false
		s.mu.Unlock()
	}()

	// Only Kill() here — the background goroutine launched for oldCmd is the
	// sole owner of its Wait() call. Calling Wait() here races with the
	// goroutine: the loser receives "waitid: no child processes", which doesn't
	// match "signal: killed", so it incorrectly closes errCh and poisons every
	// segment waiter with an immediate 500.
	if oldCmd != nil && oldCmd.Process != nil {
		log.Printf("[HLS:%s] startFfmpeg(%d): killing old ffmpeg pid=%d", s.id, startSeg, oldCmd.Process.Pid)
		_ = oldCmd.Process.Kill()
	}

	// Create a fresh error channel for this ffmpeg run. We assign it now,
	// before launching the goroutine, and pass it explicitly so the goroutine
	// closes *this* channel — not whatever s.errCh happens to be when Wait()
	// eventually returns (which could be a later run's channel after a seek).
	newErrCh := make(chan struct{})
	s.mu.Lock()
	s.errCh = newErrCh
	s.mu.Unlock()

	// Clear segments from the previous run so seek detection
	// reflects only what the current run has written.
	var removed int
	if entries, err := os.ReadDir(s.dir); err == nil {
		for _, entry := range entries {
			if strings.HasSuffix(entry.Name(), ".ts") {
				os.Remove(filepath.Join(s.dir, entry.Name()))
				removed++
			}
		}
	}
	if removed > 0 {
		log.Printf("[HLS:%s] startFfmpeg(%d): cleared %d old segment(s)", s.id, startSeg, removed)
	}

	startTime := float64(startSeg) * 10.0

	var args []string
	if startTime > 0 {
		// -ss before -i: fast input seek to the segment boundary.
		args = append(args, "-ss", fmt.Sprintf("%.3f", startTime))
	}
	args = append(args, "-i", s.input)
	// Timeline handling on a seek is the crux of the cascade bug. The previous
	// approach combined an input -ss with -output_ts_offset, which is two
	// independent attempts to position the output timeline and was producing
	// segments whose MSE-visible PTS span equalled the seek offset.
	//
	// Instead, preserve the seeked source timestamps directly with -copyts:
	// after -ss 830 the frames keep their true PTS (~830), so segment v083
	// lands at 830 on the global timeline with NO separate -output_ts_offset
	// and therefore no possibility of a double shift. -avoid_negative_ts is
	// disabled because make_zero would fight -copyts (it shifts the smallest
	// PTS back to 0, undoing the offset we want to keep).
	if startTime > 0 {
		args = append(args, "-copyts", "-avoid_negative_ts", "disabled")
	} else {
		// Fresh start at 0: keep the original normalisation that fixed
		// negative audio PTS at stream start.
		args = append(args, "-avoid_negative_ts", "make_zero")
	}

	// ── Video output ────────────────────────────────────────────────────────
	// Always re-encode with libx264 and forced keyframes at exactly the
	// -hls_time boundary (every 10 s).
	//
	// Copy mode (h264 → h264) is tempting but BROKEN for HLS: ffmpeg can only
	// cut a segment at an existing keyframe. If the source has a GOP of 11
	// minutes (common in anime BDRips and some streaming encodes), each .ts
	// file will contain 11 minutes of video even though the VOD playlist
	// declares it as 10 seconds. hls.js uses the real MSE buffered range—not
	// the EXTINF value—to decide what to fetch next. After appending an
	// 11-minute segment, it requests the segment starting 11 minutes later,
	// which restarts ffmpeg, which produces another 11-minute segment, ad
	// infinitum. The cascade is impossible to stop without guaranteed
	// 10-second segments.
	//
	// libx264 veryfast on a modern CPU encodes 1080p at ~5–10× real-time, so
	// a 10-second segment is ready in 1–2 s — faster than the network round
	// trip in most cases.
	//
	// Forced keyframes every 10 s, aligned to the global timeline. With -copyts
	// the output PTS already starts at startTime, so the boundary expression must
	// bake that in: keyframes at startTime, startTime+10, startTime+20, …
	// -sc_threshold 0 disables libx264's scene-cut keyframes so the ONLY
	// keyframes are the forced 10 s ones. Without it, scene cuts add extra
	// keyframes and the muxer cuts a little past each 10 s mark, making segments
	// ~11 s. Since the served VOD playlist declares every segment as exactly
	// 10 s, that drift accumulates and throws off later seeks.
	keyFrameExpr := "expr:gte(t,n_forced*10)"
	if startTime > 0 {
		keyFrameExpr = fmt.Sprintf("expr:gte(t,%.3f+n_forced*10)", startTime)
	}
	args = append(args,
		"-map", "0:v:0",
		"-c:v", "libx264",
		"-preset", "veryfast",
		"-crf", "23",
		"-threads", "4",
		"-force_key_frames", keyFrameExpr,
		"-sc_threshold", "0",
		"-an",
	)
	args = append(args,
		"-f", "hls",
		"-hls_time", "10",
		"-hls_list_size", "0",
		"-hls_flags", "independent_segments+temp_file",
		"-start_number", strconv.Itoa(startSeg),
		"-hls_segment_filename", filepath.Join(s.dir, "v%03d.ts"),
		filepath.Join(s.dir, "video.m3u8"),
	)

	// ── Audio outputs ───────────────────────────────────────────────────────
	for i, t := range s.tracks {
		if browserSafeAudioCodecs[strings.ToLower(t.Codec)] {
			// Already browser-safe: copy as-is.
			args = append(args,
				"-map", fmt.Sprintf("0:a:%d", i),
				"-vn", "-c:a", "copy",
			)
		} else {
			// Transcode to AAC-LC stereo.
			// -ac 2           : downmix to stereo — EAC3/DTS are often 5.1/7.1
			//                   and some Chrome MSE builds reject multichannel AAC in TS.
			// -profile:a aac_low : explicitly request AAC-LC (mp4a.40.2); the
			//                   default encoder profile can vary by ffmpeg build.
			// -ar 48000       : normalise to 48 kHz — avoids rate-mismatch errors
			//                   when the source uses an unusual sample rate.
			args = append(args,
				"-map", fmt.Sprintf("0:a:%d", i),
				"-vn", "-c:a", "aac",
				"-profile:a", "aac_low",
				"-ac", "2",
				"-ar", "48000",
				"-b:a", "192k",
			)
		}
		args = append(args,
			"-f", "hls",
			"-hls_time", "10",
			"-hls_list_size", "0",
			"-hls_flags", "independent_segments+temp_file",
			"-start_number", strconv.Itoa(startSeg),
			"-hls_segment_filename", filepath.Join(s.dir, fmt.Sprintf("a%d_%%03d.ts", i)),
			filepath.Join(s.dir, fmt.Sprintf("audio%d.m3u8", i)),
		)
	}

	s.cmd = exec.Command(ffmpegBin(), args...)
	var ffmpegStderr strings.Builder
	s.cmd.Stderr = &ffmpegStderr

	if err := s.cmd.Start(); err != nil {
		return fmt.Errorf("start ffmpeg: %w", err)
	}
	log.Printf("[HLS:%s] startFfmpeg(%d): ffmpeg started with pid=%d, startTime=%.3fs", s.id, startSeg, s.cmd.Process.Pid, startTime)
	s.running = true
	// Run ffmpeg at lower OS priority so it yields to the Go server and the
	// browser under load.  Nice 10 still gives plenty of CPU when idle.
	if s.cmd.Process != nil {
		_ = lowerPriority(s.cmd.Process.Pid)
	}

	go func(cmd *exec.Cmd, sess *hlsSession, ch chan struct{}) {
		err := cmd.Wait()
		sess.mu.Lock()
		// Only clear `running` if this is still the active process. Otherwise an
		// old (just-killed) ffmpeg's Wait() returning late would clobber the
		// running=true that a newer run set, corrupting seek detection.
		if sess.cmd == cmd {
			sess.running = false
		}
		sess.mu.Unlock()
		if err != nil && !strings.Contains(err.Error(), "signal: killed") {
			log.Printf("[HLS:%s] ffmpeg pid=%d exited with error: %v\nstderr:\n%s", sess.id, cmd.Process.Pid, err, ffmpegStderr.String())
			// Close the channel for THIS run, not sess.errCh which may have
			// already been replaced by a subsequent seek's startFfmpeg call.
			select {
			case <-ch: // already closed
			default:
				close(ch)
			}
		} else if err == nil {
			log.Printf("[HLS:%s] ffmpeg pid=%d finished cleanly", sess.id, cmd.Process.Pid)
		} else {
			log.Printf("[HLS:%s] ffmpeg pid=%d killed (expected on seek/stop)", sess.id, cmd.Process.Pid)
		}
	}(s.cmd, s, newErrCh)

	return nil
}

// StartHLSSession launches a single ffmpeg process that writes:
//   - video.m3u8 + v###.ts   — video-only, codec copied
//   - audioN.m3u8 + aN_###.ts — one audio-only stream per track
//   - master.m3u8             — written immediately so the client can start requesting
//
// Audio is copied when the codec is already browser-safe (AAC etc.),
// otherwise transcoded to AAC. Video is always copied.
func (p *Player) StartHLSSession(input string, tracks []AudioTrackInfo, duration float64, videoCodec string) (string, error) {
	id, err := newSessionID()
	if err != nil {
		return "", err
	}

	dir, err := os.MkdirTemp("", "cove-hls-"+id+"-")
	if err != nil {
		return "", err
	}

	master := buildMasterPlaylist(tracks, videoCodec)
	if err := os.WriteFile(filepath.Join(dir, "master.m3u8"), []byte(master), 0o644); err != nil {
		os.RemoveAll(dir)
		return "", fmt.Errorf("write master playlist: %w", err)
	}

	session := &hlsSession{
		id: id, input: input, videoCodec: videoCodec,
		tracks: tracks, duration: duration, dir: dir, lastUsed: time.Now(),
		errCh: make(chan struct{}),
	}

	if err := session.startFfmpeg(0); err != nil {
		os.RemoveAll(dir)
		return "", err
	}

	p.hlsMu.Lock()
	p.hlsSessions[id] = session
	p.hlsMu.Unlock()

	return id, nil
}

func generateVODPlaylist(duration float64, file string) string {
	totalSegs := int(math.Ceil(duration / 10.0))
	var out strings.Builder
	out.WriteString("#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-TARGETDURATION:10\n#EXT-X-PLAYLIST-TYPE:VOD\n")

	prefix := "v"
	if strings.HasPrefix(file, "audio") {
		var idx int
		_, err := fmt.Sscanf(file, "audio%d.m3u8", &idx)
		if err != nil {
			log.Println(err)
			return ""
		}
		prefix = fmt.Sprintf("a%d_", idx)
	}

	for i := 0; i < totalSegs; i++ {
		segDuration := 10.0
		if i == totalSegs-1 {
			remainder := duration - float64(i*10)
			if remainder > 0 {
				segDuration = remainder
			}
		}
		out.WriteString(fmt.Sprintf("#EXTINF:%.3f,\n", segDuration))
		out.WriteString(fmt.Sprintf("%s%03d.ts\n", prefix, i))
	}
	out.WriteString("#EXT-X-ENDLIST\n")
	return out.String()
}

func getHighestSegmentOnDisk(dir string, file string) int {
	prefix := "v"
	if strings.HasPrefix(file, "a") {
		parts := strings.Split(file, "_")
		if len(parts) > 0 {
			prefix = parts[0] + "_"
		}
	}

	files, err := os.ReadDir(dir)
	if err != nil {
		return -1
	}

	highest := -1
	for _, f := range files {
		name := f.Name()
		if strings.HasPrefix(name, prefix) && strings.HasSuffix(name, ".ts") {
			var n int
			if prefix == "v" {
				_, err := fmt.Sscanf(name, "v%d.ts", &n)
				if err != nil {
					log.Println(err)
					continue
				}
			} else {
				_, err := fmt.Sscanf(name, prefix+"%d.ts", &n)
				if err != nil {
					log.Println(err)
					continue
				}
			}
			if n > highest {
				highest = n
			}
		}
	}
	return highest
}

// logSegmentTiming ffprobes a produced .ts segment and logs the real first/last
// video PTS, the span between them, and the packet + keyframe counts. This is a
// diagnostic: a segment that the playlist declares as 10 s but whose PTS span is
// hundreds of seconds (with only a handful of packets) reveals a timestamp-offset
// bug rather than a genuinely long encode.
func logSegmentTiming(sessionID, fullPath, file string) {
	out, err := exec.Command(ffprobeBin(),
		"-v", "error",
		"-select_streams", "v",
		"-show_entries", "packet=pts_time,flags",
		"-of", "csv=p=0",
		fullPath,
	).Output()
	if err != nil {
		// A concurrent seek's restart clears old .ts files, so the segment can
		// vanish between "appeared" and this probe. That's expected — don't log.
		if _, statErr := os.Stat(fullPath); os.IsNotExist(statErr) {
			return
		}
		log.Printf("[HLS:%s] segment-timing probe failed for %s: %v", sessionID, file, err)
		return
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	var firstPTS, lastPTS float64
	var havePTS bool
	pkts := 0
	keyframes := 0
	for _, ln := range lines {
		ln = strings.TrimSpace(ln)
		if ln == "" {
			continue
		}
		fields := strings.Split(ln, ",")
		pts, perr := strconv.ParseFloat(strings.TrimSpace(fields[0]), 64)
		if perr == nil {
			if !havePTS {
				firstPTS = pts
				havePTS = true
			}
			lastPTS = pts
		}
		pkts++
		if len(fields) > 1 && strings.Contains(fields[1], "K") {
			keyframes++
		}
	}

	log.Printf("[HLS:%s] segment-timing %s: packets=%d keyframes=%d firstPTS=%.3f lastPTS=%.3f span=%.3fs",
		sessionID, file, pkts, keyframes, firstPTS, lastPTS, lastPTS-firstPTS)
}

// For segment (.ts) files it waits up to 30 s for ffmpeg to write them before giving up.
func (p *Player) ServeHLSFile(sessionID, file string, w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	p.hlsMu.RLock()
	session, ok := p.hlsSessions[sessionID]
	p.hlsMu.RUnlock()
	if !ok {
		http.Error(w, "hls session not found", http.StatusNotFound)
		return
	}

	session.mu.Lock()
	session.lastUsed = time.Now()
	session.mu.Unlock()

	file = filepath.Base(file)
	if strings.Contains(file, "..") {
		http.Error(w, "invalid file", http.StatusBadRequest)
		return
	}
	fullPath := filepath.Join(session.dir, file)

	if file == "master.m3u8" {
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeFile(w, r, fullPath)
		return
	}

	// Serve our fabricated timeline instead of FFmpeg's sequential one
	if strings.HasSuffix(file, ".m3u8") {
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		w.Header().Set("Cache-Control", "no-cache")
		_, err := w.Write([]byte(generateVODPlaylist(session.duration, file)))
		if err != nil {
			log.Println(err)
			return
		}
		return
	}

	if strings.HasSuffix(file, ".ts") {
		var requestedSeg int
		if strings.HasPrefix(file, "v") {
			_, err := fmt.Sscanf(file, "v%d.ts", &requestedSeg)
			if err != nil {
				log.Println(err)
				return
			}
		} else if strings.HasPrefix(file, "a") {
			parts := strings.Split(file, "_")
			if len(parts) == 2 {
				_, err := fmt.Sscanf(parts[1], "%d.ts", &requestedSeg)
				if err != nil {
					log.Println(err)
					return
				}
			}
		}

		if _, err := os.Stat(fullPath); os.IsNotExist(err) {
			highestDisk := getHighestSegmentOnDisk(session.dir, file)

			session.mu.Lock()
			running := session.running
			startSeg := session.startSeg
			session.mu.Unlock()

			// Only video segments may trigger an ffmpeg restart. Video and
			// audio come from the same process, so if video restarts to
			// position N, audio at N will follow automatically. Letting audio
			// segments restart independently causes them to race with video:
			// a stale audio request can kill a freshly-started video ffmpeg,
			// leaving video waiters stranded for up to 60 seconds.
			isVideoSeg := strings.HasPrefix(file, "v")

			needsRestart := false
			var restartReason string
			if highestDisk == -1 {
				if isVideoSeg && !running {
					needsRestart = true
					restartReason = "no segments on disk and ffmpeg not running"
				}
			} else if requestedSeg > highestDisk+2 {
				if isVideoSeg {
					needsRestart = true
					restartReason = fmt.Sprintf("requestedSeg(%d) > highestDisk(%d)+2 — forward seek", requestedSeg, highestDisk)
				}
			} else if requestedSeg < startSeg {
				if isVideoSeg {
					needsRestart = true
					restartReason = fmt.Sprintf("requestedSeg(%d) < startSeg(%d) — backward seek", requestedSeg, startSeg)
				}
			}

			if needsRestart {
				log.Printf("[HLS:%s] restart for %s: %s (highestDisk=%d startSeg=%d running=%v)",
					sessionID, file, restartReason, highestDisk, startSeg, running)
				if err := session.startFfmpeg(requestedSeg); err != nil {
					log.Println(err)
					return
				}
			}
		}

		deadline := time.Now().Add(60 * time.Second)
		for time.Now().Before(deadline) {
			if _, err := os.Stat(fullPath); err == nil {
				// Lightweight timing diagnostic on the produced video segment:
				// logs real PTS span and packet/keyframe counts so segment length
				// can be verified at a glance. Runs in a goroutine so it never
				// delays serving.
				if strings.HasPrefix(file, "v") {
					go logSegmentTiming(sessionID, fullPath, file)
				}
				break
			}
			select {
			case <-r.Context().Done():
				return
			case <-session.errCh:
				log.Printf("[HLS:%s] wait for %s aborted: ffmpeg error", sessionID, file)
				http.Error(w, "ffmpeg error", http.StatusInternalServerError)
				return
			case <-time.After(250 * time.Millisecond):
			}
		}
		if _, err := os.Stat(fullPath); err != nil {
			log.Printf("[HLS:%s] segment %s never appeared after 60s timeout (highestDisk at timeout: %d)", sessionID, file, getHighestSegmentOnDisk(session.dir, file))
		}

		w.Header().Set("Content-Type", "video/MP2T")
		http.ServeFile(w, r, fullPath)
	}
}

// hlsVideoCodecString returns the CODECS string for the video portion of the
// EXT-X-STREAM-INF line. We use broad/safe profile strings so hls.js can
// pre-configure the video SourceBuffer without having to sniff segments.
func hlsVideoCodecString(codec string) string {
	switch strings.ToLower(codec) {
	case "hevc", "h265", "hvc1":
		return "hvc1.1.6.L93.B0"
	case "vp9":
		return "vp09.00.31.08"
	case "av1":
		return "av01.0.08M.08"
	default:
		// H.264 High Profile Level 4.0 — safe catch-all for avc/h264/avc1
		return "avc1.640028"
	}
}

// buildMasterPlaylist writes an HLS master playlist that lists each audio track
// as a separate EXT-X-MEDIA rendition and the single video stream.
// Vidstack's HLS provider reads this and exposes the tracks through its AudioTrackList API.
func buildMasterPlaylist(tracks []AudioTrackInfo, videoCodec string) string {
	var b strings.Builder
	b.WriteString("#EXTM3U\n#EXT-X-VERSION:3\n\n")

	for i, t := range tracks {
		name := t.Title
		if name == "" {
			name = t.Language
		}
		if name == "" {
			name = fmt.Sprintf("Track %d", i+1)
		}
		lang := t.Language
		if lang == "" {
			lang = "und"
		}
		def := "NO"
		if i == 0 {
			def = "YES"
		}
		_, err := fmt.Fprintf(&b,
			"#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID=\"audio\",NAME=\"%s\",LANGUAGE=\"%s\",DEFAULT=%s,URI=\"audio%d.m3u8\"\n",
			name, lang, def, i,
		)
		if err != nil {
			log.Println(err)
			return ""
		}
	}

	// CODECS lets hls.js pre-configure its SourceBuffers correctly without
	// having to sniff codec info from the first segment bytes. Without it,
	// the audio SourceBuffer can be initialised with the wrong type and then
	// reject the first appended segment with a MediaSource error.
	// Audio is always re-encoded to AAC-LC (mp4a.40.2) by our pipeline.
	videoCodecStr := hlsVideoCodecString(videoCodec)
	b.WriteString(fmt.Sprintf(
		"\n#EXT-X-STREAM-INF:BANDWIDTH=5000000,CODECS=\"%s,mp4a.40.2\",AUDIO=\"audio\"\nvideo.m3u8\n",
		videoCodecStr,
	))
	return b.String()
}

// CleanupHLSSessions kills and removes sessions idle for more than 2 hours.
// Call this on a ticker from main (e.g. every 30 minutes).
func (p *Player) CleanupHLSSessions() {
	cutoff := time.Now().Add(-2 * time.Hour)

	p.hlsMu.Lock()
	defer p.hlsMu.Unlock()

	for id, session := range p.hlsSessions {
		session.mu.Lock()
		idle := session.lastUsed.Before(cutoff)
		session.mu.Unlock()

		if idle {
			if session.cmd != nil && session.cmd.Process != nil {
				err := session.cmd.Process.Kill()
				if err != nil {
					log.Println(err)
					continue
				}
			}
			select {
			case <-session.errCh:
			default:
				close(session.errCh)
			}
			err := os.RemoveAll(session.dir)
			if err != nil {
				log.Println(err)
				continue
			}
			delete(p.hlsSessions, id)
			log.Printf("HLS session %s cleaned up", id)
		}
	}
}

// StopHLSSession immediately kills the ffmpeg process and removes the temp
// directory for a single session. Safe to call from any goroutine.
func (p *Player) StopHLSSession(id string) {
	p.hlsMu.Lock()
	session, ok := p.hlsSessions[id]
	if ok {
		delete(p.hlsSessions, id)
	}
	p.hlsMu.Unlock()

	if !ok {
		return
	}

	session.mu.Lock()
	cmd := session.cmd
	dir := session.dir
	errCh := session.errCh
	session.cmd = nil
	session.running = false
	session.mu.Unlock()

	// Unblock any goroutines stuck in the segment wait loop.
	select {
	case <-errCh: // already closed
	default:
		close(errCh)
	}

	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait() // reap to avoid zombie; safe here because the goroutine
		// for this cmd already exited via the errCh close above, or will
		// receive ECHILD and return harmlessly.
	}
	_ = os.RemoveAll(dir)
	log.Printf("HLS session %s stopped", id)
}
