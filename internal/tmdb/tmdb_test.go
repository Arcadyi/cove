package tmdb

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDisplayTitle(t *testing.T) {
	assert.Equal(t, "Inception", (&Media{Title: "Inception"}).DisplayTitle())
	assert.Equal(t, "Breaking Bad", (&Media{Name: "Breaking Bad"}).DisplayTitle())
	// Title takes precedence over Name
	assert.Equal(t, "Movie", (&Media{Title: "Movie", Name: "Show"}).DisplayTitle())
}

func TestDisplayDate(t *testing.T) {
	assert.Equal(t, "2010-07-16", (&Media{Released: "2010-07-16"}).DisplayDate())
	assert.Equal(t, "2008-01-20", (&Media{FirstAir: "2008-01-20"}).DisplayDate())
	// Released takes precedence
	assert.Equal(t, "2020-01-01", (&Media{Released: "2020-01-01", FirstAir: "2018-01-01"}).DisplayDate())
	assert.Equal(t, "", (&Media{}).DisplayDate())
}

func TestAgeRating_Movie(t *testing.T) {
	d := &Details{}
	d.ReleaseDates.Results = []struct {
		ISO31661     string `json:"iso_3166_1"`
		ReleaseDates []struct {
			Certification string `json:"certification"`
		} `json:"release_dates"`
	}{
		{ISO31661: "GB", ReleaseDates: []struct {
			Certification string `json:"certification"`
		}{{Certification: "15"}}},
		{ISO31661: "US", ReleaseDates: []struct {
			Certification string `json:"certification"`
		}{{Certification: "PG-13"}}},
	}
	assert.Equal(t, "PG-13", d.AgeRating())
}

func TestAgeRating_TV(t *testing.T) {
	d := &Details{}
	d.ContentRatings.Results = []struct {
		ISO31661 string `json:"iso_3166_1"`
		Rating   string `json:"rating"`
	}{
		{ISO31661: "US", Rating: "TV-MA"},
	}
	assert.Equal(t, "TV-MA", d.AgeRating())
}

func TestAgeRating_Missing(t *testing.T) {
	assert.Equal(t, "", (&Details{}).AgeRating())
}

func TestDisplayRuntime_Movie(t *testing.T) {
	assert.Equal(t, "2h 10m", (&Details{Runtime: 130}).DisplayRuntime())
	assert.Equal(t, "0h 45m", (&Details{Runtime: 45}).DisplayRuntime())
}

func TestDisplayRuntime_TV(t *testing.T) {
	assert.Equal(t, "22m / ep", (&Details{EpisodeRunTime: []int{22}}).DisplayRuntime())
}

func TestDisplayRuntime_Empty(t *testing.T) {
	assert.Equal(t, "", (&Details{}).DisplayRuntime())
}

func TestKeywordPairs_Movie(t *testing.T) {
	d := &Details{}
	d.Keywords.Keywords = []struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	}{
		{ID: 1, Name: "sci-fi"},
		{ID: 2, Name: "space"},
	}
	pairs := d.KeywordPairs()
	assert.Equal(t, map[int]string{1: "sci-fi", 2: "space"}, pairs)
}

func TestKeywordPairs_TV(t *testing.T) {
	d := &Details{}
	d.Keywords.Results = []struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	}{
		{ID: 5, Name: "drama"},
	}
	pairs := d.KeywordPairs()
	assert.Equal(t, map[int]string{5: "drama"}, pairs)
}

func TestKeywordPairs_MovieTakesPrecedence(t *testing.T) {
	d := &Details{}
	d.Keywords.Keywords = []struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	}{{ID: 1, Name: "movie-kw"}}
	d.Keywords.Results = []struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	}{{ID: 2, Name: "tv-kw"}}
	pairs := d.KeywordPairs()
	assert.Equal(t, map[int]string{1: "movie-kw"}, pairs)
}

func TestNormalizeQuery(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		// Hyphens/dots become spaces, multiple spaces collapse, case preserved
		{"Spider-Man: No Way Home", "Spider Man: No Way Home"},
		{"iron.man", "iron man"},
		{"  extra   spaces  ", "extra spaces"},
		// NFC normalization — passthrough for already-normal ASCII
		{"Hello World", "Hello World"},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			assert.Equal(t, tc.want, normalizeQuery(tc.input))
		})
	}
}

func TestQueryVariants(t *testing.T) {
	// "the matrix" → original + stripped (letters/digits only, lowercased)
	variants := queryVariants("the matrix")
	assert.Contains(t, variants, "the matrix")
	assert.Contains(t, variants, "thematrix")
	assert.Greater(t, len(variants), 1)

	// Hyphenated title: original + normalized (hyphen→space) + stripped
	variants2 := queryVariants("Spider-Man")
	assert.Contains(t, variants2, "Spider-Man")
	assert.Contains(t, variants2, "Spider Man") // normalized
	assert.Contains(t, variants2, "spiderman")  // stripped
}
