//go:build !discover

// Package discover provides personalized media recommendations.
// This stub is compiled when Cove is built without the proprietary
// recommendation engine. Personalized rows are empty; the genre list
// still works via TMDB. Build with -tags discover to enable.
package discover

import (
	"encoding/json"
	"net/http"

	"github.com/coveninja/cove/internal/library"
	"github.com/coveninja/cove/internal/tmdb"
	"github.com/coveninja/cove/internal/utils"
)

type TasteProvider interface {
	TasteSignals() []library.TasteSignal
	Generation() uint64
}

type Service struct{ tmdb *tmdb.Client }

func New(t *tmdb.Client, lib TasteProvider) *Service { return &Service{tmdb: t} }

func (s *Service) SetupHandlers(mux *http.ServeMux) {
	empty := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("[]"))
	}
	mux.HandleFunc("/api/discover", utils.CorsMiddleware(empty))
	mux.HandleFunc("/api/discover/genres", utils.CorsMiddleware(empty))
	mux.HandleFunc("/api/discover/keywords", utils.CorsMiddleware(empty))
	mux.HandleFunc("/api/discover/genre", utils.CorsMiddleware(empty))
	mux.HandleFunc("/api/discover/keyword", utils.CorsMiddleware(empty))
	mux.HandleFunc("/api/discover/insights", utils.CorsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(struct{}{})
	}))
	mux.HandleFunc("/api/genres", utils.CorsMiddleware(s.handleGenreList))
}

func (s *Service) handleGenreList(w http.ResponseWriter, r *http.Request) {
	mt := r.URL.Query().Get("type")
	if mt != "movie" && mt != "tv" {
		http.Error(w, "type must be movie or tv", http.StatusBadRequest)
		return
	}
	genres, err := s.tmdb.GenreList(mt)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(genres)
}
