package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/Arcadyi/cove/internal/addons"
	"github.com/Arcadyi/cove/internal/player"
	"github.com/Arcadyi/cove/internal/tmdb"
	"github.com/joho/godotenv"
)

func corsMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		next(w, r)
	}
}

func main() {
	ex, envErr := os.Executable()
	if envErr == nil {
		envErr := godotenv.Load(filepath.Join(filepath.Dir(ex), ".env"))
		if envErr != nil {
			return
		}
	} else {
		envErr := godotenv.Load()
		if envErr != nil {
			return
		}
	}
	apiKey := os.Getenv("TMDB_API_KEY")
	_, err := addons.AddAddon("https://torrentio.strem.fun")
	if err != nil {
		log.Fatal(err)
		return
	}

	http.HandleFunc("/api/debug", corsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		// check addons
		configured := addons.GetAddons()

		// check IMDB lookup
		imdbID, err := tmdb.GetIMDBId(27205, apiKey)

		err = json.NewEncoder(w).Encode(map[string]interface{}{
			"addons":   configured,
			"imdb_id":  imdbID,
			"imdb_err": fmt.Sprintf("%v", err),
		})
		if err != nil {
			log.Println(err)
			return
		}
	}))

	// get all configured addons
	http.HandleFunc("/api/addons", corsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		err := json.NewEncoder(w).Encode(addons.GetAddons())
		if err != nil {
			log.Println(err)
			return
		}
	}))

	// add a new addon by URL
	http.HandleFunc("/api/addons/add", corsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		url := r.URL.Query().Get("url")
		addon, err := addons.AddAddon(url)
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

	// replace the old /api/streams endpoint
	http.HandleFunc("/api/streams", corsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		tmdbID := r.URL.Query().Get("id")
		id := 0
		_, err := fmt.Sscanf(tmdbID, "%d", &id)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}

		imdbID, err := tmdb.GetIMDBId(id, apiKey)
		if err != nil || imdbID == "" {
			http.Error(w, "could not get IMDB id", http.StatusInternalServerError)
			return
		}

		streams, err := addons.GetAllStreams("movie", imdbID)
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

	http.HandleFunc("/api/ping", corsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		err := json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
		if err != nil {
			log.Println(err)
			return
		}
	}))

	http.HandleFunc("/api/keywords", corsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query().Get("q")
		if query == "" {
			http.Error(w, "missing query", http.StatusBadRequest)
			return
		}
		keywords, err := tmdb.SuggestKeywords(query, apiKey)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(keywords); err != nil {
			log.Println(err)
		}
	}))

	http.HandleFunc("/api/quality/batch", corsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		idsParam := r.URL.Query().Get("ids")
		if idsParam == "" {
			http.Error(w, "missing ids", http.StatusBadRequest)
			return
		}

		idStrs := strings.Split(idsParam, ",")
		sem := make(chan struct{}, 5)

		type entry struct {
			ID      string `json:"id"`
			Quality string `json:"quality"`
		}

		w.Header().Set("Content-Type", "application/x-ndjson")
		w.Header().Set("X-Accel-Buffering", "no")
		flusher, canFlush := w.(http.Flusher)

		var mu sync.Mutex
		var wg sync.WaitGroup
		enc := json.NewEncoder(w)

		for _, s := range idStrs {
			id, err := strconv.Atoi(strings.TrimSpace(s))
			if err != nil {
				continue
			}
			wg.Add(1)
			go func(tmdbID int) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()

				imdbID, err := tmdb.GetIMDBId(tmdbID, apiKey)
				if err != nil || imdbID == "" {
					return
				}
				streams, err := addons.GetAllStreams("movie", imdbID)
				if err != nil || len(streams) == 0 {
					return
				}
				q := addons.GetMaxQuality(streams)
				if q == "" {
					return
				}
				mu.Lock()
				enc.Encode(entry{ID: strconv.Itoa(tmdbID), Quality: q})
				if canFlush {
					flusher.Flush()
				}
				mu.Unlock()
			}(id)
		}

		wg.Wait()
	}))

	http.HandleFunc("/api/search", corsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query().Get("q")
		if query == "" {
			http.Error(w, "missing query", http.StatusBadRequest)
			return
		}

		regular, err := tmdb.Search(query, apiKey)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		byKeyword, _ := tmdb.SearchByKeywords(query, apiKey) // non-fatal if it fails

		// Deduplicate: regular results take priority
		seen := make(map[string]bool)
		merged := make([]tmdb.Media, 0, len(regular)+len(byKeyword))
		for _, m := range regular {
			key := fmt.Sprintf("%d-%s", m.ID, m.MediaType)
			seen[key] = true
			merged = append(merged, m)
		}
		for _, m := range byKeyword {
			key := fmt.Sprintf("%d-%s", m.ID, m.MediaType)
			if !seen[key] {
				seen[key] = true
				merged = append(merged, m)
			}
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(merged); err != nil {
			log.Println(err)
		}
	}))

	if err := player.Init(); err != nil {
		log.Fatal("could not init torrent client:", err)
	}

	http.HandleFunc("/api/play", corsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		infoHash := r.URL.Query().Get("hash")
		streamURL := r.URL.Query().Get("url")

		if streamURL != "" {
			// direct HTTP stream — just redirect to it
			http.Redirect(w, r, streamURL, http.StatusTemporaryRedirect)
			return
		}

		if infoHash != "" {
			player.StreamTorrent(infoHash, w, r)
			return
		}

		http.Error(w, "missing hash or url", http.StatusBadRequest)
	}))

	http.HandleFunc("/api/trailer", corsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		tmdbID := r.URL.Query().Get("id")
		mediaType := r.URL.Query().Get("type")
		id := 0
		_, err := fmt.Sscanf(tmdbID, "%d", &id)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		trailer, err := tmdb.GetTrailer(id, mediaType, apiKey)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		err = json.NewEncoder(w).Encode(map[string]string{"url": trailer})
		if err != nil {
			log.Println(err)
		}
	}))

	http.HandleFunc("/api/images", corsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		tmdbID := r.URL.Query().Get("id")
		mediaType := r.URL.Query().Get("type")
		id := 0
		_, err := fmt.Sscanf(tmdbID, "%d", &id)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		images, err := tmdb.GetImages(id, mediaType, apiKey)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		err = json.NewEncoder(w).Encode(images)
		if err != nil {
			log.Println(err)
		}
	}))

	http.HandleFunc("/api/details", corsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		tmdbID := r.URL.Query().Get("id")
		mediaType := r.URL.Query().Get("type")
		id := 0
		_, err := fmt.Sscanf(tmdbID, "%d", &id)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		details, err := tmdb.GetDetails(id, mediaType, apiKey)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		err = json.NewEncoder(w).Encode(details)
		if err != nil {
			log.Println(err)
		}
	}))

	http.HandleFunc("/api/similar", corsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.Atoi(r.URL.Query().Get("id"))
		mediaType := r.URL.Query().Get("type")
		results, err := tmdb.GetSimilar(id, mediaType, apiKey)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		err = json.NewEncoder(w).Encode(results)
		if err != nil {
			log.Println(err)
			return
		}
	}))

	http.HandleFunc("/api/progress", corsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		hash := r.URL.Query().Get("hash")
		err := json.NewEncoder(w).Encode(player.GetProgress(hash))
		if err != nil {
			log.Println(err)
		}
	}))

	http.HandleFunc("/api/logos", corsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		tmdbID := r.URL.Query().Get("id")
		mediaType := r.URL.Query().Get("type")
		id := 0
		_, err := fmt.Sscanf(tmdbID, "%d", &id)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		logos, err := tmdb.GetLogos(id, mediaType, apiKey)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		err = json.NewEncoder(w).Encode(logos)
		if err != nil {
			log.Println(err)
		}
	}))

	http.HandleFunc("/api/imdb", corsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		tmdbID := r.URL.Query().Get("id")
		id := 0
		_, err := fmt.Sscanf(tmdbID, "%d", &id)
		if err != nil {
			log.Println(err)
			return
		}
		imdbID, err := tmdb.GetIMDBId(id, apiKey)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		err = json.NewEncoder(w).Encode(map[string]string{"imdb_id": imdbID})
		if err != nil {
			log.Println(err)
			return
		}
	}))

	log.Println("Server Running on: 6969")
	log.Fatal(http.ListenAndServe(":6969", nil))
}
