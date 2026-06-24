<script lang="ts">
  import { api } from "$lib/api";
  import type { Media } from "$lib/types/tmdb";
  import type { LibraryEntry } from "$lib/types/library";
  import { libraryChanged } from "$lib/stores/library";
  import { ChevronLeft, ChevronRight } from "lucide-svelte";
  import ReadyToWatchCard, { type ReadyItem } from "./ReadyToWatchCard.svelte";
  import { Button } from "$lib/components/ui/button/index.js";
  import { Skeleton } from "$lib/components/ui/skeleton/index.js";

  let { onSelectMedia }: { onSelectMedia: (m: Media) => void } = $props();

  let items = $state<ReadyItem[]>([]);
  let loading = $state(true);
  let trackEl = $state<HTMLElement | null>(null);

  // ── Season math ────────────────────────────────────────────────────────────
  // We need per-season episode counts to (a) count how many episodes are queued
  // up across season boundaries and (b) roll a season-finale watch over to the
  // next season's episode 1. tvSeasons gives us those counts cheaply.

  type SeasonMeta = { season_number: number; episode_count: number };

  // Real seasons only (episode_count > 0 drops empty/placeholder seasons),
  // ascending. Linear-index math below assumes this consistent ordering.
  function orderSeasons(seasons: SeasonMeta[]): SeasonMeta[] {
    return seasons
      .filter((s) => (s.episode_count ?? 0) > 0)
      .toSorted((a, b) => a.season_number - b.season_number);
  }

  // 1-based position of (season, episode) within the flattened season list.
  // e.g. with S1=10 eps, S2E3 → 13. null if the season isn't in the list.
  function linearIndex(
    seasons: SeasonMeta[],
    season: number,
    episode: number,
  ): number | null {
    let acc = 0;
    for (const s of seasons) {
      if (s.season_number === season) return acc + episode;
      acc += s.episode_count;
    }
    return null;
  }

  // Inverse of linearIndex: a flattened position back to (season, episode).
  function fromLinear(
    seasons: SeasonMeta[],
    index: number,
  ): { season: number; episode: number } | null {
    let acc = 0;
    for (const s of seasons) {
      if (index <= acc + s.episode_count) {
        return { season: s.season_number, episode: index - acc };
      }
      acc += s.episode_count;
    }
    return null;
  }

  // Same numeric test as MyListPage's hasNewEpisodes, minus the status check
  // (we already query the "watching" list). Cheap gate so we only hit the
  // seasons/episodes endpoints for shows that actually have a backlog.
  function isAhead(entry: LibraryEntry): boolean {
    const airedS = entry.last_aired_season;
    const airedE = entry.last_aired_episode;
    if (airedS == null || airedE == null) return false;

    const watchedS = entry.last_watched_season ?? 0;
    const watchedE = entry.last_watched_episode ?? 0;

    if (airedS > watchedS) return true;
    return airedS === watchedS && airedE > watchedE;
  }

  // ── Data ─────────────────────────────────────────────────────────────────────

  async function loadReady(): Promise<void> {
    loading = true;
    try {
      const entries = await api.libraryList("watching");
      const shows = entries.filter((e) => e.media_type === "tv" && isAhead(e));

      const results = await Promise.all(
        shows.map(async (entry): Promise<ReadyItem | null> => {
          try {
            const seasons = orderSeasons(
              await api.tvSeasons<SeasonMeta>(entry.tmdb_id),
            );
            if (seasons.length === 0) return null;

            const watchedS = entry.last_watched_season ?? 0;
            const watchedE = entry.last_watched_episode ?? 0;
            // 0 when nothing's been watched yet → next-to-watch is the very
            // first episode and every aired episode counts as waiting.
            const watchedIndex =
              watchedS > 0
                ? (linearIndex(seasons, watchedS, watchedE) ?? 0)
                : 0;

            const airedIndex = linearIndex(
              seasons,
              entry.last_aired_season!,
              entry.last_aired_episode!,
            );
            if (airedIndex == null) return null;

            const waiting = Math.max(1, airedIndex - watchedIndex);
            const next = fromLinear(seasons, watchedIndex + 1);
            if (!next) return null;

            // Pull the next episode's still + name for the card. Optional —
            // if it fails we still show a usable card off the poster.
            let stillPath = "";
            let episodeName = "";
            let airDate = entry.last_air_date ?? "";
            try {
              const eps = await api.tvEpisodes(entry.tmdb_id, next.season);
              const ep = eps.find((e) => e.episode_number === next.episode);
              if (ep) {
                stillPath = ep.still_path ?? "";
                episodeName = ep.name ?? "";
                airDate = ep.air_date ?? airDate;
              }
            } catch {
              /* still/name are nice-to-have, not required */
            }

            return {
              tmdbId: entry.tmdb_id,
              title: entry.title,
              posterPath: entry.poster_path,
              stillPath,
              season: next.season,
              episode: next.episode,
              episodeName,
              airDate,
              waiting,
            } satisfies ReadyItem;
          } catch {
            return null; // one show failing shouldn't sink the row
          }
        }),
      );

      items = results
        .filter((r): r is ReadyItem => r !== null)
        // Freshest next-episode first. Swap to `b.waiting - a.waiting` to lead
        // with the biggest backlogs instead.
        .toSorted((a, b) => (b.airDate ?? "").localeCompare(a.airDate ?? ""));
    } finally {
      loading = false;
    }
  }

  $effect(() => {
    $libraryChanged;
    loadReady();
  });

  // ── Carousel scroll ───────────────────────────────────────────────────────────

  function scrollByCards(direction: 1 | -1): void {
    if (!trackEl) return;
    trackEl.scrollBy({
      left: direction * (trackEl.clientWidth * 0.9),
      behavior: "smooth",
    });
  }
</script>

{#if loading}
  <div class="space-y-3">
    <div class="flex items-center justify-between px-1">
      <h2 class="text-lg font-semibold">Ready</h2>
    </div>

    <div class="flex items-center justify-between gap-2">
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
        class="flex min-w-0 flex-1 scrollbar-none gap-4 overflow-x-auto px-1 pb-1 [&::-webkit-scrollbar]:hidden"
        style="scroll-snap-type: x mandatory;"
      >
        {#each { length: 5 } as _, i (i)}
          <Skeleton class="h-40 w-70 shrink-0 rounded-2xl" />
        {/each}
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
{:else if items.length > 0}
  <div class="space-y-3">
    <div class="flex items-center justify-between px-1">
      <h2 class="text-lg font-semibold">Ready</h2>
    </div>

    <div class="flex items-center justify-between gap-2">
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
        class="flex min-w-0 flex-1 scrollbar-none gap-4 overflow-x-auto px-1 pb-1 [&::-webkit-scrollbar]:hidden"
        style="scroll-snap-type: x mandatory;"
      >
        {#each items as item (item.tmdbId)}
          <ReadyToWatchCard {item} {onSelectMedia} />
        {/each}
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
{:else}
  <!-- nothing waiting — hide the whole section -->
{/if}
