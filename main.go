package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/Arcadyi/cove/internal/addons"
	"github.com/Arcadyi/cove/internal/discover"
	"github.com/Arcadyi/cove/internal/library"
	"github.com/Arcadyi/cove/internal/player"
	"github.com/Arcadyi/cove/internal/profiles"
	"github.com/Arcadyi/cove/internal/settings"
	supapkg "github.com/Arcadyi/cove/internal/supabase"
	"github.com/Arcadyi/cove/internal/tmdb"
	"github.com/Arcadyi/cove/internal/updater"
	"github.com/Arcadyi/cove/internal/utils"
	"github.com/joho/godotenv"
)

// Version is injected at build time via -ldflags "-X main.Version=vX.Y.Z".
// The zero value "dev" disables the auto-update check on development builds.
var Version = "dev"

// TmdbApiKey is injected at build time via -ldflags "-X main.TmdbApiKey=...".
// Release builds have it compiled in so no .env or runtime env var is needed.
// During local development, set TMDB_API_KEY in a .env file instead.
var TmdbApiKey = ""

func main() {
	// Load .env if present — for local development only.
	// Release builds have TmdbApiKey compiled in via ldflags.
	if ex, err := os.Executable(); err == nil {
		if err := godotenv.Load(filepath.Join(filepath.Dir(ex), ".env")); err != nil {
			log.Println("no .env next to binary; relying on the environment:", err)
		}
	} else if err := godotenv.Load(); err != nil {
		log.Println("no .env in working dir; relying on the environment:", err)
	}

	// Env var overrides the compiled-in key (useful for dev/testing).
	apiKey := os.Getenv("TMDB_API_KEY")
	if apiKey == "" {
		apiKey = TmdbApiKey
	}
	if apiKey == "" {
		log.Println("warning: TMDB_API_KEY is not set — TMDB metadata requests will fail")
	}

	// Profiles must be initialised first — all other packages are profile-scoped.
	var addonMgr *addons.Manager
	var st *settings.Store
	var lib *library.Library

	profileStore, err := profiles.New(func(profileID string) {
		// Reload all data stores when the active profile switches.
		if err := lib.SetProfile(profileID); err != nil {
			log.Println("profile switch: reload library:", err)
		}
		if err := st.SetProfile(profileID); err != nil {
			log.Println("profile switch: reload settings:", err)
		}
		if err := addonMgr.SetProfile(profileID); err != nil {
			log.Println("profile switch: reload addons:", err)
		}
	})
	if err != nil {
		log.Fatal("could not init profiles:", err)
	}
	activeID := profileStore.ActiveProfileID()

	addonMgr = addons.New(activeID)

	st, err = settings.New(activeID)
	if err != nil {
		log.Println("could not load settings:", err)
	}
	lib, err = library.New(activeID)
	if err != nil {
		log.Println("could not load library:", err)
	}

	tmdbClient := tmdb.New(apiKey)

	// The torrent client is core functionality — if it can't start, there's
	// nothing to stream, so a New failure is fatal.
	p, err := player.New(tmdbClient, addonMgr)
	if err != nil {
		log.Fatal("could not init torrent client:", err)
	}

	addonMgr.SetupHandlers(func(tmdbID int) string {
		id, err := tmdbClient.GetTVIMDBId(tmdbID)
		if err != nil {
			return ""
		}
		return id
	})
	tmdbClient.SetupHandlers(addonMgr)
	p.SetupHandlers()
	st.SetupHandlers()
	lib.SetupHandlers()
	profileStore.SetupHandlers()
	updater.SetupHandlers(Version)

	// Supabase auth + sync (no-op if SUPABASE_URL is not set).
	supaCfg := supapkg.ConfigFromEnv()
	supaServer := supapkg.NewServer(supaCfg, profileStore, lib, st, addonMgr)
	supaServer.SetupHandlers()

	disc := discover.New(tmdbClient, lib)
	disc.SetupHandlers()

	go func() {
		ticker := time.NewTicker(30 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			p.CleanupTorrents()
		}
	}()

	http.HandleFunc("/api/ping", utils.CorsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		err := json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
		if err != nil {
			log.Println(err)
			return
		}
	}))

	srv := &http.Server{
		Addr:              ":6969",
		ReadHeaderTimeout: 10 * time.Second,
		// Don't set WriteTimeout — torrent streaming is long-lived
	}

	log.Println("Server Running on: 6969")
	log.Fatal(srv.ListenAndServe())
}
