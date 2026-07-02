//go:build supabase

// Copyright (c) 2025 coveninja. All Rights Reserved.
// This file is proprietary and is not covered by the AGPL-3.0 license
// that applies to the rest of the Cove repository.

package supabase

import (
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"time"

	"github.com/coveninja/cove/internal/addons"
	"github.com/coveninja/cove/internal/library"
	"github.com/coveninja/cove/internal/settings"
)

// remoteProfile is the `profiles` table row on Supabase.
type remoteProfile struct {
	ID        string    `json:"id"`      // matches local Profile.ID
	UserID    string    `json:"user_id"` // Supabase auth.users.id
	Name      string    `json:"name"`
	IsPrimary bool      `json:"is_primary"`
	UpdatedAt time.Time `json:"updated_at"`
}

// EnsureProfile upserts the profile row in Supabase, creating it if absent.
func (c *Config) EnsureProfile(userJWT, localProfileID, supabaseUID, name string, isPrimary bool) error {
	row := map[string]any{
		"id":         localProfileID,
		"user_id":    supabaseUID,
		"name":       name,
		"is_primary": isPrimary,
		"updated_at": time.Now().UTC(),
	}
	return c.Upsert(userJWT, "profiles", []any{row})
}

// RemoteProfilesForUser returns all profile rows owned by the given Supabase user.
func (c *Config) RemoteProfilesForUser(userJWT, supabaseUID string) ([]remoteProfile, error) {
	q := "user_id=eq." + url.QueryEscape(supabaseUID)
	rows, err := c.Select(userJWT, "profiles", q)
	if err != nil {
		return nil, err
	}
	out := make([]remoteProfile, 0, len(rows))
	for _, r := range rows {
		var p remoteProfile
		if err := json.Unmarshal(r, &p); err != nil {
			continue
		}
		out = append(out, p)
	}
	return out, nil
}

// PushLibrary uploads all local library entries, progress records, and dismissals
// for a profile to Supabase. Existing remote rows are merged (last-write-wins).
func (c *Config) PushLibrary(userJWT, profileID string, lib *library.Library) error {
	entries := lib.AllEntries()
	if len(entries) > 0 {
		rows := make([]map[string]any, 0, len(entries))
		for _, e := range entries {
			pid := profileID
			e.ProfileID = &pid
			rows = append(rows, entryToMap(e))
		}
		if err := c.Upsert(userJWT, "library_entries", rows); err != nil {
			return fmt.Errorf("push library entries: %w", err)
		}
	}

	progress := lib.AllProgress()
	if len(progress) > 0 {
		rows := make([]map[string]any, 0, len(progress))
		for _, p := range progress {
			pid := profileID
			p.ProfileID = &pid
			rows = append(rows, progressToMap(p))
		}
		if err := c.Upsert(userJWT, "watch_progress", rows); err != nil {
			return fmt.Errorf("push watch progress: %w", err)
		}
	}

	dismissals := lib.AllDismissals()
	if len(dismissals) > 0 {
		rows := make([]map[string]any, 0, len(dismissals))
		for _, d := range dismissals {
			rows = append(rows, map[string]any{
				"profile_id":   profileID,
				"tmdb_id":      d.TmdbID,
				"media_type":   d.MediaType,
				"dismissed_at": d.DismissedAt,
			})
		}
		if err := c.Upsert(userJWT, "dismissals", rows); err != nil {
			return fmt.Errorf("push dismissals: %w", err)
		}
	}
	return nil
}

// PushSettings uploads current settings for the profile.
func (c *Config) PushSettings(userJWT, profileID string, st *settings.Store) error {
	data, err := json.Marshal(st.Get())
	if err != nil {
		return err
	}
	return c.Upsert(userJWT, "profile_settings", []any{map[string]any{
		"profile_id": profileID,
		"data":       json.RawMessage(data),
		"updated_at": time.Now().UTC(),
	}})
}

// PushAddons uploads current addon configuration for the profile.
// PushAddons syncs Stremio-addon config cross-device. Nuvio repo/scraper
// config (internal/nuvio) is a conscious, documented gap for this iteration —
// it is not covered by cross-device sync yet.
func (c *Config) PushAddons(userJWT, profileID string, mgr *addons.Manager) error {
	entries := mgr.GetEntries()
	data, err := json.Marshal(entries)
	if err != nil {
		return err
	}
	return c.Upsert(userJWT, "profile_addons", []any{map[string]any{
		"profile_id": profileID,
		"data":       json.RawMessage(data),
		"updated_at": time.Now().UTC(),
	}})
}

// PulledData is returned by PullAll containing all remote data for a profile.
type PulledData struct {
	Entries    []*library.LibraryEntry
	Progress   []*library.WatchProgress
	Dismissals []*library.Dismissal
	Settings   *settings.Settings
}

// PullAll downloads all Supabase data for the given profile.
func (c *Config) PullAll(userJWT, profileID string) (*PulledData, error) {
	q := "profile_id=eq." + url.QueryEscape(profileID)
	out := &PulledData{}

	// Library entries
	rows, err := c.Select(userJWT, "library_entries", q)
	if err != nil {
		return nil, fmt.Errorf("pull library_entries: %w", err)
	}
	for _, r := range rows {
		var e library.LibraryEntry
		if err := json.Unmarshal(r, &e); err != nil {
			log.Println("supabase pull: decode library_entry:", err)
			continue
		}
		out.Entries = append(out.Entries, &e)
	}

	// Watch progress
	rows, err = c.Select(userJWT, "watch_progress", q)
	if err != nil {
		return nil, fmt.Errorf("pull watch_progress: %w", err)
	}
	for _, r := range rows {
		var p library.WatchProgress
		if err := json.Unmarshal(r, &p); err != nil {
			log.Println("supabase pull: decode watch_progress:", err)
			continue
		}
		out.Progress = append(out.Progress, &p)
	}

	// Dismissals
	rows, err = c.Select(userJWT, "dismissals", q)
	if err != nil {
		return nil, fmt.Errorf("pull dismissals: %w", err)
	}
	for _, r := range rows {
		var d library.Dismissal
		if err := json.Unmarshal(r, &d); err != nil {
			log.Println("supabase pull: decode dismissal:", err)
			continue
		}
		out.Dismissals = append(out.Dismissals, &d)
	}

	// Settings
	settingsRows, err := c.Select(userJWT, "profile_settings", "profile_id=eq."+url.QueryEscape(profileID))
	if err == nil && len(settingsRows) > 0 {
		var row struct {
			Data json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(settingsRows[0], &row); err == nil {
			var s settings.Settings
			if err := json.Unmarshal(row.Data, &s); err == nil {
				out.Settings = &s
			}
		}
	}

	return out, nil
}

// ── mapping helpers ───────────────────────────────────────────────────────────

func entryToMap(e *library.LibraryEntry) map[string]any {
	return map[string]any{
		"id":                   e.ID,
		"profile_id":           e.ProfileID,
		"tmdb_id":              e.TmdbID,
		"media_type":           e.MediaType,
		"title":                e.Title,
		"poster_path":          e.PosterPath,
		"status":               e.Status,
		"rating":               e.Rating,
		"vote_average":         e.VoteAverage,
		"last_air_date":        e.LastAirDate,
		"last_watched_at":      e.LastWatchedAt,
		"last_watched_season":  e.LastWatchedSeason,
		"last_watched_episode": e.LastWatchedEpisode,
		"last_aired_season":    e.LastAiredSeason,
		"last_aired_episode":   e.LastAiredEpisode,
		"added_at":             e.AddedAt,
		"updated_at":           e.UpdatedAt,
	}
}

func progressToMap(p *library.WatchProgress) map[string]any {
	return map[string]any{
		"id":               p.ID,
		"profile_id":       p.ProfileID,
		"tmdb_id":          p.TmdbID,
		"media_type":       p.MediaType,
		"season":           p.Season,
		"episode":          p.Episode,
		"position_seconds": p.PositionSeconds,
		"duration_seconds": p.DurationSeconds,
		"completed":        p.Completed,
		"watched_at":       p.WatchedAt,
	}
}
