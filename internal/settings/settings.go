package settings

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"

	"github.com/coveninja/cove/internal/utils"
)

// Settings holds all user-configurable preferences persisted to disk.
type Settings struct {
	// Playback
	OpenOnMute       bool    `json:"openOnMute"`       // start videos muted
	DefaultVolume    float64 `json:"defaultVolume"`    // 0.0–1.0
	AutoPlay         bool    `json:"autoPlay"`         // autoplay next episode
	RememberPosition bool    `json:"rememberPosition"` // resume from last position

	// Provider / streams
	DefaultProvider string `json:"defaultProvider"` // e.g. "torrentio", "debrid"
	PreferHLS       bool   `json:"preferHLS"`       // use HLS pipeline over direct stream

	// Stream auto-selection
	AutoSelectStream      bool    `json:"autoSelectStream"`      // skip the manual stream list and play immediately
	StreamSelectionMode   string  `json:"streamSelectionMode"`   // "balanced" | "seeders" | "quality" | "smallest" | "bandwidth"
	MeasuredBandwidthMbps float64 `json:"measuredBandwidthMbps"` // from the in-app speed test; 0 = never measured

	// Subtitles
	SubtitlesEnabled    bool    `json:"subtitlesEnabled"`
	DefaultSubtitleLang string  `json:"defaultSubtitleLang"` // ISO 639-1 e.g. "en"
	DefaultAudioLang    string  `json:"defaultAudioLang"`    // ISO 639-1 e.g. "en"
	SubtitleSize        float64 `json:"subtitleSize"`        // percentage, 50–200
	SubtitlePosition    float64 `json:"subtitlePosition"`    // percent from bottom, 2–90
	SubtitleBackground  bool    `json:"subtitleBackground"`  // dark box behind subtitle text

	// UI
	ShowStreamDetails bool `json:"showStreamDetails"` // show codec/resolution badges on stream list
	HideSpoilers      bool `json:"hideSpoilers"`

	// Segment skip (via IntroDB)
	AutoSkipIntro   bool `json:"autoSkipIntro"`
	AutoSkipRecap   bool `json:"autoSkipRecap"`
	AutoSkipCredits bool `json:"autoSkipCredits"`
	AutoSkipPreview bool `json:"autoSkipPreview"`
}

var defaultSettings = Settings{
	OpenOnMute:            false,
	DefaultVolume:         1.0,
	AutoPlay:              false,
	RememberPosition:      true,
	DefaultProvider:       "torrentio",
	PreferHLS:             true,
	AutoSelectStream:      false,
	StreamSelectionMode:   "balanced",
	MeasuredBandwidthMbps: 0,
	SubtitlesEnabled:      false,
	DefaultSubtitleLang:   "en",
	DefaultAudioLang:      "en",
	SubtitleSize:          100,
	SubtitlePosition:      8,
	SubtitleBackground:    true,
	ShowStreamDetails:     true,
	HideSpoilers:          false,
	AutoSkipIntro:         false,
	AutoSkipRecap:         false,
	AutoSkipCredits:       false,
	AutoSkipPreview:       false,
}

// Store owns the package's mutable state. Fields are unexported, so tygo emits
// nothing for Store — only the Settings data type crosses into the generated TS.
type Store struct {
	mu     sync.RWMutex
	cached Settings
	path   string
}

// New resolves settings-{profileID}.json in the per-user config directory (see
// utils.ConfigPath) and loads it, or writes the defaults on first run. It always
// returns a usable (non-nil) *Store even on error, so the caller can register
// handlers against in-memory defaults rather than crashing.
func New(profileID string) (*Store, error) {
	s := &Store{cached: defaultSettings}

	path, err := utils.ConfigPath(fmt.Sprintf("settings-%s.json", profileID))
	if err != nil {
		return s, err
	}
	s.path = path

	data, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		// First run — persist defaults so the file exists for the user to inspect.
		return s, s.write()
	}
	if err != nil {
		return s, err
	}

	// cached already holds defaults, so unmarshalling the file over it means
	// newly-added fields are never left zero-valued.
	if err := json.Unmarshal(data, &s.cached); err != nil {
		return s, err
	}
	return s, nil
}

func (s *Store) write() error {
	data, err := json.MarshalIndent(s.cached, "", "  ")
	if err != nil {
		return err
	}
	return utils.AtomicWriteFile(s.path, data, 0o644)
}

// Get returns the current settings value. Safe for concurrent use.
func (s *Store) Get() Settings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cached
}

// MergeFrom replaces the cached settings with incoming (from a Supabase pull).
func (s *Store) MergeFrom(incoming Settings) {
	s.mu.Lock()
	s.cached = incoming
	err := s.write()
	s.mu.Unlock()
	if err != nil {
		log.Println("settings: merge write:", err)
	}
}

// SetProfile reloads settings from the given profile's data file.
func (s *Store) SetProfile(profileID string) error {
	path, err := utils.ConfigPath(fmt.Sprintf("settings-%s.json", profileID))
	if err != nil {
		return err
	}
	cur := defaultSettings
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err == nil {
		if err := json.Unmarshal(data, &cur); err != nil {
			return err
		}
	}
	s.mu.Lock()
	s.cached = cur
	s.path = path
	s.mu.Unlock()
	return nil
}

// SetupHandlers registers GET/PUT /api/settings on mux.
func (s *Store) SetupHandlers(mux *http.ServeMux) {
	mux.HandleFunc("/api/settings", utils.CorsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions {
			w.Header().Set("Access-Control-Allow-Methods", "GET, PUT, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			w.WriteHeader(http.StatusNoContent)
			return
		}

		// GET /api/settings — return current settings
		if r.Method == http.MethodGet {
			s.mu.RLock()
			current := s.cached
			s.mu.RUnlock()

			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(current); err != nil {
				log.Println("settings encode:", err)
			}
			return
		}

		// PUT /api/settings — merge & persist
		if r.Method == http.MethodPut {
			var incoming Settings
			if err := json.NewDecoder(r.Body).Decode(&incoming); err != nil {
				http.Error(w, "invalid body: "+err.Error(), http.StatusBadRequest)
				return
			}

			s.mu.Lock()
			s.cached = incoming
			err := s.write()
			s.mu.Unlock()

			if err != nil {
				log.Println("settings write:", err)
				http.Error(w, "could not save settings", http.StatusInternalServerError)
				return
			}

			w.Header().Set("Content-Type", "application/json")
			s.mu.RLock()
			_ = json.NewEncoder(w).Encode(s.cached)
			s.mu.RUnlock()
			return
		}

		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}))
}
