# API reference

All routes are served by the Go backend on `:6969`, registered on a single
shared `http.DefaultServeMux` (`main.go`). Every route below is wrapped in
`utils.CorsMiddleware`, which **auto-answers every `OPTIONS` request with
204** before the wrapped handler runs — if you see a `case
http.MethodOptions:` branch inside a handler in the source, it's dead code,
unreachable in practice, and not reproduced here.

Unless noted otherwise, request/response bodies are JSON. Struct fields below
use their JSON tag names (camelCase or snake_case as the Go struct declares).

See [ARCHITECTURE.md](../ARCHITECTURE.md) for how these packages fit together
and what the OSS vs. proprietary build split means.

## `internal/tmdb` — metadata (`internal/tmdb/tmdb.go`)

Always compiled identically; no build-tag variance. None of these routes
enforce an HTTP method — any verb works, GET by convention.

| Route | Query params | Response |
|---|---|---|
| `GET /api/keywords` | `q` (required) | `[]Keyword{id, name}` |
| `GET /api/search` | `q` (required) | `[]Media` (regular + keyword search, merged/deduped) |
| `GET /api/search/multi` | `q` (required) | `SearchResults{movies, tv []Media, people []Person, providers []Provider}` |
| `GET /api/person` | `id` (required int) | `PersonDetails{id, name, biography, profile_path, known_for_department, birthday, place_of_birth, credits []Media}` |
| `GET /api/provider` | `id` (required int), `limit` (default 40) | `[]Media` (blended movie+tv titles for that provider, US region) |
| `GET /api/images` | `id` (required int), `type` (required: `movie`\|`tv`) | `MediaImages{backdrops, logos, posters []MediaImageObject}` |
| `GET /api/videos` | `id` (required int), `type` (required) | `MediaVideos{results []MediaVideoObject}` |
| `GET /api/media` | `id` (required int), `type` (required) | `Media` (single title) |
| `GET /api/details` | `id` (required), `type` | `Details{overview, genres, runtime, episode_run_time, credits, release_dates, content_ratings, keywords, origin_country, number_of_seasons/episodes, seasons, last/next_episode_to_air}` |
| `GET /api/similar` | `id`, `type` | `[]Media` (up to 12, TMDB's own `/recommendations`) |
| `GET /api/logos` | `id`, `type` | `[]string` (up to 3 logo URLs) |
| `GET /api/imdb` | `id` (int) | `{"imdb_id": string}` |
| `GET /api/tv/seasons` | `id` (required int) | `[]TVSeason{season_number, episode_count, name, poster_path}` |
| `GET /api/tv/episodes` | `id` (required int), `season` (required int ≥1) | `[]TVEpisode{episode_number, name, overview, still_path, air_date}` |
| `GET /api/quality/batch` | `ids` (required, comma-separated) | **NDJSON stream** (`application/x-ndjson`), one line per resolved id: `{"id":"123","quality":"1080p"}`; ids that fail are silently skipped; concurrency capped at 5 |

`Media`: `{id, title, name, overview, release_date, first_air_date, poster_path, vote_average, media_type, trailer_url, clip_urls, images, popularity, genre_ids, adult}`.

Note: `/api/genres` (TMDB's static genre list) is registered by `internal/discover`, not here, even though `tmdb.GenreList()` lives in this package — see below.

## `internal/library` — watch history (`internal/library/library.go`)

Always compiled identically; no build-tag variance.

| Route | Method | Body | Response |
|---|---|---|---|
| `/api/library` | GET | — (query: `status` optional filter) | `[]*LibraryEntry` |
| `/api/library` | POST | `{tmdb_id, media_type, title, poster_path, status, vote_average, last_air_date, last_aired_season, last_aired_episode}` (upsert, keyed by tmdb_id+media_type; `status` defaults to `"watch_later"`) | `*LibraryEntry` |
| `/api/library/progress` | GET | query: `tmdb_id` (required), `media_type` (required), `season`, `episode` (optional) | `*WatchProgress` or `null` (200 either way — absence isn't an error) |
| `/api/library/progress` | POST | `{tmdb_id, media_type, title, poster_path, vote_average, last_air_date, last_aired_season, last_aired_episode, season, episode, position_seconds, duration_seconds, completed}` — auto-creates a `"watching"` library entry if none exists | `*WatchProgress` |
| `/api/library/{id}/{type}` | GET | — | `{entry: *LibraryEntry\|null, progress: []*WatchProgress, dismissed: bool}` — always 200 |
| `/api/library/{id}/{type}` | DELETE | — | `204` (removes the entry only; progress records are kept deliberately) |
| `/api/library/{id}/{type}/status` | PATCH | `{"status": string}` | updated `*LibraryEntry`; `404` if entry doesn't exist |
| `/api/library/{id}/{type}/rating` | PATCH | `{"rating": *float64}` (0–5, nullable to clear) | updated `*LibraryEntry`; `404` if entry doesn't exist |
| `/api/library/dismiss` | POST / DELETE | `{"tmdb_id": int, "media_type": string}` | `204` |
| `/api/library/stats` | GET | — | `Stats{total, by_type, by_status, finished, dismissed, rated, avg_rating, movie_share, tv_share}` |

`LibraryEntry`: `{id(uuid), profile_id, tmdb_id, media_type, title, poster_path, status, rating, vote_average, last_air_date, last_watched_at, last_watched_season, last_watched_episode, last_aired_season, last_aired_episode, added_at, updated_at}`.

`WatchProgress`: `{id, profile_id, library_entry_id, tmdb_id, media_type, season, episode, position_seconds, duration_seconds, completed, watched_at}`.

## `internal/discover` — recommendations (OSS vs proprietary differ significantly)

Build-tagged: `internal/discover/noop.go` (`!discover`, default) vs. the
proprietary implementation (`-tags discover`, sourced from the
`_private/cove-discover` submodule).

| Route | OSS (`noop.go`) | Proprietary (`-tags discover`) |
|---|---|---|
| `GET /api/discover?type=movie\|tv&limit=` | Returns `[]` unless a custom algorithm URL is configured in Settings, in which case it fetches a plain TMDB-popularity pool and scores it via that URL (empty taste-profile arrays sent — no real personalization) | Real personalized recommendations (genre/keyword/cast-crew scoring, recency decay, re-ranking) |
| `GET /api/discover/genres?type=` | Stub — always `[]` | Real — top genres for the user |
| `GET /api/discover/keywords` | Stub — always `[]` | Real — top keywords |
| `GET /api/discover/genre?type=&genre=` | Stub — always `[]` | Real — single-genre browse |
| `GET /api/discover/keyword?type=&keyword=` | Stub — always `[]` | Real — single-keyword browse |
| `GET /api/discover/insights` | Stub — always `{}` | Real — `Insights{top_movie_genres, top_tv_genres, disliked_genres, top_keywords, top_people, signals_used}` |
| `GET /api/discover/people?limit=` | **Does not exist (404)** | Top actors/directors by taste affinity |
| `GET /api/discover/person?type=&person=` | **Does not exist (404)** | Single-person browse ("because you like X") |
| `GET /api/discover/favorites?limit=` | **Does not exist (404)** | Seed titles for "because you watched X" rows |
| `GET /api/discover/similar-to?type=&tmdb_id=` | **Does not exist (404)** | TMDB-recommendations-based row for one seed title, library-excluded |
| `GET /api/genres?type=movie\|tv` | Real in both — proxies `tmdb.GenreList` | Same |
| `POST /api/discover/algorithm/test` | Real in both — `{"url": string}` body, tests a custom algorithm endpoint against synthetic sample data, returns `{"ok": bool}` or `{"ok": false, "error": string}` | Same |

## `internal/addons` — provider/subtitle addons (`internal/addons/addon.go`)

Always compiled identically.

| Route | Method | Query | Body | Response |
|---|---|---|---|---|
| `/api/addons` | GET | — | — | `[]AddonEntry` |
| `/api/addons` | POST | — | `{"url": string}` | `AddonEntry` (newly added Stremio addon) |
| `/api/addons` | PATCH | `id` or `url` | `{"enabled": bool}` | `204`; `404` if not found |
| `/api/addons` | DELETE | `id` or `url` | — | `204`; `400` on error |
| `/api/timestamps` | GET only | `id` (required), `season`, `episode` (optional) | — | `*TimestampData{intro, recap, credits, preview []TimestampSegment{start_ms, end_ms}}` |
| `/api/watch-options` | GET only | `id` (required), `type` (required) | — | `[]WatchOption{providerId, providerName, logoPath, type, link}` (`[]` on error) |

`AddonEntry`: `{id, url, manifest{id, name, description, version, resources, types}, kind("provider"|"subtitle"|"timestamps"), source("official"|"stremio"), enabled}`.

## `internal/nuvio` — community plugin scrapers (`internal/nuvio/nuvio.go`)

Always compiled identically. A separate ecosystem from `internal/addons`: a
"repo" is a GitHub repository publishing a `manifest.json` that lists
scraper entries, each a JS file executed in a sandboxed `goja` VM to produce
direct stream URLs. Off by default — nothing is fetched or executed until
the user adds a repo and enables an individual scraper. Feeds into
`GET /api/streams` only (see `internal/player` below); intentionally excluded
from `internal/tmdb`'s batched `/api/quality/batch`.

| Route | Method | Query | Body | Response |
|---|---|---|---|---|
| `/api/nuvio/repos` | GET | — | — | `[]Repo` |
| `/api/nuvio/repos` | POST | — | `{"url": string}` (a `github.com/owner/repo` URL or a direct raw-manifest link) | `Repo` (newly added, all scrapers disabled) |
| `/api/nuvio/repos` | PATCH | `id` | `{"enabled": bool}` | `204`; `404` if not found — master on/off switch for the whole repo |
| `/api/nuvio/repos` | DELETE | `id` | — | `204`; `400` on error |
| `/api/nuvio/repos/refresh` | POST only | `id` | — | `204`; refetches the manifest and any currently-enabled scrapers' code |
| `/api/nuvio/scrapers` | PATCH only | `repoId`, `scraperId` | `{"enabled": bool}` | `204`; on first enable, lazily fetches and caches the scraper's JS — refuses to enable on fetch/parse error |

`Repo`: `{id("owner/repo"), owner, repo, branch, url, enabled, scrapers: []Scraper, fetchedAt, fetchErr?}`.
`Scraper`: `{id, name, description?, version?, filename, supportedTypes?, logo?, contentLanguage?, supportsExternalPlayer?, enabled, code?, codeFetchedAt?, codeErr?}` — `code` is empty until the scraper's first enable.

## `internal/settings` — preferences (`internal/settings/settings.go`)

Always compiled identically.

| Route | Method | Body | Response |
|---|---|---|---|
| `/api/settings` | GET | — | full `Settings` struct |
| `/api/settings` | PUT | full `Settings` struct — **whole-object replace, no partial merge**; any field you omit is written as its Go zero value, not left as the previous value | echoes the saved `Settings` |

`Settings` fields (defaults in parens): `openOnMute`, `defaultVolume(1.0)`,
`autoPlay`, `rememberPosition(true)`, `defaultProvider("")`,
`autoSelectStream`, `streamSelectionMode("balanced")`, `measuredBandwidthMbps`,
`subtitlesEnabled`, `defaultSubtitleLang("en")`, `defaultAudioLang("en")`,
`subtitleSize(100)`, `subtitlePosition(8)`, `subtitleBackground(true)`,
`showStreamDetails(true)`, `hideSpoilers`, `autoSkipIntro/Recap/Credits/Preview`,
`onboardingDone`, `discoveryAlgorithm("smart")`, `customAlgorithmUrl`.

## `internal/profiles` — local user profiles (`internal/profiles/profiles.go`)

Always compiled identically. Not to be confused with content-rating/kid mode.

| Route | Method | Body | Response |
|---|---|---|---|
| `/api/profiles` | GET | — | `{profiles: []Profile, active_profile_id: string}` |
| `/api/profiles` | POST | `{"name": string}` (required) | `Profile` (new) |
| `/api/profiles/{id}/activate` | POST | — | `Profile` (now active) |
| `/api/profiles/{id}` | PATCH | `{"name": string}` | `{id, name}` |
| `/api/profiles/{id}` | DELETE | — | `204`; `400` if primary profile or not found |

`Profile`: `{id(uuid), name, is_primary, supabase_uid}`.

## `internal/player` — streaming (`internal/player/player.go`)

Always compiled identically; torrenting is core functionality. No route
enforces an HTTP method.

| Route | Query params | Response |
|---|---|---|
| `GET /api/subtitles` | `id` (required int), `type` (`tv` requires `season`/`episode` too) | `[]addons.Subtitle{id, url, lang}` (`[]` if none) |
| `GET /api/streams` | `id` (required int), `type` (default `movie`; `tv` requires `season`/`episode`) | `[]addons.Stream{name, title, url, infoHash, addonName, subtitles, headers?}` — includes results from any enabled Nuvio scrapers alongside addon-sourced streams |
| `GET /api/play` | `hash` **or** `url` (one required) | `url` with no remembered headers → `307` redirect to the origin; `url` for a stream that carries headers (e.g. Referer/Origin some Nuvio-sourced streams require) → proxied instead of redirected, since a redirect can't carry them; `hash` → seekable video stream (`http.ServeContent`, Range-request support) |
| `GET /api/progress` | `hash` | `{"found": false}` or `{found: true, progress, peers, speed}` — legacy one-shot polling, prefer the SSE stream below |
| `GET /api/progress/stream` | `hash` | Server-Sent Events, one `data:` line every 2s, same shape as `/api/progress` |
| `GET /api/speedtest` | — | Streams a fixed 25 MiB zero-byte payload for client-side bandwidth measurement |
| `GET /api/subtitle-proxy` | `url` (required) | Proxies an external subtitle as `text/vtt`, converting SRT→VTT if needed |

## `internal/updater` — self-update (`internal/updater/updater.go`)

Always compiled identically.

| Route | Method | Response |
|---|---|---|
| `/api/update/check` | any | `CheckResult{available, current_version, latest_version, release_name}`. Skips the GitHub call entirely on managed distros (`APPIMAGE`/`FLATPAK_ID` set) or dev builds (non-semver version) |
| `/api/update/apply` | POST only (405 otherwise) | Downloads and applies the previously-checked release asset (server-cached URL, not client-supplied), then exits with code `42` so the Qt shell can restart the process |

## `internal/clientsession` — opaque client blob (`internal/clientsession/clientsession.go`)

Always compiled identically. Exists because QtWebEngine's `localStorage`
isn't reliably durable across restarts, so auth tokens round-trip through
this instead.

| Route | Method | Body | Response |
|---|---|---|---|
| `/api/client-session` | GET | — | Raw contents of the stored blob; `404` if none saved |
| `/api/client-session` | POST | Any valid JSON | `204`; overwrites the stored blob (mode `0600`) |
| `/api/client-session` | DELETE | — | `204` |

## `internal/supabase` — auth + sync (OSS vs proprietary differ, plus a known path mismatch)

Build-tagged: `noop.go` (`!supabase`, default) vs. the proprietary
implementation (`-tags supabase`, sourced from `_private/cove-auth`).

**OSS build**: every route below returns `503` with body `"Supabase
integration not enabled (build with -tags supabase)"` (still answers `OPTIONS`
successfully, since `CorsMiddleware` wraps the stub too).

**Proprietary build**: real Supabase-backed account creation, login, and
cross-device library/settings/addon sync.

| Route (OSS build) | Route (proprietary build) |
|---|---|
| `/api/auth/register` | `/api/auth/register` |
| `/api/auth/confirm-register` | `/api/auth/register/confirm` ⚠️ |
| `/api/auth/login` | `/api/auth/login` |
| `/api/auth/otp` | `/api/auth/otp` |
| `/api/auth/verify-otp` | `/api/auth/verify-otp` |
| `/api/auth/logout` | `/api/auth/logout` |
| `/api/auth/me` | `/api/auth/me` |
| `/api/auth/sync` | `/api/auth/sync` |

⚠️ **Known inconsistency**: the OSS stub and the proprietary build register
*different paths* for the registration-confirmation step
(`/api/auth/confirm-register` vs. `/api/auth/register/confirm`). Neither
build registers the other's path, so whichever one a frontend build assumes,
the other build 404s on that specific route. Not fixed as part of this
documentation pass — flagged here so it doesn't get load-bearing assumptions
built on top of it silently.

## `main.go` — routes registered directly (not via any package's `SetupHandlers`)

| Route | Method | Response |
|---|---|---|
| `/api/ping` | any | `{"status": "ok"}` |
