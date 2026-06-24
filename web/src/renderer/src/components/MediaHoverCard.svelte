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
  import { api, STATUS_LABELS, type LibraryStatus } from "$lib/api";
  import type { LibraryEntry, WatchProgress } from "$lib/types/library";
  import LibraryStatusPanel from "./LibraryStatusPanel.svelte";
  import StarRating from "./StarRating.svelte";

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
    lastAiredSeason = null,
    lastAiredEpisode = null,
    quality,
    onwatch,
    onexpand,
    onmouseleave,
    onpopoverchange,
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
    lastAiredSeason?: number | null;
    lastAiredEpisode?: number | null;
    quality: string | null;
    onwatch: (season?: number, episode?: number) => void;
    onexpand: () => void;
    onmouseleave?: (e: MouseEvent) => void;
    onpopoverchange?: (open: boolean) => void;
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

  // ── Library state ─────────────────────────────────────────────────────────────

  let libraryEntry = $state<LibraryEntry | null>(null);
  let movieProgress = $state<WatchProgress | null>(null);

  $effect(() => {
    api
      .libraryGet(media.id, media.media_type)
      .then((result) => {
        if (!result) return;
        libraryEntry = result.entry;
        if (media.media_type === "movie") {
          movieProgress = result.progress[0] ?? null;
        }
      })
      .catch(console.error);
  });

  const movieProgressPct = $derived(
    movieProgress && movieProgress.duration_seconds > 0
      ? Math.min(
          100,
          (movieProgress.position_seconds / movieProgress.duration_seconds) *
            100,
        )
      : 0,
  );

  const hasIncompleteProgress = $derived(
    movieProgress !== null && !movieProgress.completed && movieProgressPct > 1,
  );

  // For TV shows show which episode to resume; for movies "Continue" or "Watch".
  const watchButtonLabel = $derived.by(() => {
    if (media.media_type === "tv") {
      const s = libraryEntry?.last_watched_season;
      const e = libraryEntry?.last_watched_episode;
      if (s != null && e != null) return `Continue S${s}E${e}`;
    }
    return hasIncompleteProgress ? "Continue" : "Watch";
  });

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

<!-- svelte-ignore a11y_no_static_element_interactions a11y_click_events_have_key_events -->
<span
  bind:this={el}
  role="presentation"
  class="z-50 flex min-w-75 cursor-pointer flex-col overflow-hidden rounded-lg border border-border bg-background shadow-2xl"
  {onmouseleave}
  onclick={() => onexpand()}
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

    <span class="flex flex-1 flex-col gap-2 pr-3">
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

    <span class="line-clamp-3 text-xs text-muted-foreground"
      >{media.overview}</span
    >

    <!-- Movie progress bar -->
    {#if hasIncompleteProgress}
      <div class="h-1 w-full overflow-hidden rounded-full bg-secondary">
        <div
          class="h-full rounded-full bg-foreground/60 transition-all"
          style="width: {movieProgressPct}%"
        ></div>
      </div>
    {/if}

    <span class="flex w-full items-center justify-between gap-1 pt-0.5">
      <span class="contents" onclick={(e) => e.stopPropagation()}>
        <StarRating
          {libraryEntry}
          {media}
          {lastAiredSeason}
          {lastAiredEpisode}
        />
      </span>

      <!-- svelte-ignore a11y_no_static_element_interactions a11y_click_events_have_key_events -->
      <span class="contents" onclick={(e) => e.stopPropagation()}>
        <LibraryStatusPanel
          {libraryEntry}
          {media}
          {lastAiredSeason}
          {lastAiredEpisode}
          size="icon-lg"
          {onpopoverchange}
        />
      </span>
    </span>
  </span>
</span>
