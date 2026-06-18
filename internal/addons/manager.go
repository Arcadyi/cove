package addons

import (
	"net/http"
	"sync"
	"time"
)

// Manager owns the configured addon registry and the HTTP client used to talk
// to addons. This state used to live in package globals; holding it on a struct
// lets callers construct independent managers (and tests inject a custom client
// or pre-seed the registry). Fields are unexported, so tygo emits nothing for
// Manager — only the data types (Manifest, Stream, Subtitle, Addon) cross into
// the generated TS.
//
// in memory for now, will move to SQLite later
type Manager struct {
	mu     sync.RWMutex
	addons []Addon
	client *http.Client
}

// New returns a Manager with a default HTTP client. The timeout is generous
// because torrentio's stream resolution can legitimately take 10–20s as it
// scrapes trackers; its only job is to stop a dead upstream from holding a
// request goroutine open indefinitely.
func New() *Manager {
	return &Manager{
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

func (m *Manager) AddAddon(url string) (Addon, error) {
	manifest, err := m.FetchManifest(url)
	if err != nil {
		return Addon{}, err
	}
	addon := Addon{URL: url, Manifest: manifest}
	m.mu.Lock()
	m.addons = append(m.addons, addon)
	m.mu.Unlock()
	return addon, nil
}

func (m *Manager) GetAddons() []Addon {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.addons
}

func (m *Manager) GetAllStreams(mediaType string, imdbID string) ([]Stream, error) {
	m.mu.RLock()
	addons := m.addons
	m.mu.RUnlock()

	var allStreams []Stream
	for _, addon := range addons {
		streams, err := m.FetchStreams(addon.URL, mediaType, imdbID)
		if err != nil {
			continue // don't fail if one addon is down
		}
		// tag each stream with which addon it came from
		for i := range streams {
			streams[i].AddonName = addon.Manifest.Name
		}
		allStreams = append(allStreams, streams...)
	}
	return allStreams, nil
}
