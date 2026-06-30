package library

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coveninja/cove/internal/utils"
)

// ── Types ──────────────────────────────────────────────────────────────────────
//
// All struct fields use JSON snake_case to match Supabase column names directly.

type Status = string

const (
	StatusWatchLater Status = "watch_later"
	StatusWatching   Status = "watching"
	StatusFinished   Status = "finished"
	StatusDropped    Status = "dropped"
)

// LibraryEntry mirrors the `library_entries` Supabase table.
type LibraryEntry struct {
	ID        string  `json:"id"`         // UUIDv4
	ProfileID *string `json:"profile_id"`
	TmdbID    int     `json:"tmdb_id"`
	MediaType   string   `json:"media_type"` // "movie" | "tv"
	Title       string   `json:"title"`
	PosterPath  string   `json:"poster_path"`
	Status      Status   `json:"status"`
	Rating      *float64 `json:"rating"`       // 0.0–5.0; null = unrated
	VoteAverage float64  `json:"vote_average"` // TMDB community rating, stored for offline display
	// TV tracking — used for "new episodes" detection and resume labels
	LastAirDate        string     `json:"last_air_date"`        // TMDB last_air_date; empty for movies
	LastWatchedAt      *time.Time `json:"last_watched_at"`      // updated every time progress is saved
	LastWatchedSeason  *int       `json:"last_watched_season"`  // most recently watched TV episode
	LastWatchedEpisode *int       `json:"last_watched_episode"` // most recently watched TV episode
	LastAiredSeason    *int       `json:"last_aired_season"`    // TMDB last_episode_to_air.season_number
	LastAiredEpisode   *int       `json:"last_aired_episode"`   // TMDB last_episode_to_air.episode_number
	AddedAt            time.Time  `json:"added_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
}

// WatchProgress mirrors the `watch_progress` Supabase table.
// One row per unique (tmdb_id, media_type, season, episode) tuple.
type WatchProgress struct {
	ID             string  `json:"id"`
	ProfileID      *string `json:"profile_id"`
	LibraryEntryID string  `json:"library_entry_id"`
	TmdbID          int       `json:"tmdb_id"`
	MediaType       string    `json:"media_type"`
	Season          *int      `json:"season"`  // null for movies
	Episode         *int      `json:"episode"` // null for movies
	PositionSeconds float64   `json:"position_seconds"`
	DurationSeconds float64   `json:"duration_seconds"`
	Completed       bool      `json:"completed"`
	WatchedAt       time.Time `json:"watched_at"`
}

// Dismissal records a "not interested" — a title to keep out of recommendations
// and to nudge taste away from, without it being a library shelf entry.
type Dismissal struct {
	TmdbID      int       `json:"tmdb_id"`
	MediaType   string    `json:"media_type"`
	DismissedAt time.Time `json:"dismissed_at"`
}

// diskStore is the on-disk JSON format.
type diskStore struct {
	Entries   map[string]*LibraryEntry  `json:"entries"`   // key: entryKey()
	Progress  map[string]*WatchProgress `json:"progress"`  // key: progressKey()
	Dismissed map[string]*Dismissal     `json:"dismissed"` // key: entryKey()
}

// TasteSignal is the minimal per-title signal the discover package needs,
// without exposing the library's internals.
type TasteSignal struct {
	TmdbID     int
	MediaType  string
	Status     Status
	UserRating *float64 // user's 0–5 rating; nil if unrated
	Completed  bool     // any progress record for this title is completed
	Dismissed  bool
}

// AllEntries returns a snapshot of all library entries. Used for sync.
func (l *Library) AllEntries() []*LibraryEntry {
	l.mu.RLock()
	defer l.mu.RUnlock()
	out := make([]*LibraryEntry, 0, len(l.db.Entries))
	for _, e := range l.db.Entries {
		cp := *e
		out = append(out, &cp)
	}
	return out
}

// AllProgress returns a snapshot of all watch progress records. Used for sync.
func (l *Library) AllProgress() []*WatchProgress {
	l.mu.RLock()
	defer l.mu.RUnlock()
	out := make([]*WatchProgress, 0, len(l.db.Progress))
	for _, p := range l.db.Progress {
		cp := *p
		out = append(out, &cp)
	}
	return out
}

// AllDismissals returns a snapshot of all dismissal records. Used for sync.
func (l *Library) AllDismissals() []*Dismissal {
	l.mu.RLock()
	defer l.mu.RUnlock()
	out := make([]*Dismissal, 0, len(l.db.Dismissed))
	for _, d := range l.db.Dismissed {
		cp := *d
		out = append(out, &cp)
	}
	return out
}

// MergeFrom merges remote data (pulled from Supabase) into the local store.
// Remote entries replace local ones for the same (tmdb_id, media_type) key.
// Watch progress takes the max position_seconds. Dismissals are unioned.
func (l *Library) MergeFrom(entries []*LibraryEntry, progress []*WatchProgress, dismissals []*Dismissal) {
	l.mu.Lock()
	for _, e := range entries {
		key := entryKey(e.TmdbID, e.MediaType)
		if local, ok := l.db.Entries[key]; ok {
			if e.UpdatedAt.After(local.UpdatedAt) {
				l.db.Entries[key] = e
			}
		} else {
			l.db.Entries[key] = e
		}
	}
	for _, p := range progress {
		key := progressKey(p.TmdbID, p.MediaType, p.Season, p.Episode)
		if local, ok := l.db.Progress[key]; ok {
			if p.PositionSeconds > local.PositionSeconds {
				l.db.Progress[key] = p
			}
		} else {
			l.db.Progress[key] = p
		}
	}
	for _, d := range dismissals {
		key := entryKey(d.TmdbID, d.MediaType)
		if _, ok := l.db.Dismissed[key]; !ok {
			l.db.Dismissed[key] = d
		}
	}
	l.mu.Unlock()
	_ = l.persist()
	l.gen.Add(1)
}

// TasteSignals returns one signal per title the user has any history with —
// every library entry, plus titles with completed watch history even if the
// entry was later removed. Safe to call concurrently.
func (l *Library) TasteSignals() []TasteSignal {
	l.mu.RLock()
	defer l.mu.RUnlock()

	completed := make(map[string]bool)
	for _, p := range l.db.Progress {
		if p.Completed {
			completed[entryKey(p.TmdbID, p.MediaType)] = true
		}
	}

	out := make([]TasteSignal, 0, len(l.db.Entries))
	seen := make(map[string]bool, len(l.db.Entries))
	for _, e := range l.db.Entries {
		key := entryKey(e.TmdbID, e.MediaType)
		seen[key] = true
		out = append(out, TasteSignal{
			TmdbID:     e.TmdbID,
			MediaType:  e.MediaType,
			Status:     e.Status,
			UserRating: e.Rating,
			Completed:  completed[key],
		})
	}

	// Titles removed from the list but still finished: don't re-recommend them,
	// and keep them as a positive taste signal.
	for _, p := range l.db.Progress {
		if !p.Completed {
			continue
		}
		key := entryKey(p.TmdbID, p.MediaType)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, TasteSignal{
			TmdbID:    p.TmdbID,
			MediaType: p.MediaType,
			Completed: true, // no entry => no status
		})
	}
	for key, d := range l.db.Dismissed {
		if seen[key] {
			continue // already represented; exclusion is handled either way
		}
		seen[key] = true
		out = append(out, TasteSignal{
			TmdbID:    d.TmdbID,
			MediaType: d.MediaType,
			Dismissed: true,
		})
	}
	return out
}

type Stats struct {
	Total      int            `json:"total"`     // library entries (dismissals excluded)
	ByType     map[string]int `json:"by_type"`   // entries per media type
	ByStatus   map[string]int `json:"by_status"` // status -> count
	Finished   map[string]int `json:"finished"`  // finished entries per type
	Dismissed  int            `json:"dismissed"`
	Rated      int            `json:"rated"`
	AvgRating  float64        `json:"avg_rating"`  // mean user rating over rated titles, 0–5
	MovieShare float64        `json:"movie_share"` // preference, from finished+watching counts
	TVShare    float64        `json:"tv_share"`
}

func (l *Library) Stats() Stats {
	l.mu.RLock()
	defer l.mu.RUnlock()

	st := Stats{
		ByType:   map[string]int{"movie": 0, "tv": 0},
		ByStatus: map[string]int{},
		Finished: map[string]int{"movie": 0, "tv": 0},
	}
	engaged := map[string]int{"movie": 0, "tv": 0}
	var ratingSum float64

	for _, e := range l.db.Entries {
		st.Total++
		st.ByType[e.MediaType]++
		st.ByStatus[e.Status]++
		if e.Status == StatusFinished {
			st.Finished[e.MediaType]++
		}
		if e.Status == StatusFinished || e.Status == StatusWatching {
			engaged[e.MediaType]++
		}
		if e.Rating != nil {
			st.Rated++
			ratingSum += *e.Rating
		}
	}
	if st.Rated > 0 {
		st.AvgRating = ratingSum / float64(st.Rated)
	}
	st.Dismissed = len(l.db.Dismissed)

	if total := engaged["movie"] + engaged["tv"]; total > 0 {
		st.MovieShare = float64(engaged["movie"]) / float64(total)
		st.TVShare = float64(engaged["tv"]) / float64(total)
	}
	return st
}

// Library ── Service ──────────────────────────────────────────────────────────────────
//
// Library owns all of the package's mutable state. Fields are unexported, so
// tygo emits nothing for this type — only the JSON data types (LibraryEntry,
// WatchProgress, diskStore) cross into the generated TS.
type Library struct {
	mu   sync.RWMutex
	db   diskStore
	path string
	gen  atomic.Uint64
}

// ── New ────────────────────────────────────────────────────────────────────────

// New loads library-{profileID}.json from the per-user config directory (see
// utils.ConfigPath), creating it with empty maps if it doesn't exist yet. It
// always returns a usable (non-nil) *Library even on error, so the caller can
// still register handlers against an empty store rather than crashing —
// matching the old Init's best-effort behaviour.
func New(profileID string) (*Library, error) {
	l := &Library{
		db: diskStore{
			Entries:   make(map[string]*LibraryEntry),
			Progress:  make(map[string]*WatchProgress),
			Dismissed: make(map[string]*Dismissal),
		},
	}

	path, err := utils.ConfigPath(fmt.Sprintf("library-%s.json", profileID))
	if err != nil {
		return l, err
	}
	l.path = path

	raw, err := os.ReadFile(l.path)
	if os.IsNotExist(err) {
		// First run — persist empty maps so the file exists.
		return l, l.persist()
	}
	if err != nil {
		return l, err
	}

	// Maps are pre-initialised above, so a successful unmarshal merges into
	// them and a failure still leaves a usable empty store.
	if err := json.Unmarshal(raw, &l.db); err != nil {
		return l, err
	}
	return l, nil
}

func (l *Library) persist() error {
	raw, err := json.MarshalIndent(l.db, "", "  ")
	if err != nil {
		return err
	}
	l.gen.Add(1)
	return utils.AtomicWriteFile(l.path, raw, 0o644)
}

func (l *Library) Generation() uint64 { return l.gen.Load() }

// SetProfile reloads the library from the given profile's data file.
// Safe to call while handlers are live — takes the write lock while swapping.
func (l *Library) SetProfile(profileID string) error {
	path, err := utils.ConfigPath(fmt.Sprintf("library-%s.json", profileID))
	if err != nil {
		return err
	}
	newDB := diskStore{
		Entries:   make(map[string]*LibraryEntry),
		Progress:  make(map[string]*WatchProgress),
		Dismissed: make(map[string]*Dismissal),
	}
	raw, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err == nil {
		if err := json.Unmarshal(raw, &newDB); err != nil {
			return err
		}
	}
	l.mu.Lock()
	l.db = newDB
	l.path = path
	l.mu.Unlock()
	l.gen.Add(1)
	return nil
}

// ── Key helpers ───────────────────────────────────────────────────────────────

func entryKey(tmdbID int, mediaType string) string {
	return fmt.Sprintf("%d:%s", tmdbID, mediaType)
}

// progressKey builds a unique key for a playback position record.
// Movies: "12345:movie"
// TV episodes: "12345:tv:1:3" (show id, type, season, episode)
func progressKey(tmdbID int, mediaType string, season, episode *int) string {
	if season != nil && episode != nil {
		return fmt.Sprintf("%d:%s:%d:%d", tmdbID, mediaType, *season, *episode)
	}
	return fmt.Sprintf("%d:%s", tmdbID, mediaType)
}

func newUUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant bits
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

// ── SetupHandlers ─────────────────────────────────────────────────────────────

func (l *Library) SetupHandlers(mux *http.ServeMux) {
	// /api/library/progress must be registered before /api/library/ so Go's mux
	// matches it as the more-specific fixed path.
	mux.HandleFunc("/api/library/progress", utils.CorsMiddleware(l.handleProgress))
	mux.HandleFunc("/api/library", utils.CorsMiddleware(l.handleCollection))
	mux.HandleFunc("/api/library/", utils.CorsMiddleware(l.handleItem))
	mux.HandleFunc("/api/library/dismiss", utils.CorsMiddleware(l.handleDismiss))
	mux.HandleFunc("/api/library/stats", utils.CorsMiddleware(l.handleStats))
}

// ── Handler: /api/library ─────────────────────────────────────────────────────
//
// GET  — list all entries; filter by ?status=watching etc.
// POST — upsert an entry (idempotent on tmdb_id+media_type)

func (l *Library) handleCollection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodOptions:
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.WriteHeader(http.StatusNoContent)

	case http.MethodGet:
		statusFilter := r.URL.Query().Get("status")
		l.mu.RLock()
		list := make([]*LibraryEntry, 0, len(l.db.Entries))
		for _, e := range l.db.Entries {
			if statusFilter == "" || e.Status == statusFilter {
				list = append(list, e)
			}
		}
		l.mu.RUnlock()
		jsonOK(w, list)

	case http.MethodPost:
		var body struct {
			TmdbID           int     `json:"tmdb_id"`
			MediaType        string  `json:"media_type"`
			Title            string  `json:"title"`
			PosterPath       string  `json:"poster_path"`
			Status           Status  `json:"status"`
			VoteAverage      float64 `json:"vote_average"`
			LastAirDate      string  `json:"last_air_date"`
			LastAiredSeason  *int    `json:"last_aired_season"`
			LastAiredEpisode *int    `json:"last_aired_episode"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid body: "+err.Error(), http.StatusBadRequest)
			return
		}
		if body.Status == "" {
			body.Status = StatusWatchLater
		}

		l.mu.Lock()
		key := entryKey(body.TmdbID, body.MediaType)
		now := time.Now()
		entry, exists := l.db.Entries[key]
		if !exists {
			entry = &LibraryEntry{ID: newUUID(), AddedAt: now}
			l.db.Entries[key] = entry
		}
		entry.TmdbID = body.TmdbID
		entry.MediaType = body.MediaType
		entry.Title = body.Title
		entry.PosterPath = body.PosterPath
		entry.Status = body.Status
		entry.VoteAverage = body.VoteAverage
		entry.LastAirDate = body.LastAirDate
		// Only overwrite if the caller actually sent values — avoids resetting
		// fields populated by other code paths (e.g. progressSave).
		if body.LastAiredSeason != nil {
			entry.LastAiredSeason = body.LastAiredSeason
		}
		if body.LastAiredEpisode != nil {
			entry.LastAiredEpisode = body.LastAiredEpisode
		}
		entry.UpdatedAt = now
		// Re-link any progress records orphaned when the entry was previously
		// removed — keeps library_entry_id consistent for Supabase sync.
		if !exists {
			for _, p := range l.db.Progress {
				if p.TmdbID == body.TmdbID && p.MediaType == body.MediaType {
					p.LibraryEntryID = entry.ID
				}
			}
		}
		err := l.persist()
		result := *entry // copy before unlock
		l.mu.Unlock()

		if err != nil {
			http.Error(w, "persist failed", http.StatusInternalServerError)
			return
		}
		jsonOK(w, &result)

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// ── Handler: /api/library/progress ────────────────────────────────────────────
//
// GET  — fetch one progress record by query params
// POST — upsert a progress record; auto-creates library entry as "watching"

func (l *Library) handleProgress(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodOptions:
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.WriteHeader(http.StatusNoContent)

	case http.MethodGet:
		tmdbID, err := strconv.Atoi(r.URL.Query().Get("tmdb_id"))
		if err != nil {
			http.Error(w, "invalid tmdb_id", http.StatusBadRequest)
			return
		}
		mediaType := r.URL.Query().Get("media_type")
		if mediaType == "" {
			http.Error(w, "missing media_type", http.StatusBadRequest)
			return
		}

		var season, episode *int
		if s := r.URL.Query().Get("season"); s != "" {
			if v, err := strconv.Atoi(s); err == nil {
				season = &v
			}
		}
		if e := r.URL.Query().Get("episode"); e != "" {
			if v, err := strconv.Atoi(e); err == nil {
				episode = &v
			}
		}

		pKey := progressKey(tmdbID, mediaType, season, episode)
		l.mu.RLock()
		p := l.db.Progress[pKey]
		l.mu.RUnlock()
		// Return null JSON if not found (not a 404 — absence is normal)
		jsonOK(w, p)

	case http.MethodPost:
		var body struct {
			TmdbID           int     `json:"tmdb_id"`
			MediaType        string  `json:"media_type"`
			Title            string  `json:"title"`
			PosterPath       string  `json:"poster_path"`
			VoteAverage      float64 `json:"vote_average"`
			LastAirDate      string  `json:"last_air_date"`
			LastAiredSeason  *int    `json:"last_aired_season"`
			LastAiredEpisode *int    `json:"last_aired_episode"`
			Season           *int    `json:"season"`
			Episode          *int    `json:"episode"`
			PositionSeconds  float64 `json:"position_seconds"`
			DurationSeconds  float64 `json:"duration_seconds"`
			Completed        bool    `json:"completed"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid body: "+err.Error(), http.StatusBadRequest)
			return
		}

		l.mu.Lock()
		now := time.Now()

		// Auto-create library entry as "watching" if it doesn't exist yet.
		eKey := entryKey(body.TmdbID, body.MediaType)
		entry, exists := l.db.Entries[eKey]
		if !exists {
			entry = &LibraryEntry{
				ID:          newUUID(),
				TmdbID:      body.TmdbID,
				MediaType:   body.MediaType,
				Title:       body.Title,
				PosterPath:  body.PosterPath,
				VoteAverage: body.VoteAverage,
				LastAirDate: body.LastAirDate,
				Status:      StatusWatching,
				AddedAt:     now,
				UpdatedAt:   now,
			}
			l.db.Entries[eKey] = entry
		}

		// Always update last-watched metadata so resume labels stay current.
		entry.LastWatchedAt = &now
		if body.Season != nil {
			entry.LastWatchedSeason = body.Season
		}
		if body.Episode != nil {
			entry.LastWatchedEpisode = body.Episode
		}
		if body.LastAirDate != "" {
			entry.LastAirDate = body.LastAirDate
		}
		if body.LastAiredSeason != nil {
			entry.LastAiredSeason = body.LastAiredSeason
		}
		if body.LastAiredEpisode != nil {
			entry.LastAiredEpisode = body.LastAiredEpisode
		}

		pKey := progressKey(body.TmdbID, body.MediaType, body.Season, body.Episode)
		prog, progExists := l.db.Progress[pKey]
		if !progExists {
			prog = &WatchProgress{
				ID:             newUUID(),
				LibraryEntryID: entry.ID,
				TmdbID:         body.TmdbID,
				MediaType:      body.MediaType,
				Season:         body.Season,
				Episode:        body.Episode,
			}
			l.db.Progress[pKey] = prog
		}
		prog.PositionSeconds = body.PositionSeconds
		prog.DurationSeconds = body.DurationSeconds
		prog.Completed = body.Completed
		prog.WatchedAt = now

		// If the episode/movie was just completed and the entry was "watching",
		// don't auto-flip to "finished" — user controls that manually.

		err := l.persist()
		result := *prog // copy before unlock
		l.mu.Unlock()

		if err != nil {
			http.Error(w, "persist failed", http.StatusInternalServerError)
			return
		}
		jsonOK(w, &result)

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// ── Handler: /api/library/{tmdbId}/{mediaType}[/{sub}] ────────────────────────
//
// GET    /api/library/{id}/{type}         — entry + all its progress records
// DELETE /api/library/{id}/{type}         — remove entry + all its progress
// PATCH  /api/library/{id}/{type}/status  — update status field
// PATCH  /api/library/{id}/{type}/rating  — update rating field (null to clear)

func (l *Library) handleItem(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.Header().Set("Access-Control-Allow-Methods", "GET, DELETE, PATCH, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// "/api/library/12345/movie" → trimmed = "12345/movie"
	// "/api/library/12345/movie/rating" → trimmed = "12345/movie/rating"
	trimmed := strings.TrimPrefix(r.URL.Path, "/api/library/")
	parts := strings.SplitN(trimmed, "/", 3)
	if len(parts) < 2 {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}

	tmdbID, err := strconv.Atoi(parts[0])
	if err != nil {
		http.Error(w, "invalid tmdb_id", http.StatusBadRequest)
		return
	}
	mediaType := parts[1]
	sub := ""
	if len(parts) == 3 {
		sub = parts[2]
	}
	key := entryKey(tmdbID, mediaType)

	switch {
	// ── GET /api/library/{id}/{type} ──────────────────────────────────────────
	case sub == "" && r.Method == http.MethodGet:
		l.mu.RLock()
		entry := l.db.Entries[key]
		_, dismissed := l.db.Dismissed[key]
		var progList []*WatchProgress
		for _, p := range l.db.Progress {
			if p.TmdbID == tmdbID && p.MediaType == mediaType {
				progList = append(progList, p)
			}
		}
		l.mu.RUnlock()
		// Always respond 200, even when the title isn't in the library. Every
		// MediaCard probes this endpoint to check whether its title is saved, so
		// a 404 here floods the devtools console with "failed to load resource"
		// for every not-yet-saved title. A null `entry` is the not-found signal
		// instead (the frontend's requestOrNull / `result?.entry` handles it).
		if progList == nil {
			progList = []*WatchProgress{}
		}
		jsonOK(w, map[string]any{"entry": entry, "progress": progList, "dismissed": dismissed})

	// ── DELETE /api/library/{id}/{type} ───────────────────────────────────────
	case sub == "" && r.Method == http.MethodDelete:
		l.mu.Lock()
		delete(l.db.Entries, key)
		// Deliberately do NOT delete WatchProgress records. The user is only
		// removing the title from their list, not erasing their watch history.
		// Progress records are keyed by (tmdb_id, media_type, season, episode)
		// and will be re-linked to a new entry if the title is added back later.
		err := l.persist()
		l.mu.Unlock()
		if err != nil {
			http.Error(w, "persist failed", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)

	// ── PATCH /api/library/{id}/{type}/status ─────────────────────────────────
	case sub == "status" && r.Method == http.MethodPatch:
		var body struct {
			Status Status `json:"status"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid body", http.StatusBadRequest)
			return
		}
		l.mu.Lock()
		entry := l.db.Entries[key]
		if entry == nil {
			l.mu.Unlock()
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		entry.Status = body.Status
		entry.UpdatedAt = time.Now()
		err := l.persist()
		result := *entry
		l.mu.Unlock()
		if err != nil {
			http.Error(w, "persist failed", http.StatusInternalServerError)
			return
		}
		jsonOK(w, &result)

	// ── PATCH /api/library/{id}/{type}/rating ─────────────────────────────────
	case sub == "rating" && r.Method == http.MethodPatch:
		var body struct {
			Rating *float64 `json:"rating"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid body", http.StatusBadRequest)
			return
		}
		if body.Rating != nil && (*body.Rating < 0 || *body.Rating > 5) {
			http.Error(w, "rating must be between 0 and 5", http.StatusBadRequest)
			return
		}
		l.mu.Lock()
		entry := l.db.Entries[key]
		if entry == nil {
			l.mu.Unlock()
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		entry.Rating = body.Rating
		entry.UpdatedAt = time.Now()
		err := l.persist()
		result := *entry
		l.mu.Unlock()
		if err != nil {
			http.Error(w, "persist failed", http.StatusInternalServerError)
			return
		}
		jsonOK(w, &result)

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (l *Library) handleDismiss(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodOptions:
		w.Header().Set("Access-Control-Allow-Methods", "POST, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.WriteHeader(http.StatusNoContent)

	case http.MethodPost, http.MethodDelete:
		var body struct {
			TmdbID    int    `json:"tmdb_id"`
			MediaType string `json:"media_type"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid body: "+err.Error(), http.StatusBadRequest)
			return
		}
		if body.TmdbID == 0 || body.MediaType == "" {
			http.Error(w, "tmdb_id and media_type required", http.StatusBadRequest)
			return
		}

		key := entryKey(body.TmdbID, body.MediaType)
		l.mu.Lock()
		if r.Method == http.MethodPost {
			l.db.Dismissed[key] = &Dismissal{
				TmdbID:      body.TmdbID,
				MediaType:   body.MediaType,
				DismissedAt: time.Now(),
			}
		} else {
			delete(l.db.Dismissed, key)
		}
		err := l.persist()
		l.mu.Unlock()
		if err != nil {
			http.Error(w, "persist failed", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (l *Library) handleStats(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	jsonOK(w, l.Stats())
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func jsonOK(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Println("library json encode:", err)
	}
}
