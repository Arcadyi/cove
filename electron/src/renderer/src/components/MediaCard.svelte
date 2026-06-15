<script lang="ts">
  import type { Details, Media, MediaImages } from "$lib/types/tmdb";
  import { animate } from "animejs";
  import {
    getImageOpt,
    formatRating,
    formatRuntime,
    qualityClass,
  } from "$lib/utils";
  import { api } from "$lib/api";
  import { onMount } from "svelte";
  import { Spinner } from "$lib/components/ui/spinner";
  import MediaHoverCard from "./MediaHoverCard.svelte";
  import MediaExpandedModal from "./MediaExpandedModal.svelte";

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

  // ── DOM refs ──────────────────────────────────────────────────────────────
  let posterEl = $state<HTMLElement | null>(null);
  let buttonEl = $state<HTMLElement | null>(null);
  let hoverCardInstance = $state<MediaHoverCard | null>(null);

  // ── UI state ──────────────────────────────────────────────────────────────
  let hovered = $state(false);
  let expanded = $state(false);
  let hoverCardStyle = $state("");
  let hoverTimeout: ReturnType<typeof setTimeout>;

  // ── Data ──────────────────────────────────────────────────────────────────
  let fetched = false;
  let clips = $state<string[] | null>(null);
  let trailer = $state<string | null>(null);
  let similar = $state<Media[]>([]);
  let images = $state<MediaImages>();
  let logoLoaded = $state(false);
  let genres = $state<string[]>([]);
  let runtime = $state<string>("");
  let cast = $state<string[]>([]);
  let ageRating = $state<string>("");
  let keywords = $state<string[]>([]);
  let originCountry = $state<string[]>([]);
  let numberOfSeasons = $state<number | null>(null);
  let numberOfEpisodes = $state<number | null>(null);

  // ── Derived ───────────────────────────────────────────────────────────────
  const title = $derived(media.media_type === "tv" ? media.name : media.title);

  const videoUrl = $derived.by(() => {
    if (clips?.length) {
      const url = clips[Math.floor(Math.random() * clips.length)];
      if (url?.trim()) return url;
    }
    return trailer?.trim() || null;
  });

  const overviewParagraphs = $derived(
    media.overview
      .split(". ")
      .map((s, i, arr) => (i < arr.length - 1 ? s + "." : s))
      .filter((s) => s.trim().length > 0),
  );

  // ── Data fetching ─────────────────────────────────────────────────────────
  function fetchData(): void {
    if (fetched) return;
    fetched = true;

    api.getClips(media).then((urls) => (clips = urls));
    api.getTrailer(media).then((url) => (trailer = url));
    api.getSimilar(media).then((d) => (similar = d));
    api.getDetails(media).then((d: Details) => {
      genres = d.genres?.map((g: { name: string }) => g.name).slice(0, 3) ?? [];
      runtime = formatRuntime(d);
      cast =
        d.credits?.cast?.slice(0, 5).map((c: { name: string }) => c.name) ?? [];
      ageRating = formatRating(d);
      keywords =
        (media.media_type === "movie"
          ? d.keywords?.keywords
          : d.keywords?.results
        )
          ?.slice(0, 4)
          .map((k: { name: string }) => k.name) ?? [];
      originCountry = d.origin_country;
      if (media.media_type === "tv") {
        numberOfSeasons = d.number_of_seasons ?? null;
        numberOfEpisodes = d.number_of_episodes ?? null;
      }
    });
  }

  // ── Hover card positioning ────────────────────────────────────────────────
  function computeHoverStyle(): void {
    if (!posterEl) return;
    const rect = posterEl.getBoundingClientRect();
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

    hoverCardStyle = `
      position: fixed;
      top: ${centerY}px;
      left: ${left}px;
      translate: ${translateX} -50%;
      width: ${cardWidth}px;
      pointer-events: auto;
    `;
  }

  // ── Hover handlers ────────────────────────────────────────────────────────
  function onHover(): void {
    if (expanded) return;
    hoverTimeout = setTimeout(() => {
      computeHoverStyle();
      hovered = true;
      fetchData();
    }, 400);
  }

  function onLeave(e?: MouseEvent): void {
    clearTimeout(hoverTimeout);
    if (expanded) return;
    const hoverEl = hoverCardInstance?.getEl();
    if (e && hoverEl && hoverEl.contains(e.relatedTarget as Node)) return;
    if (hoverCardInstance) {
      hoverCardInstance.animateClose(() => {
        hovered = false;
      });
    } else {
      hovered = false;
    }
  }

  // ── Expand / close ────────────────────────────────────────────────────────
  function expand(): void {
    expanded = true;
    fetchData();
  }

  function closeExpanded(): void {
    expanded = false;
    hovered = false;
    onclose?.();
  }

  // ── Initial expanded (e.g. deep-linked) ──────────────────────────────────
  $effect(() => {
    if (initialExpanded) {
      expanded = true;
      hovered = true;
      fetchData();
    }
  });

  // ── Load animation ────────────────────────────────────────────────────────
  onMount(() => {
    api.getImages(media).then((d) => {
      images = d;
      logoLoaded = true;
      if (buttonEl) {
        animate(buttonEl, {
          scale: [0.3, 1.05, 1],
          opacity: [0, 1],
          duration: 500,
          easing: "easeOutExpo",
          onComplete: () => {
            // Clear the inline transform so this element no longer acts as a
            // containing block for position:fixed children (hover card).
            if (buttonEl) buttonEl.style.transform = "";
          },
        });
      }
    });
  });
</script>

<div
  bind:this={buttonEl}
  onclick={() => !expanded && onclick(media)}
  onmouseenter={onHover}
  onmouseleave={onLeave}
  class={initialExpanded
    ? "contents"
    : `relative ${!expanded ? "cursor-pointer" : ""} ${hovered || expanded ? "z-50" : "z-0"}`}
  role="button"
  tabindex="0"
  onkeydown={(e) => e.key === "Enter" && !expanded && onclick(media)}
>
  {#if !initialExpanded}
    <div bind:this={posterEl} class="relative">
      {#if logoLoaded && images.posters.length > 0}
        <img
          src={getImageOpt(images, "posters", {
            iso: "en",
            voteAverage: 5,
            randomize: true,
          })}
          alt={title}
          class="block aspect-2/3 w-full rounded-md object-cover"
        />
      {:else if logoLoaded && media.poster_path}
        <img
          src={media.poster_path}
          alt={title}
          class="block aspect-2/3 w-full rounded-md object-cover"
        />
      {:else}
        <div
          class="flex aspect-2/3 w-full items-center justify-center rounded-md"
        >
          <Spinner class="size-10" />
        </div>
      {/if}
      {#if quality}
        <span
          class="absolute bottom-1.5 left-1.5 rounded border px-1.5 py-0.5 text-xs font-medium {qualityClass(
            quality,
          )}"
        >
          {quality.toUpperCase()}
        </span>
      {/if}
      {#if media.media_type === "tv" && numberOfSeasons !== null}
        <span
          class="absolute top-1.5 right-1.5 rounded bg-black/70 px-1.5 py-0.5 text-[10px] font-medium text-white"
        >
          {numberOfSeasons}S
        </span>
      {/if}
    </div>
  {/if}
</div>

{#if hovered && !expanded}
  <MediaHoverCard
    bind:this={hoverCardInstance}
    {media}
    style={hoverCardStyle}
    {videoUrl}
    {genres}
    {runtime}
    {ageRating}
    {originCountry}
    {numberOfSeasons}
    {numberOfEpisodes}
    {quality}
    onmouseleave={onLeave}
    onwatch={() => onclick(media)}
    onexpand={expand}
  />
{/if}

{#if expanded}
  <MediaExpandedModal
    {media}
    {videoUrl}
    {overviewParagraphs}
    {genres}
    {runtime}
    {ageRating}
    {originCountry}
    {numberOfSeasons}
    {numberOfEpisodes}
    {cast}
    {keywords}
    {similar}
    {quality}
    onwatch={() => onclick(media)}
    onclose={closeExpanded}
    onsimilar={(m) => {
      closeExpanded();
      onsimilar?.(m);
    }}
  />
{/if}
