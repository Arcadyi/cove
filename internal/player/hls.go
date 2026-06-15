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
	"syscall"
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
}

// browserSafeVideoCodecs can be stream-copied into HLS without re-encoding.
// Everything else falls back to libx264 (capped threads to avoid CPU saturation).
var browserSafeVideoCodecs = map[string]bool{
	"h264": true, "avc": true, "avc1": true,
}

var (
	hlsMu       sync.RWMutex
	hlsSessions = map[string]*hlsSession{}
)

func newSessionID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

var browserSafeAudioCodecs = map[string]bool{

	"aac": true, "mp3": true, "opus": true, "vorbis": true, "flac": true,
}

// startFfmpeg kills any running process for this session and starts a new one
// exactly at the requested segment offset.
func (s *hlsSession) startFfmpeg(startSeg int) error {
	s.mu.Lock()
	if s.restarting {
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

	// Blocking Wait is safe outside the lock
	if oldCmd != nil && oldCmd.Process != nil {
		_ = oldCmd.Process.Kill()
		_ = oldCmd.Wait()
	}

	// Clear segments from the previous run so seek detection
	// reflects only what the current run has written.
	if entries, err := os.ReadDir(s.dir); err == nil {
		for _, entry := range entries {
			if strings.HasSuffix(entry.Name(), ".ts") {
				os.Remove(filepath.Join(s.dir, entry.Name()))
			}
		}
	}

	startTime := float64(startSeg) * 10.0

	var args []string
	if startTime > 0 {
		args = append(args, "-ss", fmt.Sprintf("%.3f", startTime))
	}
	args = append(args, "-i", s.input)

	// ── Video output ────────────────────────────────────────────────────────
	// Copy when the codec is already browser-safe (H.264) — zero transcode CPU.
	// Fall back to libx264 for other codecs, capped to 4 threads so ffmpeg
	// can't saturate all cores even during a full re-encode.
	if browserSafeVideoCodecs[strings.ToLower(s.videoCodec)] {
		args = append(args, "-map", "0:v:0", "-c:v", "copy", "-an")
	} else {
		args = append(args,
			"-map", "0:v:0",
			"-c:v", "libx264",
			"-preset", "veryfast",
			"-crf", "23",
			"-threads", "4",
			"-force_key_frames", "expr:gte(t,n_forced*10)",
			"-an",
		)
	}
	if startTime > 0 {
		args = append(args, "-output_ts_offset", fmt.Sprintf("%.3f", startTime))
	}
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
		codec := "aac"
		extra := []string{"-b:a", "192k"}
		if browserSafeAudioCodecs[strings.ToLower(t.Codec)] {
			codec = "copy"
			extra = nil
		}
		args = append(args, "-map", fmt.Sprintf("0:a:%d", i), "-vn", "-c:a", codec)
		args = append(args, extra...)
		if startTime > 0 {
			args = append(args, "-output_ts_offset", fmt.Sprintf("%.3f", startTime))
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

	s.cmd = exec.Command("ffmpeg", args...)
	var ffmpegStderr strings.Builder
	s.cmd.Stderr = &ffmpegStderr

	if err := s.cmd.Start(); err != nil {
		return fmt.Errorf("start ffmpeg: %w", err)
	}
	s.running = true
	// Run ffmpeg at lower OS priority so it yields to the Go server and the
	// browser under load.  Nice 10 still gives plenty of CPU when idle.
	if s.cmd.Process != nil {
		_ = syscall.Setpriority(syscall.PRIO_PROCESS, s.cmd.Process.Pid, 10)
	}

	go func(cmd *exec.Cmd, sess *hlsSession) {
		err := cmd.Wait()
		sess.mu.Lock()
		sess.running = false
		sess.mu.Unlock()
		if err != nil && !strings.Contains(err.Error(), "signal: killed") {
			log.Printf("HLS session %s ffmpeg: %v\n%s", sess.id, err, ffmpegStderr.String())
		}
	}(s.cmd, s)

	return nil
}

// StartHLSSession launches a single ffmpeg process that writes:
//   - video.m3u8 + v###.ts   — video-only, codec copied
//   - audioN.m3u8 + aN_###.ts — one audio-only stream per track
//   - master.m3u8             — written immediately so the client can start requesting
//
// Audio is copied when the codec is already browser-safe (AAC etc.),
// otherwise transcoded to AAC. Video is always copied.
func StartHLSSession(input string, tracks []AudioTrackInfo, duration float64, videoCodec string) (string, error) {
	id, err := newSessionID()
	if err != nil {
		return "", err
	}

	dir, err := os.MkdirTemp("", "cove-hls-"+id+"-")
	if err != nil {
		return "", err
	}

	master := buildMasterPlaylist(tracks)
	if err := os.WriteFile(filepath.Join(dir, "master.m3u8"), []byte(master), 0o644); err != nil {
		err := os.RemoveAll(dir)
		if err != nil {
			log.Println(err)
			return "", err
		}
		return "", fmt.Errorf("write master playlist: %w", err)
	}

	session := &hlsSession{
		id:         id,
		input:      input,
		videoCodec: videoCodec,
		tracks:     tracks,
		duration:   duration,
		dir:        dir,
		lastUsed:   time.Now(),
	}

	if err := session.startFfmpeg(0); err != nil {
		err := os.RemoveAll(dir)
		if err != nil {
			log.Println(err)
			return "", err
		}
		return "", err
	}

	hlsMu.Lock()
	hlsSessions[id] = session
	hlsMu.Unlock()

	firstSeg := filepath.Join(dir, "v000.ts")
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(firstSeg); err == nil {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}

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

// ServeHLSFile serves a playlist or segment file belonging to a session.
// For segment (.ts) files it waits up to 30 s for ffmpeg to write them before giving up.
func ServeHLSFile(sessionID, file string, w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	hlsMu.RLock()
	session, ok := hlsSessions[sessionID]
	hlsMu.RUnlock()
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

			needsRestart := false
			if highestDisk == -1 {
				needsRestart = !running
			} else if requestedSeg > highestDisk+2 {
				// forward seek past what ffmpeg has written
				needsRestart = true
			} else if requestedSeg < startSeg {
				// backward seek before where the current ffmpeg run started
				needsRestart = true
			}

			if needsRestart {
				log.Printf("Seek detected for %s: requested %d, highest %d. Restarting ffmpeg.", file, requestedSeg, highestDisk)
				if err := session.startFfmpeg(requestedSeg); err != nil {
					log.Println(err)
					return
				}
			}
		}

		deadline := time.Now().Add(60 * time.Second)
		for time.Now().Before(deadline) {
			if _, err := os.Stat(fullPath); err == nil {
				break
			}
			select {
			case <-r.Context().Done():
				return
			case <-time.After(250 * time.Millisecond):
			}
		}

		w.Header().Set("Content-Type", "video/MP2T")
		http.ServeFile(w, r, fullPath)
	}
}

// buildMasterPlaylist writes an HLS master playlist that lists each audio track
// as a separate EXT-X-MEDIA rendition and the single video stream.
// Vidstack's HLS provider reads this and exposes the tracks through its AudioTrackList API.
func buildMasterPlaylist(tracks []AudioTrackInfo) string {
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

	b.WriteString("\n#EXT-X-STREAM-INF:BANDWIDTH=5000000,AUDIO=\"audio\"\nvideo.m3u8\n")
	return b.String()
}

// CleanupHLSSessions kills and removes sessions idle for more than 2 hours.
// Call this on a ticker from main (e.g. every 30 minutes).
func CleanupHLSSessions() {
	cutoff := time.Now().Add(-2 * time.Hour)

	hlsMu.Lock()
	defer hlsMu.Unlock()

	for id, session := range hlsSessions {
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
			err := os.RemoveAll(session.dir)
			if err != nil {
				log.Println(err)
				continue
			}
			delete(hlsSessions, id)
			log.Printf("HLS session %s cleaned up", id)
		}
	}
}
