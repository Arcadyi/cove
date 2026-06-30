package discover

import (
	"testing"

	"github.com/coveninja/cove/internal/library"
	"github.com/coveninja/cove/internal/tmdb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubLib implements TasteProvider using a fixed signal list.
type stubLib struct {
	signals []library.TasteSignal
	gen     uint64
}

func (s *stubLib) TasteSignals() []library.TasteSignal { return s.signals }
func (s *stubLib) Generation() uint64                   { return s.gen }

func TestSignalWeight_Finished(t *testing.T) {
	w := signalWeight(library.TasteSignal{Status: library.StatusFinished})
	assert.Greater(t, w, 0.0)
}

func TestSignalWeight_Dropped(t *testing.T) {
	w := signalWeight(library.TasteSignal{Status: library.StatusDropped})
	assert.Less(t, w, 0.0)
}

func TestSignalWeight_Dismissed(t *testing.T) {
	w := signalWeight(library.TasteSignal{Dismissed: true})
	assert.Less(t, w, 0.0)
}

func TestSignalWeight_HighRatingBoosts(t *testing.T) {
	r5 := 5.0
	r1 := 1.0
	hi := signalWeight(library.TasteSignal{Status: library.StatusFinished, UserRating: &r5})
	lo := signalWeight(library.TasteSignal{Status: library.StatusFinished, UserRating: &r1})
	assert.Greater(t, hi, lo)
}

func TestRankTaste_Order(t *testing.T) {
	scores := map[int]float64{1: 3.0, 2: 5.0, 3: -1.0}
	names := map[int]string{1: "Action", 2: "Comedy", 3: "Horror"}
	ranked := rankTaste(scores, names)
	require.Len(t, ranked, 3)
	assert.Equal(t, 2, ranked[0].ID)
	assert.Equal(t, 1, ranked[1].ID)
	assert.Equal(t, 3, ranked[2].ID)
}

func TestRankTaste_Empty(t *testing.T) {
	assert.Empty(t, rankTaste(map[int]float64{}, nil))
}

func TestCapTaste(t *testing.T) {
	input := []Taste{{ID: 1, Score: 5}, {ID: 2, Score: 4}, {ID: 3, Score: 3}}
	assert.Len(t, capTaste(input, 2), 2)
	assert.Len(t, capTaste(input, 10), 3)
	// limit=0 means no cap — returns all
	assert.Len(t, capTaste(input, 0), 3)
	assert.Len(t, capTaste(nil, 5), 0)
}

func TestPositiveTop(t *testing.T) {
	// positiveTop breaks on first non-positive score, so input must be sorted desc
	input := []Taste{{ID: 1, Score: 3}, {ID: 3, Score: 1}, {ID: 2, Score: -1}}
	out := positiveTop(input)
	assert.Len(t, out, 2)
	for _, item := range out {
		assert.Greater(t, item.Score, 0.0)
	}
}

func TestMergeWeighted_HalfHalf(t *testing.T) {
	movies := []tmdb.Media{{ID: 1}, {ID: 2}, {ID: 3}}
	shows := []tmdb.Media{{ID: 10}, {ID: 11}, {ID: 12}}
	out := mergeWeighted(movies, shows, 0.5, 4)
	require.Len(t, out, 4)
	mCount, sCount := 0, 0
	for _, m := range out {
		if m.ID < 10 {
			mCount++
		} else {
			sCount++
		}
	}
	// With movieShare=0.5 and limit=4, expect 2 of each
	assert.Equal(t, 2, mCount)
	assert.Equal(t, 2, sCount)
}

func TestMergeWeighted_AllMovies(t *testing.T) {
	movies := []tmdb.Media{{ID: 1}, {ID: 2}, {ID: 3}}
	out := mergeWeighted(movies, nil, 1.0, 3)
	// No shows available, should still return what it can
	assert.Len(t, out, 3)
}

func TestMergeWeighted_LimitRespected(t *testing.T) {
	movies := make([]tmdb.Media, 10)
	shows := make([]tmdb.Media, 10)
	for i := range movies {
		movies[i].ID = i + 1
		shows[i].ID = i + 100
	}
	out := mergeWeighted(movies, shows, 0.5, 6)
	assert.Len(t, out, 6)
}

func TestInsights_StubLib(t *testing.T) {
	lib := &stubLib{signals: []library.TasteSignal{}}
	svc := &Service{
		lib: lib,
		ttl: 0, // disable TTL so profile always rebuilds
	}
	// BuildProfile with empty library should still work without panic
	prof := svc.BuildProfile()
	assert.NotNil(t, prof)
	assert.Equal(t, 0, prof.Contributors)
}
