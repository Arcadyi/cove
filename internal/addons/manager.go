package addons

import (
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/Arcadyi/cove/internal/utils"
)

// Manager owns the configured addon registry and the HTTP client used to talk
// to addons. Fields are unexported, so tygo emits nothing for Manager — only
// the data types (AddonEntry, Stream, Subtitle, WatchOption, etc.) cross into
// the generated TS.
type Manager struct {
	mu              sync.RWMutex
	stremioAddons   []AddonEntry
	officialEnabled map[string]bool // persisted enabled-state overrides for official addons
	client          *http.Client
	storePath       string
	imdbLookup      func(tmdbID int) string // returns IMDB ID for a TV show, or "" on failure
}

// officialAddons lists the built-in addons that ship with Cove. Their definitions
// are reconstructed from code on each startup; only enabled-state is persisted.
var officialAddons = []AddonEntry{
	{
		ID:     "cove.justwatch",
		Kind:   KindProvider,
		Source: SourceOfficial,
		Manifest: Manifest{
			ID:          "cove.justwatch",
			Name:        "JustWatch",
			Description: "Streaming availability via TMDB/JustWatch",
		},
		Enabled: true,
	},
	{
		ID:     "cove.introdb",
		Kind:   KindTimestamps,
		Source: SourceOfficial,
		Manifest: Manifest{
			ID:          "cove.introdb",
			Name:        "IntroSkip",
			Description: "Intro, recap, credits, and preview timestamps. Sources: theintrodb.org (TMDB-based, all segment types) and introdb.app (IMDB-based, higher coverage for TV shows). Results are merged, with theintrodb.org taking priority.",
		},
		Enabled: true,
	},
}

// New returns a Manager loaded from the profile-scoped store (or empty on first run).
func New(profileID string) *Manager {
	m := &Manager{
		client:          &http.Client{Timeout: 30 * time.Second},
		officialEnabled: make(map[string]bool),
	}

	path, err := utils.ConfigPath(fmt.Sprintf("addons-%s.json", profileID))
	if err != nil {
		log.Println("addons: could not determine config path:", err)
		return m
	}
	m.storePath = path

	store, err := loadStore(path)
	if err != nil {
		log.Println("addons: could not load store:", err)
		return m
	}
	m.stremioAddons = store.StremioAddons
	if store.OfficialEnabled != nil {
		m.officialEnabled = store.OfficialEnabled
	}
	return m
}

// SetProfile reloads addon configuration from the given profile's data file.
func (m *Manager) SetProfile(profileID string) error {
	path, err := utils.ConfigPath(fmt.Sprintf("addons-%s.json", profileID))
	if err != nil {
		return err
	}
	store, err := loadStore(path)
	if err != nil {
		return err
	}
	m.mu.Lock()
	m.storePath = path
	m.stremioAddons = store.StremioAddons
	if store.OfficialEnabled != nil {
		m.officialEnabled = store.OfficialEnabled
	} else {
		m.officialEnabled = make(map[string]bool)
	}
	m.mu.Unlock()
	return nil
}

// GetEntries returns all addons (official + stremio) with current enabled state.
func (m *Manager) GetEntries() []AddonEntry {
	m.mu.RLock()
	defer m.mu.RUnlock()

	entries := make([]AddonEntry, 0, len(officialAddons)+len(m.stremioAddons))
	for _, a := range officialAddons {
		if enabled, ok := m.officialEnabled[a.ID]; ok {
			a.Enabled = enabled
		}
		entries = append(entries, a)
	}
	entries = append(entries, m.stremioAddons...)
	return entries
}

// AddStremioAddon fetches the manifest at url, classifies it as provider or
// subtitle addon, persists it, and returns the new entry. If an addon with the
// same ID already exists it is updated in place. The URL is normalized so users
// can paste either the base URL or the full manifest URL.
func (m *Manager) AddStremioAddon(url string) (AddonEntry, error) {
	url = normalizeAddonURL(url)
	manifest, err := m.FetchManifest(url)
	if err != nil {
		return AddonEntry{}, err
	}

	entry := AddonEntry{
		ID:       manifest.ID,
		URL:      url,
		Manifest: manifest,
		Kind:     detectKind(manifest),
		Source:   SourceStremio,
		Enabled:  true,
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	for i, a := range m.stremioAddons {
		if a.ID == entry.ID {
			m.stremioAddons[i] = entry
			return entry, m.saveL()
		}
	}
	m.stremioAddons = append(m.stremioAddons, entry)
	return entry, m.saveL()
}

// RemoveAddon removes a user-added (stremio) addon by ID or URL. Matching by
// URL lets callers clean up entries that were stored with an empty ID due to a
// bad manifest fetch. Returns an error for official addons or if nothing matches.
func (m *Manager) RemoveAddon(id, addonURL string) error {
	for _, a := range officialAddons {
		if a.ID == id {
			return fmt.Errorf("cannot remove built-in addon %q", id)
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, a := range m.stremioAddons {
		if (id != "" && a.ID == id) || (addonURL != "" && a.URL == addonURL) {
			m.stremioAddons = append(m.stremioAddons[:i], m.stremioAddons[i+1:]...)
			return m.saveL()
		}
	}
	return fmt.Errorf("addon not found")
}

// SetEnabled toggles an addon on or off. Matches by id or url (url fallback
// handles entries that were stored with an empty id).
func (m *Manager) SetEnabled(id, addonURL string, enabled bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, a := range officialAddons {
		if a.ID == id {
			m.officialEnabled[id] = enabled
			return m.saveL()
		}
	}
	for i, a := range m.stremioAddons {
		if (id != "" && a.ID == id) || (addonURL != "" && a.URL == addonURL) {
			m.stremioAddons[i].Enabled = enabled
			return m.saveL()
		}
	}
	return fmt.Errorf("addon not found")
}

// GetAllStreams fans out to all enabled stremio provider addons.
func (m *Manager) GetAllStreams(mediaType string, imdbID string) ([]Stream, error) {
	m.mu.RLock()
	addons := make([]AddonEntry, len(m.stremioAddons))
	copy(addons, m.stremioAddons)
	m.mu.RUnlock()

	var allStreams []Stream
	for _, addon := range addons {
		if !addon.Enabled || addon.Kind != KindProvider {
			continue
		}
		streams, err := m.FetchStreams(addon.URL, mediaType, imdbID)
		if err != nil {
			continue
		}
		for i := range streams {
			streams[i].AddonName = addon.Manifest.Name
		}
		allStreams = append(allStreams, streams...)
	}
	return allStreams, nil
}

// GetAllSubtitles fans out to all enabled stremio subtitle addons.
func (m *Manager) GetAllSubtitles(mediaType string, stremioID string) []Subtitle {
	m.mu.RLock()
	addons := make([]AddonEntry, len(m.stremioAddons))
	copy(addons, m.stremioAddons)
	m.mu.RUnlock()

	var all []Subtitle
	for _, addon := range addons {
		if !addon.Enabled || addon.Kind != KindSubtitle {
			continue
		}
		subs, err := m.FetchSubtitles(addon.URL, mediaType, stremioID)
		if err != nil {
			continue
		}
		all = append(all, subs...)
	}
	return all
}

// GetTimestamps returns merged intro/recap/credits/preview timestamps.
// It queries theintrodb.org first (TMDB IDs, all segment types), then
// supplements any missing segments from introdb.app (IMDB IDs, TV-only).
func (m *Manager) GetTimestamps(tmdbID int, season, episode *int) (*TimestampData, error) {
	m.mu.RLock()
	enabled := m.isOfficialEnabledL("cove.introdb")
	lookup := m.imdbLookup
	m.mu.RUnlock()

	if !enabled {
		return &TimestampData{}, nil
	}

	base, err := fetchTimestamps(m.client, tmdbID, season, episode)
	if err != nil {
		base = &TimestampData{}
	}

	// Supplement with introdb.app for TV episodes when an IMDB lookup is wired up.
	if lookup != nil && season != nil && episode != nil {
		if imdbID := lookup(tmdbID); imdbID != "" {
			if fill, err2 := fetchIntroDBApp(m.client, imdbID, *season, *episode); err2 == nil {
				base = mergeTimestamps(base, fill)
			}
		}
	}
	return base, nil
}

// GetWatchOptions returns streaming availability from JustWatch (via TMDB) if
// the built-in JustWatch addon is enabled.
func (m *Manager) GetWatchOptions(mediaType string, tmdbID string) ([]WatchOption, error) {
	m.mu.RLock()
	enabled := m.isOfficialEnabledL("cove.justwatch")
	m.mu.RUnlock()

	if !enabled {
		return []WatchOption{}, nil
	}
	return fetchWatchOptions(mediaType, tmdbID)
}

// isOfficialEnabledL returns whether an official addon is enabled.
// Defaults to true (official addons are on by default). Must be called with m.mu held.
func (m *Manager) isOfficialEnabledL(id string) bool {
	if enabled, ok := m.officialEnabled[id]; ok {
		return enabled
	}
	return true
}

// saveL persists the current state. Must be called with m.mu write-locked.
func (m *Manager) saveL() error {
	if m.storePath == "" {
		return nil
	}
	return saveStore(m.storePath, addonStore{
		StremioAddons:   m.stremioAddons,
		OfficialEnabled: m.officialEnabled,
	})
}

// detectKind classifies an addon as a stream provider or subtitle provider based
// on its manifest resources.
func detectKind(manifest Manifest) AddonKind {
	for _, r := range manifest.Resources {
		if r.Name == "stream" {
			return KindProvider
		}
	}
	for _, r := range manifest.Resources {
		if r.Name == "subtitles" {
			return KindSubtitle
		}
	}
	return KindProvider // safe default
}
