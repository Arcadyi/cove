package addons

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"
)

// watchOptionsClient bounds the TMDB providers call so a hung upstream can't
// pin the request goroutine (http.DefaultClient has no timeout).
var watchOptionsClient = &http.Client{Timeout: 15 * time.Second}

// fetchWatchOptions queries TMDB's watch/providers endpoint for a title and
// returns the streaming availability for the US region. It uses os.Getenv
// directly because internal/tmdb imports this package, making a reverse import
// impossible.
func fetchWatchOptions(mediaType, tmdbID string) ([]WatchOption, error) {
	apiKey := os.Getenv("TMDB_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("TMDB_API_KEY not set")
	}

	url := fmt.Sprintf(
		"https://api.themoviedb.org/3/%s/%s/watch/providers?api_key=%s",
		mediaType, tmdbID, apiKey,
	)

	resp, err := watchOptionsClient.Get(url) //nolint:noctx
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result struct {
		Results map[string]struct {
			Flatrate []struct {
				ProviderID   int    `json:"provider_id"`
				ProviderName string `json:"provider_name"`
				LogoPath     string `json:"logo_path"`
			} `json:"flatrate"`
			Rent []struct {
				ProviderID   int    `json:"provider_id"`
				ProviderName string `json:"provider_name"`
				LogoPath     string `json:"logo_path"`
			} `json:"rent"`
			Link string `json:"link"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	us, ok := result.Results["US"]
	if !ok {
		return []WatchOption{}, nil
	}

	var options []WatchOption
	for _, p := range us.Flatrate {
		options = append(options, WatchOption{
			ProviderID:   p.ProviderID,
			ProviderName: p.ProviderName,
			LogoPath:     p.LogoPath,
			Type:         "flatrate",
			Link:         us.Link,
		})
	}
	for _, p := range us.Rent {
		options = append(options, WatchOption{
			ProviderID:   p.ProviderID,
			ProviderName: p.ProviderName,
			LogoPath:     p.LogoPath,
			Type:         "rent",
			Link:         us.Link,
		})
	}
	if options == nil {
		options = []WatchOption{}
	}
	return options, nil
}
