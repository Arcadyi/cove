package library

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Arcadyi/cove/internal/utils"
)

// ── Types ──────────────────────────────────────────────────────────────────────
//
// All struct fields use JSON snake_case to match future Supabase column names
// directly — no transformation needed when syncing.

type Status = string

const (
	StatusWatchLater Status = "watch_later"
	StatusWatching   Status = "watching"
	StatusFinished   Status = "finished"
	StatusDropped    Status = "dropped"
)

// LibraryEntry mirrors the `library_entries` Supabase table.
type LibraryEntry struct {
	ID          string   `json:"id"`      // UUIDv4
	UserID      *string  `json:"user_id"` // null until Supabase auth is wired up
	TmdbID      int      `json:"tmdb_id"`
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
	ID              string    `json:"id"`
	UserID          *string   `json:"user_id"`
	LibraryEntryID  string    `json:"library_entry_id"`
	TmdbID          int       `json:"tmdb_id"`
	MediaType       string    `json:"media_type"`
	Season          *int      `json:"season"`  // null for movies
	Episode         *int      `json:"episode"` // null for movies
	PositionSeconds float64   `json:"position_seconds"`
	DurationSeconds float64   `json:"duration_seconds"`
	Completed       bool      `json:"completed"`
	WatchedAt       time.Time `json:"watched_at"`
}

// diskStore is the on-disk JSON format.
type diskStore struct {
	Entries  map[string]*LibraryEntry  `json:"entries"`  // key: entryKey()
	Progress map[string]*WatchProgress `json:"progress"` // key: progressKey()
}

// Library ── Service ──────────────────────────────────────────────────────────────────
//
// Library owns all of the package's mutable state. It used to live in package
// globals; holding it on a struct lets callers construct (and tests spin up)
// independent instances, and removes the hidden coupling between Init and the
// handlers. Fields are unexported on purpose: nothing outside this package
// touches them, and keeping them unexported means tygo emits nothing for this
// type — only the JSON data types (LibraryEntry, WatchProgress, diskStore)
// cross into the generated TS.
type Library struct {
	mu   sync.RWMutex
	db   diskStore
	path string
}

// ── New ────────────────────────────────────────────────────────────────────────

// New loads library.json from the same directory as the binary, creating it
// with empty maps if it doesn't exist yet. It always returns a usable (non-nil)
// *Library even on error, so the caller can still register handlers against an
// empty store rather than crashing — matching the old Init's best-effort
// behaviour.
func New() (*Library, error) {
	l := &Library{
		db: diskStore{
			Entries:  make(map[string]*LibraryEntry),
			Progress: make(map[string]*WatchProgress),
		},
	}

	ex, err := os.Executable()
	if err != nil {
		return l, err
	}
	l.path = filepath.Join(filepath.Dir(ex), "library.json")

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
	return utils.AtomicWriteFile(l.path, raw, 0o644)
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

func (l *Library) SetupHandlers() {
	// /api/library/progress must be registered before /api/library/ so Go's mux
	// matches it as the more-specific fixed path.
	http.HandleFunc("/api/library/progress", utils.CorsMiddleware(l.handleProgress))
	http.HandleFunc("/api/library", utils.CorsMiddleware(l.handleCollection))
	http.HandleFunc("/api/library/", utils.CorsMiddleware(l.handleItem))
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
		// Re-link any progress records that became orphaned when the entry was
		// previously removed. This keeps library_entry_id consistent for a
		// future Supabase sync where it may be used as a foreign key.
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

		// Upsert the progress record.
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
		var progList []*WatchProgress
		for _, p := range l.db.Progress {
			if p.TmdbID == tmdbID && p.MediaType == mediaType {
				progList = append(progList, p)
			}
		}
		l.mu.RUnlock()
		// Only 404 when there is genuinely nothing at all — no entry and no
		// progress history. An entry-less response with progress records is
		// valid: it means the user removed the title from their list but their
		// watch history is still intact.
		if entry == nil && len(progList) == 0 {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if progList == nil {
			progList = []*WatchProgress{}
		}
		jsonOK(w, map[string]any{"entry": entry, "progress": progList})

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

// ── Helpers ───────────────────────────────────────────────────────────────────

func jsonOK(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Println("library json encode:", err)
	}
}
