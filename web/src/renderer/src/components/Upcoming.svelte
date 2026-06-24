<script lang="ts">
  import { api } from "$lib/api";
  import type { Media } from "$lib/types/tmdb";
  import { libraryChanged } from "$lib/stores/library";
  import { ChevronLeft, ChevronRight } from "lucide-svelte";
  import UpcomingMediaCard from "./UpcomingMediaCard.svelte";
  import type { UpcomingItem } from "$lib/types/types";
  import { Button } from "$lib/components/ui/button/index.js";
  import { Skeleton } from "$lib/components/ui/skeleton/index.js";

  let { onSelectMedia }: { onSelectMedia: (m: Media) => void } = $props();

  let items = $state<UpcomingItem[]>([]);
  let loading = $state(true);
  let trackEl = $state<HTMLElement | null>(null);

  // ── Data ─────────────────────────────────────────────────────────────────────

  async function loadUpcoming(): Promise<void> {
    loading = true;
    try {
      const entries = await api.libraryList("watching");
      const shows = entries.filter((e) => e.media_type === "tv");

      const results = await Promise.all(
        shows.map(async (entry) => {
          try {
            const details = await api.getDetails({
              id: entry.tmdb_id,
              media_type: "tv",
            } as Media);
            const next = details.next_episode_to_air;
            if (!next?.air_date) return null;
            return {
              tmdbId: entry.tmdb_id,
              title: entry.title,
              posterPath: entry.poster_path,
              stillPath: next.still_path ?? "",
              season: next.season_number,
              episode: next.episode_number,
              episodeName: next.name ?? "",
              airDate: next.air_date,
            } satisfies UpcomingItem;
          } catch {
            return null; // a single show's details failing shouldn't break the carousel
          }
        }),
      );

      items = results
        .filter((r): r is UpcomingItem => r !== null)
        .toSorted((a, b) => a.airDate.localeCompare(b.airDate));
    } finally {
      loading = false;
    }
  }

  $effect(() => {
    $libraryChanged;
    loadUpcoming();
  });

  // ── Display helpers ────────────────────────────────────────────────────────────

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
      <h2 class="text-lg font-semibold">New Episodes</h2>
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
      <h2 class="text-lg font-semibold">New Episodes</h2>
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
          <UpcomingMediaCard {item} {onSelectMedia} />
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
  <!-- nothing, or hide the whole section -->
{/if}
