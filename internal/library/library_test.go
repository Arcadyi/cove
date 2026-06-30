package library

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newLib(t *testing.T) *Library {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	l, err := New("test")
	require.NoError(t, err)
	return l
}

func TestNew_Empty(t *testing.T) {
	l := newLib(t)
	assert.Empty(t, l.AllEntries())
	assert.Empty(t, l.AllProgress())
	assert.Empty(t, l.AllDismissals())
}

func TestGeneration_Bumps(t *testing.T) {
	l := newLib(t)
	g0 := l.Generation()
	l.MergeFrom([]*LibraryEntry{{
		ID: "id1", TmdbID: 1, MediaType: "movie", Title: "Test",
		Status: StatusWatchLater, AddedAt: time.Now(), UpdatedAt: time.Now(),
	}}, nil, nil)
	assert.Greater(t, l.Generation(), g0)
}

func TestMergeFrom_AddEntry(t *testing.T) {
	l := newLib(t)
	now := time.Now()
	e := &LibraryEntry{ID: "abc", TmdbID: 42, MediaType: "movie", Title: "Foo", Status: StatusWatching, AddedAt: now, UpdatedAt: now}
	l.MergeFrom([]*LibraryEntry{e}, nil, nil)
	entries := l.AllEntries()
	require.Len(t, entries, 1)
	assert.Equal(t, 42, entries[0].TmdbID)
}

func TestMergeFrom_LastWriteWins(t *testing.T) {
	l := newLib(t)
	old := time.Now().Add(-time.Hour)
	recent := time.Now()
	e1 := &LibraryEntry{ID: "e1", TmdbID: 7, MediaType: "tv", Title: "Old", Status: StatusWatchLater, AddedAt: old, UpdatedAt: old}
	l.MergeFrom([]*LibraryEntry{e1}, nil, nil)

	e2 := &LibraryEntry{ID: "e1", TmdbID: 7, MediaType: "tv", Title: "New", Status: StatusFinished, AddedAt: old, UpdatedAt: recent}
	l.MergeFrom([]*LibraryEntry{e2}, nil, nil)
	entries := l.AllEntries()
	require.Len(t, entries, 1)
	assert.Equal(t, StatusFinished, entries[0].Status)
}

func TestMergeFrom_MaxPosition(t *testing.T) {
	l := newLib(t)
	p1 := &WatchProgress{ID: "p1", TmdbID: 1, MediaType: "movie", PositionSeconds: 100, WatchedAt: time.Now()}
	l.MergeFrom(nil, []*WatchProgress{p1}, nil)
	p2 := &WatchProgress{ID: "p2", TmdbID: 1, MediaType: "movie", PositionSeconds: 200, WatchedAt: time.Now()}
	l.MergeFrom(nil, []*WatchProgress{p2}, nil)
	progs := l.AllProgress()
	require.Len(t, progs, 1)
	assert.Equal(t, float64(200), progs[0].PositionSeconds)

	// Older position doesn't overwrite
	p3 := &WatchProgress{ID: "p3", TmdbID: 1, MediaType: "movie", PositionSeconds: 50, WatchedAt: time.Now()}
	l.MergeFrom(nil, []*WatchProgress{p3}, nil)
	progs = l.AllProgress()
	require.Len(t, progs, 1)
	assert.Equal(t, float64(200), progs[0].PositionSeconds)
}

func TestDismissal(t *testing.T) {
	l := newLib(t)
	d := &Dismissal{TmdbID: 99, MediaType: "movie", DismissedAt: time.Now()}
	l.MergeFrom(nil, nil, []*Dismissal{d})
	ds := l.AllDismissals()
	require.Len(t, ds, 1)
	assert.Equal(t, 99, ds[0].TmdbID)
}

func TestStats(t *testing.T) {
	l := newLib(t)
	rating := 4.0
	l.MergeFrom([]*LibraryEntry{
		{ID: "a", TmdbID: 1, MediaType: "movie", Status: StatusFinished, Rating: &rating, AddedAt: time.Now(), UpdatedAt: time.Now()},
		{ID: "b", TmdbID: 2, MediaType: "tv", Status: StatusWatching, AddedAt: time.Now(), UpdatedAt: time.Now()},
	}, nil, []*Dismissal{{TmdbID: 3, MediaType: "movie", DismissedAt: time.Now()}})

	st := l.Stats()
	assert.Equal(t, 2, st.Total)
	assert.Equal(t, 1, st.ByType["movie"])
	assert.Equal(t, 1, st.ByType["tv"])
	assert.Equal(t, 1, st.Dismissed)
	assert.Equal(t, 1, st.Rated)
	assert.Equal(t, 4.0, st.AvgRating)
}

func TestTasteSignals(t *testing.T) {
	l := newLib(t)
	l.MergeFrom([]*LibraryEntry{
		{ID: "a", TmdbID: 1, MediaType: "movie", Status: StatusFinished, AddedAt: time.Now(), UpdatedAt: time.Now()},
	}, nil, []*Dismissal{{TmdbID: 2, MediaType: "tv", DismissedAt: time.Now()}})

	signals := l.TasteSignals()
	require.Len(t, signals, 2)
	byID := make(map[int]TasteSignal)
	for _, s := range signals {
		byID[s.TmdbID] = s
	}
	assert.Equal(t, StatusFinished, byID[1].Status)
	assert.False(t, byID[1].Dismissed)
	assert.True(t, byID[2].Dismissed)
}

func TestHandlers_GetLibrary(t *testing.T) {
	l := newLib(t)
	mux := http.NewServeMux()
	l.SetupHandlers(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/library", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	var entries []*LibraryEntry
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&entries))
	assert.Empty(t, entries)
}

func TestHandlers_PostLibrary(t *testing.T) {
	l := newLib(t)
	mux := http.NewServeMux()
	l.SetupHandlers(mux)

	body := `{"tmdb_id":123,"media_type":"movie","title":"Test Movie","status":"watch_later"}`
	req := httptest.NewRequest(http.MethodPost, "/api/library", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	var e LibraryEntry
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&e))
	assert.Equal(t, 123, e.TmdbID)
	assert.Equal(t, "watch_later", e.Status)
}

func TestHandlers_GetStats(t *testing.T) {
	l := newLib(t)
	mux := http.NewServeMux()
	l.SetupHandlers(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/library/stats", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	var st Stats
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&st))
	assert.Equal(t, 0, st.Total)
}
