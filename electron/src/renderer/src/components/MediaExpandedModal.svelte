<script lang="ts">
  import type { Media } from "$lib/types/tmdb";
  import { Separator } from "$lib/components/ui/separator/index.js";
  import { animate } from "animejs";
  import { Badge } from "$lib/components/ui/badge/index.js";
  import * as ButtonGroup from "$lib/components/ui/button-group/index.js";
  import { Button } from "$lib/components/ui/button";
  import { Play, Star, X } from "lucide-svelte";
  import { countryName, qualityClass } from "$lib/utils";
  import PlayerSimple from "./PlayerSimple.svelte";

  let {
    media,
    videoUrl,
    overviewParagraphs,
    genres,
    runtime,
    ageRating,
    originCountry,
    numberOfSeasons,
    numberOfEpisodes,
    cast,
    keywords,
    similar,
    quality,
    onwatch,
    onclose,
    onsimilar,
  }: {
    media: Media;
    videoUrl: string | null;
    overviewParagraphs: string[];
    genres: string[];
    runtime: string;
    ageRating: string;
    originCountry: string[];
    numberOfSeasons: number | null;
    numberOfEpisodes: number | null;
    cast: string[];
    keywords: string[];
    similar: Media[];
    quality: string | null;
    onwatch: () => void;
    onclose: () => void;
    onsimilar?: (m: Media) => void;
  } = $props();

  let el = $state<HTMLElement | null>(null);

  const title = $derived(media.media_type === "tv" ? media.name : media.title);
  const year = $derived(
    (media.media_type === "tv"
      ? media.first_air_date
      : media.release_date
    )?.slice(0, 4),
  );

  // Click-outside to close
  $effect(() => {
    function handleClickOutside(e: MouseEvent): void {
      if (el && !el.contains(e.target as Node)) close();
    }
    document.addEventListener("mousedown", handleClickOutside);
    return () => document.removeEventListener("mousedown", handleClickOutside);
  });

  // Animate in on mount
  $effect(() => {
    if (!el) return;
    animate(el, {
      scale: [0.9, 1],
      opacity: [0, 1],
      duration: 200,
      easing: "easeOutQuart",
    });
  });

  function close(): void {
    if (!el) {
      onclose();
      return;
    }
    animate(el, {
      scale: [1, 0.9],
      opacity: [1, 0],
      duration: 200,
      easing: "easeInQuart",
      onComplete: onclose,
    });
  }
</script>

<!-- Backdrop -->
<div
  role="presentation"
  class="fixed inset-0 z-40 bg-black/50 backdrop-blur-sm"
  onmousedown={close}
></div>

<!-- Centering wrapper — handles position only, never animated -->
<div
  class="pointer-events-none fixed inset-0 z-50 flex items-center justify-center"
>
  <!-- Modal — only scale is animated, no translate conflict -->
  <!-- svelte-ignore a11y_no_static_element_interactions -->

  <div
    bind:this={el}
    role="presentation"
    class="pointer-events-auto flex max-h-[90vh] w-[min(860px,92vw)] cursor-default flex-col overflow-hidden overflow-y-auto rounded-lg border border-border bg-background shadow-2xl"
    onclick={(e) => e.stopPropagation()}
    onkeydown={(e) => e.stopPropagation()}
    style="opacity: 0; transform: scale(0.9);"
  >

    {#if videoUrl}
      <PlayerSimple src={videoUrl} controls={true} bg={media.poster_path} />
    {:else}
      <img
        src={media.poster_path}
        alt={title}
        class="aspect-video w-full object-cover"
      />
    {/if}

    <div class="flex flex-col gap-2 p-5">
      <div class="flex w-full items-baseline justify-between pb-3">
        <span class="flex min-w-0 flex-1 items-baseline gap-2 pr-3">
          <span class="text-md truncate leading-none font-semibold"
            >{title}</span
          >
          {#if year}<Badge variant="default">{year}</Badge>{/if}
        </span>
        <span
          class="flex flex-row items-center justify-center gap-1 text-xs leading-none whitespace-nowrap text-yellow-400"
        >
          <Star class="size-4" />
          {media.vote_average?.toFixed(1)}
        </span>
      </div>

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

      <div class="grid grid-cols-[1fr_auto] gap-x-3 gap-y-3">
        <div class="flex flex-col justify-between gap-3 rounded-lg">
          {#each overviewParagraphs as paragraph, i (i)}
            <p class="text-sm leading-relaxed text-muted-foreground">
              {paragraph}
            </p>
          {/each}

          {#if similar.length}
            <div class="rounded-lg border border-border">
              <div class="px-3 py-2 text-xs font-medium">More like this</div>
              <Separator />
              <div class="grid grid-cols-6 gap-2 p-3">
                {#each similar as item (item.id)}
                  <div
                    role="button"
                    tabindex="0"
                    class="cursor-pointer overflow-hidden rounded-md"
                    onclick={(e) => {
                      e.stopPropagation();
                      onsimilar?.(item);
                    }}
                    onkeydown={(e) => e.key === "Enter" && onsimilar?.(item)}
                  >
                    <img
                      src={item.poster_path}
                      alt={item.media_type === "tv" ? item.name : item.title}
                      class="aspect-2/3 w-full object-cover transition-opacity hover:opacity-75"
                    />
                  </div>
                {/each}
              </div>
            </div>
          {/if}
        </div>

        <div class="flex w-48 flex-col gap-3">
          {#if cast.length}
            <div class="rounded-lg border border-border">
              <div class="px-3 py-2 text-xs font-medium">Cast</div>
              <Separator />
              <div class="flex flex-wrap gap-1.5 p-3">
                {#each cast.slice(0, 5) as person (person)}
                  <Button
                    onclick={(e) => e.stopPropagation()}
                    variant="outline"
                    size="xs"
                  >
                    {person}
                  </Button>
                {/each}
              </div>
            </div>
          {/if}

          {#if keywords.length}
            <div class="rounded-lg border border-border">
              <div class="px-3 py-2 text-xs font-medium">
                This {media.media_type === "tv" ? "show" : "film"} is
              </div>
              <Separator />
              <div class="flex flex-wrap gap-1.5 p-3">
                {#each keywords as keyword (keyword)}
                  <Button
                    onclick={(e) => e.stopPropagation()}
                    variant="outline"
                    size="xs"
                  >
                    {keyword}
                  </Button>
                {/each}
              </div>
            </div>
          {/if}
        </div>
      </div>

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
              close();
            }}
          >
            <X class="size-3" /> Close
          </Button>
        </ButtonGroup.Root>
      </span>
    </div>
  </div>
</div>
