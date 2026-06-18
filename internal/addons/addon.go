package addons

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"

	"github.com/Arcadyi/cove/internal/utils"
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

type Addon struct {
	URL      string
	Manifest Manifest
}

func (r *ManifestResource) UnmarshalJSON(data []byte) error {
	// Try string first
	var name string
	if err := json.Unmarshal(data, &name); err == nil {
		r.Name = name
		return nil
	}
	// Fall back to object form
	type Alias ManifestResource
	var obj Alias
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

func (m *Manager) FetchManifest(addonURL string) (Manifest, error) {
	res, err := m.addonRequest(addonURL + "/manifest.json")
	if err != nil {
		return Manifest{}, err
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			log.Println(err)
		}
	}(res.Body)

	var manifest Manifest
	err = json.NewDecoder(res.Body).Decode(&manifest)
	if err != nil {
		return Manifest{}, err
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
		err := Body.Close()
		if err != nil {
			log.Println(err)
		}
	}(res.Body)

	var data struct {
		Streams []Stream `json:"streams"`
	}
	err = json.NewDecoder(res.Body).Decode(&data)
	if err != nil {
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
		err := Body.Close()
		if err != nil {
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

func (m *Manager) SetupHandlers() {
	http.HandleFunc("/api/addons", utils.CorsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		err := json.NewEncoder(w).Encode(m.GetAddons())
		if err != nil {
			log.Println(err)
			return
		}
	}))

	http.HandleFunc("/api/addons/add", utils.CorsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		url := r.URL.Query().Get("url")
		addon, err := m.AddAddon(url)
		if err != nil {
			http.Error(w, "could not fetch addon manifest: "+err.Error(), http.StatusBadRequest)
			return
		}
		err = json.NewEncoder(w).Encode(addon)
		if err != nil {
			log.Println(err)
			return
		}
	}))
}
