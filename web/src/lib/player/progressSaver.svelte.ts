// $lib/player/progressSaver.svelte.ts
//
// Owns watch-progress: loading the saved position from the server, seeking to
// it once when playback is ready, and saving the current position (throttled)
// while playing plus on completion.
//
// Save methods take a context *getter* called at save time rather than a snapshot
// captured up front, so they always read the current media metadata (title,
// season, …) even as the user navigates.

import { api } from "$lib/api";

export interface ProgressContext {
  tmdbId: number;
  mediaType: string;
  title: string;
  posterPath: string;
  voteAverage: number;
  lastAirDate: string;
  season: number | null;
  episode: number | null;
  /** Fallback when the player doesn't yet report a duration. */
  probedDuration: number | null;
}

export class ProgressSaver {
  // Position loaded from the server; null = nothing saved or already completed.
  savedPosition = $state<number | null>(null);
  // Prevents seeking twice if "ready" fires more than once.
  hasSeekedToSaved = $state(false);

  #lastSaveMs = 0;
  // Set once a save with completed=true has gone out for the current source;
  // cleared in reset(). Guards against a later incomplete save (e.g. onDestroy
  // firing right after the "ended" effect already marked it done) flipping
  // the record back to not-completed.
  #completedSaved = false;

  /** Clear state — call when the source changes. */
  reset(): void {
    this.savedPosition = null;
    this.hasSeekedToSaved = false;
    this.#lastSaveMs = 0;
    this.#completedSaved = false;
  }

  /** Fetch the saved position; only restores a meaningful, not-yet-finished one. */
  async load(
    tmdbId: number,
    mediaType: string,
    season: number | null,
    episode: number | null,
  ): Promise<void> {
    try {
      const prog = await api.progressGet(tmdbId, mediaType, season, episode);
      if (prog && !prog.completed && prog.position_seconds > 10) {
        this.savedPosition = prog.position_seconds;
      }
    } catch (e) {
      console.error(e);
    }
  }

  /** Seek to the saved position once, via the provided seek function. */
  resume(seek: (seconds: number) => void): void {
    if (this.savedPosition !== null && !this.hasSeekedToSaved) {
      this.hasSeekedToSaved = true;
      seek(this.savedPosition);
    }
  }

  /**
   * Throttled save (at most every 10s) from the live position/duration. Call on
   * position changes while playing. Auto-detects completion past 90%.
   */
  maybeSave(
    position: number,
    duration: number,
    getCtx: () => ProgressContext,
  ): void {
    const now = Date.now();
    if (now - this.#lastSaveMs < 10_000) return;
    this.#lastSaveMs = now;
    const dur = duration || getCtx().probedDuration || 0;
    this.#save(position, dur, getCtx, dur > 0 && position / dur >= 0.9);
  }

  /** Immediate save — e.g. on end-of-file or when the player unmounts. */
  saveNow(
    position: number,
    duration: number,
    getCtx: () => ProgressContext,
    completed: boolean,
  ): void {
    const dur = duration || getCtx().probedDuration || 0;
    // Auto-upgrade to completed past the same 90% threshold maybeSave uses —
    // e.g. closing the player right before the credits roll should still
    // count as finished, even though the "ended" effect never fired.
    const isCompleted = completed || (dur > 0 && position / dur >= 0.9);
    // Never let an incomplete save downgrade a title that's already been
    // recorded as completed.
    if (this.#completedSaved && !isCompleted) return;
    this.#lastSaveMs = Date.now();
    this.#save(position, dur, getCtx, isCompleted);
  }

  #save(
    position: number,
    dur: number,
    getCtx: () => ProgressContext,
    completed: boolean,
  ): void {
    if (!dur || position < 5) return; // skip the very start
    if (completed) this.#completedSaved = true;
    const c = getCtx();
    api
      .progressSave({
        tmdb_id: c.tmdbId,
        media_type: c.mediaType,
        title: c.title,
        poster_path: c.posterPath,
        vote_average: c.voteAverage,
        last_air_date: c.lastAirDate,
        season: c.season,
        episode: c.episode,
        position_seconds: completed ? dur : position,
        duration_seconds: dur,
        completed,
      })
      .catch(console.error);
  }
}
