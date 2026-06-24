import type {
  Details,
  Media,
  MediaImages,
  MediaVideos,
  TVEpisode,
} from "$lib/types/tmdb";
import type { Stream } from "$lib/types/addons";
import type { Settings } from "$lib/types/settings"; // tygo-generated
import type { LibraryEntry, WatchProgress } from "$lib/types/library"; // tygo-generated

// Single source of truth for the backend origin. Override per-environment with
// VITE_API_BASE (e.g. in .env.production); falls back to the local dev server.
// Everything in this module — fetches and the URL builders handed to <video>,
// <track>, hls.js, and EventSource — is derived from this, so the host appears
// exactly once in the frontend.
const BASE =
  (import.meta as unknown as { env?: Record<string, string | undefined> }).env
    ?.VITE_API_BASE ?? "http://localhost:6969/api";

// ── Request helpers ───────────────────────────────────────────────────────────

// Concurrency limiter. A full homepage (Continue Watching + every taste row,
// each packed with cards) fires hundreds of metadata fetches at once —
// getImages + libraryGet per card, plus getDetails / getMediaByID per item.
// Chromium can't track that many pending requests and starts failing them with
// net::ERR_INSUFFICIENT_RESOURCES (which surfaces as "TypeError: Failed to
// fetch"). We cap how many fetches are actually in flight; the rest wait in a
// cheap in-memory queue rather than as pending browser requests.
//
// Only request/requestOrNull go through this. Long-lived streams (HLS, the
// progress SSE, speedtest) deliberately bypass it — they'd hold a slot open
// indefinitely and starve everything else.
const MAX_CONCURRENT = 8;
let inFlight = 0;
const waiters: Array<() => void> = [];

function acquireSlot(): Promise<void> {
  if (inFlight < MAX_CONCURRENT) {
    inFlight++;
    return Promise.resolve();
  }
  return new Promise<void>((resolve) => waiters.push(resolve));
}

function releaseSlot(): void {
  const next = waiters.shift();
  // Hand the freed slot straight to the next waiter (inFlight unchanged), or
  // give it back to the pool if nobody's waiting.
  if (next) next();
  else inFlight--;
}

/** fetch(), but never more than MAX_CONCURRENT calls outstanding at once. */
async function limitedFetch(
  input: RequestInfo | URL,
  init?: RequestInit,
): Promise<Response> {
  await acquireSlot();
  try {
    return await fetch(input, init);
  } finally {
    releaseSlot();
  }
}

// In-flight request coalescing. A title often appears in several rows at once
// (Continue Watching + a genre row + the tastes row), so the same getDetails /
// getImages / getMediaByID fires multiple times simultaneously. We share one
// pending promise per identical request instead of duplicating the fetch.
//
// Only GETs are coalesced — mutations (PUT/POST/PATCH/DELETE) must each run.
// Entries are evicted the moment the request settles, so this collapses
// concurrent bursts without ever serving a cached/stale response: a request
// made after the previous one finished always hits the network fresh.
const inflight = new Map<string, Promise<unknown>>();

function coalesce<T>(
  key: string,
  init: RequestInit | undefined,
  exec: () => Promise<T>,
): Promise<T> {
  const method = (init?.method ?? "GET").toUpperCase();
  if (method !== "GET") return exec();

  const existing = inflight.get(key) as Promise<T> | undefined;
  if (existing) return existing;

  const p = exec().finally(() => inflight.delete(key));
  inflight.set(key, p);
  return p;
}

/** Thrown for any non-2xx response so callers can distinguish HTTP failures. */
export class ApiError extends Error {
  constructor(
    public status: number,
    public body: string,
    public path: string,
  ) {
    super(`API ${status} on ${path}${body ? `: ${body}` : ""}`);
    this.name = "ApiError";
  }
}

/** fetch + ok-check + JSON parse. Throws ApiError on non-2xx. */
async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const exec = async (): Promise<T> => {
    const res = await limitedFetch(`${BASE}${path}`, init);
    if (!res.ok) {
      throw new ApiError(res.status, await res.text().catch(() => ""), path);
    }
    const text = await res.text();
    return (text ? JSON.parse(text) : undefined) as T;
  };
  return coalesce(`request:${path}`, init, exec);
}

/**
 * Like request, but treats 404 / empty body as a normal `null` rather than an
 * error — for endpoints where "nothing saved yet" is an expected outcome.
 * Note: unlike the old inline handlers, a 500 now throws instead of silently
 * returning null, so genuine server errors surface.
 */
async function requestOrNull<T>(
  path: string,
  init?: RequestInit,
): Promise<T | null> {
  const exec = async (): Promise<T | null> => {
    const res = await limitedFetch(`${BASE}${path}`, init);
    if (res.status === 404) return null;
    if (!res.ok) {
      throw new ApiError(res.status, await res.text().catch(() => ""), path);
    }
    const text = await res.text();
    return text ? (JSON.parse(text) as T) : null;
  };
  return coalesce(`requestOrNull:${path}`, init, exec);
}

/** A torrent src is a bare infohash; anything starting with http is a direct URL. */
function isHashSrc(src: string): boolean {
  return !src.startsWith("http");
}

// ── Library: TypeScript-only types ────────────────────────────────────────────
//
// LibraryStatus can't be a Go union type, so we define it here and let tygo
// emit `string` for the Status field in LibraryEntry. Both are correct at
// runtime; the union just gives callers better autocomplete.

export type LibraryStatus = "watch_later" | "watching" | "finished" | "dropped";

export const STATUS_LABELS: Record<LibraryStatus, string> = {
  watch_later: "Watch Later",
  watching: "Watching",
  finished: "Finished",
  dropped: "Dropped",
};

/** "1h 23m" / "4m 12s" / "8s" */
export function formatPosition(seconds: number): string {
  const h = Math.floor(seconds / 3600);
  const m = Math.floor((seconds % 3600) / 60);
  const s = Math.floor(seconds % 60);
  if (h > 0) return `${h}h ${m}m`;
  if (m > 0) return `${m}m ${s}s`;
  return `${s}s`;
}

// ── Probe response shape ──────────────────────────────────────────────────────
//
// probe() is generic so callers keep their own AudioTrackInfo/SubtitleTrackInfo
// types without this module having to own them.

export interface StreamQuery {
  type?: string;
  season?: number;
  episode?: number;
}

/** Addon subtitle shape — matches the Go Subtitle struct and PlayerSession.subtitles. */
export interface AddonSubtitle {
  id: string;
  url: string;
  lang: string;
}

export interface Taste {
  id: number;
  name: string;
  score: number;
}

export type DiscoverProfile = "adult" | "kid";

export interface LibraryStats {
  total: number;
  by_type: Record<string, number>;
  by_status: Record<string, number>;
  finished: Record<string, number>;
  dismissed: number;
  rated: number;
  avg_rating: number;
  movie_share: number;
  tv_share: number;
}

export interface DiscoverInsights {
  top_movie_genres: Taste[];
  top_tv_genres: Taste[];
  disliked_genres: Taste[];
  top_keywords: Taste[];
  signals_used: number;
}

// A /search/person result. profile_path / known_for posters arrive as fully
// qualified URLs (the backend absolutises them).
export interface Person {
  id: number;
  name: string;
  profile_path: string;
  known_for_department: string;
  popularity: number;
  known_for: Media[];
}

// A streaming/rental service from /watch/providers; logo_path is a full URL.
export interface Provider {
  provider_id: number;
  provider_name: string;
  logo_path: string;
  display_priority: number;
}

// Sectioned payload from /api/search/multi.
export interface SearchResults {
  movies: Media[];
  tv: Media[];
  people: Person[];
  providers: Provider[];
}

// Full person payload for the person overlay (bio + filmography).
export interface PersonDetails {
  id: number;
  name: string;
  biography: string;
  profile_path: string;
  known_for_department: string;
  birthday: string;
  place_of_birth: string;
  credits: Media[];
}

// ── API ────────────────────────────────────────────────────────────────────────

export const api = {
  // ── TMDB ────────────────────────────────────────────────────────────────────
  search: (q: string): Promise<Media[]> =>
    request(`/search?q=${encodeURIComponent(q)}`),

  // Sectioned search: titles (split movie/tv), people, and providers.
  searchMulti: (q: string): Promise<SearchResults> =>
    request(`/search/multi?q=${encodeURIComponent(q)}`),

  // Person bio + filmography for the person overlay.
  getPerson: (id: number): Promise<PersonDetails> =>
    request(`/person?id=${id}`),

  // Popular titles available on a watch provider (US region).
  providerTitles: (id: number, limit?: number): Promise<Media[]> => {
    const p = new URLSearchParams({ id: String(id) });
    if (limit != null) p.set("limit", String(limit));
    return request(`/provider?${p}`);
  },

  getKeywords: (q: string): Promise<{ id: number; name: string }[]> =>
    request(`/keywords?q=${encodeURIComponent(q)}`),

  getSimilar: (media: Media): Promise<Media[]> =>
    request(`/similar?id=${media.id}&type=${media.media_type}`),

  // Fetches a genuine, fully-populated Media object by ID — for places that
  // only have a bare tmdb_id (e.g. a LibraryEntry) and would otherwise have
  // to reconstruct a partial Media object by hand.
  getMediaByID: (tmdbId: number, mediaType: string): Promise<Media> =>
    request(`/media?id=${tmdbId}&type=${mediaType}`),

  getDetails: (media: Media): Promise<Details> =>
    request(`/details?id=${media.id}&type=${media.media_type}`),

  getImages: (media: Media): Promise<MediaImages> =>
    request(`/images?id=${media.id}&type=${media.media_type}`),

  getVideos: (media: Media): Promise<MediaVideos> =>
    request(`/videos?id=${media.id}&type=${media.media_type}`),

  getLogos: (id: number, mediaType: string): Promise<string[]> =>
    request(`/logos?id=${id}&type=${mediaType}`),

  tvSeasons: <T = unknown>(id: number): Promise<T[]> =>
    request(`/tv/seasons?id=${id}`),

  tvEpisodes: (id: number, season: number): Promise<TVEpisode[]> =>
    request(`/tv/episodes?id=${id}&season=${season}`),

  // ── Streams & subtitles (addons) ──────────────────────────────────────────────
  getStreams: (tmdbId: number, opts: StreamQuery = {}): Promise<Stream[]> => {
    const p = new URLSearchParams({ id: String(tmdbId) });
    if (opts.type) p.set("type", opts.type);
    if (opts.season != null) p.set("season", String(opts.season));
    if (opts.episode != null) p.set("episode", String(opts.episode));
    return request(`/streams?${p}`);
  },

  getSubtitles: (p: {
    id: number;
    type: string;
    season?: number;
    episode?: number;
  }): Promise<AddonSubtitle[]> => {
    const q = new URLSearchParams({ id: String(p.id), type: p.type });
    if (p.season != null) q.set("season", String(p.season));
    if (p.episode != null) q.set("episode", String(p.episode));
    return request(`/subtitles?${q}`);
  },

  // ── Player: source URL builders ───────────────────────────────────────────────
  //
  // These return strings rather than fetching, because the result is handed to
  // <video src>, hls.js, <track src>, or EventSource, which do their own loading.

  /** Direct torrent stream (or the original URL if src is already absolute). */
  playUrl: (src: string): string =>
    isHashSrc(src) ? `${BASE}/play?hash=${src}` : src,

  hlsMasterUrl: (sessionId: string): string =>
    `${BASE}/hls/${sessionId}/master.m3u8`,

  subtitleExtractUrl: (src: string, index: number): string => {
    const q = isHashSrc(src) ? `hash=${src}` : `url=${encodeURIComponent(src)}`;
    return `${BASE}/subtitle/extract?${q}&index=${index}`;
  },

  subtitleProxyUrl: (externalUrl: string): string =>
    `${BASE}/subtitle-proxy?url=${encodeURIComponent(externalUrl)}`,

  progressStreamUrl: (src: string): string =>
    `${BASE}/progress/stream?hash=${src}`,

  /** Fixed-size payload endpoint for the in-app bandwidth test. Caller measures blob size vs. elapsed time. */
  speedtestUrl: (): string => `${BASE}/speedtest`,

  // ── Player: probe & HLS session ───────────────────────────────────────────────

  /** ffprobe the source. Generic so the caller supplies the result shape. */
  probe: <T = unknown>(src: string, signal?: AbortSignal): Promise<T> => {
    const q = isHashSrc(src) ? `hash=${src}` : `url=${encodeURIComponent(src)}`;
    return request(`/probe?${q}`, signal ? { signal } : undefined);
  },

  hlsStart: (
    body: {
      input: string;
      tracks: unknown[];
      duration: number;
      videoCodec: string;
    },
    signal?: AbortSignal,
  ): Promise<{ sessionID: string }> =>
    request(`/hls/start`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
      signal,
    }),

  // Fire-and-forget teardown. keepalive lets it complete during page unload /
  // component destroy, when a normal fetch would be cancelled. Not awaited.
  hlsStop: (sessionId: string): void => {
    fetch(`${BASE}/hls/stop/${sessionId}`, {
      method: "POST",
      headers: { "Content-Type": "application/x-www-form-urlencoded" },
      keepalive: true,
    }).catch((e) => console.error("hls stop failed for " + sessionId, e));
  },

  // ── Settings ─────────────────────────────────────────────────────────────────
  getSettings: (): Promise<Settings> => request(`/settings`),

  updateSettings: (s: Settings): Promise<Settings> =>
    request(`/settings`, {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(s),
    }),

  // ── Library ──────────────────────────────────────────────────────────────────
  libraryList: (status?: LibraryStatus): Promise<LibraryEntry[]> =>
    request(`/library${status ? `?status=${status}` : ""}`),

  libraryUpsert: (p: {
    tmdb_id: number;
    media_type: string;
    title: string;
    poster_path: string;
    vote_average?: number;
    last_air_date?: string;
    last_aired_season?: number | null;
    last_aired_episode?: number | null;
    status?: LibraryStatus;
  }): Promise<LibraryEntry> =>
    request(`/library`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(p),
    }),

  // Returns null only when the title has no entry AND no progress history.
  // entry can be null if the user removed the title from their list but
  // progress records (watch history) still exist.
  libraryGet: (
    tmdbId: number,
    mediaType: string,
  ): Promise<{
    entry: LibraryEntry | null;
    progress: WatchProgress[];
  } | null> => requestOrNull(`/library/${tmdbId}/${mediaType}`),

  libraryRemove: (tmdbId: number, mediaType: string): Promise<void> =>
    request(`/library/${tmdbId}/${mediaType}`, { method: "DELETE" }),

  librarySetStatus: (
    tmdbId: number,
    mediaType: string,
    status: LibraryStatus,
  ): Promise<LibraryEntry> =>
    request(`/library/${tmdbId}/${mediaType}/status`, {
      method: "PATCH",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ status }),
    }),

  librarySetRating: (
    tmdbId: number,
    mediaType: string,
    rating: number | null,
  ): Promise<LibraryEntry> =>
    request(`/library/${tmdbId}/${mediaType}/rating`, {
      method: "PATCH",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ rating }),
    }),

  // ── Watch progress ────────────────────────────────────────────────────────────
  // Returns null when no progress has been saved yet (not an error).
  progressGet: (
    tmdbId: number,
    mediaType: string,
    season?: number | null,
    episode?: number | null,
  ): Promise<WatchProgress | null> => {
    const p = new URLSearchParams({
      tmdb_id: String(tmdbId),
      media_type: mediaType,
    });
    if (season != null) p.set("season", String(season));
    if (episode != null) p.set("episode", String(episode));
    return requestOrNull(`/library/progress?${p}`);
  },

  // Upserts a progress record. Also auto-creates a "watching" library entry
  // if one doesn't already exist (handled server-side).
  progressSave: (p: {
    tmdb_id: number;
    media_type: string;
    title?: string;
    poster_path?: string;
    vote_average?: number;
    last_air_date?: string;
    last_aired_season?: number | null;
    last_aired_episode?: number | null;
    season?: number | null;
    episode?: number | null;
    position_seconds: number;
    duration_seconds: number;
    completed: boolean;
  }): Promise<WatchProgress> =>
    request(`/library/progress`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(p),
    }),

  // ── Discovery ────────────────────────────────────────────────────────────────
  discover: (
    type: "movie" | "tv" | "all",
    opts: { limit?: number; profile?: DiscoverProfile } = {},
  ): Promise<Media[]> => {
    const p = new URLSearchParams({ type });
    if (opts.limit != null) p.set("limit", String(opts.limit));
    if (opts.profile) p.set("profile", opts.profile);
    return request(`/discover?${p}`);
  },

  discoverByGenre: (
    type: "movie" | "tv",
    genreId: number,
    opts: { limit?: number; profile?: DiscoverProfile } = {},
  ): Promise<Media[]> => {
    const p = new URLSearchParams({ type, genre: String(genreId) });
    if (opts.limit != null) p.set("limit", String(opts.limit));
    if (opts.profile) p.set("profile", opts.profile);
    return request(`/discover/genre?${p}`);
  },

  discoverByKeyword: (
    type: "movie" | "tv",
    keywordId: number,
    opts: { limit?: number; profile?: DiscoverProfile } = {},
  ): Promise<Media[]> => {
    const p = new URLSearchParams({ type, keyword: String(keywordId) });
    if (opts.limit != null) p.set("limit", String(opts.limit));
    if (opts.profile) p.set("profile", opts.profile);
    return request(`/discover/keyword?${p}`);
  },

  discoverTopGenres: (
    type: "movie" | "tv",
    limit?: number,
  ): Promise<Taste[]> => {
    const p = new URLSearchParams({ type });
    if (limit != null) p.set("limit", String(limit));
    return request(`/discover/genres?${p}`);
  },

  discoverTopKeywords: (limit?: number): Promise<Taste[]> =>
    request(`/discover/keywords${limit ? `?limit=${limit}` : ""}`),

  genreList: (type: "movie" | "tv"): Promise<{ id: number; name: string }[]> =>
    request(`/genres?type=${type}`),

  notInterested: (media: Media): Promise<void> =>
    request(`/library/dismiss`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ tmdb_id: media.id, media_type: media.media_type }),
    }),

  undoNotInterested: (media: Media): Promise<void> =>
    request(`/library/dismiss`, {
      method: "DELETE",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ tmdb_id: media.id, media_type: media.media_type }),
    }),

  // ── Profile / insights ───────────────────────────────────────────────────────
  libraryStats: (): Promise<LibraryStats> => request(`/library/stats`),

  discoverInsights: (): Promise<DiscoverInsights> => request(`/discover/insights`),
};
