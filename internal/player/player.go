package player

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coveninja/cove/internal/addons"
	"github.com/coveninja/cove/internal/tmdb"
	"github.com/coveninja/cove/internal/utils"
	"github.com/anacrolix/torrent"
)

// Player owns all of the package's mutable state — the torrent client and the
// active-torrent registry — plus the injected TMDB client and addon manager.
// Fields are unexported, so tygo emits nothing for Player.
type Player struct {
	client *torrent.Client

	activeTorrents   map[string]*torrentState
	activeTorrentsMu sync.RWMutex

	tmdbClient *tmdb.Client
	addonMgr   *addons.Manager
}

// torrentDataDir is where the anacrolix client writes downloaded pieces. The
// reaper removes per-torrent subdirectories under here when a torrent is
// dropped, so New() and CleanupTorrents must agree on the path.
var torrentDataDir = filepath.Join(os.TempDir(), "cove-torrents")

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

// addReader adjusts readers (+1 on open, -1 on return) and refreshes lastUsed.
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
// touched within the idle cutoff. anacrolix
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
	// read is protected.
	p.addReader(infoHash, +1)
	defer p.addReader(infoHash, -1)

	reader := largest.NewReader()
	// Closing the reader matters: anacrolix readers hold piece-download
	// priorities until Close(), and the player opens a new request (new reader)
	// on each seek. Closing on handler return releases stale prioritisation so
	// the swarm bandwidth follows the region the user actually seeked to.
	defer reader.Close()

	// Responsive mode hands the consumer whatever bytes have arrived instead of
	// blocking until a full readahead window is downloaded, and a generous
	// readahead lets the client fetch pieces ahead of playback so a seek
	// doesn't stall.
	reader.SetResponsive()
	reader.SetReadahead(16 << 20) // 16 MiB

	http.ServeContent(w, r, largest.DisplayPath(), time.Time{}, reader)
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

// NewServer returns a pre-configured *http.Server with ReadHeaderTimeout set.
func NewServer(addr string) *http.Server {
	return &http.Server{
		Addr: addr,
		// Guards against slow-loris attacks. Torrent streaming runs for minutes,
		// so WriteTimeout and IdleTimeout are intentionally not set.
		ReadHeaderTimeout: 10 * time.Second,
	}
}

func (p *Player) SetupHandlers(mux *http.ServeMux) {
	mux.HandleFunc("/api/subtitles", utils.CorsMiddleware(func(w http.ResponseWriter, r *http.Request) {
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

		allSubs := p.addonMgr.GetAllSubtitles(mediaType, stremioID)
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
	mux.HandleFunc("/api/streams", utils.CorsMiddleware(func(w http.ResponseWriter, r *http.Request) {
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

	mux.HandleFunc("/api/play", utils.CorsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		infoHash := r.URL.Query().Get("hash")
		streamURL := r.URL.Query().Get("url")

		// Direct http(s) sources: redirect mpv straight to them.
		if streamURL != "" {
			http.Redirect(w, r, streamURL, http.StatusTemporaryRedirect)
			return
		}

		// Torrent sources: stream the largest file as seekable http. mpv handles
		// every codec/container natively, so no transcoding is involved.
		if infoHash != "" {
			p.StreamTorrent(infoHash, w, r)
			return
		}

		http.Error(w, "missing hash or url", http.StatusBadRequest)
	}))

	// Legacy polling endpoint — kept for compatibility; prefer /api/progress/stream (SSE).
	mux.HandleFunc("/api/progress", utils.CorsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		hash := r.URL.Query().Get("hash")
		err := json.NewEncoder(w).Encode(p.GetProgress(hash))
		if err != nil {
			log.Println(err)
		}
	}))

	mux.HandleFunc("/api/progress/stream", utils.CorsMiddleware(func(w http.ResponseWriter, r *http.Request) {
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
	mux.HandleFunc("/api/speedtest", utils.CorsMiddleware(func(w http.ResponseWriter, r *http.Request) {
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

	mux.HandleFunc("/api/subtitle-proxy", utils.CorsMiddleware(func(w http.ResponseWriter, r *http.Request) {
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
