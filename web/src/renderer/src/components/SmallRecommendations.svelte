<script lang="ts">
  import type { Media } from "$lib/types/tmdb";
  import MediaCard from "./MediaCard.svelte";
  import { ChevronLeft, ChevronRight } from "lucide-svelte";
  import { Button } from "$lib/components/ui/button/index.js";

  // Purely presentational: the parent owns data fetching and the select/watch
  // handlers, so the same row backs "Based on your tastes", per-genre rows,
  // keyword rows, "Because you watched X", etc.
  let {
    header = "",
    medias = [],
    loading = false,
    onSelect = () => {},
    onWatch,
  } = $props<{
    header?: string | null;
    medias?: Media[];
    loading?: boolean;
    onSelect?: (media: Media) => void;
    onWatch?: (media: Media, season?: number, episode?: number) => void;
  }>();

  let trackEl = $state<HTMLElement | null>(null);

  function scrollByCards(direction: 1 | -1): void {
    if (!trackEl) return;
    trackEl.scrollBy({
      left: direction * (trackEl.clientWidth * 0.9),
      behavior: "smooth",
    });
  }
</script>

<!-- Don't render an empty row: only show while loading or when we have items. -->
{#if loading || medias.length > 0}
  <div class="w-full space-y-3 px-4">
    {#if header}
      <div class="ml-12 flex items-center justify-between px-1">
        <h2 class="text-lg font-semibold">{header}</h2>
      </div>
    {/if}

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
        style="scroll-snap-type: x mandatory;"
      >
        {#if loading}
          {#each { length: 8 } as _, i (i)}
            <div class="w-36 shrink-0">
              <div
                class="aspect-2/3 w-full animate-pulse rounded-lg bg-muted"
              ></div>
            </div>
          {/each}
        {:else}
          {#each medias as media (media.media_type + "-" + media.id)}
            <div class="w-36 shrink-0" style="scroll-snap-align: start;">
              <MediaCard
                {media}
                onclick={() => onSelect(media)}
                onwatch={onWatch}
              />
            </div>
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
