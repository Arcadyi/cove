package tmdb

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	neturl "net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/coveninja/cove/internal/addons"
	"github.com/coveninja/cove/internal/utils"
	"golang.org/x/text/unicode/norm"
)

// Client talks to the TMDB API. Fields are unexported, so tygo emits nothing
// for Client — only the data types (Media, Details, MediaImages, ...) cross
// into the generated TS.
type Client struct {
	apiKey string
	client *http.Client
}

// New returns a TMDB client. The 15s timeout matters because http.DefaultClient
// has none, so a stalled TMDB response would otherwise hold a request goroutine
// open forever; TMDB is normally fast, so 15s only trips on a dead connection.
func New(apiKey string) *Client {
	return &Client{
		apiKey: apiKey,
		client: &http.Client{Timeout: 15 * time.Second},
	}
}

type Media struct {
	ID         int      `json:"id"`
	Title      string   `json:"title"`
	Name       string   `json:"name"`
	Overview   string   `json:"overview"`
	Released   string   `json:"release_date"`
	FirstAir   string   `json:"first_air_date"`
	PosterURL  string   `json:"poster_path"`
	Rating     float64  `json:"vote_average"`
	MediaType  string   `json:"media_type"`
	TrailerURL string   `json:"trailer_url"`
	ClipURLs   string   `json:"clip_urls"`
	Images     []string `json:"images"`
	Popularity float64  `json:"popularity"`
	GenreIDs   []int    `json:"genre_ids,omitempty"`
	Adult      bool     `json:"adult,omitempty"`
}

type MediaDetails struct {
	ImdbID string `json:"imdb_id"`
}

type TVExternalIds struct {
	ImdbID string `json:"imdb_id"`
}

// TVSeason is a season summary returned by /tv/{id}.
type TVSeason struct {
	SeasonNumber int    `json:"season_number"`
	EpisodeCount int    `json:"episode_count"`
	Name         string `json:"name"`
	PosterPath   string `json:"poster_path"`
}

// TVEpisode is a single episode returned by /tv/{id}/season/{n}.
type TVEpisode struct {
	EpisodeNumber int    `json:"episode_number"`
	Name          string `json:"name"`
	Overview      string `json:"overview"`
	StillPath     string `json:"still_path"`
	AirDate       string `json:"air_date"`
}

type Details struct {
	Overview string `json:"overview"`
	Genres   []struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	} `json:"genres"`
	Runtime        int   `json:"runtime"`
	EpisodeRunTime []int `json:"episode_run_time"`
	Credits        struct {
		Cast []struct {
			Name string `json:"name"`
		} `json:"cast"`
	} `json:"credits"`
	ReleaseDates struct {
		Results []struct {
			ISO31661     string `json:"iso_3166_1"`
			ReleaseDates []struct {
				Certification string `json:"certification"`
			} `json:"release_dates"`
		} `json:"results"`
	} `json:"release_dates"`
	ContentRatings struct {
		Results []struct {
			ISO31661 string `json:"iso_3166_1"`
			Rating   string `json:"rating"`
		} `json:"results"`
	} `json:"content_ratings"`
	Keywords struct {
		Keywords []struct {
			ID   int    `json:"id"`
			Name string `json:"name"`
		} `json:"keywords"` // movies
		Results []struct {
			ID   int    `json:"id"`
			Name string `json:"name"`
		} `json:"results"` // tv shows
	} `json:"keywords"`
	OriginCountry    []string   `json:"origin_country"`
	NumberOfSeasons  int        `json:"number_of_seasons"`
	NumberOfEpisodes int        `json:"number_of_episodes"`
	Seasons          []TVSeason `json:"seasons"`
	// LastEpisodeToAir is TV-only. Used client-side to detect unwatched new
	// episodes by comparing season/episode_number against the user's
	// last-watched position.
	LastEpisodeToAir *struct {
		SeasonNumber  int    `json:"season_number"`
		EpisodeNumber int    `json:"episode_number"`
		AirDate       string `json:"air_date"`
	} `json:"last_episode_to_air"`
	// NextEpisodeToAir is TV-only and only present while the show is still
	// airing. Used to power the "Upcoming" widget — null once a show has
	// ended or gone on indefinite hiatus with nothing scheduled.
	NextEpisodeToAir *struct {
		Name          string `json:"name"`
		SeasonNumber  int    `json:"season_number"`
		EpisodeNumber int    `json:"episode_number"`
		AirDate       string `json:"air_date"`
		StillPath     string `json:"still_path"`
	} `json:"next_episode_to_air"`
}

type searchResponse struct {
	Results []Media `json:"results"`
}

// Person is a /search/person result. KnownFor carries a few representative
// titles TMDB attaches to the person, so a search for "Jackie Chan" can surface
// his films alongside the person entry itself.
type Person struct {
	ID                 int     `json:"id"`
	Name               string  `json:"name"`
	ProfileURL         string  `json:"profile_path"`
	KnownForDepartment string  `json:"known_for_department"`
	Popularity         float64 `json:"popularity"`
	KnownFor           []Media `json:"known_for"`
}

// Provider is a streaming/rental service from /watch/providers. TMDB has no
// name-search for providers, so SearchProviders fetches the regional directory
// and filters by name.
type Provider struct {
	ID       int    `json:"provider_id"`
	Name     string `json:"provider_name"`
	LogoURL  string `json:"logo_path"`
	Priority int    `json:"display_priority"`
}

// SearchResults is the sectioned payload for /api/search/multi.
type SearchResults struct {
	Movies    []Media    `json:"movies"`
	TV        []Media    `json:"tv"`
	People    []Person   `json:"people"`
	Providers []Provider `json:"providers"`
}

// PersonDetails is the full /person/{id} payload used by the person overlay:
// biography plus a deduped, popularity-sorted filmography (combined_credits).
type PersonDetails struct {
	ID                 int     `json:"id"`
	Name               string  `json:"name"`
	Biography          string  `json:"biography"`
	ProfileURL         string  `json:"profile_path"`
	KnownForDepartment string  `json:"known_for_department"`
	Birthday           string  `json:"birthday"`
	PlaceOfBirth       string  `json:"place_of_birth"`
	Credits            []Media `json:"credits"`
}

type Keyword struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type scoredMedia struct {
	media Media
	score float64
}

type MediaImageObject struct {
	AspectRatio float32 `json:"aspect_ratio"`
	Height      int     `json:"height"`
	Iso6391     string  `json:"iso_639_1"`
	FilePath    string  `json:"file_path"`
	URL         string  `json:"url"`
	VoteAverage float32 `json:"vote_average"`
	VoteCount   int     `json:"vote_count"`
	Width       int     `json:"width"`
}

type MediaImages struct {
	Backdrops []MediaImageObject `json:"backdrops"`
	Logos     []MediaImageObject `json:"logos"`
	Posters   []MediaImageObject `json:"posters"`
}

type MediaVideoObject struct {
	Iso6391     string `json:"iso_639_1"`
	Name        string `json:"name"`
	Key         string `json:"key"`
	Site        string `json:"site"`
	Size        int    `json:"size"`
	Type        string `json:"type"`
	Official    bool   `json:"official"`
	PublishedAt string `json:"published_at"`
	EmbedURL    string `json:"embed_url"`
}
type MediaVideos struct {
	Results []MediaVideoObject `json:"results"`
}

const baseURL = "https://api.themoviedb.org/3"
const imageBase = "https://image.tmdb.org/t/p/w500"
const imageBaseOriginal = "https://image.tmdb.org/t/p/original"
const stillBase = "https://image.tmdb.org/t/p/w300"

func (c *Client) SearchByKeywords(query string) ([]Media, error) {
	normalized := normalizeQuery(query)
	kwURL := fmt.Sprintf("%s/search/keyword?api_key=%s&query=%s", baseURL, c.apiKey, normalized)
	res, err := c.client.Get(kwURL)
	if err != nil {
		return nil, err
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			log.Println(err)
		}
	}(res.Body)

	var kwData struct {
		Results []struct {
			ID int `json:"id"`
		} `json:"results"`
	}
	if err := json.NewDecoder(res.Body).Decode(&kwData); err != nil {
		return nil, err
	}
	if len(kwData.Results) == 0 {
		return nil, nil
	}

	ids := make([]string, 0, 3)
	for i := 0; i < len(kwData.Results) && i < 3; i++ {
		ids = append(ids, strconv.Itoa(kwData.Results[i].ID))
	}
	kwParam := strings.Join(ids, "|")

	var results []Media
	for _, mediaType := range []string{"movie", "tv"} {
		discURL := fmt.Sprintf("%s/discover/%s?api_key=%s&with_keywords=%s&sort_by=popularity.desc",
			baseURL, mediaType, c.apiKey, kwParam)
		r, err := c.client.Get(discURL)
		if err != nil {
			continue
		}
		var data searchResponse
		err = json.NewDecoder(r.Body).Decode(&data)
		if err != nil {
			log.Println(err)
			return nil, err
		}
		err = r.Body.Close()
		if err != nil {
			log.Println(err)
			return nil, err
		}

		for i := range data.Results {
			data.Results[i].PosterURL = imageBase + data.Results[i].PosterURL
			data.Results[i].MediaType = mediaType
		}
		for _, m := range data.Results {
			if m.PosterURL != imageBase {
				results = append(results, m)
			}
		}
	}
	return results, nil
}

func normalizeQuery(q string) string {
	q = norm.NFC.String(q)
	q = strings.Map(func(r rune) rune {
		if r == '-' || r == '.' || r == '·' || r == '_' {
			return ' '
		}
		return r
	}, q)
	q = strings.Join(strings.FieldsFunc(q, unicode.IsSpace), " ")
	return strings.TrimSpace(q)
}

func queryVariants(q string) []string {
	seen := map[string]bool{q: true}
	variants := []string{q}

	normalized := normalizeQuery(q)
	if !seen[normalized] {
		seen[normalized] = true
		variants = append(variants, normalized)
	}

	var b strings.Builder
	for _, r := range strings.ToLower(q) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	stripped := b.String()
	if !seen[stripped] {
		seen[stripped] = true
		variants = append(variants, stripped)
	}

	return variants
}

func (c *Client) Search(query string) ([]Media, error) {
	variantBoost := []float64{3.0, 1.5, 1.0}

	seen := make(map[int]bool)
	var scored []scoredMedia

	for vi, variant := range queryVariants(query) {
		boost := variantBoost[vi]
		encoded := neturl.QueryEscape(variant)

		for _, mediaType := range []string{"movie", "tv"} {
			url := fmt.Sprintf("%s/search/%s?api_key=%s&query=%s", baseURL, mediaType, c.apiKey, encoded)
			res, err := c.client.Get(url)
			if err != nil {
				continue
			}
			var data searchResponse
			err = json.NewDecoder(res.Body).Decode(&data)
			if err != nil {
				log.Println(err)
				return nil, err
			}
			err = res.Body.Close()
			if err != nil {
				log.Println(err)
				return nil, err
			}

			for i := range data.Results {
				data.Results[i].PosterURL = imageBase + data.Results[i].PosterURL
				data.Results[i].MediaType = mediaType
			}
			for _, m := range data.Results {
				if m.PosterURL == imageBase || seen[m.ID] {
					continue
				}
				seen[m.ID] = true
				scored = append(scored, scoredMedia{
					media: m,
					score: m.Popularity * boost,
				})
			}
		}
	}

	sort.Slice(scored, func(i, j int) bool {
		return scored[i].score > scored[j].score
	})

	merged := make([]Media, len(scored))
	for i, s := range scored {
		merged[i] = s.media
	}
	return merged, nil
}

// SearchPeople finds people by name and returns each with their representative
// titles (poster + profile URLs absolutised, non-movie/tv known-for dropped).
func (c *Client) SearchPeople(query string) ([]Person, error) {
	encoded := neturl.QueryEscape(normalizeQuery(query))
	url := fmt.Sprintf("%s/search/person?api_key=%s&query=%s", baseURL, c.apiKey, encoded)
	res, err := c.client.Get(url)
	if err != nil {
		return nil, err
	}
	defer func() { _ = res.Body.Close() }()

	var data struct {
		Results []Person `json:"results"`
	}
	if err := json.NewDecoder(res.Body).Decode(&data); err != nil {
		return nil, err
	}

	people := make([]Person, 0, len(data.Results))
	for _, p := range data.Results {
		if p.ProfileURL == "" {
			continue // faceless entries are usually noise
		}
		p.ProfileURL = imageBase + p.ProfileURL

		kf := make([]Media, 0, len(p.KnownFor))
		for _, m := range p.KnownFor {
			if (m.MediaType != "movie" && m.MediaType != "tv") || m.PosterURL == "" {
				continue
			}
			m.PosterURL = imageBase + m.PosterURL
			kf = append(kf, m)
		}
		p.KnownFor = kf
		people = append(people, p)
	}
	return people, nil
}

// SearchProviders matches streaming/rental services by name. TMDB exposes no
// provider name-search, so we pull the US movie+tv provider directories and
// filter locally. Region is fixed to US for now.
func (c *Client) SearchProviders(query string) ([]Provider, error) {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return nil, nil
	}

	seen := make(map[int]bool)
	var out []Provider
	for _, mediaType := range []string{"movie", "tv"} {
		url := fmt.Sprintf("%s/watch/providers/%s?api_key=%s&language=en-US&watch_region=US",
			baseURL, mediaType, c.apiKey)
		res, err := c.client.Get(url)
		if err != nil {
			continue
		}
		var data struct {
			Results []Provider `json:"results"`
		}
		err = json.NewDecoder(res.Body).Decode(&data)
		_ = res.Body.Close()
		if err != nil {
			continue
		}
		for _, p := range data.Results {
			if seen[p.ID] || !strings.Contains(strings.ToLower(p.Name), q) {
				continue
			}
			seen[p.ID] = true
			if p.LogoURL != "" {
				p.LogoURL = imageBase + p.LogoURL
			}
			out = append(out, p)
		}
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Priority < out[j].Priority })
	if len(out) > 12 {
		out = out[:12]
	}
	return out, nil
}

// MultiSearch fans out across titles, people, and providers and returns them in
// separate sections. Titles reuse the scored Search + keyword merge, split by
// media type so relevance order is preserved within each section.
func (c *Client) MultiSearch(query string) (SearchResults, error) {
	regular, err := c.Search(query)
	if err != nil {
		return SearchResults{}, err
	}
	byKeyword, _ := c.SearchByKeywords(query)

	seen := make(map[string]bool)
	movies, tv := []Media{}, []Media{}
	add := func(m Media) {
		key := fmt.Sprintf("%d-%s", m.ID, m.MediaType)
		if seen[key] {
			return
		}
		seen[key] = true
		if m.MediaType == "tv" {
			tv = append(tv, m)
		} else {
			movies = append(movies, m)
		}
	}
	for _, m := range regular {
		add(m)
	}
	for _, m := range byKeyword {
		add(m)
	}

	// People and providers are best-effort: a failure in either shouldn't sink
	// the whole search. Coerce nils so each section marshals as [] not null.
	people, _ := c.SearchPeople(query)
	if people == nil {
		people = []Person{}
	}
	providers, _ := c.SearchProviders(query)
	if providers == nil {
		providers = []Provider{}
	}

	return SearchResults{Movies: movies, TV: tv, People: people, Providers: providers}, nil
}

// GetPerson returns a person's bio and their filmography (combined credits),
// deduped, movie/tv only, sorted by popularity and capped.
func (c *Client) GetPerson(id int) (PersonDetails, error) {
	url := fmt.Sprintf("%s/person/%d?api_key=%s&append_to_response=combined_credits",
		baseURL, id, c.apiKey)
	res, err := c.client.Get(url)
	if err != nil {
		return PersonDetails{}, err
	}
	defer func() { _ = res.Body.Close() }()

	var data struct {
		ID                 int    `json:"id"`
		Name               string `json:"name"`
		Biography          string `json:"biography"`
		ProfilePath        string `json:"profile_path"`
		KnownForDepartment string `json:"known_for_department"`
		Birthday           string `json:"birthday"`
		PlaceOfBirth       string `json:"place_of_birth"`
		CombinedCredits    struct {
			Cast []Media `json:"cast"`
		} `json:"combined_credits"`
	}
	if err := json.NewDecoder(res.Body).Decode(&data); err != nil {
		return PersonDetails{}, err
	}

	pd := PersonDetails{
		ID:                 data.ID,
		Name:               data.Name,
		Biography:          data.Biography,
		KnownForDepartment: data.KnownForDepartment,
		Birthday:           data.Birthday,
		PlaceOfBirth:       data.PlaceOfBirth,
		Credits:            []Media{},
	}
	if data.ProfilePath != "" {
		pd.ProfileURL = imageBase + data.ProfilePath
	}

	seen := make(map[int]bool)
	for _, m := range data.CombinedCredits.Cast {
		if (m.MediaType != "movie" && m.MediaType != "tv") || m.PosterURL == "" || seen[m.ID] {
			continue
		}
		seen[m.ID] = true
		m.PosterURL = imageBase + m.PosterURL
		pd.Credits = append(pd.Credits, m)
	}
	sort.Slice(pd.Credits, func(i, j int) bool {
		return pd.Credits[i].Popularity > pd.Credits[j].Popularity
	})
	if len(pd.Credits) > 24 {
		pd.Credits = pd.Credits[:24]
	}
	return pd, nil
}

// DiscoverByProvider lists popular titles of one media type available on a
// watch provider. Region is fixed to US (providers are region-specific).
func (c *Client) DiscoverByProvider(mediaType string, providerID, limit int) ([]Media, error) {
	if mediaType != "movie" && mediaType != "tv" {
		mediaType = "movie"
	}
	url := fmt.Sprintf("%s/discover/%s?api_key=%s&watch_region=US&with_watch_providers=%d&sort_by=popularity.desc",
		baseURL, mediaType, c.apiKey, providerID)
	res, err := c.client.Get(url)
	if err != nil {
		return nil, err
	}
	var data searchResponse
	err = json.NewDecoder(res.Body).Decode(&data)
	_ = res.Body.Close()
	if err != nil {
		return nil, err
	}

	out := make([]Media, 0, len(data.Results))
	for i := range data.Results {
		if data.Results[i].PosterURL == "" {
			continue
		}
		data.Results[i].PosterURL = imageBase + data.Results[i].PosterURL
		data.Results[i].MediaType = mediaType
		out = append(out, data.Results[i])
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

// ProviderTitles blends a provider's popular movies and TV into one
// popularity-sorted list.
func (c *Client) ProviderTitles(providerID, limit int) ([]Media, error) {
	var all []Media
	for _, mt := range []string{"movie", "tv"} {
		list, err := c.DiscoverByProvider(mt, providerID, limit)
		if err == nil {
			all = append(all, list...)
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Popularity > all[j].Popularity })
	if limit > 0 && len(all) > limit {
		all = all[:limit]
	}
	if all == nil {
		all = []Media{}
	}
	return all, nil
}

// GetIMDBId returns the IMDB ID for a movie by TMDB ID.
func (c *Client) GetIMDBId(tmdbID int) (string, error) {
	url := fmt.Sprintf("%s/movie/%d?api_key=%s", baseURL, tmdbID, c.apiKey)
	res, err := c.client.Get(url)
	if err != nil {
		return "", err
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			fmt.Println(err)
		}
	}(res.Body)

	var details MediaDetails
	err = json.NewDecoder(res.Body).Decode(&details)
	if err != nil {
		fmt.Println(err)
		return "", err
	}
	return details.ImdbID, nil
}

// GetTVIMDBId returns the IMDB ID for a TV show by TMDB ID.
func (c *Client) GetTVIMDBId(tmdbID int) (string, error) {
	url := fmt.Sprintf("%s/tv/%d/external_ids?api_key=%s", baseURL, tmdbID, c.apiKey)
	res, err := c.client.Get(url)
	if err != nil {
		return "", err
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			log.Println(err)
		}
	}(res.Body)

	var ext TVExternalIds
	if err := json.NewDecoder(res.Body).Decode(&ext); err != nil {
		return "", err
	}
	return ext.ImdbID, nil
}

// GetSeasons returns the season list for a TV show (skipping specials season 0).
func (c *Client) GetSeasons(tmdbID int) ([]TVSeason, error) {
	url := fmt.Sprintf("%s/tv/%d?api_key=%s", baseURL, tmdbID, c.apiKey)
	res, err := c.client.Get(url)
	if err != nil {
		return nil, err
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			log.Println(err)
		}
	}(res.Body)

	var data struct {
		Seasons []TVSeason `json:"seasons"`
	}
	if err := json.NewDecoder(res.Body).Decode(&data); err != nil {
		return nil, err
	}

	// Filter out season 0 (specials) unless it's the only one
	var filtered []TVSeason
	for _, s := range data.Seasons {
		if s.SeasonNumber > 0 {
			if s.PosterPath != "" {
				s.PosterPath = imageBase + s.PosterPath
			}
			filtered = append(filtered, s)
		}
	}
	return filtered, nil
}

// GetEpisodes returns the episodes for a specific season of a TV show.
func (c *Client) GetEpisodes(tmdbID int, seasonNumber int) ([]TVEpisode, error) {
	url := fmt.Sprintf("%s/tv/%d/season/%d?api_key=%s", baseURL, tmdbID, seasonNumber, c.apiKey)
	res, err := c.client.Get(url)
	if err != nil {
		return nil, err
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			log.Println(err)
		}
	}(res.Body)

	var data struct {
		Episodes []TVEpisode `json:"episodes"`
	}
	if err := json.NewDecoder(res.Body).Decode(&data); err != nil {
		return nil, err
	}

	for i := range data.Episodes {
		if data.Episodes[i].StillPath != "" {
			data.Episodes[i].StillPath = stillBase + data.Episodes[i].StillPath
		}
	}
	return data.Episodes, nil
}

func (c *Client) GetImages(tmdbID int, mediaType string) (*MediaImages, error) {
	url := fmt.Sprintf("%s/%s/%d/images?api_key=%s", baseURL, mediaType, tmdbID, c.apiKey)

	res, err := c.client.Get(url)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API request failed with status code: %d", res.StatusCode)
	}

	var data MediaImages

	if err := json.NewDecoder(res.Body).Decode(&data); err != nil {
		return nil, err
	}

	for i := range data.Backdrops {
		data.Backdrops[i].URL = imageBaseOriginal + data.Backdrops[i].FilePath
	}

	for i := range data.Logos {
		data.Logos[i].URL = imageBase + data.Logos[i].FilePath
	}

	for i := range data.Posters {
		data.Posters[i].URL = imageBase + data.Posters[i].FilePath
	}

	return &data, nil
}

func (c *Client) GetVideos(tmdbID int, mediaType string) (*MediaVideos, error) {
	url := fmt.Sprintf("%s/%s/%d/videos?api_key=%s", baseURL, mediaType, tmdbID, c.apiKey)

	res, err := c.client.Get(url)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API request failed with status code: %d", res.StatusCode)
	}

	var data MediaVideos

	if err := json.NewDecoder(res.Body).Decode(&data); err != nil {
		return nil, err
	}

	for _, v := range data.Results {
		if v.Site == "YouTube" {
			v.EmbedURL = fmt.Sprintf("https://www.youtube.com/embed/%s", v.Key)
		}
	}

	return &data, nil
}

func (m *Media) DisplayTitle() string {
	if m.Title != "" {
		return m.Title
	}
	return m.Name
}

func (m *Media) DisplayDate() string {
	if m.Released != "" {
		return m.Released
	}
	return m.FirstAir
}

func (c *Client) GetDetails(tmdbID int, mediaType string) (*Details, error) {
	url := fmt.Sprintf("%s/%s/%d?api_key=%s&append_to_response=credits,release_dates,content_ratings,keywords,origin_country",
		baseURL, mediaType, tmdbID, c.apiKey)
	res, err := c.client.Get(url)
	if err != nil {
		return nil, err
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			log.Println(err)
		}
	}(res.Body)

	var details Details
	if err := json.NewDecoder(res.Body).Decode(&details); err != nil {
		return nil, err
	}
	if details.NextEpisodeToAir != nil && details.NextEpisodeToAir.StillPath != "" {
		details.NextEpisodeToAir.StillPath = stillBase + details.NextEpisodeToAir.StillPath
	}
	return &details, nil
}

// GetMediaByID fetches a single movie or TV show directly by ID. Exists so
// callers that only have a tmdb_id (e.g. a LibraryEntry) can get a fully-
// populated Media instead of reconstructing a partial one client-side.
func (c *Client) GetMediaByID(tmdbID int, mediaType string) (*Media, error) {
	url := fmt.Sprintf("%s/%s/%d?api_key=%s", baseURL, mediaType, tmdbID, c.apiKey)
	res, err := c.client.Get(url)
	if err != nil {
		return nil, err
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			log.Println(err)
		}
	}(res.Body)

	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API request failed with status code: %d", res.StatusCode)
	}

	var data Media
	if err := json.NewDecoder(res.Body).Decode(&data); err != nil {
		return nil, err
	}

	// /movie/{id} and /tv/{id} don't return media_type — unlike search
	// results, this is a single known-type lookup, so just set it directly.
	data.MediaType = mediaType
	if data.PosterURL != "" {
		data.PosterURL = imageBase + data.PosterURL
	}

	return &data, nil
}

func (d *Details) AgeRating() string {
	for _, r := range d.ReleaseDates.Results {
		if r.ISO31661 == "US" {
			for _, rd := range r.ReleaseDates {
				if rd.Certification != "" {
					return rd.Certification
				}
			}
		}
	}
	for _, r := range d.ContentRatings.Results {
		if r.ISO31661 == "US" && r.Rating != "" {
			return r.Rating
		}
	}
	return ""
}

func (d *Details) DisplayRuntime() string {
	if d.Runtime > 0 {
		return fmt.Sprintf("%dh %dm", d.Runtime/60, d.Runtime%60)
	}
	if len(d.EpisodeRunTime) > 0 {
		return fmt.Sprintf("%dm / ep", d.EpisodeRunTime[0])
	}
	return ""
}

func (d *Details) KeywordPairs() map[int]string {
	kws := d.Keywords.Keywords
	if len(kws) == 0 {
		kws = d.Keywords.Results
	}
	out := make(map[int]string, len(kws))
	for _, k := range kws {
		out[k.ID] = k.Name
	}
	return out
}

func (c *Client) GetSimilar(tmdbID int, mediaType string) ([]Media, error) {
	url := fmt.Sprintf("%s/%s/%d/recommendations?api_key=%s", baseURL, mediaType, tmdbID, c.apiKey)
	res, err := c.client.Get(url)
	if err != nil {
		return nil, err
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			log.Println(err)
		}
	}(res.Body)

	var data searchResponse
	if err := json.NewDecoder(res.Body).Decode(&data); err != nil {
		return nil, err
	}

	for i := range data.Results {
		data.Results[i].PosterURL = imageBase + data.Results[i].PosterURL
		data.Results[i].MediaType = mediaType
	}

	var filtered []Media
	for _, m := range data.Results {
		if m.PosterURL == imageBase {
			continue
		}
		filtered = append(filtered, m)
	}
	if len(filtered) > 12 {
		filtered = filtered[:12]
	}
	return filtered, nil
}

func (c *Client) GetLogos(tmdbID int, mediaType string) ([]string, error) {
	url := fmt.Sprintf("%s/%s/%d/images?api_key=%s&include_image_language=en,null", baseURL, mediaType, tmdbID, c.apiKey)
	res, err := c.client.Get(url)
	if err != nil {
		return nil, err
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			log.Println(err)
		}
	}(res.Body)

	var data struct {
		Logos []struct {
			FilePath string  `json:"file_path"`
			VoteAvg  float64 `json:"vote_average"`
		} `json:"logos"`
	}
	if err := json.NewDecoder(res.Body).Decode(&data); err != nil {
		return nil, err
	}

	var urls []string
	for i, l := range data.Logos {
		if i >= 3 {
			break
		}
		urls = append(urls, "https://image.tmdb.org/t/p/w500"+l.FilePath)
	}
	return urls, nil
}

// ── Discover ──────────────────────────────────────────────────────────────────

// DiscoverParams configures one /discover query. with_genres / with_keywords
// are OR'd (pipe) to broaden recall; without_genres is comma-joined to exclude
// all listed.
type DiscoverParams struct {
	MediaType      string // "movie" | "tv" (required)
	Page           int    // 1-based; 0 lets TMDB default to 1
	SortBy         string // default "popularity.desc"
	WithGenres     []int
	WithoutGenres  []int
	WithKeywords   []int
	MinVoteCount   float64
	MinVoteAverage float64
	IncludeAdult   bool
	Region         string
	CertCountry    string // movie-only; e.g. "US"
	CertLTE        string // movie-only; e.g. "PG"
}

type DiscoverResult struct {
	Results    []Media `json:"results"`
	Page       int     `json:"page"`
	TotalPages int     `json:"total_pages"`
}

func joinIDs(ids []int, sep string) string {
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = strconv.Itoa(id)
	}
	return strings.Join(parts, sep)
}

// Discover runs TMDB's /discover/{movie|tv}. TMDB concerns only — all
// personalization lives in the discover package.
func (c *Client) Discover(p DiscoverParams) (*DiscoverResult, error) {
	if p.MediaType != "movie" && p.MediaType != "tv" {
		return nil, fmt.Errorf("discover: invalid media type %q", p.MediaType)
	}

	q := neturl.Values{}
	q.Set("api_key", c.apiKey)
	q.Set("include_adult", strconv.FormatBool(p.IncludeAdult))
	sortBy := p.SortBy
	if sortBy == "" {
		sortBy = "popularity.desc"
	}
	q.Set("sort_by", sortBy)
	if p.Page > 0 {
		q.Set("page", strconv.Itoa(p.Page))
	}
	if len(p.WithGenres) > 0 {
		q.Set("with_genres", joinIDs(p.WithGenres, "|"))
	}
	if len(p.WithoutGenres) > 0 {
		q.Set("without_genres", joinIDs(p.WithoutGenres, ","))
	}
	if len(p.WithKeywords) > 0 {
		q.Set("with_keywords", joinIDs(p.WithKeywords, "|"))
	}
	if p.MinVoteCount > 0 {
		q.Set("vote_count.gte", strconv.FormatFloat(p.MinVoteCount, 'f', -1, 64))
	}
	if p.MinVoteAverage > 0 {
		q.Set("vote_average.gte", strconv.FormatFloat(p.MinVoteAverage, 'f', -1, 64))
	}
	if p.Region != "" {
		q.Set("region", p.Region)
	}
	if p.MediaType == "movie" && p.CertCountry != "" && p.CertLTE != "" {
		q.Set("certification_country", p.CertCountry)
		q.Set("certification.lte", p.CertLTE)
	}

	url := fmt.Sprintf("%s/discover/%s?%s", baseURL, p.MediaType, q.Encode())
	res, err := c.client.Get(url)
	if err != nil {
		return nil, err
	}
	defer func(Body io.ReadCloser) {
		if err := Body.Close(); err != nil {
			log.Println(err)
		}
	}(res.Body)

	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("discover: TMDB returned %d", res.StatusCode)
	}

	var data DiscoverResult
	if err := json.NewDecoder(res.Body).Decode(&data); err != nil {
		return nil, err
	}

	// Match the shape the rest of the app expects: absolute poster URLs and a
	// populated media_type (/discover omits it).
	for i := range data.Results {
		data.Results[i].MediaType = p.MediaType
		if data.Results[i].PosterURL != "" {
			data.Results[i].PosterURL = imageBase + data.Results[i].PosterURL
		}
	}
	return &data, nil
}

// GenreList returns TMDB's full genre list (id+name) for a media type — for
// browse UIs and the kid-mode genre picker. Reuses the Keyword {id,name} shape.
func (c *Client) GenreList(mediaType string) ([]Keyword, error) {
	if mediaType != "movie" && mediaType != "tv" {
		return nil, fmt.Errorf("genre list: invalid media type %q", mediaType)
	}
	url := fmt.Sprintf("%s/genre/%s/list?api_key=%s", baseURL, mediaType, c.apiKey)
	res, err := c.client.Get(url)
	if err != nil {
		return nil, err
	}
	defer func(Body io.ReadCloser) {
		if err := Body.Close(); err != nil {
			log.Println(err)
		}
	}(res.Body)

	var data struct {
		Genres []Keyword `json:"genres"`
	}
	if err := json.NewDecoder(res.Body).Decode(&data); err != nil {
		return nil, err
	}
	return data.Genres, nil
}

func (c *Client) SuggestKeywords(query string) ([]Keyword, error) {
	normalized := normalizeQuery(query)
	url := fmt.Sprintf("%s/search/keyword?api_key=%s&query=%s", baseURL, c.apiKey, normalized)
	res, err := c.client.Get(url)
	if err != nil {
		return nil, err
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			log.Println(err)
		}
	}(res.Body)

	var data struct {
		Results []Keyword `json:"results"`
	}
	if err := json.NewDecoder(res.Body).Decode(&data); err != nil {
		return nil, err
	}
	if len(data.Results) > 10 {
		data.Results = data.Results[:10]
	}
	return data.Results, nil
}

func (c *Client) SetupHandlers(mux *http.ServeMux, addonMgr *addons.Manager) {
	mux.HandleFunc("/api/keywords", utils.CorsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query().Get("q")
		if query == "" {
			http.Error(w, "missing query", http.StatusBadRequest)
			return
		}
		keywords, err := c.SuggestKeywords(query)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(keywords); err != nil {
			log.Println(err)
		}
	}))

	mux.HandleFunc("/api/search", utils.CorsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query().Get("q")
		if query == "" {
			http.Error(w, "missing query", http.StatusBadRequest)
			return
		}

		regular, err := c.Search(query)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		byKeyword, _ := c.SearchByKeywords(query)

		seen := make(map[string]bool)
		merged := make([]Media, 0, len(regular)+len(byKeyword))
		for _, m := range regular {
			key := fmt.Sprintf("%d-%s", m.ID, m.MediaType)
			seen[key] = true
			merged = append(merged, m)
		}
		for _, m := range byKeyword {
			key := fmt.Sprintf("%d-%s", m.ID, m.MediaType)
			if !seen[key] {
				seen[key] = true
				merged = append(merged, m)
			}
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(merged); err != nil {
			log.Println(err)
		}
	}))

	// GET /api/search/multi?q=<query> — sectioned results (titles split into
	// movies/tv, plus people and providers).
	mux.HandleFunc("/api/search/multi", utils.CorsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query().Get("q")
		if query == "" {
			http.Error(w, "missing query", http.StatusBadRequest)
			return
		}

		results, err := c.MultiSearch(query)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(results); err != nil {
			log.Println(err)
		}
	}))

	// GET /api/person?id=<personID> — bio + filmography for the person overlay.
	mux.HandleFunc("/api/person", utils.CorsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.Atoi(r.URL.Query().Get("id"))
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		person, err := c.GetPerson(id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(person); err != nil {
			log.Println(err)
		}
	}))

	// GET /api/provider?id=<providerID>&limit=<n> — popular titles on a provider
	// (US region). Blends movies and TV.
	mux.HandleFunc("/api/provider", utils.CorsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.Atoi(r.URL.Query().Get("id"))
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		limit := 40
		if l, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && l > 0 {
			limit = l
		}
		titles, err := c.ProviderTitles(id, limit)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(titles); err != nil {
			log.Println(err)
		}
	}))

	mux.HandleFunc("/api/images", utils.CorsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		tmdbIDStr := r.URL.Query().Get("id")
		mediaType := r.URL.Query().Get("type")

		if tmdbIDStr == "" || mediaType == "" {
			http.Error(w, "missing required parameters", http.StatusBadRequest)
			return
		}

		if mediaType != "movie" && mediaType != "tv" {
			http.Error(w, "invalid media type", http.StatusBadRequest)
			return
		}

		id, err := strconv.Atoi(tmdbIDStr)
		if err != nil || id <= 0 {
			http.Error(w, "invalid id format", http.StatusBadRequest)
			return
		}

		images, err := c.GetImages(id, mediaType)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")

		err = json.NewEncoder(w).Encode(images)
		if err != nil {
			log.Println(err)
		}
	}))

	mux.HandleFunc("/api/videos", utils.CorsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		tmdbIDStr := r.URL.Query().Get("id")
		mediaType := r.URL.Query().Get("type")

		if tmdbIDStr == "" || mediaType == "" {
			http.Error(w, "missing required parameters", http.StatusBadRequest)
			return
		}

		if mediaType != "movie" && mediaType != "tv" {
			http.Error(w, "invalid media type", http.StatusBadRequest)
			return
		}

		id, err := strconv.Atoi(tmdbIDStr)
		if err != nil || id <= 0 {
			http.Error(w, "invalid id format", http.StatusBadRequest)
			return
		}

		videos, err := c.GetVideos(id, mediaType)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")

		err = json.NewEncoder(w).Encode(videos)
		if err != nil {
			log.Println(err)
		}
	}))

	// GET /api/media?id=<tmdbID>&type=<movie|tv>
	// Returns a single, fully-populated Media object by ID — for callers that
	// only have a bare tmdb_id (e.g. from a LibraryEntry) and need the real
	// TMDB object rather than reconstructing a partial one client-side.
	mux.HandleFunc("/api/media", utils.CorsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		tmdbIDStr := r.URL.Query().Get("id")
		mediaType := r.URL.Query().Get("type")

		if tmdbIDStr == "" || mediaType == "" {
			http.Error(w, "missing required parameters", http.StatusBadRequest)
			return
		}
		if mediaType != "movie" && mediaType != "tv" {
			http.Error(w, "invalid media type", http.StatusBadRequest)
			return
		}

		id, err := strconv.Atoi(tmdbIDStr)
		if err != nil || id <= 0 {
			http.Error(w, "invalid id format", http.StatusBadRequest)
			return
		}

		media, err := c.GetMediaByID(id, mediaType)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(media); err != nil {
			log.Println(err)
		}
	}))

	mux.HandleFunc("/api/details", utils.CorsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		tmdbID := r.URL.Query().Get("id")
		mediaType := r.URL.Query().Get("type")
		id := 0
		_, err := fmt.Sscanf(tmdbID, "%d", &id)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		details, err := c.GetDetails(id, mediaType)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		err = json.NewEncoder(w).Encode(details)
		if err != nil {
			log.Println(err)
		}
	}))

	mux.HandleFunc("/api/similar", utils.CorsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.Atoi(r.URL.Query().Get("id"))
		mediaType := r.URL.Query().Get("type")
		results, err := c.GetSimilar(id, mediaType)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		err = json.NewEncoder(w).Encode(results)
		if err != nil {
			log.Println(err)
			return
		}
	}))

	mux.HandleFunc("/api/logos", utils.CorsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		tmdbID := r.URL.Query().Get("id")
		mediaType := r.URL.Query().Get("type")
		id := 0
		_, err := fmt.Sscanf(tmdbID, "%d", &id)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		logos, err := c.GetLogos(id, mediaType)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		err = json.NewEncoder(w).Encode(logos)
		if err != nil {
			log.Println(err)
		}
	}))

	mux.HandleFunc("/api/imdb", utils.CorsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		tmdbID := r.URL.Query().Get("id")
		id := 0
		_, err := fmt.Sscanf(tmdbID, "%d", &id)
		if err != nil {
			log.Println(err)
			return
		}
		imdbID, err := c.GetIMDBId(id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		err = json.NewEncoder(w).Encode(map[string]string{"imdb_id": imdbID})
		if err != nil {
			log.Println(err)
			return
		}
	}))

	// GET /api/tv/seasons?id=<tmdbID>
	// Returns the list of seasons for a TV show.
	mux.HandleFunc("/api/tv/seasons", utils.CorsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		tmdbID := r.URL.Query().Get("id")
		id := 0
		if _, err := fmt.Sscanf(tmdbID, "%d", &id); err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		seasons, err := c.GetSeasons(id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(seasons); err != nil {
			log.Println(err)
		}
	}))

	// GET /api/tv/episodes?id=<tmdbID>&season=<seasonNumber>
	// Returns the episodes for a given season of a TV show.
	mux.HandleFunc("/api/tv/episodes", utils.CorsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		tmdbID := r.URL.Query().Get("id")
		seasonStr := r.URL.Query().Get("season")
		id := 0
		if _, err := fmt.Sscanf(tmdbID, "%d", &id); err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		season, err := strconv.Atoi(seasonStr)
		if err != nil || season < 1 {
			http.Error(w, "invalid season", http.StatusBadRequest)
			return
		}
		episodes, err := c.GetEpisodes(id, season)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(episodes); err != nil {
			log.Println(err)
		}
	}))

	mux.HandleFunc("/api/quality/batch", utils.CorsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		idsParam := r.URL.Query().Get("ids")
		if idsParam == "" {
			http.Error(w, "missing ids", http.StatusBadRequest)
			return
		}

		idStrs := strings.Split(idsParam, ",")
		sem := make(chan struct{}, 5)

		type entry struct {
			ID      string `json:"id"`
			Quality string `json:"quality"`
		}

		w.Header().Set("Content-Type", "application/x-ndjson")
		w.Header().Set("X-Accel-Buffering", "no")
		flusher, canFlush := w.(http.Flusher)

		var mu sync.Mutex
		var wg sync.WaitGroup
		enc := json.NewEncoder(w)

		for _, s := range idStrs {
			id, err := strconv.Atoi(strings.TrimSpace(s))
			if err != nil {
				continue
			}
			wg.Add(1)
			go func(tmdbID int) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()

				imdbID, err := c.GetIMDBId(tmdbID)
				if err != nil || imdbID == "" {
					return
				}
				streams, err := addonMgr.GetAllStreams("movie", imdbID)
				if err != nil || len(streams) == 0 {
					return
				}
				q := addons.GetMaxQuality(streams)
				if q == "" {
					return
				}
				mu.Lock()
				err = enc.Encode(entry{ID: strconv.Itoa(tmdbID), Quality: q})
				if err != nil {
					log.Println(err)
				}
				if canFlush {
					flusher.Flush()
				}
				mu.Unlock()
			}(id)
		}

		wg.Wait()
	}))
}
