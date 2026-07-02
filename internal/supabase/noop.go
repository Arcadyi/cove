//go:build !supabase

// Package supabase provides a no-op stub when Cove is built without the
// proprietary Supabase integration. Auth endpoints return 503 so the frontend
// receives a proper error rather than a CORS failure. Build with -tags supabase
// to enable the real implementation.
package supabase

import (
	"net/http"

	"github.com/coveninja/cove/internal/addons"
	"github.com/coveninja/cove/internal/library"
	"github.com/coveninja/cove/internal/profiles"
	"github.com/coveninja/cove/internal/settings"
	"github.com/coveninja/cove/internal/utils"
)

// Config is a placeholder so main.go compiles without the real implementation.
type Config struct{}

// ConfigFromEnv always returns nil when built without -tags supabase.
func ConfigFromEnv(url, anonKey string) *Config { return nil }

// Server is a no-op auth server.
type Server struct{}

// NewServer returns a no-op server.
func NewServer(
	_ *Config,
	_ *profiles.Store,
	_ *library.Library,
	_ *settings.Store,
	_ *addons.Manager,
) *Server {
	return &Server{}
}

// SetupHandlers registers stub auth endpoints that return 503. This ensures
// CORS preflight requests succeed and the frontend gets a clear error message
// instead of an opaque CORS failure.
func (s *Server) SetupHandlers(mux *http.ServeMux) {
	stub := utils.CorsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Supabase integration not enabled (build with -tags supabase)", http.StatusServiceUnavailable)
	})
	for _, path := range []string{
		"/api/auth/register",
		"/api/auth/login",
		"/api/auth/otp",
		"/api/auth/verify-otp",
		"/api/auth/logout",
		"/api/auth/me",
		"/api/auth/sync",
		"/api/auth/confirm-register",
	} {
		mux.HandleFunc(path, stub)
	}
}
