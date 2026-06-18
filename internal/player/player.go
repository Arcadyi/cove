package player

import (
	"bytes"
	"container/list"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Arcadyi/cove/internal/addons"
	"github.com/Arcadyi/cove/internal/tmdb"
	"github.com/Arcadyi/cove/internal/utils"
	"github.com/anacrolix/torrent"
	"golang.org/x/sync/singleflight"
)

// Player owns all of the package's mutable state — the torrent client, the
// active-torrent registry, the HLS session table, and the subtitle cache (all
// previously package globals) — plus the injected TMDB client and addon
// manager that used to be threaded through SetupHandlers. Its methods are split
// across player.go and hls.go but all hang off this one type. Fields are
// unexported, so tygo emits nothing for Player.
type Player struct {
	client *torrent.Client

	activeTorrents   map[string]*torrentState
	activeTorrentsMu sync.RWMutex

	hlsSessions map[string]*hlsSession
	hlsMu       sync.RWMutex

	subtitleCacheMu sync.Mutex
	subtitleLRU     *list.List
	subtitleCache   map[string]*list.Element
	subtitleGroup   singleflight.Group

	tmdbClient *tmdb.Client
	addonMgr   *addons.Manager
}

// torrentDataDir is where the anacrolix client writes downloaded pieces. The
// reaper removes per-torrent subdirectories under here when a torrent is
// dropped, so Init and CleanupTorrents must agree on the path.
const torrentDataDir = "/tmp/cove-torrents"

type torrentState struct {
	torrent      *torrent.Torrent
	lastBytes    int64
	lastCheck    time.Time
	speedByteSec int64

	// lastUsed is refreshed whenever something reads the torrent or polls its
	// progress, and readers counts the live stream handlers attached to it.
	// The reaper drops a torrent only when readers == 0 AND lastUsed is older
	// than the idle cutoff, so an actively-watched title is never collected.
	lastUsed time.Time
	readers  int
}

// AudioTrackInfo describes a single audio track returned by ffprobe.
type AudioTrackInfo struct {
	Index    int    `json:"index"`
	Language string `json:"language"`
	Title    string `json:"title"`
	Codec    string `json:"codec"`
}

// SubtitleTrackInfo describes a single subtitle track returned by ffprobe.
type SubtitleTrackInfo struct {
	Index    int    `json:"index"`
	Language string `json:"language"`
	Title    string `json:"title"`
	Codec    string `json:"codec"`
}

// imageBasedSubtitleCodecs cannot be converted to WebVTT by ffmpeg without OCR.
var imageBasedSubtitleCodecs = map[string]bool{
	"hdmv_pgs_subtitle": true,
	"pgssub":            true,
	"dvd_subtitle":      true,
	"dvdsub":            true,
	"xsub":              true,
}

// New constructs a Player: it creates the torrent client and stores the
// injected TMDB client and addon manager. The torrent client is core
// functionality, so a failure here is returned for the caller to treat as fatal.
func New(tmdbClient *tmdb.Client, addonMgr *addons.Manager) (*Player, error) {
	cfg := torrent.NewDefaultClientConfig()
	cfg.DataDir = torrentDataDir
	client, err := torrent.NewClient(cfg)
	if err != nil {
		return nil, err
	}
	return &Player{
		client:         client,
		activeTorrents: map[string]*torrentState{},
		hlsSessions:    map[string]*hlsSession{},
		subtitleLRU:    list.New(),
		subtitleCache:  map[string]*list.Element{},
		tmdbClient:     tmdbClient,
		addonMgr:       addonMgr,
	}, nil
}

// largestFile returns the biggest file in a torrent whose metadata is ready.
func largestFile(t *torrent.Torrent) (*torrent.File, error) {
	var largest *torrent.File
	for _, f := range t.Files() {
		if largest == nil || f.Length() > largest.Length() {
			largest = f
		}
	}
	if largest == nil {
		return nil, fmt.Errorf("no files found in torrent")
	}
	return largest, nil
}

// addReader adjusts the live-reader count for a torrent and refreshes its idle
// timer. delta is +1 when a stream handler opens, -1 when it returns.
func (p *Player) addReader(infoHash string, delta int) {
	p.activeTorrentsMu.Lock()
	if st, ok := p.activeTorrents[infoHash]; ok {
		st.readers += delta
		if st.readers < 0 {
			st.readers = 0
		}
		st.lastUsed = time.Now()
	}
	p.activeTorrentsMu.Unlock()
}

func (p *Player) getLargestTorrentFile(infoHash string) (*torrent.File, error) {
	// Reuse a torrent we've already fetched metadata for. AddMagnet is
	// idempotent, but reusing also avoids re-running the GotInfo wait and keeps
	// the idle timer fresh.
	p.activeTorrentsMu.Lock()
	if st, ok := p.activeTorrents[infoHash]; ok && st.torrent.Info() != nil {
		t := st.torrent
		st.lastUsed = time.Now()
		p.activeTorrentsMu.Unlock()
		return largestFile(t)
	}
	p.activeTorrentsMu.Unlock()

	t, err := p.client.AddMagnet("magnet:?xt=urn:btih:" + infoHash)
	if err != nil {
		return nil, fmt.Errorf("invalid magnet for %s: %w", infoHash, err)
	}

	// Bound the metadata fetch. A dead swarm never fires GotInfo, and without a
	// deadline this blocks the request goroutine forever — the original cause
	// of goroutine pile-up under bad hashes. On timeout we drop the torrent so
	// it doesn't sit in the client holding resources.
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	select {
	case <-t.GotInfo():
	case <-ctx.Done():
		t.Drop()
		return nil, fmt.Errorf("timed out fetching metadata for %s", infoHash)
	}

	now := time.Now()
	p.activeTorrentsMu.Lock()
	p.activeTorrents[infoHash] = &torrentState{
		torrent:   t,
		lastCheck: now,
		lastUsed:  now,
	}
	p.activeTorrentsMu.Unlock()

	return largestFile(t)
}

// CleanupTorrents drops torrents that have no live readers and haven't been
// touched within the idle cutoff, mirroring CleanupHLSSessions. anacrolix
// torrents hold open file handles plus on-disk pieces under torrentDataDir;
// without this they accumulate for the life of the process and eventually
// fill /tmp. Dropping removes the torrent from the client; we then RemoveAll
// its data directory to reclaim disk (unlinking is safe even if a handle is
// briefly still open on Linux).
func (p *Player) CleanupTorrents() {
	cutoff := time.Now().Add(-30 * time.Minute)

	type dropped struct {
		hash string
		t    *torrent.Torrent
	}
	var toDrop []dropped

	p.activeTorrentsMu.Lock()
	for hash, st := range p.activeTorrents {
		if st.readers <= 0 && st.lastUsed.Before(cutoff) {
			toDrop = append(toDrop, dropped{hash, st.torrent})
			delete(p.activeTorrents, hash)
		}
	}
	p.activeTorrentsMu.Unlock()

	for _, d := range toDrop {
		name := d.t.Name() // capture before Drop; valid once metadata is known
		d.t.Drop()
		if name != "" {
			if err := os.RemoveAll(filepath.Join(torrentDataDir, name)); err != nil {
				log.Printf("torrent %s: could not remove data: %v", d.hash, err)
			}
		}
		log.Printf("torrent %s dropped (idle)", d.hash)
	}
}

func (p *Player) StreamTorrent(infoHash string, w http.ResponseWriter, r *http.Request) {
	largest, err := p.getLargestTorrentFile(infoHash)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	// Mark the torrent as in-use for as long as this handler streams. The
	// reaper will not drop a torrent with readers > 0, so a long-running
	// ffmpeg read (the HLS pipeline holds one open continuously) is protected.
	p.addReader(infoHash, +1)
	defer p.addReader(infoHash, -1)

	reader := largest.NewReader()
	// Closing the reader matters a lot here. Every ffmpeg (re)start on a seek
	// opens a new request to this handler and a new reader. anacrolix readers
	// hold piece-download priorities until Close(); if we never close them, each
	// killed-ffmpeg's reader keeps prioritising its now-stale region and they all
	// compete for the same swarm bandwidth, starving the region the user just
	// seeked to. Closing on handler return releases that stale prioritisation.
	defer reader.Close()

	// Responsive mode hands ffmpeg whatever bytes have arrived instead of
	// blocking until a full readahead window is downloaded, and a generous
	// readahead lets the client fetch pieces ahead of the encoder so a seek
	// doesn't stall mid-segment.
	reader.SetResponsive()
	reader.SetReadahead(16 << 20) // 16 MiB

	http.ServeContent(w, r, largest.DisplayPath(), time.Time{}, reader)
}

// ProbeAll runs a single ffprobe invocation and returns audio tracks,
// subtitle tracks, the first video codec name, and the container duration.
// Replaces the old ProbeAudioTracks / ProbeSubtitleTracks / ProbeDuration
// triple-call pattern so the torrent stream is only buffered once.
func ProbeAll(mediaURL string) ([]AudioTrackInfo, []SubtitleTrackInfo, string, float64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "ffprobe",
		"-v", "quiet",
		"-print_format", "json",
		"-show_streams",
		"-show_format",
		mediaURL,
	)
	out, err := cmd.Output()
	if err != nil {
		return nil, nil, "", 0, fmt.Errorf("ffprobe: %w", err)
	}

	var result struct {
		Streams []struct {
			CodecType string `json:"codec_type"`
			CodecName string `json:"codec_name"`
			Tags      struct {
				Language string `json:"language"`
				Title    string `json:"title"`
			} `json:"tags"`
		} `json:"streams"`
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
	}
	if err := json.Unmarshal(out, &result); err != nil {
		return nil, nil, "", 0, err
	}

	var audioTracks []AudioTrackInfo
	var subtitleTracks []SubtitleTrackInfo
	var videoCodec string
	audioIdx := 0
	subtitleIdx := 0

	for _, s := range result.Streams {
		switch s.CodecType {
		case "video":
			if videoCodec == "" {
				videoCodec = s.CodecName
			}
		case "audio":
			audioTracks = append(audioTracks, AudioTrackInfo{
				Index:    audioIdx,
				Language: s.Tags.Language,
				Title:    s.Tags.Title,
				Codec:    s.CodecName,
			})
			audioIdx++
		case "subtitle":
			if !imageBasedSubtitleCodecs[s.CodecName] {
				subtitleTracks = append(subtitleTracks, SubtitleTrackInfo{
					Index:    subtitleIdx,
					Language: s.Tags.Language,
					Title:    s.Tags.Title,
					Codec:    s.CodecName,
				})
			}
			subtitleIdx++
		}
	}

	duration, _ := strconv.ParseFloat(result.Format.Duration, 64)
	return audioTracks, subtitleTracks, videoCodec, duration, nil
}

// ExtractSubtitle extracts a single subtitle track and serves it as WebVTT.
// Results are cached so repeated requests don't re-run ffmpeg.

// maxSubtitleCacheEntries bounds the in-memory VTT cache. Extracted subtitles
// are small (tens to a few hundred KB), but the old unbounded map grew one
// entry per (file, track) for the life of the process. An LRU with a modest
// cap keeps the hot set without leaking.
const maxSubtitleCacheEntries = 64

type subtitleEntry struct {
	key  string
	data []byte
}

func subtitleCacheKey(input string, index int) string {
	return fmt.Sprintf("%s::sub::%d", input, index)
}

func (p *Player) subtitleCacheGet(key string) ([]byte, bool) {
	p.subtitleCacheMu.Lock()
	defer p.subtitleCacheMu.Unlock()
	el, ok := p.subtitleCache[key]
	if !ok {
		return nil, false
	}
	p.subtitleLRU.MoveToFront(el)
	return el.Value.(*subtitleEntry).data, true
}

func (p *Player) subtitleCachePut(key string, data []byte) {
	p.subtitleCacheMu.Lock()
	defer p.subtitleCacheMu.Unlock()
	if el, ok := p.subtitleCache[key]; ok {
		el.Value.(*subtitleEntry).data = data
		p.subtitleLRU.MoveToFront(el)
		return
	}
	p.subtitleCache[key] = p.subtitleLRU.PushFront(&subtitleEntry{key: key, data: data})
	for p.subtitleLRU.Len() > maxSubtitleCacheEntries {
		oldest := p.subtitleLRU.Back()
		if oldest == nil {
			break
		}
		p.subtitleLRU.Remove(oldest)
		delete(p.subtitleCache, oldest.Value.(*subtitleEntry).key)
	}
}

func (p *Player) ExtractSubtitle(input string, subtitleIndex int, w http.ResponseWriter) {
	key := subtitleCacheKey(input, subtitleIndex)

	if cached, ok := p.subtitleCacheGet(key); ok {
		w.Header().Set("Content-Type", "text/vtt; charset=utf-8")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Write(cached)
		return
	}

	result, err, _ := p.subtitleGroup.Do(key, func() (interface{}, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "ffmpeg",
			"-i", input,
			"-map", fmt.Sprintf("0:s:%d", subtitleIndex),
			"-c:s", "webvtt", "-f", "webvtt", "pipe:1",
		)
		out, err := cmd.Output()
		if err != nil {
			return nil, err
		}
		p.subtitleCachePut(key, out)
		return out, nil
	})

	if err != nil {
		http.Error(w, "subtitle extraction failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/vtt; charset=utf-8")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Write(result.([]byte))
}

// StreamWithAudio uses ffmpeg to select a specific audio track, transcode it to AAC,
// and serve the result as a seekable MP4 with correct duration.
func StreamWithAudio(input string, audioIndex int, w http.ResponseWriter, r *http.Request) {
	tmp, err := os.CreateTemp("", "cove-audio-*.mp4")
	if err != nil {
		http.Error(w, "temp file error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	tmpPath := tmp.Name()
	tmp.Close()
	defer os.Remove(tmpPath)

	ctx := r.Context()
	cmd := exec.CommandContext(ctx, "ffmpeg",
		"-i", input,
		"-map", "0:v:0",
		"-map", fmt.Sprintf("0:a:%d", audioIndex),
		"-c:v", "copy",
		"-c:a", "aac",
		"-b:a", "192k",
		"-movflags", "+faststart",
		"-y",
		tmpPath,
	)

	var ffmpegErr bytes.Buffer
	cmd.Stderr = &ffmpegErr

	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return
		}
		log.Printf("ffmpeg error: %v\n%s", err, ffmpegErr.String())
		http.Error(w, "ffmpeg failed", http.StatusInternalServerError)
		return
	}

	f, err := os.Open(tmpPath)
	if err != nil {
		http.Error(w, "failed to open output: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer f.Close()

	http.ServeContent(w, r, "stream.mp4", time.Time{}, f)
}

func (p *Player) GetProgress(infoHash string) map[string]interface{} {
	p.activeTorrentsMu.Lock()
	state, ok := p.activeTorrents[infoHash]
	if !ok {
		p.activeTorrentsMu.Unlock()
		return map[string]interface{}{"found": false}
	}

	now := time.Now()
	stats := state.torrent.Stats()
	currentBytes := stats.BytesReadUsefulData.Int64()
	elapsed := now.Sub(state.lastCheck).Seconds()
	if elapsed > 0 {
		state.speedByteSec = int64(float64(currentBytes-state.lastBytes) / elapsed)
	}
	state.lastBytes = currentBytes
	state.lastCheck = now
	state.lastUsed = now // progress is polled during playback: acts as a keepalive
	t := state.torrent
	p.activeTorrentsMu.Unlock()

	info := t.Info()
	if info == nil {
		return map[string]interface{}{"found": true, "progress": 0, "peers": 0, "speed": "0 B/s"}
	}

	complete := t.BytesCompleted()
	total := t.Length()
	var pct float64
	if total > 0 {
		pct = float64(complete) / float64(total) * 100
	}

	return map[string]interface{}{
		"found":    true,
		"progress": pct,
		"peers":    stats.ActivePeers,
		"speed":    formatSpeed(state.speedByteSec),
	}
}

func formatSpeed(bytesPerSec int64) string {
	switch {
	case bytesPerSec >= 1024*1024:
		return fmt.Sprintf("%.1f MB/s", float64(bytesPerSec)/1024/1024)
	case bytesPerSec >= 1024:
		return fmt.Sprintf("%.1f KB/s", float64(bytesPerSec)/1024)
	default:
		return fmt.Sprintf("%d B/s", bytesPerSec)
	}
}

// NewServer returns a pre-configured *http.Server. Callers should use this
// instead of http.ListenAndServe so that ReadHeaderTimeout is always set.
func NewServer(addr string) *http.Server {
	return &http.Server{
		Addr: addr,
		// Guards against slow-loris style attacks. Streaming responses
		// (HLS segments, torrent) legitimately run for minutes, so we
		// do NOT set WriteTimeout or IdleTimeout here.
		ReadHeaderTimeout: 10 * time.Second,
	}
}

func (p *Player) SetupHandlers() {
	http.HandleFunc("/api/subtitles", utils.CorsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		tmdbID := r.URL.Query().Get("id")
		mediaType := r.URL.Query().Get("type")
		id := 0
		if _, err := fmt.Sscanf(tmdbID, "%d", &id); err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}

		var imdbID string
		var err error
		if mediaType == "tv" {
			imdbID, err = p.tmdbClient.GetTVIMDBId(id)
		} else {
			imdbID, err = p.tmdbClient.GetIMDBId(id)
		}
		if err != nil || imdbID == "" {
			http.Error(w, "could not get IMDB id", http.StatusInternalServerError)
			return
		}

		stremioID := imdbID
		if mediaType == "tv" {
			season := r.URL.Query().Get("season")
			episode := r.URL.Query().Get("episode")
			if season != "" && episode != "" {
				stremioID = fmt.Sprintf("%s:%s:%s", imdbID, season, episode)
			}
		}

		var allSubs []addons.Subtitle
		for _, addon := range p.addonMgr.GetAddons() {
			subs, err := p.addonMgr.FetchSubtitles(addon.URL, mediaType, stremioID)
			if err != nil {
				continue
			}
			allSubs = append(allSubs, subs...)
		}
		if allSubs == nil {
			allSubs = []addons.Subtitle{}
		}
		w.Header().Set("Content-Type", "application/json")
		err = json.NewEncoder(w).Encode(allSubs)
		if err != nil {
			log.Println(err)
			return
		}
	}))

	// /api/streams?id=<tmdbID>&type=movie|tv[&season=N&episode=N]
	http.HandleFunc("/api/streams", utils.CorsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		tmdbID := r.URL.Query().Get("id")
		mediaType := r.URL.Query().Get("type")
		if mediaType == "" {
			mediaType = "movie"
		}

		id := 0
		_, err := fmt.Sscanf(tmdbID, "%d", &id)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}

		// Resolve IMDB ID based on media type
		var imdbID string
		if mediaType == "tv" {
			imdbID, err = p.tmdbClient.GetTVIMDBId(id)
		} else {
			imdbID, err = p.tmdbClient.GetIMDBId(id)
		}
		if err != nil || imdbID == "" {
			http.Error(w, "could not get IMDB id", http.StatusInternalServerError)
			return
		}

		// For TV, append season:episode to build the Stremio stream ID
		stremioID := imdbID
		if mediaType == "tv" {
			season := r.URL.Query().Get("season")
			episode := r.URL.Query().Get("episode")
			if season == "" || episode == "" {
				http.Error(w, "season and episode are required for tv streams", http.StatusBadRequest)
				return
			}
			stremioID = fmt.Sprintf("%s:%s:%s", imdbID, season, episode)
		}

		streams, err := p.addonMgr.GetAllStreams(mediaType, stremioID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if streams == nil {
			streams = []addons.Stream{}
		}
		err = json.NewEncoder(w).Encode(streams)
		if err != nil {
			log.Println(err)
			return
		}
	}))

	http.HandleFunc("/api/probe", utils.CorsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		hash := r.URL.Query().Get("hash")
		streamURL := r.URL.Query().Get("url")

		var probeInput string
		switch {
		case hash != "":
			probeInput = fmt.Sprintf("http://localhost:6969/api/play?hash=%s", hash)
		case streamURL != "":
			probeInput = streamURL
		default:
			http.Error(w, "missing hash or url", http.StatusBadRequest)
			return
		}

		audioTracks, subtitleTracks, videoCodec, duration, err := ProbeAll(probeInput)
		if err != nil {
			log.Println("probe error:", err)
			audioTracks = []AudioTrackInfo{}
			subtitleTracks = []SubtitleTrackInfo{}
		}
		if audioTracks == nil {
			audioTracks = []AudioTrackInfo{}
		}
		if subtitleTracks == nil {
			subtitleTracks = []SubtitleTrackInfo{}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"audio":      audioTracks,
			"subtitles":  subtitleTracks,
			"videoCodec": videoCodec,
			"duration":   duration,
		})
	}))

	http.HandleFunc("/api/subtitle/extract", utils.CorsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		hash := r.URL.Query().Get("hash")
		streamURL := r.URL.Query().Get("url")
		index, err := strconv.Atoi(r.URL.Query().Get("index"))
		if err != nil {
			http.Error(w, "invalid index", http.StatusBadRequest)
			return
		}
		var input string
		switch {
		case hash != "":
			input = fmt.Sprintf("http://localhost:6969/api/play?hash=%s", hash)
		case streamURL != "":
			input = streamURL
		default:
			http.Error(w, "missing hash or url", http.StatusBadRequest)
			return
		}
		p.ExtractSubtitle(input, index, w)
	}))

	http.HandleFunc("/api/play", utils.CorsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		infoHash := r.URL.Query().Get("hash")
		streamURL := r.URL.Query().Get("url")
		audioStr := r.URL.Query().Get("audio")

		if audioStr != "" {
			audioIndex, err := strconv.Atoi(audioStr)
			if err == nil {
				// For torrents, point ffmpeg at the local stream endpoint so it can seek via range requests.
				// For direct URLs, pass the URL straight through.
				var input string
				if streamURL != "" {
					input = streamURL
				} else if infoHash != "" {
					input = fmt.Sprintf("http://localhost:6969/api/play?hash=%s", infoHash)
				} else {
					http.Error(w, "missing hash or url", http.StatusBadRequest)
					return
				}
				StreamWithAudio(input, audioIndex, w, r)
				return
			}
		}

		if streamURL != "" {
			http.Redirect(w, r, streamURL, http.StatusTemporaryRedirect)
			return
		}

		if infoHash != "" {
			p.StreamTorrent(infoHash, w, r)
			return
		}

		http.Error(w, "missing hash or url", http.StatusBadRequest)
	}))

	// POST /api/hls/start — starts an HLS session, returns the session ID
	http.HandleFunc("/api/hls/start", utils.CorsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Input      string           `json:"input"`
			Tracks     []AudioTrackInfo `json:"tracks"`
			Duration   float64          `json:"duration"`
			VideoCodec string           `json:"videoCodec"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		sessionID, err := p.StartHLSSession(body.Input, body.Tracks, body.Duration, body.VideoCodec)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		err = json.NewEncoder(w).Encode(map[string]string{"sessionID": sessionID})
		if err != nil {
			log.Println(err)
			return
		}
	}))

	http.HandleFunc("/api/hls/stop/", utils.CorsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		trimmed := strings.TrimPrefix(r.URL.Path, "/api/hls/stop/")
		log.Println(trimmed)

		if r.Method == http.MethodPost {
			sessionID := strings.SplitN(trimmed, "/", 2)[0]
			if sessionID == "" {
				http.Error(w, "missing session ID", http.StatusBadRequest)
				return
			}
			p.StopHLSSession(sessionID)
			w.WriteHeader(http.StatusNoContent)
			return
		}

		parts := strings.SplitN(trimmed, "/", 2)
		if len(parts) != 2 {
			http.Error(w, "invalid path", http.StatusBadRequest)
			return
		}
		p.ServeHLSFile(parts[0], parts[1], w, r)
	}))

	// GET /api/hls/{sessionID}/{file} — serves master playlist, sub-playlists, and segments
	http.HandleFunc("/api/hls/", utils.CorsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		parts := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/api/hls/"), "/", 2)
		if len(parts) != 2 {
			http.Error(w, "invalid path", http.StatusBadRequest)
			return
		}
		p.ServeHLSFile(parts[0], parts[1], w, r)
	}))

	// Legacy polling endpoint — kept for compatibility; prefer /api/progress/stream (SSE).
	http.HandleFunc("/api/progress", utils.CorsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		hash := r.URL.Query().Get("hash")
		err := json.NewEncoder(w).Encode(p.GetProgress(hash))
		if err != nil {
			log.Println(err)
		}
	}))

	http.HandleFunc("/api/progress/stream", utils.CorsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		hash := r.URL.Query().Get("hash")
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("Access-Control-Allow-Origin", "*")

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming not supported", http.StatusInternalServerError)
			return
		}

		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-r.Context().Done():
				return
			case <-ticker.C:
				data, _ := json.Marshal(p.GetProgress(hash))
				fmt.Fprintf(w, "data: %s\n\n", data)
				flusher.Flush()
			}
		}
	}))

	// GET /api/speedtest — streams a fixed-size payload so the client can
	// measure raw download throughput for the "Match My Internet Speed"
	// stream-selection mode. Not a rigorous benchmark (single connection,
	// no compression, local network only) but good enough as a rough guide.
	http.HandleFunc("/api/speedtest", utils.CorsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		const payloadSize = 25 * 1024 * 1024 // 25 MiB
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", strconv.Itoa(payloadSize))
		w.Header().Set("Cache-Control", "no-store")

		buf := make([]byte, 1<<20) // 1 MiB chunks
		flusher, _ := w.(http.Flusher)
		for written := 0; written < payloadSize; {
			n := len(buf)
			if remaining := payloadSize - written; remaining < n {
				n = remaining
			}
			if _, err := w.Write(buf[:n]); err != nil {
				return // client aborted — nothing to clean up
			}
			written += n
			if flusher != nil {
				flusher.Flush()
			}
		}
	}))

	http.HandleFunc("/api/subtitle-proxy", utils.CorsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		rawURL := r.URL.Query().Get("url")
		if rawURL == "" {
			http.Error(w, "missing url", http.StatusBadRequest)
			return
		}
		resp, err := http.Get(rawURL)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer func(Body io.ReadCloser) {
			err := Body.Close()
			if err != nil {
				log.Println(err)
			}
		}(resp.Body)

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/vtt; charset=utf-8")
		w.Header().Set("Access-Control-Allow-Origin", "*")

		// If it's SRT, convert to WebVTT (browser only accepts VTT for <track>)
		content := string(body)
		if !strings.HasPrefix(strings.TrimSpace(content), "WEBVTT") {
			content = utils.SrtToVTT(content)
		}
		_, err = fmt.Fprint(w, content)
		if err != nil {
			log.Println(err)
			return
		}
	}))
}
