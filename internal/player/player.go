// Package player owns the torrent client and streams playback sources as
// seekable HTTP: torrents stream their largest file directly via
// http.ServeContent (mpv's Range requests just work, no transcoding
// involved), and direct-URL sources get a redirect straight to the origin.
// A background reaper (CleanupTorrents) drops idle torrents and their
// on-disk pieces after 30 minutes of no active readers, so a long-running
// process doesn't accumulate downloaded data forever.
package player

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/coveninja/cove/internal/addons"
	"github.com/coveninja/cove/internal/nuvio"
	"github.com/coveninja/cove/internal/tmdb"
	"github.com/coveninja/cove/internal/utils"
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
	nuvioMgr   *nuvio.Manager

	// streamHeaders remembers every stream URL the backend itself returned
	// from /api/streams (keyed by URL), together with any extra HTTP headers
	// (Referer/Origin, etc.) the origin CDN requires. /api/play only accepts
	// URLs found here — anything else is an open-redirect/SSRF attempt, not a
	// stream we offered. Entries expire after streamHeadersTTL, refreshed on
	// every lookup so long playback sessions stay valid.
	streamHeadersMu sync.Mutex
	streamHeaders   map[string]streamHeaderEntry
}

type streamHeaderEntry struct {
	headers map[string]string
	expires time.Time
}

const streamHeadersTTL = 30 * time.Minute

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
// injected TMDB client, addon manager, and Nuvio plugin manager. The torrent
// client is core functionality, so a failure here is returned for the caller
// to treat as fatal.
func New(tmdbClient *tmdb.Client, addonMgr *addons.Manager, nuvioMgr *nuvio.Manager) (*Player, error) {
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
		nuvioMgr:       nuvioMgr,
		streamHeaders:  map[string]streamHeaderEntry{},
	}, nil
}

// rememberStream records a stream URL the backend returned to the client
// (with any extra headers its origin requires — nil for most), authorizing
// it for later /api/play use. Sweeps expired entries on every call instead
// of running a background goroutine, since inserts only happen when streams
// are listed and that stays cheap.
func (p *Player) rememberStream(streamURL string, headers map[string]string) {
	if streamURL == "" {
		return
	}
	p.streamHeadersMu.Lock()
	defer p.streamHeadersMu.Unlock()
	now := time.Now()
	for k, v := range p.streamHeaders {
		if now.After(v.expires) {
			delete(p.streamHeaders, k)
		}
	}
	p.streamHeaders[streamURL] = streamHeaderEntry{headers: headers, expires: now.Add(streamHeadersTTL)}
}

// lookupStream reports whether streamURL is one the backend itself offered,
// returning its remembered headers (usually nil). A hit refreshes the entry's
// TTL so mpv's follow-up Range requests during a long watch never expire it.
func (p *Player) lookupStream(streamURL string) (headers map[string]string, known bool) {
	p.streamHeadersMu.Lock()
	defer p.streamHeadersMu.Unlock()
	entry, ok := p.streamHeaders[streamURL]
	if !ok || time.Now().After(entry.expires) {
		return nil, false
	}
	entry.expires = time.Now().Add(streamHeadersTTL)
	p.streamHeaders[streamURL] = entry
	return entry.headers, true
}

// proxyStream forwards the request to streamURL with extra headers attached,
// for origins that reject a bare redirect (no Referer/Origin) but work fine
// when the request carries them. Uses httputil.ReverseProxy so Range
// requests (mpv's seek mechanism), status codes, and body streaming are
// handled correctly instead of hand-rolled.
func (p *Player) proxyStream(streamURL string, headers map[string]string, w http.ResponseWriter, r *http.Request) {
	target, err := url.Parse(streamURL)
	if err != nil {
		http.Error(w, "invalid stream url", http.StatusBadGateway)
		return
	}
	proxy := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL.Scheme = target.Scheme
			req.URL.Host = target.Host
			req.URL.Path = target.Path
			req.URL.RawQuery = target.RawQuery
			req.Host = target.Host
			for k, v := range headers {
				req.Header.Set(k, v)
			}
		},
		// Bound connection setup but never the transfer itself — a stream
		// runs for the length of the film. FlushInterval -1 disables output
		// buffering so playback data reaches mpv as it arrives.
		Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			DialContext:           (&net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
			TLSHandshakeTimeout:   15 * time.Second,
			ResponseHeaderTimeout: 30 * time.Second,
		},
		FlushInterval: -1,
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			log.Println("stream proxy:", err)
			w.WriteHeader(http.StatusBadGateway)
		},
	}
	proxy.ServeHTTP(w, r)
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
	// A concurrent first play of the same hash may have registered while we
	// waited on GotInfo (AddMagnet dedupes to the same *Torrent). Keep the
	// existing state — overwriting would discard its live reader count and
	// let the reaper drop a torrent that's still being streamed.
	if st, ok := p.activeTorrents[infoHash]; ok {
		st.lastUsed = now
	} else {
		p.activeTorrents[infoHash] = &torrentState{
			torrent:   t,
			lastCheck: now,
			lastUsed:  now,
		}
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
		var seasonNum, episodeNum *int
		if mediaType == "tv" {
			season := r.URL.Query().Get("season")
			episode := r.URL.Query().Get("episode")
			if season == "" || episode == "" {
				http.Error(w, "season and episode are required for tv streams", http.StatusBadRequest)
				return
			}
			stremioID = fmt.Sprintf("%s:%s:%s", imdbID, season, episode)
			if sv, serr := strconv.Atoi(season); serr == nil {
				seasonNum = &sv
			}
			if ev, eerr := strconv.Atoi(episode); eerr == nil {
				episodeNum = &ev
			}
		}

		streams, err := p.addonMgr.GetAllStreams(mediaType, stremioID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if streams == nil {
			streams = []addons.Stream{}
		}

		// Skip the extra TMDB lookup entirely for the common case (no Nuvio
		// scrapers enabled) — it exists purely to feed Nuvio's metadata.
		if p.nuvioMgr != nil && p.nuvioMgr.HasEnabledScrapers() {
			var title string
			var year int
			if media, mErr := p.tmdbClient.GetMediaByID(id, mediaType); mErr == nil && media != nil {
				title = firstNonEmpty(media.Title, media.Name)
				year = parseYear(firstNonEmpty(media.Released, media.FirstAir))
			}
			nuvioStreams := p.nuvioMgr.GetStreams(mediaType, id, imdbID, title, year, seasonNum, episodeNum)
			streams = append(streams, nuvioStreams...)
		}

		// Register every direct-URL stream we're about to offer — /api/play
		// only accepts URLs from this registry (see rememberStream).
		for _, s := range streams {
			p.rememberStream(s.URL, s.Headers)
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

		// Direct http(s) sources: redirect mpv straight to them, unless the
		// origin needs extra headers (e.g. Referer) that a redirect can't
		// carry — in that case proxy the request instead. Only URLs this
		// backend itself returned from /api/streams are accepted; anything
		// else would make this an open redirect / request proxy.
		if streamURL != "" {
			if u, err := url.Parse(streamURL); err != nil ||
				(u.Scheme != "http" && u.Scheme != "https") {
				http.Error(w, "invalid stream url", http.StatusBadRequest)
				return
			}
			headers, known := p.lookupStream(streamURL)
			if !known {
				http.Error(w, "unknown stream url (list streams first)", http.StatusForbidden)
				return
			}
			if len(headers) > 0 {
				p.proxyStream(streamURL, headers, w, r)
				return
			}
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
		// Subtitle URLs come from third-party addons; only plain http(s) to
		// public hosts is allowed (SafeHTTPClient refuses local/private IPs).
		if _, err := utils.ValidatePublicURL(rawURL); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, rawURL, nil)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		resp, err := utils.SafeHTTPClient.Do(req)
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

		// 10 MiB is far beyond any real subtitle file.
		body, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/vtt; charset=utf-8")

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

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// parseYear extracts the year from a TMDB-style "YYYY-MM-DD" date string.
// Returns 0 (not an error) for an empty or malformed date, since a Nuvio
// scraper's metadata.year is best-effort context, not something to fail over.
func parseYear(date string) int {
	if len(date) < 4 {
		return 0
	}
	year, err := strconv.Atoi(date[:4])
	if err != nil {
		return 0
	}
	return year
}
