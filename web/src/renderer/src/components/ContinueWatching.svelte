<script lang="ts">
  import { api } from "$lib/api";
  import type { Media, TVEpisode } from "$lib/types/tmdb";
  import type { LibraryEntry, WatchProgress } from "$lib/types/library";
  import { libraryChanged } from "$lib/stores/library";
  import { ChevronLeft, ChevronRight } from "lucide-svelte";
  import { Button } from "$lib/components/ui/button/index.js";
  import { Skeleton } from "$lib/components/ui/skeleton/index.js";
  import ContinueWatchingCard, {
    type ContinueItem,
  } from "./cards/ContinueWatchingCard.svelte";
  import { SvelteDate, SvelteMap } from "svelte/reactivity";
  import { animate } from "animejs";

  // Resume is the point of this row, so we take onWatch. onSelectMedia is the
  // fallback (open details) when no player handler is wired.
  let {
    onWatch,
    onSelectMedia,
  }: {
    onWatch?: (m: Media, season?: number, episode?: number) => void;
    onSelectMedia: (m: Media) => void;
  } = $props();

  let items = $state<ContinueItem[]>([]);
  let loading = $state(true);
  let trackEl = $state<HTMLElement | null>(null);

  // A movie opened for a few seconds isn't really "in progress"; a resume past
  // this fraction is treated as basically finished (TV rolls forward instead).
  const MOVIE_MIN_SECONDS = 15;
  const MAX_FRACTION = 0.95;

  // Episode lists are reused within one load (a show's resume still and its
  // roll-forward lookup both hit the same season), so memoize per load cycle.
  let seasonCache = new SvelteMap<string, Promise<TVEpisode[]>>();

  function fetchSeason(id: number, season: number): Promise<TVEpisode[]> {
    const key = `${id}:${season}`;
    let p = seasonCache.get(key);
    if (!p) {
      // A non-existent season comes back as an empty/null body (resolves to
      // null, not a rejection), so coerce here — every caller can assume an array.
      p = api
        .tvEpisodes(id, season)
        .then((eps) => eps ?? [])
        .catch(() => [] as TVEpisode[]);
      seasonCache.set(key, p);
    }
    return p;
  }

  function hasAired(ep: TVEpisode): boolean {
    if (!ep.air_date) return false;
    const today = new SvelteDate();
    today.setHours(0, 0, 0, 0);
    return new Date(ep.air_date + "T00:00:00").getTime() <= today.getTime();
  }

  async function episodeStill(
    id: number,
    season: number,
    episode: number,
  ): Promise<string> {
    const eps = await fetchSeason(id, season);
    return eps.find((e) => e.episode_number === episode)?.still_path ?? "";
  }

  // The next *aired* episode after (season, episode): the following episode in
  // the same season, else the first of the next season. An existing-but-unaired
  // next episode means the user is caught up → null (the New Episodes row owns
  // that case).
  // TVEpisode has no season_number, so we return the season we queried with.
  async function nextAiredEpisode(
    id: number,
    season: number,
    episode: number,
  ): Promise<{ season: number; episode: TVEpisode } | null> {
    const same = await fetchSeason(id, season);
    const inSeason = same.find((e) => e.episode_number === episode + 1);
    if (inSeason)
      return hasAired(inSeason) ? { season, episode: inSeason } : null;

    const next = await fetchSeason(id, season + 1);
    const first = next
      .filter((e) => e.episode_number >= 1)
      .toSorted((a, b) => a.episode_number - b.episode_number)[0];
    if (first)
      return hasAired(first) ? { season: season + 1, episode: first } : null;
    return null;
  }

  function latestProgress(progress: WatchProgress[]): WatchProgress | null {
    if (progress.length === 0) return null;
    return progress.toSorted(
      (a, b) =>
        new Date(b.watched_at).getTime() - new Date(a.watched_at).getTime(),
    )[0];
  }

  function toMedia(entry: LibraryEntry): Media {
    return {
      id: entry.tmdb_id,
      media_type: entry.media_type,
      title: entry.title,
      name: entry.title,
      poster_path: entry.poster_path,
      vote_average: entry.vote_average,
      overview: "",
    } as unknown as Media;
  }

  async function buildItem(entry: LibraryEntry): Promise<ContinueItem | null> {
    let progress: WatchProgress[];
    try {
      const data = await api.libraryGet(entry.tmdb_id, entry.media_type);
      progress = data?.progress ?? [];
    } catch {
      return null; // one title failing shouldn't break the row
    }

    const latest = latestProgress(progress);
    if (!latest) return null;

    const key = `${entry.tmdb_id}-${entry.media_type}`;
    const media = toMedia(entry);

    // ── Movie: resume only, no roll-forward ──
    if (entry.media_type !== "tv") {
      if (latest.completed || latest.duration_seconds <= 0) return null;
      if (latest.position_seconds < MOVIE_MIN_SECONDS) return null;
      const frac = latest.position_seconds / latest.duration_seconds;
      if (frac > MAX_FRACTION) return null;
      return {
        key,
        media,
        title: entry.title,
        image: entry.poster_path, // posters are fine for movies
        mediaType: "movie",
        season: null,
        episode: null,
        upNext: false,
        position: latest.position_seconds,
        duration: latest.duration_seconds,
        watchedAt: latest.watched_at,
        progress: frac,
      };
    }

    // ── TV ──
    const s = latest.season ?? 1;
    const e = latest.episode ?? 1;
    const frac =
      latest.duration_seconds > 0
        ? latest.position_seconds / latest.duration_seconds
        : 0;

    // Mid-episode → resume that episode at its position.
    if (!latest.completed && frac <= MAX_FRACTION) {
      const still = await episodeStill(entry.tmdb_id, s, e);
      return {
        key,
        media,
        title: entry.title,
        image: still || entry.poster_path,
        mediaType: "tv",
        season: s,
        episode: e,
        upNext: false,
        position: latest.position_seconds,
        duration: latest.duration_seconds,
        watchedAt: latest.watched_at,
        progress: frac,
      };
    }

    // Finished that episode → roll forward to the next aired one ("Up Next").
    const next = await nextAiredEpisode(entry.tmdb_id, s, e);
    if (!next) return null; // caught up
    return {
      key,
      media,
      title: entry.title,
      image: next.episode.still_path || entry.poster_path,
      mediaType: "tv",
      season: next.season,
      episode: next.episode.episode_number,
      upNext: true,
      position: 0,
      duration: 0,
      watchedAt: latest.watched_at,
      progress: 0,
    };
  }

  async function loadContinue(): Promise<void> {
    loading = true;
    seasonCache = new SvelteMap();
    try {
      // Anything with watch progress has a "watching" entry (progressSave
      // auto-creates one server-side), so this is the right starting set.
      const entries = await api.libraryList("watching");
      const results = await Promise.all(entries.map(buildItem));
      items = results
        .filter((r): r is ContinueItem => r !== null)
        .toSorted(
          (a, b) =>
            new Date(b.watchedAt).getTime() - new Date(a.watchedAt).getTime(),
        );
    } finally {
      loading = false;
    }
  }

  // Refetch whenever the library changes — finishing/advancing an episode
  // should reorder, roll forward, or drop a tile here.
  $effect(() => {
    $libraryChanged;
    loadContinue();
  });

  function resume(item: ContinueItem): void {
    if (onWatch) {
      onWatch(item.media, item.season ?? undefined, item.episode ?? undefined);
    } else {
      onSelectMedia(item.media);
    }
  }

  let activeAnim: ReturnType<typeof animate> | null = null;

  function scrollByCards(direction: 1 | -1): void {
    if (!trackEl) return;
    activeAnim?.pause();

    const target = trackEl.scrollLeft + direction * (trackEl.clientWidth * 0.9);

    activeAnim = animate(trackEl, {
      scrollLeft: target,
      duration: 400,
      ease: "inOutQuad",
    });
  }
</script>

{#if loading || items.length > 0}
  <div class="w-full space-y-3 px-4">
    <div class="ml-12 flex items-center justify-between px-1">
      <h2 class="text-lg font-semibold">Continue Watching</h2>
    </div>

    <div class="flex items-center justify-between gap-2 overflow-hidden">
      <Button
        onclick={() => scrollByCards(-1)}
        variant="outline"
        size="icon"
        aria-label="Scroll left"
      >
        <ChevronLeft class="size-4" />
      </Button>

      <div
        bind:this={trackEl}
        class="flex min-w-0 flex-1 gap-4 overflow-x-auto px-1 pb-1 [&::-webkit-scrollbar]:hidden"
      >
        {#if loading}
          {#each { length: 5 } as _, i (i)}
            <Skeleton class="aspect-video w-70 shrink-0 rounded-md" />
          {/each}
        {:else}
          {#each items as item (item.key)}
            <ContinueWatchingCard {item} onResume={resume} />
          {/each}
        {/if}
      </div>

      <Button
        onclick={() => scrollByCards(1)}
        variant="outline"
        size="icon"
        aria-label="Scroll right"
      >
        <ChevronRight class="size-4" />
      </Button>
    </div>
  </div>
{/if}
