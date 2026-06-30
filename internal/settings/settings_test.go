package settings

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew_Defaults(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	st, err := New("test")
	require.NoError(t, err)

	s := st.Get()
	assert.Equal(t, defaultSettings, s)
}

func TestNew_LoadExisting(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	// First create with defaults
	st, err := New("test")
	require.NoError(t, err)

	// Change one field and persist via MergeFrom
	modified := st.Get()
	modified.DefaultVolume = 0.5
	modified.AutoPlay = true
	st.MergeFrom(modified)

	// New store loading the same profile should see the saved values
	st2, err := New("test")
	require.NoError(t, err)
	s := st2.Get()
	assert.Equal(t, 0.5, s.DefaultVolume)
	assert.True(t, s.AutoPlay)
	// Other defaults preserved
	assert.Equal(t, defaultSettings.DefaultProvider, s.DefaultProvider)
}

func TestMergeFrom(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	st, err := New("test")
	require.NoError(t, err)

	updated := st.Get()
	updated.HideSpoilers = true
	updated.SubtitleSize = 150
	st.MergeFrom(updated)

	s := st.Get()
	assert.True(t, s.HideSpoilers)
	assert.Equal(t, float64(150), s.SubtitleSize)
}

func TestSetProfile(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	st, err := New("primary")
	require.NoError(t, err)

	// Modify primary profile
	m := st.Get()
	m.DefaultVolume = 0.3
	st.MergeFrom(m)

	// Switch to a fresh kid profile
	require.NoError(t, st.SetProfile("kid"))
	assert.Equal(t, defaultSettings.DefaultVolume, st.Get().DefaultVolume)
}

func TestHandlers_GetSettings(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	st, err := New("test")
	require.NoError(t, err)

	mux := http.NewServeMux()
	st.SetupHandlers(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/settings", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var s Settings
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&s))
	assert.Equal(t, defaultSettings, s)
}

func TestHandlers_PutSettings(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	st, err := New("test")
	require.NoError(t, err)

	mux := http.NewServeMux()
	st.SetupHandlers(mux)

	updated := st.Get()
	updated.AutoPlay = true
	updated.DefaultVolume = 0.7
	body, err := json.Marshal(updated)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPut, "/api/settings", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.True(t, st.Get().AutoPlay)
	assert.Equal(t, 0.7, st.Get().DefaultVolume)
}
