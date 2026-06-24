<script lang="ts">
  import { api } from "$lib/api";
  import type { Media } from "$lib/types/tmdb";
  import { libraryChanged } from "$lib/stores/library";
  import { ChevronLeft, ChevronRight } from "lucide-svelte";
  import ComingSoonCard, {
    type ComingSoonItem,
  } from "./cards/ComingSoonCard.svelte";
  import { Button } from "$lib/components/ui/button/index.js";
  import { Skeleton } from "$lib/components/ui/skeleton/index.js";
  import { SvelteSet } from "svelte/reactivity";

  let { onSelectMedia }: { onSelectMedia: (m: Media) => void } = $props();

  let items = $state<ComingSoonItem[]>([]);
  let loading = $state(true);
  let trackEl = $state<HTMLElement | null>(null);

  // ── Helpers ───────────────────────────────────────────────────────────────────

  // Days between today (midnight local) and a future ISO date string.
  // Negative means the date has already passed.
  function daysUntil(dateStr: string): number {
    const today = new Date();
    today.setHours(0, 0, 0, 0);
    const target = new Date(dateStr + "T00:00:00");
    return Math.round((target.getTime() - today.getTime()) / 86_400_000);
  }

  // ── Data ─────────────────────────────────────────────────────────────────────

  async function loadComingSoon(): Promise<void> {
    loading = true;
    try {
      // Pull both statuses in parallel; deduplicate by tmdb_id+media_type.
      const [watchLater, watching] = await Promise.all([
        api.libraryList("watch_later"),
        api.libraryList("watching"),
      ]);

      // Merge, preferring watch_later entries if the same title appears in both.
      const seen = new SvelteSet<string>();
      const entries = [...watchLater, ...watching].filter((e) => {
        const key = `${e.tmdb_id}:${e.media_type}`;
        if (seen.has(key)) return false;
        seen.add(key);
        return true;
      });

      const results = await Promise.all(
        entries.map(async (entry): Promise<ComingSoonItem | null> => {
          try {
            const details = await api.getDetails({
              id: entry.tmdb_id,
              media_type: entry.media_type,
            } as Media);

            if (entry.media_type === "movie") {
              // Movies: include when release_date is in the future (or today).
              const rd = (details as { release_date?: string }).release_date;
              if (!rd) return null;
              const days = daysUntil(rd);
              if (days < 0) return null; // already released

              return {
                tmdbId: entry.tmdb_id,
                title: entry.title,
                mediaType: "movie",
                posterPath: entry.poster_path,
                backdropPath:
                  (details as { backdrop_path?: string }).backdrop_path ?? "",
                releaseDate: rd,
                daysUntil: days,
              } satisfies ComingSoonItem;
            } else {
              // TV shows: only season premieres (episode_number === 1) so we
              // don't duplicate what Upcoming already surfaces (mid-season eps).
              const next = (
                details as {
                  next_episode_to_air?: {
                    air_date?: string;
                    season_number?: number;
                    episode_number?: number;
                  };
                }
              ).next_episode_to_air;

              if (
                !next?.air_date ||
                next.episode_number !== 1 ||
                next.season_number == null
              ) {
                return null;
              }

              const days = daysUntil(next.air_date);
              if (days < 0) return null;

              return {
                tmdbId: entry.tmdb_id,
                title: entry.title,
                mediaType: "tv",
                posterPath: entry.poster_path,
                backdropPath:
                  (details as { backdrop_path?: string }).backdrop_path ?? "",
                releaseDate: next.air_date,
                seasonNumber: next.season_number,
                daysUntil: days,
              } satisfies ComingSoonItem;
            }
          } catch {
            return null; // one failing entry shouldn't break the whole carousel
          }
        }),
      );

      items = results
        .filter((r): r is ComingSoonItem => r !== null)
        .toSorted((a, b) => a.releaseDate.localeCompare(b.releaseDate));
    } finally {
      loading = false;
    }
  }

  $effect(() => {
    $libraryChanged;
    loadComingSoon();
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
      <h2 class="text-lg font-semibold">Coming Soon</h2>
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
      <h2 class="text-lg font-semibold">Coming Soon</h2>
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
        {#each items as item (item.tmdbId + item.mediaType)}
          <ComingSoonCard {item} {onSelectMedia} />
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
  <!-- nothing upcoming from the user's lists — hide the section -->
{/if}
