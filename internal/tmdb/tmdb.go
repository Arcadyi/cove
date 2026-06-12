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
	"unicode"

	"golang.org/x/text/unicode/norm"
)

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
	Images     []string `json:"images"`
	Popularity float64  `json:"popularity"`
}

type MediaDetails struct {
	ImdbID string `json:"imdb_id"`
}

type Details struct {
	Genres []struct {
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
			Name string `json:"name"`
		} `json:"keywords"` // movies
		Results []struct {
			Name string `json:"name"`
		} `json:"results"` // tv shows
	} `json:"keywords"`
	OriginCountry []string `json:"origin_country"`
}

type searchResponse struct {
	Results []Media `json:"results"`
}

type Keyword struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type scoredMedia struct {
	media Media
	score float64
}

const baseURL = "https://api.themoviedb.org/3"
const imageBase = "https://image.tmdb.org/t/p/w500"

func SearchByKeywords(query string, apiKey string) ([]Media, error) {
	normalized := normalizeQuery(query)
	kwURL := fmt.Sprintf("%s/search/keyword?api_key=%s&query=%s", baseURL, apiKey, normalized)
	res, err := http.Get(kwURL)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

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

	// Use up to 3 keyword IDs with OR logic
	ids := make([]string, 0, 3)
	for i := 0; i < len(kwData.Results) && i < 3; i++ {
		ids = append(ids, strconv.Itoa(kwData.Results[i].ID))
	}
	kwParam := strings.Join(ids, "|")

	var results []Media
	for _, mediaType := range []string{"movie", "tv"} {
		discURL := fmt.Sprintf("%s/discover/%s?api_key=%s&with_keywords=%s&sort_by=popularity.desc",
			baseURL, mediaType, apiKey, kwParam)
		r, err := http.Get(discURL)
		if err != nil {
			continue
		}
		var data searchResponse
		json.NewDecoder(r.Body).Decode(&data)
		r.Body.Close()

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
	// Normalize unicode (WALL·E -> WALL·E stays, but composed chars get decomposed)
	q = norm.NFC.String(q)

	// Replace hyphens, dots, middle dots with spaces
	q = strings.Map(func(r rune) rune {
		if r == '-' || r == '.' || r == '·' || r == '_' {
			return ' '
		}
		return r
	}, q)

	// Collapse multiple spaces
	q = strings.Join(strings.FieldsFunc(q, unicode.IsSpace), " ")

	return strings.TrimSpace(q)
}

func queryVariants(q string) []string {
	seen := map[string]bool{q: true}
	variants := []string{q}

	// "wall e" - spaces instead of punctuation
	normalized := normalizeQuery(q)
	if !seen[normalized] {
		seen[normalized] = true
		variants = append(variants, normalized)
	}

	// "walle" - letters/digits only, no spaces
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

func Search(query string, apiKey string) ([]Media, error) {
	// Weight by variant: exact query > normalized > stripped
	variantBoost := []float64{3.0, 1.5, 1.0}

	seen := make(map[int]bool)
	var scored []scoredMedia

	for vi, variant := range queryVariants(query) {
		boost := variantBoost[vi]
		encoded := neturl.QueryEscape(variant)

		for _, mediaType := range []string{"movie", "tv"} {
			url := fmt.Sprintf("%s/search/%s?api_key=%s&query=%s", baseURL, mediaType, apiKey, encoded)
			res, err := http.Get(url)
			if err != nil {
				continue
			}
			var data searchResponse
			json.NewDecoder(res.Body).Decode(&data)
			res.Body.Close()

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

func GetIMDBId(tmdbID int, apiKey string) (string, error) {
	url := fmt.Sprintf("%s/movie/%d?api_key=%s", baseURL, tmdbID, apiKey)
	res, err := http.Get(url)
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

func GetTrailer(tmdbID int, mediaType string, apiKey string) (string, error) {
	url := fmt.Sprintf("%s/%s/%d/videos?api_key=%s", baseURL, mediaType, tmdbID, apiKey)
	res, err := http.Get(url)
	if err != nil {
		return "", err
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			log.Println(err)
		}
	}(res.Body)

	var data struct {
		Results []struct {
			Key  string `json:"key"`
			Type string `json:"type"`
			Site string `json:"site"`
		} `json:"results"`
	}
	if err := json.NewDecoder(res.Body).Decode(&data); err != nil {
		return "", err
	}
	for _, v := range data.Results {
		if v.Type == "Trailer" && v.Site == "YouTube" {
			return fmt.Sprintf("https://www.youtube.com/embed/%s", v.Key), nil
		}
	}
	return "", nil
}

func GetImages(tmdbID int, mediaType string, apiKey string) ([]string, error) {
	url := fmt.Sprintf("%s/%s/%d/images?api_key=%s", baseURL, mediaType, tmdbID, apiKey)
	res, err := http.Get(url)
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
		Backdrops []struct {
			FilePath string `json:"file_path"`
		} `json:"backdrops"`
	}
	if err := json.NewDecoder(res.Body).Decode(&data); err != nil {
		return nil, err
	}

	var urls []string
	for i, b := range data.Backdrops {
		if i >= 5 {
			break
		} // limit to 5
		urls = append(urls, imageBase+b.FilePath)
	}
	return urls, nil
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

func GetDetails(tmdbID int, mediaType string, apiKey string) (*Details, error) {
	url := fmt.Sprintf("%s/%s/%d?api_key=%s&append_to_response=credits,release_dates,content_ratings,keywords, origin_country",
		baseURL, mediaType, tmdbID, apiKey)
	res, err := http.Get(url)
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
	return &details, nil
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

func (d *Details) KeywordNames() []string {
	keywords := d.Keywords.Keywords
	if len(keywords) == 0 {
		keywords = d.Keywords.Results
	}
	names := make([]string, 0, min(6, len(keywords)))
	for i, k := range keywords {
		if i >= 6 {
			break
		}
		names = append(names, k.Name)
	}
	return names
}

func GetSimilar(tmdbID int, mediaType string, apiKey string) ([]Media, error) {
	url := fmt.Sprintf("%s/%s/%d/similar?api_key=%s", baseURL, mediaType, tmdbID, apiKey)
	res, err := http.Get(url)
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

	// filter out results without posters
	var filtered []Media
	for _, m := range data.Results {
		if m.PosterURL == imageBase {
			continue
		}
		filtered = append(filtered, m)
	}
	if len(filtered) > 6 {
		filtered = filtered[:6]
	}
	return filtered, nil
}

func GetLogos(tmdbID int, mediaType string, apiKey string) ([]string, error) {
	url := fmt.Sprintf("%s/%s/%d/images?api_key=%s&include_image_language=en,null", baseURL, mediaType, tmdbID, apiKey)
	res, err := http.Get(url)
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

func SuggestKeywords(query string, apiKey string) ([]Keyword, error) {
	normalized := normalizeQuery(query)
	url := fmt.Sprintf("%s/search/keyword?api_key=%s&query=%s", baseURL, apiKey, normalized)
	res, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

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
