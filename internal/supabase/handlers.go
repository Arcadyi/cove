//go:build supabase

// Copyright (c) 2025 coveninja. All Rights Reserved.
// This file is proprietary and is not covered by the AGPL-3.0 license
// that applies to the rest of the Cove repository.

package supabase

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"sync/atomic"

	"github.com/coveninja/cove/internal/addons"
	"github.com/coveninja/cove/internal/library"
	"github.com/coveninja/cove/internal/profiles"
	"github.com/coveninja/cove/internal/settings"
	"github.com/coveninja/cove/internal/utils"
)

// Server wires together all the auth + sync HTTP handlers.
type Server struct {
	cfg          *Config
	profileStore *profiles.Store
	lib          *library.Library
	st           *settings.Store
	addonMgr     *addons.Manager

	// pushMu serializes background pushes; pushQueued coalesces bursts so
	// rapid sync calls queue at most one extra push run instead of spawning
	// an unbounded pile of overlapping goroutines.
	pushMu     sync.Mutex
	pushQueued atomic.Bool
}

// pushAsync uploads the profile's library/settings/addons in the background.
// A push already queued behind a running one will observe this call's data
// too, so additional requests are dropped rather than stacked.
func (s *Server) pushAsync(userJWT, profileID, context string) {
	if !s.pushQueued.CompareAndSwap(false, true) {
		return
	}
	go func() {
		s.pushMu.Lock()
		defer s.pushMu.Unlock()
		s.pushQueued.Store(false)
		if err := s.cfg.PushLibrary(userJWT, profileID, s.lib); err != nil {
			log.Println(context+": push library:", err)
		}
		if err := s.cfg.PushSettings(userJWT, profileID, s.st); err != nil {
			log.Println(context+": push settings:", err)
		}
		if err := s.cfg.PushAddons(userJWT, profileID, s.addonMgr); err != nil {
			log.Println(context+": push addons:", err)
		}
	}()
}

// NewServer creates the auth handler set. cfg may be nil (Supabase not configured),
// in which case all auth endpoints return 503.
func NewServer(
	cfg *Config,
	ps *profiles.Store,
	lib *library.Library,
	st *settings.Store,
	mgr *addons.Manager,
) *Server {
	return &Server{cfg: cfg, profileStore: ps, lib: lib, st: st, addonMgr: mgr}
}

// SetupHandlers registers all /api/auth/* endpoints on mux.
func (s *Server) SetupHandlers(mux *http.ServeMux) {
	mux.HandleFunc("/api/auth/register", utils.CorsMiddleware(s.handleRegister))
	mux.HandleFunc("/api/auth/register/confirm", utils.CorsMiddleware(s.handleConfirmRegistration))
	mux.HandleFunc("/api/auth/login", utils.CorsMiddleware(s.handleLogin))
	mux.HandleFunc("/api/auth/otp", utils.CorsMiddleware(s.handleOTP))
	mux.HandleFunc("/api/auth/verify-otp", utils.CorsMiddleware(s.handleVerifyOTP))
	mux.HandleFunc("/api/auth/logout", utils.CorsMiddleware(s.handleLogout))
	mux.HandleFunc("/api/auth/me", utils.CorsMiddleware(s.handleMe))
	mux.HandleFunc("/api/auth/sync", utils.CorsMiddleware(s.handleSync))
}

func (s *Server) notConfigured(w http.ResponseWriter) {
	http.Error(w, "Supabase not configured", http.StatusServiceUnavailable)
}

func jsonOK(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// POST /api/auth/register  {email, password, profile_name}
// Creates a Supabase account, links the active local profile, pushes local data.
func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.cfg == nil {
		s.notConfigured(w)
		return
	}

	var body struct {
		Email       string `json:"email"`
		Password    string `json:"password"`
		ProfileName string `json:"profile_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Email == "" || body.Password == "" {
		http.Error(w, "email and password required", http.StatusBadRequest)
		return
	}

	userID, accessToken, err := s.cfg.SignUp(body.Email, body.Password)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if accessToken == "" {
		// Email confirmation is required. Tell the frontend to show OTP input.
		jsonOK(w, map[string]any{"confirmation_required": true})
		return
	}

	// Immediate session (email confirmation disabled in Supabase project settings).
	s.finishRegistration(w, userID, accessToken, "", body.ProfileName)
}

// POST /api/auth/register/confirm  {email, token, profile_name}
// Verifies the OTP from the signup confirmation email and creates the session.
func (s *Server) handleConfirmRegistration(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.cfg == nil {
		s.notConfigured(w)
		return
	}

	var body struct {
		Email       string `json:"email"`
		Token       string `json:"token"`
		Password    string `json:"password"`
		ProfileName string `json:"profile_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Email == "" || body.Token == "" || body.Password == "" {
		http.Error(w, "email, token, and password required", http.StatusBadRequest)
		return
	}

	// Confirm the email address via OTP.
	if err := s.cfg.VerifySignup(body.Email, body.Token); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Supabase may not return a session from /verify, so sign in explicitly.
	userID, accessToken, refreshToken, err := s.cfg.SignIn(body.Email, body.Password)
	if err != nil {
		http.Error(w, "email confirmed but sign-in failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	s.finishRegistration(w, userID, accessToken, refreshToken, body.ProfileName)
}

// finishRegistration links the Supabase user to the active local profile,
// creates the remote profile row, kicks off an async data push, and writes
// the session response. Shared by handleRegister and handleConfirmRegistration.
func (s *Server) finishRegistration(w http.ResponseWriter, userID, accessToken, refreshToken, profileName string) {
	activeProfile := s.profileStore.ActiveProfile()
	if profileName == "" {
		profileName = activeProfile.Name
	}

	if err := s.profileStore.LinkSupabase(activeProfile.ID, userID); err != nil {
		log.Println("supabase register: link profile:", err)
	}
	if err := s.cfg.EnsureProfile(accessToken, activeProfile.ID, userID, profileName, activeProfile.IsPrimary); err != nil {
		http.Error(w, "could not create remote profile: "+err.Error(), http.StatusInternalServerError)
		return
	}

	s.pushAsync(accessToken, activeProfile.ID, "supabase register")

	jsonOK(w, map[string]any{
		"access_token":  accessToken,
		"refresh_token": refreshToken,
		"profile":       s.profileStore.ActiveProfile(),
	})
}

// POST /api/auth/login  {email, password}
// Signs in, pulls remote data, merges into local store.
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.cfg == nil {
		s.notConfigured(w)
		return
	}

	var body struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Email == "" {
		http.Error(w, "email and password required", http.StatusBadRequest)
		return
	}

	userID, accessToken, refreshToken, err := s.cfg.SignIn(body.Email, body.Password)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}

	s.mergeRemote(userID, accessToken)

	jsonOK(w, map[string]any{
		"access_token":  accessToken,
		"refresh_token": refreshToken,
		"profiles":      s.profileStore.All(),
		"active":        s.profileStore.ActiveProfile(),
	})
}

// POST /api/auth/otp  {email}
// Sends an OTP / magic-link email.
func (s *Server) handleOTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.cfg == nil {
		s.notConfigured(w)
		return
	}

	var body struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Email == "" {
		http.Error(w, "email required", http.StatusBadRequest)
		return
	}

	if err := s.cfg.SendOTP(body.Email); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	jsonOK(w, map[string]string{"status": "ok"})
}

// POST /api/auth/verify-otp  {email, token}
func (s *Server) handleVerifyOTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.cfg == nil {
		s.notConfigured(w)
		return
	}

	var body struct {
		Email string `json:"email"`
		Token string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Email == "" || body.Token == "" {
		http.Error(w, "email and token required", http.StatusBadRequest)
		return
	}

	userID, accessToken, refreshToken, err := s.cfg.VerifyOTP(body.Email, body.Token)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}

	s.mergeRemote(userID, accessToken)

	jsonOK(w, map[string]any{
		"access_token":  accessToken,
		"refresh_token": refreshToken,
		"profiles":      s.profileStore.All(),
		"active":        s.profileStore.ActiveProfile(),
	})
}

// POST /api/auth/logout — clear the SupabaseUID link from the active profile.
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// Clear the supabase link on the active profile by linking to empty string...
	// actually just return OK; the frontend clears its session via supabase-js.
	jsonOK(w, map[string]string{"status": "ok"})
}

// GET /api/auth/me — return current auth state.
func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	active := s.profileStore.ActiveProfile()
	jsonOK(w, map[string]any{
		"profile": active,
		"linked":  active.SupabaseUID != nil,
	})
}

// POST /api/auth/sync — pull remote data and merge.
func (s *Server) handleSync(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.cfg == nil {
		s.notConfigured(w)
		return
	}

	// Extract and validate the JWT from the Authorization header.
	token := BearerFromRequest(r)
	if token == "" {
		http.Error(w, "authorization required", http.StatusUnauthorized)
		return
	}
	userID, err := s.cfg.ValidateJWT(token)
	if err != nil {
		http.Error(w, "invalid token: "+err.Error(), http.StatusUnauthorized)
		return
	}

	// Pull remote → merge locally.
	s.mergeRemote(userID, token)

	// Push local → remote (catches any records that failed during initial push).
	s.pushAsync(token, s.profileStore.ActiveProfile().ID, "supabase sync")

	// library_generation lets the frontend tell whether the merge actually
	// changed anything, so an idle focus-triggered sync doesn't force a full
	// UI refetch.
	jsonOK(w, map[string]any{
		"status":             "ok",
		"library_generation": s.lib.Generation(),
	})
}

// mergeRemote pulls all Supabase data for a user and merges it into the active profile.
func (s *Server) mergeRemote(supabaseUID, userJWT string) {
	active := s.profileStore.ActiveProfile()

	// Link UID to profile if not already set.
	if active.SupabaseUID == nil {
		if err := s.profileStore.LinkSupabase(active.ID, supabaseUID); err != nil {
			log.Println("supabase: link profile:", err)
		}
	}

	pulled, err := s.cfg.PullAll(userJWT, active.ID)
	if err != nil {
		log.Println("supabase: pull:", err)
		return
	}

	s.lib.MergeFrom(pulled.Entries, pulled.Progress, pulled.Dismissals)

	if pulled.Settings != nil {
		s.st.MergeFrom(*pulled.Settings)
	}
}
