package discover

import (
	"encoding/json"
	"log"
	"math"
	"net/http"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/Arcadyi/cove/internal/library"
	"github.com/Arcadyi/cove/internal/tmdb"
	"github.com/Arcadyi/cove/internal/utils"
)

// Service ── Personalized discovery ──────────────────────────────────────────
//
// Service turns the user's library (what they finished, rated, dropped) into a
// taste profile, then drives TMDB's /discover endpoint with it: biasing toward
// genres/keywords they enjoy, away from ones they don't, and filtering out
// anything already in their library. It depends on tmdb (raw API) and a
// TasteProvider (the library); neither imports this package, so there's no
// import cycle.
type Service struct {
	tmdb *tmdb.Client
	lib  TasteProvider

	// A recommendation page fans out into many rows (top-genres + one row per
	// genre), each of which needs the taste profile. Cache it for a short TTL so
	// we build it once per page instead of once per row.
	mu        sync.Mutex
	cached    *Profile
	cachedAt  time.Time
	ttl       time.Duration
	cachedGen uint64
}

// TasteProvider is the slice of the library this package needs. *library.Library
// satisfies it. Defining it as an interface keeps discovery testable without a
// real library on disk.
type TasteProvider interface {
	TasteSignals() []library.TasteSignal
	Generation() uint64
}

func New(t *tmdb.Client, lib TasteProvider) *Service {
	return &Service{tmdb: t, lib: lib, ttl: 5 * time.Minute}
}

// profile returns a recently-built profile, rebuilding at most once per ttl.
// Slightly stale taste is fine; the win is not re-fetching Details for the
// whole library on every row of a discovery page.
func (s *Service) profile() *Profile {
	gen := s.lib.Generation()

	s.mu.Lock()
	if s.cached != nil && s.cachedGen == gen && time.Since(s.cachedAt) < s.ttl {
		p := s.cached
		s.mu.Unlock()
		return p
	}
	s.mu.Unlock()

	p := s.BuildProfile()

	s.mu.Lock()
	s.cached = p
	s.cachedGen = gen
	s.cachedAt = time.Now()
	s.mu.Unlock()
	return p
}

// Insights is the taste-side view for the profile page.
type Insights struct {
	TopMovieGenres []Taste `json:"top_movie_genres"`
	TopTVGenres    []Taste `json:"top_tv_genres"`
	DislikedGenres []Taste `json:"disliked_genres"`
	TopKeywords    []Taste `json:"top_keywords"`
	SignalsUsed    int     `json:"signals_used"`
}

func (s *Service) Insights() Insights {
	prof := s.profile()
	return Insights{
		TopMovieGenres: capTaste(positiveTop(rankTaste(prof.GenreScoresMovie, prof.GenreNames)), 8),
		TopTVGenres:    capTaste(positiveTop(rankTaste(prof.GenreScoresTV, prof.GenreNames)), 8),
		DislikedGenres: dislikedTaste(prof, 8),
		TopKeywords:    capTaste(positiveTop(rankTaste(prof.KeywordScores, prof.KeywordNames)), 12),
		SignalsUsed:    prof.Contributors,
	}
}

// dislikedTaste pools net-negative genre scores across both type maps, most
// disliked first.
func dislikedTaste(p *Profile, limit int) []Taste {
	merged := map[int]float64{}
	for id, sc := range p.GenreScoresMovie {
		if sc < 0 {
			merged[id] += sc
		}
	}
	for id, sc := range p.GenreScoresTV {
		if sc < 0 {
			merged[id] += sc
		}
	}
	out := make([]Taste, 0, len(merged))
	for id, sc := range merged {
		out = append(out, Taste{ID: id, Name: p.GenreNames[id], Score: sc})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Score < out[j].Score })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

// ── Content policy (powers the future kid-friendly profile) ───────────────────

// ContentPolicy gates what may be recommended. The adult default simply omits
// porn (include_adult=false); the kid policy additionally caps movie
// certification and blocks mature genres.
type ContentPolicy struct {
	IncludeAdult  bool   // false => include_adult=false on every query
	MaxMovieCert  string // e.g. "PG"; movie-only (TMDB has no TV cert filter on /discover)
	CertCountry   string // e.g. "US"; required alongside MaxMovieCert
	BlockedGenres []int  // merged into without_genres
}

// AdultPolicy is the default for the normal profile: no porn, everything else allowed.
var AdultPolicy = ContentPolicy{IncludeAdult: false}

// KidPolicy is a starting point for the kid-friendly profile. Genre IDs below
// are TMDB *movie* genre IDs (Horror=27, Thriller=53, War=10752, Crime=80).
// NOTE: TMDB TV genre IDs differ and /discover/tv has no certification filter,
// so for TV this leans on include_adult + blocked genres only.
var KidPolicy = ContentPolicy{
	IncludeAdult:  false,
	MaxMovieCert:  "PG",
	CertCountry:   "US",
	BlockedGenres: []int{27, 53, 10752, 80},
}

func policyFromName(name string) ContentPolicy {
	if name == "kid" {
		return KidPolicy
	}
	return AdultPolicy
}

// ── Taste profile ─────────────────────────────────────────────────────────────

// Taste is one ranked genre or keyword with the score it accumulated.
type Taste struct {
	ID    int     `json:"id"`
	Name  string  `json:"name"`
	Score float64 `json:"score"`
}

// Profile is the aggregated signal derived from the whole library.
//
// Genre IDs are namespaced per media type on TMDB — Horror (27) is a movie
// genre with no TV equivalent, TV has Sci-Fi & Fantasy (10765) where movies
// split Sci-Fi and Fantasy, etc. So genre scores are kept in separate maps and
// a movie's genres only ever inform movie recommendations (and likewise TV).
// Keyword IDs share one namespace across both types, so those stay pooled.
type Profile struct {
	GenreScoresMovie map[int]float64
	GenreScoresTV    map[int]float64
	KeywordScores    map[int]float64
	GenreNames       map[int]string // shared-ID genres carry the same name in both namespaces
	KeywordNames     map[int]string
	// Already-in-library IDs, split by type so a movie and a show that happen to
	// share an ID don't collide. Everything here is excluded from results.
	ExcludeMovie map[int]bool
	ExcludeTV    map[int]bool
	Contributors int
	// MovieShare is the fraction of what the user actually watches (finished or
	// currently watching) that is movies, 0..1. Drives the blended-feed mix;
	// defaults to 0.5 when there's no watch history yet.
	MovieShare float64
}

// genreScores returns the genre-score map for the given media type.
func (p *Profile) genreScores(mediaType string) map[int]float64 {
	if mediaType == "tv" {
		return p.GenreScoresTV
	}
	return p.GenreScoresMovie
}

func (p *Profile) excluded(mediaType string, id int) bool {
	if mediaType == "tv" {
		return p.ExcludeTV[id]
	}
	return p.ExcludeMovie[id]
}

// signalWeight turns one library signal into a single like/dislike weight.
// Positive = enjoyed, negative = disliked. Status and an explicit user rating
// stack: a finished title rated 5 is a much stronger signal than a finished
// title with no rating.
func signalWeight(s library.TasteSignal) float64 {
	var w float64
	switch s.Status {
	case library.StatusFinished:
		w += 1.5
	case library.StatusDropped:
		w -= 2.0
	case library.StatusWatching:
		w += 0.5
	case library.StatusWatchLater:
		// Saving a title for later is a deliberate "this interests me" — a mild
		// positive, on par with currently-watching, so its genres/keywords nudge
		// recommendations toward similar things.
		w += 0.5
	}
	if s.Dismissed {
		w -= 2.0
	}
	// A completed playthrough that isn't (yet) marked finished still counts as
	// "watched to the end" — a positive signal.
	if s.Completed && s.Status != library.StatusFinished {
		w += 1.0
	}
	// Rating is on a 0–5 scale; 3 is neutral. Each point away from neutral is
	// worth 1.5, so 5 => +3, 4 => +1.5, 2 => -1.5, 1 => -3.
	if s.UserRating != nil {
		w += (*s.UserRating - 3.0) * 1.5
	}
	return w
}

// BuildProfile reads every library signal, excludes those titles from future
// results, and (for titles with a meaningful like/dislike) fetches TMDB Details
// to attribute that weight across the title's genres and keywords.
//
// Details lookups run concurrently with a small worker pool — the same pattern
// as /api/quality/batch. For large libraries this is the hot path; a TTL cache
// on GetDetails would be the natural next optimization.
func (s *Service) BuildProfile() *Profile {
	signals := s.lib.TasteSignals()

	p := &Profile{
		GenreScoresMovie: map[int]float64{},
		GenreScoresTV:    map[int]float64{},
		KeywordScores:    map[int]float64{},
		GenreNames:       map[int]string{},
		KeywordNames:     map[int]string{},
		ExcludeMovie:     map[int]bool{},
		ExcludeTV:        map[int]bool{},
	}

	type job struct {
		id        int
		mediaType string
		weight    float64
	}
	var jobs []job

	// Consumption mix for the blended feed: count only what the user actually
	// watches (finished or currently watching), not saved-for-later or dropped.
	var engagedMovie, engagedTV int

	for _, sig := range signals {
		// Exclude everything in the library regardless of weight: we never want
		// to "discover" something the user is already watching, finished,
		// dropped, or saved for later.
		if sig.MediaType == "tv" {
			p.ExcludeTV[sig.TmdbID] = true
		} else {
			p.ExcludeMovie[sig.TmdbID] = true
		}

		if sig.Status == library.StatusFinished || sig.Status == library.StatusWatching {
			if sig.MediaType == "tv" {
				engagedTV++
			} else {
				engagedMovie++
			}
		}

		w := signalWeight(sig)
		// Skip near-neutral signals — they'd cost a Details call but move no
		// scores (a positive status cancelled out by a below-neutral rating).
		if w > -0.5 && w < 0.5 {
			continue
		}
		jobs = append(jobs, job{sig.TmdbID, sig.MediaType, w})
	}

	p.Contributors = len(jobs)

	// Lean the blended feed toward whichever type the user watches more; even
	// split when there's nothing to go on yet.
	if total := engagedMovie + engagedTV; total > 0 {
		p.MovieShare = float64(engagedMovie) / float64(total)
	} else {
		p.MovieShare = 0.5
	}

	var (
		mu  sync.Mutex
		wg  sync.WaitGroup
		sem = make(chan struct{}, 6)
	)
	for _, j := range jobs {
		wg.Add(1)
		go func(j job) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			d, err := s.tmdb.GetDetails(j.id, j.mediaType)
			if err != nil {
				log.Println("discover: profile details:", err)
				return
			}

			mu.Lock()
			// A movie's genres only inform movie recs; a show's only TV recs.
			genreTarget := p.GenreScoresMovie
			if j.mediaType == "tv" {
				genreTarget = p.GenreScoresTV
			}
			for _, g := range d.Genres {
				genreTarget[g.ID] += j.weight
				p.GenreNames[g.ID] = g.Name
			}
			for id, name := range d.KeywordPairs() {
				p.KeywordScores[id] += j.weight
				p.KeywordNames[id] = name
			}
			mu.Unlock()
		}(j)
	}
	wg.Wait()

	return p
}

// ── Ranking helpers ───────────────────────────────────────────────────────────

func rankTaste(scores map[int]float64, names map[int]string) []Taste {
	out := make([]Taste, 0, len(scores))
	for id, sc := range scores {
		out = append(out, Taste{ID: id, Name: names[id], Score: sc})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	return out
}

// positiveTopIDs returns the IDs of the highest-scoring (net-positive) entries.
func positiveTopIDs(scores map[int]float64, limit int) []int {
	ranked := rankTaste(scores, nil)
	ids := make([]int, 0, limit)
	for _, t := range ranked {
		if t.Score <= 0 || len(ids) >= limit {
			break
		}
		ids = append(ids, t.ID)
	}
	return ids
}

// dislikedIDs returns IDs whose net score is at or below threshold (negative).
func dislikedIDs(scores map[int]float64, threshold float64) []int {
	var ids []int
	for id, sc := range scores {
		if sc <= threshold {
			ids = append(ids, id)
		}
	}
	return ids
}

// ── Public discovery API ──────────────────────────────────────────────────────

// Recommend returns a personalized feed for the given media type. It biases
// toward the user's top genres, excludes their disliked genres and anything
// already in their library, then falls back to a popularity browse if the
// library is too sparse to fill the page (so the row is never empty for a new
// user).
func (s *Service) Recommend(mediaType string, policy ContentPolicy, limit int) []tmdb.Media {
	if limit <= 0 {
		limit = 20
	}
	prof := s.profile()

	scores := prof.genreScores(mediaType)
	likedGenres := positiveTopIDs(scores, 3)
	disliked := dislikedIDs(scores, -1.0)

	out := s.collect(tmdb.DiscoverParams{
		MediaType:     mediaType,
		SortBy:        "popularity.desc",
		WithGenres:    likedGenres,
		WithoutGenres: disliked,
		MinVoteCount:  voteFloor(mediaType),
	}, prof, policy, limit, 6)

	if len(out) < limit {
		seen := make(map[int]bool, len(out))
		for _, m := range out {
			seen[m.ID] = true
		}
		fallback := s.collect(tmdb.DiscoverParams{
			MediaType:     mediaType,
			SortBy:        "popularity.desc",
			WithoutGenres: disliked,
			MinVoteCount:  voteFloor(mediaType),
		}, prof, policy, limit, 6)
		for _, m := range fallback {
			if seen[m.ID] {
				continue
			}
			out = append(out, m)
			if len(out) >= limit {
				break
			}
		}
	}
	return out
}

// RecommendMixed returns one feed blending movies and TV, leaning toward the
// type the user actually watches more (see Profile.MovieShare) rather than a
// flat 50/50. TMDB popularity isn't comparable across media types (movies score
// higher), so a straight popularity merge would bury the TV; an interleave keyed
// to the consumption mix keeps the proportions intentional. Both halves draw on
// their own genre scores, so the picks stay personalized. The taste profile is
// built once and shared via the profile cache.
func (s *Service) RecommendMixed(policy ContentPolicy, limit int) []tmdb.Media {
	if limit <= 0 {
		limit = 20
	}
	movieShare := s.profile().MovieShare

	// Split the page by the consumption mix (e.g. 65% movies → ~13 of 20).
	movieN := int(math.Round(float64(limit) * movieShare))
	if movieN < 0 {
		movieN = 0
	}
	if movieN > limit {
		movieN = limit
	}
	tvN := limit - movieN

	// Over-fetch each side so the weighted merge can still reach `limit` if one
	// side comes back thin (e.g. a library that's almost all movies).
	const buf = 3
	movies := s.Recommend("movie", policy, movieN+buf)
	shows := s.Recommend("tv", policy, tvN+buf)
	return mergeWeighted(movies, shows, movieShare, limit)
}

// mergeWeighted interleaves movies and shows so the running movie share tracks
// `movieShare` (0..1) throughout the list — not merely in the final totals —
// while still drawing from whichever side has material left. At each slot it
// takes whichever choice lands the running share closest to target.
func mergeWeighted(movies, shows []tmdb.Media, movieShare float64, limit int) []tmdb.Media {
	out := make([]tmdb.Media, 0, limit)
	mi, si, mUsed := 0, 0, 0
	for len(out) < limit && (mi < len(movies) || si < len(shows)) {
		moviesLeft := mi < len(movies)
		showsLeft := si < len(shows)

		takeMovie := moviesLeft
		if moviesLeft && showsLeft {
			total := len(out)
			withMovie := math.Abs(movieShare - float64(mUsed+1)/float64(total+1))
			withShow := math.Abs(movieShare - float64(mUsed)/float64(total+1))
			takeMovie = withMovie <= withShow
		}

		if takeMovie {
			out = append(out, movies[mi])
			mi++
			mUsed++
		} else {
			out = append(out, shows[si])
			si++
		}
	}
	return out
}

// DiscoverByGenre recommends within a single genre (e.g. for a "Because you
// like Horror" row). Library exclusions still apply.
func (s *Service) DiscoverByGenre(mediaType string, genreID int, policy ContentPolicy, limit int) []tmdb.Media {
	if limit <= 0 {
		limit = 20
	}
	prof := s.profile()
	return s.collect(tmdb.DiscoverParams{
		MediaType:    mediaType,
		SortBy:       "popularity.desc",
		WithGenres:   []int{genreID},
		MinVoteCount: voteFloor(mediaType),
	}, prof, policy, limit, 6)
}

// DiscoverByKeyword recommends by a single keyword (e.g. "post-apocalyptic").
// Library exclusions still apply.
func (s *Service) DiscoverByKeyword(mediaType string, keywordID int, policy ContentPolicy, limit int) []tmdb.Media {
	if limit <= 0 {
		limit = 20
	}
	prof := s.profile()
	return s.collect(tmdb.DiscoverParams{
		MediaType:    mediaType,
		SortBy:       "popularity.desc",
		WithKeywords: []int{keywordID},
		MinVoteCount: voteFloor(mediaType),
	}, prof, policy, limit, 6)
}

// TopGenres returns the genres the user enjoys most for a media type
// (net-positive only), each with a display name — ready to drive per-genre
// recommendation rows. Type-scoped because TMDB genre IDs differ between movies
// and TV.
func (s *Service) TopGenres(mediaType string, limit int) []Taste {
	prof := s.profile()
	ranked := rankTaste(prof.genreScores(mediaType), prof.GenreNames)
	return capTaste(positiveTop(ranked), limit)
}

// TopKeywords returns the keywords most associated with what the user enjoys.
// Keywords share one namespace across movies and TV, so this isn't type-scoped.
func (s *Service) TopKeywords(limit int) []Taste {
	prof := s.profile()
	ranked := rankTaste(prof.KeywordScores, prof.KeywordNames)
	return capTaste(positiveTop(ranked), limit)
}

func positiveTop(ranked []Taste) []Taste {
	out := make([]Taste, 0, len(ranked))
	for _, t := range ranked {
		if t.Score <= 0 {
			break
		}
		out = append(out, t)
	}
	return out
}

func capTaste(t []Taste, limit int) []Taste {
	if limit > 0 && len(t) > limit {
		return t[:limit]
	}
	return t
}

// ── Internals ─────────────────────────────────────────────────────────────────

// collect pages through /discover, applying the content policy, dropping
// excluded/already-seen/poster-less results, until it has `limit` items or runs
// out of pages (capped at maxPages so a thin genre can't spin forever).
func (s *Service) collect(base tmdb.DiscoverParams, prof *Profile, policy ContentPolicy, limit, maxPages int) []tmdb.Media {
	applyPolicy(&base, policy)

	seen := map[int]bool{}
	out := make([]tmdb.Media, 0, limit)

	start := base.Page
	if start < 1 {
		start = 1
	}
	for page := start; page < start+maxPages; page++ {
		base.Page = page
		res, err := s.tmdb.Discover(base)
		if err != nil {
			log.Println("discover:", err)
			break
		}
		for _, m := range res.Results {
			if m.PosterURL == "" || seen[m.ID] {
				continue
			}
			if prof != nil && prof.excluded(base.MediaType, m.ID) {
				continue
			}
			if m.Adult && !policy.IncludeAdult {
				continue
			}
			seen[m.ID] = true
			out = append(out, m)
			if len(out) >= limit {
				return out
			}
		}
		if res.TotalPages <= page {
			break
		}
	}
	return out
}

func applyPolicy(p *tmdb.DiscoverParams, policy ContentPolicy) {
	p.IncludeAdult = policy.IncludeAdult
	if len(policy.BlockedGenres) > 0 {
		p.WithoutGenres = append(p.WithoutGenres, policy.BlockedGenres...)
	}
	if p.MediaType == "movie" && policy.MaxMovieCert != "" && policy.CertCountry != "" {
		p.CertCountry = policy.CertCountry
		p.CertLTE = policy.MaxMovieCert
	}
}

// voteFloor keeps obscure, barely-rated titles out of recommendations. Movies
// have far more votes than TV on TMDB, so the floors differ.
func voteFloor(mediaType string) float64 {
	if mediaType == "tv" {
		return 40
	}
	return 100
}

// ── HTTP handlers ─────────────────────────────────────────────────────────────

func (s *Service) SetupHandlers() {
	http.HandleFunc("/api/discover", utils.CorsMiddleware(s.handleRecommend))
	http.HandleFunc("/api/discover/genres", utils.CorsMiddleware(s.handleTopGenres))
	http.HandleFunc("/api/discover/keywords", utils.CorsMiddleware(s.handleTopKeywords))
	http.HandleFunc("/api/discover/genre", utils.CorsMiddleware(s.handleByGenre))
	http.HandleFunc("/api/discover/keyword", utils.CorsMiddleware(s.handleByKeyword))
	// Static TMDB genre list (id+name) for building browse UIs / kid-mode pickers.
	http.HandleFunc("/api/genres", utils.CorsMiddleware(s.handleGenreList))
	http.HandleFunc("/api/discover/insights", utils.CorsMiddleware(s.handleInsights))
}

// GET /api/discover?type=movie|tv|all&limit=20&profile=kid
func (s *Service) handleRecommend(w http.ResponseWriter, r *http.Request) {
	policy := policyFromName(r.URL.Query().Get("profile"))
	limit := limitParam(r, 20, 60)

	switch t := r.URL.Query().Get("type"); t {
	case "all", "both":
		writeJSON(w, s.RecommendMixed(policy, limit))
	case "movie", "tv":
		writeJSON(w, s.Recommend(t, policy, limit))
	default:
		http.Error(w, "type must be movie, tv, or all", http.StatusBadRequest)
	}
}

// GET /api/discover/genre?type=movie&genre=27&limit=20&profile=kid
func (s *Service) handleByGenre(w http.ResponseWriter, r *http.Request) {
	mt, ok := mediaTypeParam(r)
	if !ok {
		http.Error(w, "type must be movie or tv", http.StatusBadRequest)
		return
	}
	genreID, err := strconv.Atoi(r.URL.Query().Get("genre"))
	if err != nil || genreID <= 0 {
		http.Error(w, "invalid genre", http.StatusBadRequest)
		return
	}
	policy := policyFromName(r.URL.Query().Get("profile"))
	writeJSON(w, s.DiscoverByGenre(mt, genreID, policy, limitParam(r, 20, 60)))
}

// GET /api/discover/keyword?type=tv&keyword=4565&limit=20
func (s *Service) handleByKeyword(w http.ResponseWriter, r *http.Request) {
	mt, ok := mediaTypeParam(r)
	if !ok {
		http.Error(w, "type must be movie or tv", http.StatusBadRequest)
		return
	}
	keywordID, err := strconv.Atoi(r.URL.Query().Get("keyword"))
	if err != nil || keywordID <= 0 {
		http.Error(w, "invalid keyword", http.StatusBadRequest)
		return
	}
	policy := policyFromName(r.URL.Query().Get("profile"))
	writeJSON(w, s.DiscoverByKeyword(mt, keywordID, policy, limitParam(r, 20, 60)))
}

// GET /api/discover/genres?type=movie — top genres for the user, per media type
func (s *Service) handleTopGenres(w http.ResponseWriter, r *http.Request) {
	mt, ok := mediaTypeParam(r)
	if !ok {
		http.Error(w, "type must be movie or tv", http.StatusBadRequest)
		return
	}
	writeJSON(w, s.TopGenres(mt, limitParam(r, 8, 50)))
}

// GET /api/discover/keywords — top keywords for the user
func (s *Service) handleTopKeywords(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.TopKeywords(limitParam(r, 12, 50)))
}

// GET /api/genres?type=movie — TMDB's full genre list (id+name)
func (s *Service) handleGenreList(w http.ResponseWriter, r *http.Request) {
	mt, ok := mediaTypeParam(r)
	if !ok {
		http.Error(w, "type must be movie or tv", http.StatusBadRequest)
		return
	}
	genres, err := s.tmdb.GenreList(mt)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, genres)
}

func (s *Service) handleInsights(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.Insights())
}

// ── tiny request/response helpers ─────────────────────────────────────────────

func mediaTypeParam(r *http.Request) (string, bool) {
	mt := r.URL.Query().Get("type")
	return mt, mt == "movie" || mt == "tv"
}

func limitParam(r *http.Request, def, max int) int {
	v, err := strconv.Atoi(r.URL.Query().Get("limit"))
	if err != nil || v <= 0 {
		return def
	}
	if v > max {
		return max
	}
	return v
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Println("discover json encode:", err)
	}
}
