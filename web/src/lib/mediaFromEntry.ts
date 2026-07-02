// $lib/mediaFromEntry.ts
//
// Several row components (ContinueWatching, UpcomingMediaCard,
// ReadyToWatchCard, ComingSoonCard) only have a lightweight, row-specific
// shape (a LibraryEntry, or a backend-composed Upcoming/ReadyToWatch/
// ComingSoon item) but need to hand a Media object to onSelectMedia /
// openMediaDetail so the click opens the detail overlay. That overlay
// immediately re-fetches the full TMDB object by id, so only the fields
// actually rendered before the refetch completes (poster, title/name,
// media type) need to be real — the rest of Media is legitimately
// fabricated. This centralizes that one cast instead of repeating it at
// every call site.

import type { Media } from "$lib/types/tmdb";

export interface MediaStub {
  id: number;
  media_type: string;
  poster_path: string;
  /** Omit when the source shape doesn't carry a movie-style title. */
  title?: string;
  /** Omit when the source shape doesn't carry a TV-style name. */
  name?: string;
  vote_average?: number;
  overview?: string;
}

/**
 * Builds a partial Media stand-in from a row-specific shape. Only fields the
 * caller passes end up on the object (aside from the vote_average/overview
 * defaults below) — this intentionally does NOT synthesize a `title` from
 * `name` or vice versa, since some call sites only ever populate one of the
 * two and downstream code (e.g. media_type === "tv" ? .name : .title)
 * depends on that.
 */
export function mediaFromEntry(p: MediaStub): Media {
  return {
    id: p.id,
    media_type: p.media_type,
    ...(p.title !== undefined ? { title: p.title } : {}),
    ...(p.name !== undefined ? { name: p.name } : {}),
    poster_path: p.poster_path,
    vote_average: p.vote_average ?? 0,
    overview: p.overview ?? "",
  } as unknown as Media;
}
