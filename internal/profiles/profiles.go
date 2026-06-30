package profiles

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/coveninja/cove/internal/utils"
)

// Profile is one named identity within a Cove installation. A single Supabase
// account can own many profiles; each profile has its own library, settings,
// and addon config stored in separate files on disk.
type Profile struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	IsPrimary   bool    `json:"is_primary"`
	SupabaseUID *string `json:"supabase_uid"` // nil = guest / local-only
}

type diskStore struct {
	Profiles        []Profile `json:"profiles"`
	ActiveProfileID string    `json:"active_profile_id"`
}

// Store owns the profile list and the currently active profile ID.
type Store struct {
	mu       sync.RWMutex
	disk     diskStore
	path     string
	onChange func(profileID string) // called when active profile switches
}

// New loads profiles.json from the per-user config directory. On first run it
// creates a "Primary" profile and migrates any pre-existing library.json /
// settings.json / addons.json into profile-scoped filenames.
// onChange is invoked (from a goroutine) whenever the active profile changes;
// the caller should reload library, settings, and addons for the new ID.
func New(onChange func(profileID string)) (*Store, error) {
	s := &Store{onChange: onChange}

	path, err := utils.ConfigPath("profiles.json")
	if err != nil {
		return nil, err
	}
	s.path = path

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		// First run: create primary profile and migrate legacy flat files.
		primary := Profile{ID: newUUID(), Name: "Primary", IsPrimary: true}
		s.disk = diskStore{
			Profiles:        []Profile{primary},
			ActiveProfileID: primary.ID,
		}
		migrateLegacyFiles(primary.ID)
		return s, s.write()
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &s.disk); err != nil {
		return nil, err
	}
	// Guard: always have at least one profile with a consistent active ID.
	if len(s.disk.Profiles) == 0 {
		primary := Profile{ID: newUUID(), Name: "Primary", IsPrimary: true}
		s.disk.Profiles = []Profile{primary}
		s.disk.ActiveProfileID = primary.ID
		_ = s.write()
	}
	return s, nil
}

// migrateLegacyFiles renames legacy single-user files to profile-scoped names.
func migrateLegacyFiles(profileID string) {
	for _, name := range []string{"library", "settings", "addons"} {
		src, err := utils.ConfigPath(name + ".json")
		if err != nil {
			continue
		}
		if _, err := os.Stat(src); os.IsNotExist(err) {
			continue
		}
		dst, err := utils.ConfigPath(name + "-" + profileID + ".json")
		if err != nil {
			continue
		}
		if _, err := os.Stat(dst); err == nil {
			continue // destination already exists; don't overwrite
		}
		if err := os.Rename(src, dst); err != nil {
			log.Printf("profiles: migrate %s.json: %v", name, err)
		} else {
			log.Printf("profiles: migrated %s.json → %s-%s.json", name, name, profileID)
		}
	}
}

func (s *Store) write() error {
	data, err := json.MarshalIndent(s.disk, "", "  ")
	if err != nil {
		return err
	}
	return utils.AtomicWriteFile(s.path, data, 0o644)
}

// ActiveProfile returns the currently active profile. Safe for concurrent use.
func (s *Store) ActiveProfile() Profile {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, p := range s.disk.Profiles {
		if p.ID == s.disk.ActiveProfileID {
			return p
		}
	}
	if len(s.disk.Profiles) > 0 {
		return s.disk.Profiles[0]
	}
	return Profile{}
}

// ActiveProfileID returns the active profile's ID. Safe for concurrent use.
func (s *Store) ActiveProfileID() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.disk.ActiveProfileID
}

// All returns a snapshot of all profiles.
func (s *Store) All() []Profile {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Profile, len(s.disk.Profiles))
	copy(out, s.disk.Profiles)
	return out
}

// SetActive switches to the given profile ID and triggers onChange.
func (s *Store) SetActive(id string) error {
	s.mu.Lock()
	found := false
	for _, p := range s.disk.Profiles {
		if p.ID == id {
			found = true
			break
		}
	}
	if !found {
		s.mu.Unlock()
		return fmt.Errorf("profile %q not found", id)
	}
	s.disk.ActiveProfileID = id
	err := s.write()
	s.mu.Unlock()
	if err != nil {
		return err
	}
	if s.onChange != nil {
		go s.onChange(id)
	}
	return nil
}

// Create adds a new profile and returns it.
func (s *Store) Create(name string) (Profile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := Profile{ID: newUUID(), Name: name}
	s.disk.Profiles = append(s.disk.Profiles, p)
	return p, s.write()
}

// Rename updates a profile's display name.
func (s *Store) Rename(id, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, p := range s.disk.Profiles {
		if p.ID == id {
			s.disk.Profiles[i].Name = name
			return s.write()
		}
	}
	return fmt.Errorf("profile %q not found", id)
}

// Delete removes a profile. Returns an error if it is the primary profile.
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, p := range s.disk.Profiles {
		if p.ID != id {
			continue
		}
		if p.IsPrimary {
			return fmt.Errorf("cannot delete the primary profile")
		}
		s.disk.Profiles = append(s.disk.Profiles[:i], s.disk.Profiles[i+1:]...)
		if s.disk.ActiveProfileID == id {
			for _, pp := range s.disk.Profiles {
				if pp.IsPrimary {
					s.disk.ActiveProfileID = pp.ID
					break
				}
			}
		}
		return s.write()
	}
	return fmt.Errorf("profile %q not found", id)
}

// LinkSupabase stores the Supabase user ID on a profile.
func (s *Store) LinkSupabase(profileID, supabaseUID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, p := range s.disk.Profiles {
		if p.ID == profileID {
			s.disk.Profiles[i].SupabaseUID = &supabaseUID
			return s.write()
		}
	}
	return fmt.Errorf("profile %q not found", profileID)
}

// ProfileFileName returns the profile-scoped filename for a data file.
// e.g. ProfileFileName("library", profileID) → "library-<id>.json"
func ProfileFileName(base, profileID string) string {
	return base + "-" + profileID + ".json"
}

// SetupHandlers registers profile CRUD endpoints on mux.
func (s *Store) SetupHandlers(mux *http.ServeMux) {
	mux.HandleFunc("/api/profiles", utils.CorsMiddleware(s.handleList))
	mux.HandleFunc("/api/profiles/", utils.CorsMiddleware(s.handleByID))
}

func jsonOK(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Store) handleList(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.mu.RLock()
		resp := map[string]any{
			"profiles":          s.disk.Profiles,
			"active_profile_id": s.disk.ActiveProfileID,
		}
		s.mu.RUnlock()
		jsonOK(w, resp)

	case http.MethodPost:
		var body struct {
			Name string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Name) == "" {
			http.Error(w, "name required", http.StatusBadRequest)
			return
		}
		p, err := s.Create(strings.TrimSpace(body.Name))
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		jsonOK(w, p)

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Store) handleByID(w http.ResponseWriter, r *http.Request) {
	trimmed := strings.TrimPrefix(r.URL.Path, "/api/profiles/")
	parts := strings.SplitN(trimmed, "/", 2)
	id := parts[0]
	sub := ""
	if len(parts) == 2 {
		sub = parts[1]
	}

	if sub == "activate" && r.Method == http.MethodPost {
		if err := s.SetActive(id); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		jsonOK(w, s.ActiveProfile())
		return
	}

	switch r.Method {
	case http.MethodPatch:
		var body struct {
			Name string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Name) == "" {
			http.Error(w, "name required", http.StatusBadRequest)
			return
		}
		if err := s.Rename(id, strings.TrimSpace(body.Name)); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		jsonOK(w, map[string]string{"id": id, "name": body.Name})

	case http.MethodDelete:
		if err := s.Delete(id); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusNoContent)

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// newUUID generates a random UUIDv4.
func newUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}
