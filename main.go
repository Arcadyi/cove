package main

import (
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/coveninja/cove/internal/addons"
	"github.com/coveninja/cove/internal/clientsession"
	"github.com/coveninja/cove/internal/discover"
	"github.com/coveninja/cove/internal/library"
	"github.com/coveninja/cove/internal/nuvio"
	"github.com/coveninja/cove/internal/player"
	"github.com/coveninja/cove/internal/profiles"
	"github.com/coveninja/cove/internal/settings"
	supapkg "github.com/coveninja/cove/internal/supabase"
	"github.com/coveninja/cove/internal/tmdb"
	"github.com/coveninja/cove/internal/updater"
	"github.com/coveninja/cove/internal/utils"
	"github.com/joho/godotenv"
)

// Version is injected at build time via -ldflags "-X main.Version=vX.Y.Z".
// The zero value "dev" disables the auto-update check on development builds.
var Version = "dev"

// TmdbApiKey is injected at build time via -ldflags "-X main.TmdbApiKey=...".
// Release builds have it compiled in so no .env or runtime env var is needed.
// During local development, set TMDB_API_KEY in a .env file instead.
var TmdbApiKey = ""

// Supabase credentials are injected at build time via -ldflags for release
// builds. During local development, set them in a .env file instead. Only the
// project URL and the publishable anon key are ever compiled in — anything
// stronger (service key, JWT secret) must never ship inside a user binary.
var SupabaseURL = ""
var SupabaseAnonKey = ""

func main() {
	// Load .env if present — for local development only.
	if ex, err := os.Executable(); err == nil {
		// Clean up stale .new / .old sidecars left by a previous self-update.
		os.Remove(ex + ".new")
		os.Remove(ex + ".old")
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
	var nuvioMgr *nuvio.Manager
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
		if err := nuvioMgr.SetProfile(profileID); err != nil {
			log.Println("profile switch: reload nuvio repos:", err)
		}
	})
	if err != nil {
		log.Fatal("could not init profiles:", err)
	}
	activeID := profileStore.ActiveProfileID()

	tmdbClient := tmdb.New(apiKey)

	addonMgr = addons.New(activeID, func(tmdbID int) string {
		id, err := tmdbClient.GetTVIMDBId(tmdbID)
		if err != nil {
			return ""
		}
		return id
	})
	nuvioMgr = nuvio.New(activeID)

	st, err = settings.New(activeID)
	if err != nil {
		log.Println("could not load settings:", err)
	}
	lib, err = library.New(activeID)
	if err != nil {
		log.Println("could not load library:", err)
	}

	// The torrent client is core functionality — if it can't start, there's
	// nothing to stream, so a New failure is fatal.
	p, err := player.New(tmdbClient, addonMgr, nuvioMgr)
	if err != nil {
		log.Fatal("could not init torrent client:", err)
	}

	mux := http.DefaultServeMux

	addonMgr.SetupHandlers(mux)
	tmdbClient.SetupHandlers(mux, addonMgr)
	nuvioMgr.SetupHandlers(mux)
	p.SetupHandlers(mux)
	st.SetupHandlers(mux)
	lib.SetupHandlers(mux)
	profileStore.SetupHandlers(mux)
	updater.SetupHandlers(mux, Version)

	// Supabase auth + sync (no-op if SUPABASE_URL is not set).
	// Env vars take precedence; compiled-in ldflags values are the fallback for
	// release builds where no .env file is present.
	supaCfg := supapkg.ConfigFromEnv(SupabaseURL, SupabaseAnonKey)
	supaServer := supapkg.NewServer(supaCfg, profileStore, lib, st, addonMgr)
	supaServer.SetupHandlers(mux)

	disc := discover.New(tmdbClient, lib, st)
	disc.SetupHandlers(mux)

	clientsession.SetupHandlers(mux)

	go func() {
		ticker := time.NewTicker(30 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			p.CleanupTorrents()
		}
	}()

	mux.HandleFunc("/api/ping", utils.CorsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		err := json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
		if err != nil {
			log.Println(err)
			return
		}
	}))

	srv := &http.Server{
		Addr:              "127.0.0.1:6969",
		ReadHeaderTimeout: 10 * time.Second,
		// Don't set WriteTimeout — torrent streaming is long-lived
	}

	// Chromium may resolve "localhost" to ::1, so also serve on the IPv6
	// loopback when available. Best-effort: failure to bind is not fatal.
	if l6, err := net.Listen("tcp", "[::1]:6969"); err == nil {
		go func() {
			if err := srv.Serve(l6); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Println("ipv6 loopback listener:", err)
			}
		}()
	}

	log.Println("Server Running on: 127.0.0.1:6969")
	log.Fatal(srv.ListenAndServe())
}
