package addons

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/coveninja/cove/internal/utils"
)

type AddonKind string
type AddonSource string

const (
	KindProvider   AddonKind = "provider"
	KindSubtitle   AddonKind = "subtitle"
	KindTimestamps AddonKind = "timestamps"

	SourceOfficial AddonSource = "official"
	SourceStremio  AddonSource = "stremio"
)

type ManifestResource struct {
	Name       string   `json:"name"`
	Types      []string `json:"types"`
	IDPrefixes []string `json:"idPrefixes"`
}

type Manifest struct {
	ID          string             `json:"id"`
	Name        string             `json:"name"`
	Description string             `json:"description"`
	Version     string             `json:"version"`
	Resources   []ManifestResource `json:"resources"`
	Types       []string           `json:"types"`
}

type AddonEntry struct {
	ID       string      `json:"id"`
	URL      string      `json:"url,omitempty"`
	Manifest Manifest    `json:"manifest"`
	Kind     AddonKind   `json:"kind"`
	Source   AddonSource `json:"source"`
	Enabled  bool        `json:"enabled"`
}

type Subtitle struct {
	ID   string `json:"id"`
	URL  string `json:"url"`
	Lang string `json:"lang"`
}

type Stream struct {
	Name      string     `json:"name"`
	Title     string     `json:"title"`
	URL       string     `json:"url"`
	InfoHash  string     `json:"infoHash"`
	AddonName string     `json:"addonName"`
	Subtitles []Subtitle `json:"subtitles,omitempty"`
}

// WatchOption represents a streaming service availability entry from JustWatch.
type WatchOption struct {
	ProviderID   int    `json:"providerId"`
	ProviderName string `json:"providerName"`
	LogoPath     string `json:"logoPath"`
	Type         string `json:"type"` // "flatrate", "rent", or "buy"
	Link         string `json:"link"` // JustWatch/provider page to open in browser
}

func (r *ManifestResource) UnmarshalJSON(data []byte) error {
	// Try string first
	var name string
	if err := json.Unmarshal(data, &name); err == nil {
		r.Name = name
		return nil
	}
	// Fall back to object form
	type alias ManifestResource
	var obj alias
	if err := json.Unmarshal(data, &obj); err != nil {
		return err
	}
	*r = ManifestResource(obj)
	return nil
}

func (m *Manager) addonRequest(url string) (*http.Response, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "application/json")
	return m.client.Do(req)
}

// normalizeAddonURL strips a trailing /manifest.json so users can paste either
// the base URL or the full manifest URL and get the same result.
func normalizeAddonURL(raw string) string {
	u := strings.TrimRight(raw, "/")
	u = strings.TrimSuffix(u, "/manifest.json")
	return strings.TrimRight(u, "/")
}

func (m *Manager) FetchManifest(addonURL string) (Manifest, error) {
	res, err := m.addonRequest(addonURL + "/manifest.json")
	if err != nil {
		return Manifest{}, err
	}
	defer func(Body io.ReadCloser) {
		if err := Body.Close(); err != nil {
			log.Println(err)
		}
	}(res.Body)

	if res.StatusCode != http.StatusOK {
		return Manifest{}, fmt.Errorf("addon returned HTTP %d", res.StatusCode)
	}

	var manifest Manifest
	if err := json.NewDecoder(res.Body).Decode(&manifest); err != nil {
		return Manifest{}, err
	}
	if manifest.ID == "" {
		return Manifest{}, fmt.Errorf("addon manifest has no id — check the URL")
	}
	return manifest, nil
}

func (m *Manager) FetchStreams(addonURL string, mediaType string, imdbID string) ([]Stream, error) {
	url := fmt.Sprintf("%s/stream/%s/%s.json", addonURL, mediaType, imdbID)

	res, err := m.addonRequest(url)
	if err != nil {
		return nil, err
	}
	defer func(Body io.ReadCloser) {
		if err := Body.Close(); err != nil {
			log.Println(err)
		}
	}(res.Body)

	var data struct {
		Streams []Stream `json:"streams"`
	}
	if err := json.NewDecoder(res.Body).Decode(&data); err != nil {
		return nil, err
	}
	return data.Streams, nil
}

func (m *Manager) FetchSubtitles(addonURL string, mediaType string, id string) ([]Subtitle, error) {
	url := fmt.Sprintf("%s/subtitles/%s/%s.json", addonURL, mediaType, id)
	res, err := m.addonRequest(url)
	if err != nil {
		return nil, err
	}
	defer func(Body io.ReadCloser) {
		if err := Body.Close(); err != nil {
			log.Println(err)
		}
	}(res.Body)

	var data struct {
		Subtitles []Subtitle `json:"subtitles"`
	}
	if err := json.NewDecoder(res.Body).Decode(&data); err != nil {
		return nil, err
	}
	return data.Subtitles, nil
}

func (m *Manager) SetupHandlers(mux *http.ServeMux, imdbLookup func(tmdbID int) string) {
	m.imdbLookup = imdbLookup
	// GET  /api/addons          — list all addons
	// POST /api/addons          — add stremio addon (body: {"url":"..."})
	// PATCH /api/addons?id=X   — toggle enabled (body: {"enabled":true})
	// DELETE /api/addons?id=X  — remove stremio addon
	mux.HandleFunc("/api/addons", utils.CorsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(m.GetEntries()); err != nil {
				log.Println("addons list:", err)
			}

		case http.MethodPost:
			var body struct {
				URL string `json:"url"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.URL == "" {
				http.Error(w, `body must be {"url":"..."}`, http.StatusBadRequest)
				return
			}
			entry, err := m.AddStremioAddon(body.URL)
			if err != nil {
				http.Error(w, "could not add addon: "+err.Error(), http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(entry); err != nil {
				log.Println("addons add:", err)
			}

		case http.MethodPatch:
			id := r.URL.Query().Get("id")
			addonURL := r.URL.Query().Get("url")
			if id == "" && addonURL == "" {
				http.Error(w, "missing ?id= or ?url=", http.StatusBadRequest)
				return
			}
			var body struct {
				Enabled bool `json:"enabled"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, "invalid body", http.StatusBadRequest)
				return
			}
			if err := m.SetEnabled(id, addonURL, body.Enabled); err != nil {
				http.Error(w, err.Error(), http.StatusNotFound)
				return
			}
			w.WriteHeader(http.StatusNoContent)

		case http.MethodDelete:
			id := r.URL.Query().Get("id")
			addonURL := r.URL.Query().Get("url")
			if id == "" && addonURL == "" {
				http.Error(w, "missing ?id= or ?url=", http.StatusBadRequest)
				return
			}
			if err := m.RemoveAddon(id, addonURL); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			w.WriteHeader(http.StatusNoContent)

		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}))

	// GET /api/timestamps?id=<tmdbID>&season=1&episode=2
	mux.HandleFunc("/api/timestamps", utils.CorsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		tmdbIDStr := r.URL.Query().Get("id")
		if tmdbIDStr == "" {
			http.Error(w, "missing ?id=", http.StatusBadRequest)
			return
		}
		var tmdbID int
		if _, err := fmt.Sscanf(tmdbIDStr, "%d", &tmdbID); err != nil {
			http.Error(w, "invalid ?id=", http.StatusBadRequest)
			return
		}

		var season, episode *int
		if s := r.URL.Query().Get("season"); s != "" {
			var sv int
			if _, err := fmt.Sscanf(s, "%d", &sv); err == nil {
				season = &sv
			}
		}
		if e := r.URL.Query().Get("episode"); e != "" {
			var ev int
			if _, err := fmt.Sscanf(e, "%d", &ev); err == nil {
				episode = &ev
			}
		}

		data, err := m.GetTimestamps(tmdbID, season, episode)
		if err != nil {
			log.Println("timestamps:", err)
			data = &TimestampData{}
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(data); err != nil {
			log.Println("timestamps encode:", err)
		}
	}))

	// GET /api/watch-options?id=<tmdbID>&type=movie|tv
	mux.HandleFunc("/api/watch-options", utils.CorsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		tmdbID := r.URL.Query().Get("id")
		mediaType := r.URL.Query().Get("type")
		if tmdbID == "" || mediaType == "" {
			http.Error(w, "missing ?id= or ?type=", http.StatusBadRequest)
			return
		}
		options, err := m.GetWatchOptions(mediaType, tmdbID)
		if err != nil {
			log.Println("watch-options:", err)
			options = []WatchOption{}
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(options); err != nil {
			log.Println("watch-options encode:", err)
		}
	}))
}
