<script lang="ts">
  import type { Media } from "$lib/types/tmdb";
  import { Separator } from "$lib/components/ui/separator/index.js";
  import { animate } from "animejs";
  import { Badge } from "$lib/components/ui/badge/index.js";
  import * as ButtonGroup from "$lib/components/ui/button-group/index.js";
  import { Button } from "$lib/components/ui/button";
  import { ChevronDown, Play, Star } from "lucide-svelte";
  import { countryName, qualityClass } from "$lib/utils";
  import PlayerSimple from "./PlayerSimple.svelte";

  let {
    media,
    style,
    videoUrl,
    genres,
    runtime,
    ageRating,
    originCountry,
    numberOfSeasons,
    numberOfEpisodes,
    quality,
    onwatch,
    onexpand,
    onmouseleave,
  }: {
    media: Media;
    style: string;
    videoUrl: string | null;
    genres: string[];
    runtime: string;
    ageRating: string;
    originCountry: string[];
    numberOfSeasons: number | null;
    numberOfEpisodes: number | null;
    quality: string | null;
    onwatch: () => void;
    onexpand: () => void;
    onmouseleave?: (e: MouseEvent) => void;
  } = $props();

  // Expose the root element so the parent can check relatedTarget against it
  export function getEl(): HTMLElement | null {
    return el;
  }

  let el = $state<HTMLElement | null>(null);

  const title = $derived(media.media_type === "tv" ? media.name : media.title);
  const year = $derived(
    (media.media_type === "tv"
      ? media.first_air_date
      : media.release_date
    )?.slice(0, 4),
  );

  // Animate in when mounted
  $effect(() => {
    if (!el) return;
    animate(el, {
      scale: [0.85, 1],
      opacity: [0, 1],
      duration: 200,
      easing: "easeOutQuart",
    });
  });

  // Expose close animation so the parent can await it before hiding
  export function animateClose(onComplete: () => void): void {
    if (!el) {
      onComplete();
      return;
    }
    animate(el, {
      scale: [1, 0.85],
      opacity: [1, 0],
      duration: 150,
      easing: "easeInQuart",
      onComplete,
    });
  }
</script>

<!-- svelte-ignore a11y_no_static_element_interactions -->
<span
  bind:this={el}
  role="presentation"
  class="pointer-events-auto z-50 flex min-w-75 cursor-default flex-col overflow-hidden rounded-lg border border-border bg-background shadow-2xl"
  onclick={(e) => e.stopPropagation()}
  onkeydown={(e) => e.stopPropagation()}
  {onmouseleave}
  style="opacity: 0; transform: scale(0.85); {style}"
>
  {#if videoUrl}
    <PlayerSimple src={videoUrl} controls={false} bg={media.poster_path} />
  {:else}
    <img
      src={media.poster_path}
      alt={title}
      class="aspect-video w-full object-cover"
    />
  {/if}

  <span class="flex flex-col gap-2 p-3">
    <span class="flex w-full items-baseline justify-between pb-1">
      <span class="flex min-w-0 flex-1 items-baseline gap-2 pr-3">
        <span class="text-md truncate leading-none font-semibold">{title}</span>
        {#if year}<Badge variant="default">{year}</Badge>{/if}
      </span>
      <span
        class="flex flex-row items-center justify-center gap-1 text-xs leading-none whitespace-nowrap text-yellow-400"
      >
        <Star class="size-4" />
        {media.vote_average?.toFixed(1)}
      </span>
    </span>

    <Separator />

    <span class="flex flex-col gap-2 pr-3">
      <span class="flex flex-wrap items-center gap-2">
        {#if ageRating}
          <span class="rounded border border-border px-1.5 py-0.5 text-xs"
            >{ageRating}</span
          >
        {/if}
        {#if originCountry.length}
          <span class="rounded border border-border px-1.5 py-0.5 text-xs">
            {originCountry.map((c) => countryName(c)).join(", ")}
          </span>
        {/if}
        {#if runtime}
          <span class="rounded border border-border px-1.5 py-0.5 text-xs"
            >{runtime}</span
          >
        {/if}
        {#if media.media_type === "tv" && numberOfSeasons !== null}
          <span class="rounded border border-border px-1.5 py-0.5 text-xs">
            {numberOfSeasons} season{numberOfSeasons !== 1 ? "s" : ""}
          </span>
        {/if}
        {#if media.media_type === "tv" && numberOfEpisodes !== null}
          <span class="rounded border border-border px-1.5 py-0.5 text-xs">
            {numberOfEpisodes} ep{numberOfEpisodes !== 1 ? "s" : ""}
          </span>
        {/if}
        {#if quality}
          <span
            class="rounded border px-1.5 py-0.5 text-xs font-medium {qualityClass(
              quality,
            )}"
          >
            {quality.toUpperCase()}
          </span>
        {/if}
      </span>
      {#if genres.length}
        <span class="flex flex-wrap gap-1">
          {#each genres as genre (genre)}
            <span
              class="rounded-full bg-secondary px-2 py-0.5 text-xs whitespace-nowrap text-secondary-foreground"
            >
              {genre}
            </span>
          {/each}
        </span>
      {/if}
    </span>

    <span class="line-clamp-2 text-xs text-muted-foreground"
      >{media.overview}</span
    >

    <span class="flex w-full pt-0.5">
      <ButtonGroup.Root class="flex w-full">
        <Button
          class="w-[75%] border-b border-accent bg-accent text-accent-foreground hover:bg-accent-foreground hover:text-accent"
          variant="default"
          size="sm"
          onclick={(e) => {
            e.stopPropagation();
            onwatch();
          }}
        >
          <Play class="size-3" /> Watch
        </Button>
        <Button
          class="w-[25%]"
          variant="outline"
          size="sm"
          onclick={(e) => {
            e.stopPropagation();
            onexpand();
          }}
        >
          <ChevronDown class="size-3" /> Details
        </Button>
      </ButtonGroup.Root>
    </span>
  </span>
</span>
