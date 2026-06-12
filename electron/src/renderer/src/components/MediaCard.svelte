<script lang="ts">
  import type { Media } from "$lib/types/tmdb";
  import { Separator } from "$lib/components/ui/separator/index.js";
  import { animate } from "animejs";
  import { Star, ChevronDown, X, Play } from "lucide-svelte";
  import { Button } from "$lib/components/ui/button";
  import { Badge } from "$lib/components/ui/badge/index.js";
  import * as ButtonGroup from "$lib/components/ui/button-group/index.js";
  import { countryName, qualityClass } from "$lib/utils";

  let {
    media,
    onclick,
    quality = null,
    initialExpanded = false,
    onclose,
    onsimilar,
  }: {
    media: Media;
    onclick: (m: Media) => void;
    quality?: string | null;
    initialExpanded?: boolean;
    onclose?: () => void;
    onsimilar?: (m: Media) => void;
  } = $props();

  let trailer = $state<string | null>(null);
  let hovered = $state(false);
  let expanded = $state(false);
  let fetched = false;
  let cardEl = $state<HTMLElement | null>(null);
  let buttonEl = $state<HTMLElement | null>(null);
  let keywords = $state<string[]>([]);
  let hoverCardStyle = $state("");
  let similar = $state<Media[]>([]);
  let originCountry: string[] = $state([]);

  let genres = $state<string[]>([]);
  let runtime = $state<string>("");
  let cast = $state<string[]>([]);
  let ageRating = $state<string>("");

  $effect(() => {
    if (initialExpanded) {
      expanded = true;
      hovered = true;
      fetchData();
    }
  });

  // Click-outside handler to close expanded card
  $effect(() => {
    if (!expanded) return () => {};

    function handleClickOutside(e: MouseEvent): void {
      if (cardEl && !cardEl.contains(e.target as Node)) {
        closeExpanded();
      }
    }

    document.addEventListener("mousedown", handleClickOutside);
    return () => document.removeEventListener("mousedown", handleClickOutside);
  });

  const overviewParagraphs = $derived(
    media.overview
      .split(". ")
      .map((s, i, arr) => (i < arr.length - 1 ? s + "." : s))
      .filter((s) => s.trim().length > 0),
  );

  function computeHoverStyle(): void {
    if (!buttonEl) return;
    const rect = buttonEl.getBoundingClientRect();
    const cardWidth = rect.width * 2.2;
    const vw = window.innerWidth;
    const centerX = rect.left + rect.width / 2;
    const centerY = rect.top + rect.height / 2;

    let left: number;
    let translateX: string;

    if (centerX - cardWidth / 2 < 8) {
      left = 8;
      translateX = "0%";
    } else if (centerX + cardWidth / 2 > vw - 8) {
      left = vw - 8;
      translateX = "-100%";
    } else {
      left = centerX;
      translateX = "-50%";
    }

    hoverCardStyle = `position: fixed; top: ${centerY}px; left: ${left}px; translate: ${translateX} -50%; width: ${cardWidth}px;`;
  }

  $effect(() => {
    if (cardEl && buttonEl) {
      if (!expanded) computeHoverStyle();
      animate(cardEl, {
        scale: [0.85, 1],
        opacity: [0, 1],
        duration: 200,
        easing: "easeOutQuart",
      });
    }
  });

  function fetchData(): void {
    if (fetched) return;
    fetched = true;

    fetch(
      `http://localhost:6969/api/trailer?id=${media.id}&type=${media.media_type}`,
    )
      .then((r) => r.json())
      .then((d) => (trailer = d.url));

    fetch(
      `http://localhost:6969/api/similar?id=${media.id}&type=${media.media_type}`,
    )
      .then((r) => r.json())
      .then((d) => (similar = d ?? []));

    fetch(
      `http://localhost:6969/api/details?id=${media.id}&type=${media.media_type}`,
    )
      .then((r) => r.json())
      .then((d) => {
        genres =
          d.genres?.map((g: { name: string }) => g.name).slice(0, 3) ?? [];
        runtime =
          d.runtime > 0
            ? `${Math.floor(d.runtime / 60)}h ${d.runtime % 60}m`
            : d.episode_run_time?.[0]
              ? `${d.episode_run_time[0]}m / ep`
              : "";
        cast =
          d.credits?.cast?.slice(0, 5).map((c: { name: string }) => c.name) ??
          [];
        ageRating = (() => {
          for (const r of d.release_dates?.results ?? []) {
            if (r.iso_3166_1 === "US") {
              for (const rd of r.release_dates ?? []) {
                if (rd.certification) return rd.certification;
              }
            }
          }
          for (const r of d.content_ratings?.results ?? []) {
            if (r.iso_3166_1 === "US" && r.rating) return r.rating;
          }
          return "";
        })();
        keywords =
          (media.media_type === "movie"
            ? d.keywords?.keywords
            : d.keywords?.results
          )
            ?.slice(0, 4)
            .map((k: { name: string }) => k.name) ?? [];
        originCountry = d.origin_country ?? [];
      });
  }

  function onHover(): void {
    if (expanded) return;
    computeHoverStyle();
    hovered = true;
    fetchData();
  }

  function onLeave(): void {
    if (expanded) return; // Don't close if expanded

    if (cardEl) {
      animate(cardEl, {
        scale: [1, 0.85],
        opacity: [1, 0],
        duration: 150,
        easing: "easeInQuart",
        onComplete: () => {
          hovered = false;
        },
      });
    } else {
      hovered = false;
    }
  }

  function expandCard(e: MouseEvent): void {
    e.stopPropagation();
    expanded = true;
    fetchData();

    if (cardEl) {
      animate(cardEl, {
        scale: [1, 1.02, 1],
        duration: 250,
        easing: "easeOutQuart",
      });
    }
  }

  function closeExpanded(e?: MouseEvent): void {
    e?.stopPropagation();
    if (cardEl) {
      animate(cardEl, {
        scale: [1, 0.9],
        opacity: [1, 0],
        duration: 200,
        easing: "easeInQuart",
        onComplete: () => {
          expanded = false;
          hovered = false;
          onclose?.();
        },
      });
    } else {
      expanded = false;
      hovered = false;
      onclose?.();
    }
  }

  const title = $derived(media.media_type === "tv" ? media.name : media.title);
  const year = $derived(
    (media.media_type === "tv"
      ? media.first_air_date
      : media.release_date
    )?.slice(0, 4),
  );
  const trailerUrl = $derived(
    trailer
      ? `${trailer}?autoplay=1&controls=0&modestbranding=1&loop=1&rel=0&iv_load_policy=3&disablekb=1`
      : null,
  );
</script>

<!-- Dim backdrop when expanded -->
{#if expanded}
  <div
    role="presentation"
    class="fixed inset-0 z-40 bg-black/50 backdrop-blur-sm"
    onmousedown={closeExpanded}
  ></div>
{/if}

<div
  bind:this={buttonEl}
  onclick={() => !expanded && onclick(media)}
  onmouseenter={onHover}
  onmouseleave={onLeave}
  class={initialExpanded
    ? "contents"
    : `relative ${!expanded ? "cursor-pointer" : ""}`}
  role="button"
  tabindex="0"
  onkeydown={(e) => e.key === "Enter" && !expanded && onclick(media)}
>
  {#if !initialExpanded}
    <div class="relative">
      <img
        src={media.poster_path}
        alt={title}
        class="block aspect-2/3 w-full rounded-md object-cover"
      />
      {#if quality}
        <span
          class="absolute bottom-1.5 left-1.5 rounded border px-1.5 py-0.5 text-xs font-medium {qualityClass(
            quality,
          )}"
        >
          {quality.toUpperCase()}
        </span>
      {/if}
    </div>
  {/if}

  {#if hovered || expanded}
    <span
      bind:this={cardEl}
      role="presentation"
      onclick={(e) => expanded && e.stopPropagation()}
      onkeydown={(e) => expanded && e.stopPropagation()}
      style="opacity: 0; transform: scale(0.85); {expanded
        ? 'position: fixed; top: 50%; left: 50%; translate: -50% -50%; width: min(860px, 92vw); max-height: 90vh; overflow-y: auto;'
        : hoverCardStyle}"
      class="z-50 flex min-w-75 cursor-default flex-col overflow-hidden rounded-lg border border-border bg-background shadow-2xl"
    >
      {#if trailerUrl}
        <iframe
          src={trailerUrl}
          title="{title} trailer"
          class="aspect-video w-full"
          allow="autoplay; encrypted-media"
        ></iframe>
      {:else}
        <img
          src={media.poster_path}
          alt={title}
          class="aspect-video w-full object-cover"
        />
      {/if}

      <span class="flex flex-col gap-2 {expanded ? 'p-5' : 'p-3'}">
        <!-- Title row -->
        <span
          class="flex w-full items-baseline justify-between {expanded
            ? 'pb-3'
            : 'pb-1'}"
        >
          <span class="flex min-w-0 flex-1 items-baseline gap-2 pr-3">
            <span class="text-md truncate leading-none font-semibold">
              {title}
            </span>
            {#if year}
              <Badge variant="default">{year}</Badge>
            {/if}
          </span>
          <span
            class="flex flex-row items-center justify-center gap-1 text-xs leading-none whitespace-nowrap text-yellow-400"
          >
            <Star class="size-4" />
            {media.vote_average?.toFixed(1)}
          </span>
        </span>

        <Separator />

        <!-- Rating + runtime -->
        <span class="flex flex-col gap-2 pr-3">
          <span class="flex items-center gap-2">
            {#if ageRating}
              <span class="rounded border border-border px-1.5 py-0.5 text-xs">
                {ageRating}
              </span>
            {/if}
            {#if originCountry.length}
              <span class="rounded border border-border px-1.5 py-0.5 text-xs">
                {originCountry.map((code) => countryName(code)).join(", ")}
              </span>
            {/if}
            {#if runtime}
              <span class="rounded border border-border px-1.5 py-0.5 text-xs">
                {runtime}
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

        <!-- Overview -->
        {#if expanded}
          <div class="grid grid-cols-[1fr_auto] gap-x-3 gap-y-3">
            <!-- Left: overview -->
            <div class="flex flex-col justify-between gap-3 rounded-lg">
              {#each overviewParagraphs as paragraph, i (i)}
                <p class="text-sm leading-relaxed text-muted-foreground">
                  {paragraph}
                </p>
              {/each}
              {#if similar.length}
                <div class="rounded-lg border border-border">
                  <div class="px-3 py-2 text-xs font-medium">
                    More like this
                  </div>
                  <Separator />
                  <div class="grid grid-cols-6 gap-2 p-3">
                    {#each similar as item (item.id)}
                      <div
                        role="button"
                        tabindex="0"
                        class="cursor-pointer overflow-hidden rounded-md"
                        onclick={(e) => {
                          e.stopPropagation();
                          expanded = false;
                          hovered = false;
                          onsimilar?.(item);
                        }}
                        onkeydown={(e) => e.key === "Enter" && onclick(item)}
                      >
                        <img
                          src={item.poster_path}
                          alt={item.media_type === "tv"
                            ? item.name
                            : item.title}
                          class="aspect-2/3 w-full object-cover transition-opacity hover:opacity-75"
                        />
                      </div>
                    {/each}
                  </div>
                </div>
              {/if}
            </div>

            <!-- Right: cast + keywords -->
            <div class="flex w-48 flex-col gap-3">
              {#if cast.length}
                <div class="rounded-lg border border-border">
                  <div class="px-3 py-2 text-xs font-medium">Cast</div>
                  <Separator />
                  <div class="flex flex-wrap gap-1.5 p-3">
                    {#each cast.slice(0, 5) as person (person)}
                      <Button
                        onclick={(e) => {
                          e.stopPropagation();
                        }}
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
                        onclick={(e) => {
                          e.stopPropagation();
                        }}
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
        {:else}
          <span class="line-clamp-2 text-xs text-muted-foreground"
            >{media.overview}</span
          >
        {/if}

        <!-- Watch - Expand / close button -->
        <span class="flex w-full pt-0.5">
          <ButtonGroup.Root class="flex w-full">
            <Button
              class="w-[75%] border-b border-accent bg-accent text-accent-foreground hover:bg-accent-foreground hover:text-accent"
              variant="default"
              size="sm"
              onclick={() => onclick(media)}
            >
              <Play class="size-3" /> Watch
            </Button>
            {#if expanded}
              <Button
                class="w-[25%]"
                variant="outline"
                size="sm"
                onclick={closeExpanded}
              >
                <X class="size-3" /> Close
              </Button>
            {:else}
              <Button
                class="w-[25%]"
                variant="outline"
                size="sm"
                onclick={expandCard}
              >
                <ChevronDown class="size-3" /> Details
              </Button>
            {/if}
          </ButtonGroup.Root>
        </span>
      </span>
    </span>
  {/if}
</div>
