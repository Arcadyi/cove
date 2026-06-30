<script lang="ts">
  import { animate } from "animejs";
  import { untrack } from "svelte";
  import { fade } from "svelte/transition";
  import type { Media, MediaImages, MediaVideos } from "$lib/types/tmdb";
  import { api } from "$lib/api";
  import { getImageOpt, getVideoOpt } from "$lib/utils";
  import ExploreCard from "./ExploreCard.svelte";
  import PlayerSimple from "./PlayerSimple.svelte";
  import { ChevronLeft, ChevronRight, Info } from "lucide-svelte";
  import { Button } from "$lib/components/ui/button/index.js";
  import { Skeleton } from "$lib/components/ui/skeleton/index.js";
  import LibraryStatusPanel from "./LibraryStatusPanel.svelte";
  import type { LibraryEntry } from "$lib/types/library";

  // Featured panel: 16:9 at 270px tall
  const FEATURED_W = 679;
  const CARD_H = 382;
  // Carousel card: 2:3 at 270px tall
  const CARD_W = 255;

  let {
    header = "",
    medias = [],
    loading = false,
    onSelect = () => {},
  }: {
    header?: string | null;
    medias?: Media[];
    loading?: boolean;
    onSelect?: (media: Media) => void;
  } = $props();

  let trackEl = $state<HTMLElement | null>(null);
  let activeAnim: ReturnType<typeof animate> | null = null;
  let selectedIndex = $state(0);

  // Featured panel data
  let featuredBackdrop = $state("");
  let featuredLogo = $state("");
  let featuredVideo = $state<string | null>(null);
  let featuredLibraryEntry = $state<LibraryEntry | null>(null);

  // Reset selection when a new batch of medias arrives.
  $effect(() => {
    if (medias.length > 0) untrack(() => { selectedIndex = 0; });
  });

  // Fetch images + video for the selected card.
  // Subscribes to both selectedIndex and medias so it re-runs when either changes.
  $effect(() => {
    const idx = selectedIndex;
    const media = medias[idx]; // reactive — re-runs when medias prop changes
    if (!media) {
      featuredBackdrop = "";
      featuredLogo = "";
      featuredVideo = null;
      return;
    }

    const mediaId = media.id;
    featuredBackdrop = "";
    featuredLogo = "";
    featuredVideo = null;
    featuredLibraryEntry = null;

    api.getImages(media).then((d: MediaImages) => {
      if (untrack(() => selectedIndex) !== idx) return;
      if (untrack(() => medias)[idx]?.id !== mediaId) return;
      featuredBackdrop = getImageOpt(d, "backdrops", { iso: "" });
      featuredLogo = getImageOpt(d, "logos", { iso: "en" });
    });

    api
      .getVideos(media)
      .then((d: MediaVideos) => {
        if (untrack(() => selectedIndex) !== idx) return;
        if (untrack(() => medias)[idx]?.id !== mediaId) return;
        featuredVideo =
          getVideoOpt(d, "Clip", { iso: "en", official: true }) ||
          getVideoOpt(d, "Trailer", { iso: "en" }) ||
          null;
      })
      .catch(() => {
        featuredVideo = null;
      });

    api
      .libraryGet(media.id, media.media_type)
      .then((d) => {
        if (untrack(() => selectedIndex) !== idx) return;
        if (untrack(() => medias)[idx]?.id !== mediaId) return;
        featuredLibraryEntry = d?.entry ?? null;
      })
      .catch(() => {
        featuredLibraryEntry = null;
      });
  });

  function scrollByCards(direction: 1 | -1): void {
    const el = untrack(() => trackEl);
    if (!el) return;
    activeAnim?.pause();
    const target = el.scrollLeft + direction * el.clientWidth * 0.85;
    activeAnim = animate(el, { scrollLeft: target, duration: 400, ease: "inOutQuad" });
  }

  const rankLabel = $derived(header ? `in ${header}` : "");
  const featuredMedia = $derived(medias[selectedIndex] ?? null);
  const featuredTitle = $derived(featuredMedia?.title ?? featuredMedia?.name ?? "");
</script>

{#if loading || medias.length > 0}
  <div class="w-full px-4">
    {#if header}
      <h2 class="ml-18 text-4xl font-semibold">{header}</h2>
    {/if}
    <div class="flex items-center gap-4 overflow-hidden p-4">
      <!-- ── Featured panel + action buttons ──────────────────────────── -->
      <div class="ml-12 flex shrink-0 flex-col gap-2" style="width: {FEATURED_W}px;">
        <!-- Panel -->
        <div
          role="button"
          tabindex="0"
          style="height: {CARD_H}px;"
          class="relative cursor-pointer overflow-hidden rounded-xl"
          onclick={() => featuredMedia && onSelect(featuredMedia)}
          onkeydown={(e) => e.key === "Enter" && featuredMedia && onSelect(featuredMedia)}
        >
          {#if loading}
            <Skeleton class="absolute inset-0 rounded-xl" />
          {:else}
            {#key `${selectedIndex}-${featuredMedia?.id ?? "none"}`}
              <div class="absolute inset-0" transition:fade={{ duration: 180 }}>
                {#if featuredVideo}
                  <PlayerSimple
                    src={featuredVideo}
                    muted={true}
                    bg={featuredBackdrop}
                    class="absolute inset-0 h-full w-full"
                  />
                {:else if featuredBackdrop}
                  <img
                    src={featuredBackdrop}
                    alt={featuredTitle}
                    class="absolute inset-0 h-full w-full object-cover"
                  />
                {:else}
                  <div class="absolute inset-0 animate-pulse bg-muted"></div>
                {/if}

                <div
                  class="absolute inset-0 bg-linear-to-t from-black/80 via-black/10 to-transparent"
                ></div>

                <div
                  class="absolute inset-x-0 bottom-0 flex items-end justify-between px-3 pb-3"
                >
                  <div class="min-w-0 shrink">
                    {#if featuredLogo}
                      <img
                        src={featuredLogo}
                        alt={featuredTitle}
                        class="h-12 max-w-50 object-contain object-left drop-shadow"
                      />
                    {:else}
                      <span class="line-clamp-1 text-sm font-semibold text-white drop-shadow">
                        {featuredTitle}
                      </span>
                    {/if}
                  </div>
                  <div class="ml-2 shrink-0 text-right">
                    <div class="text-7xl font-black leading-none text-foreground/75">
                      #{selectedIndex + 1}
                    </div>
                    <div class="mt-0.5 text-base leading-none text-white/40">
                      {rankLabel}
                    </div>
                  </div>
                </div>
              </div>
            {/key}
          {/if}
        </div>

        <!-- Action buttons -->
        {#if !loading && featuredMedia}
          <div class="flex gap-2">
            <Button
              variant="outline"
              class="flex-1"
              onclick={() => onSelect(featuredMedia!)}
            >
              <Info class="size-4" />
              Details
            </Button>
            <LibraryStatusPanel
              libraryEntry={featuredLibraryEntry}
              media={featuredMedia}
              size="icon"
            />
          </div>
        {/if}
      </div>

      <!-- ── Prev button ────────────────────────────────────────────── -->
      <Button variant="outline" size="icon" onclick={() => scrollByCards(-1)} aria-label="Scroll left">
        <ChevronLeft class="size-4" />
      </Button>
      <!-- ── Poster track ───────────────────────────────────────────── -->
      <div
              bind:this={trackEl}
              class="flex min-w-0  flex-1 pb-2 [&::-webkit-scrollbar]:hidden"
              style="gap: 12px; overflow-x: auto; display: flex;"
      >
        {#if loading}
          {#each { length: 6 } as _, i (i)}
            <Skeleton
                    class="shrink-0 rounded-xl"
                    style="width: {CARD_W}px; height: {CARD_H}px"
            />
          {/each}
        {:else}
          {#each medias as media, i (media.media_type + "-" + media.id)}
            <ExploreCard
                    {media}
                    index={i}
                    selected={selectedIndex === i}
                    onclick={() => (selectedIndex = i)}
            />
          {/each}
        {/if}
      </div>


      <!-- ── Next button ────────────────────────────────────────────── -->
      <Button variant="outline" size="icon" onclick={() => scrollByCards(1)} aria-label="Scroll right">
        <ChevronRight class="size-4" />
      </Button>
    </div>
  </div>
{/if}
