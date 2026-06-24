<script lang="ts">
  import type { Details, Media, MediaImages } from "$lib/types/tmdb";
  import { animate } from "animejs";
  import {
    getImageOpt,
    formatRating,
    formatRuntime,
    getVideoOpt,
  } from "$lib/utils";
  import { api } from "$lib/api";
  import { getContext, onMount } from "svelte";
  import { Spinner } from "$lib/components/ui/spinner";
  import MediaHoverCard from "./MediaHoverCard.svelte";

  import type { LibraryEntry } from "$lib/types/library";
  import { libraryChanged } from "$lib/stores/library";
  import { CircleCheckBig, HeartOff } from "lucide-svelte";
  import * as ContextMenu from "$lib/components/ui/context-menu/index.js";
  import LibraryContextMenuContent from "./LibraryContextMenuContent.svelte";

  let {
    media,
    onclick,
    quality = null,
    newEpisodes = false,
    onwatch,
  }: {
    media: Media;
    onclick: (m: Media) => void;
    quality?: string | null;
    newEpisodes?: boolean;
    onwatch?: (m: Media, season?: number, episode?: number) => void;
  } = $props();

  // Opens the shared, app-level detail overlay. Provided via context by
  // App.svelte, so every card — wherever it sits in the tree — reaches the
  // same single modal without any prop drilling. Falls back to the onclick
  // prop if a card is ever rendered outside that provider.
  const openDetail = getContext<((m: Media) => void) | undefined>(
    "openMediaDetail",
  );
  function openOverlay(): void {
    if (openDetail) openDetail(media);
    else onclick(media);
  }

  // ── DOM refs ──────────────────────────────────────────────────────────────
  let posterEl = $state<HTMLElement | null>(null);
  let buttonEl = $state<HTMLElement | null>(null);
  let hoverCardInstance = $state<MediaHoverCard | null>(null);

  // Lazy loading: a card only fetches its art and library state once it scrolls
  // near the viewport (see the IntersectionObserver in onMount). Without this, a
  // long multi-row page fires getImages + libraryGet for every off-screen card
  // on mount — hundreds of requests at once.
  let visible = $state(false);

  // ── UI state ──────────────────────────────────────────────────────────────
  let hovered = $state(false);
  let hoverCardStyle = $state("");
  let hoverTimeout: ReturnType<typeof setTimeout>;

  // ── Data (for the hover card) ───────────────────────────────────────────────
  let fetched = false;
  let images = $state<MediaImages>();
  let logoLoaded = $state(false);
  let genres = $state<string[]>([]);
  let runtime = $state<string>("");
  let ageRating = $state<string>("");
  let originCountry = $state<string[]>([]);
  let numberOfSeasons = $state<number | null>(null);
  let numberOfEpisodes = $state<number | null>(null);
  let lastAiredSeason = $state<number | null>(null);
  let lastAiredEpisode = $state<number | null>(null);
  let videoUrl = $state<string>();
  let libraryEntry = $state<LibraryEntry | null>(null);
  const isWatched = $derived(libraryEntry?.status === "finished");
  const isDropped = $derived(libraryEntry?.status === "dropped");

  // ── Derived ───────────────────────────────────────────────────────────────
  const title = $derived(media.media_type === "tv" ? media.name : media.title);

  // ── Data fetching (hover card only) ─────────────────────────────────────────
  function fetchData(): void {
    if (fetched) return;
    fetched = true;

    api.getVideos(media).then((d) => {
      videoUrl = getVideoOpt(d, "Clip", { randomize: true });
    });
    api
      .getDetails(media)
      .then((d: Details) => {
        genres =
          d.genres?.map((g: { name: string }) => g.name).slice(0, 3) ?? [];
        runtime = formatRuntime(d);
        ageRating = formatRating(d);
        originCountry = d.origin_country;
        if (media.media_type === "tv") {
          numberOfSeasons = d.number_of_seasons ?? null;
          numberOfEpisodes = d.number_of_episodes ?? null;
          lastAiredSeason = d.last_episode_to_air?.season_number ?? null;
          lastAiredEpisode = d.last_episode_to_air?.episode_number ?? null;
        }
      })
      .catch((err) => {
        console.error("MediaCard getDetails failed", {
          tmdbId: media.id,
          mediaType: media.media_type,
          error: err,
        });
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
  let popoverOpen = $state(false);

  // True whenever the pointer is anywhere inside the card, the hover card, or
  // a popover opened from either of them. Mouse events keep this current;
  // closing itself is decided by closeIfIdle() rather than directly inside
  // the mouse handlers, since popover open/close also needs to trigger it —
  // and a popover closing doesn't fire any mouse event of its own.
  let withinZone = $state(false);

  function isWithinZone(target: Node | null): boolean {
    if (!target) return false;
    if (buttonEl?.contains(target)) return true;
    const hoverEl = hoverCardInstance?.getEl();
    if (hoverEl?.contains(target)) return true;
    // Popovers are portalled to <body>, so they're never a DOM descendant of
    // either element above — check separately.
    const popover = document.querySelector(
      "[data-radix-popper-content-wrapper]",
    );
    return !!popover?.contains(target);
  }

  function closeIfIdle(): void {
    if (popoverOpen || withinZone) return;
    if (hoverCardInstance) {
      hoverCardInstance.animateClose(() => {
        hovered = false;
      });
    } else {
      hovered = false;
    }
  }

  function onHover(): void {
    withinZone = true;
    hoverTimeout = setTimeout(() => {
      computeHoverStyle();
      hovered = true;
      fetchData();
    }, 500);
  }

  function onLeave(e?: MouseEvent): void {
    clearTimeout(hoverTimeout);
    withinZone = isWithinZone((e?.relatedTarget ?? null) as Node | null);
    closeIfIdle();
  }

  // Re-evaluate whenever a popover closes. This is the missing piece: picking
  // an option in a popover doesn't move the mouse, so no mouseleave fires —
  // without this, the hover card would only close on the next unrelated
  // hover/unhover cycle (or, as just observed, never at all once the popover
  // starts closing itself immediately on selection).
  $effect(() => {
    if (!popoverOpen) closeIfIdle();
  });

  $effect(() => {
    // eslint-disable-next-line @typescript-eslint/no-unused-expressions
    $libraryChanged;
    // Skip off-screen cards; the lookup runs once the card becomes visible (and
    // re-runs for visible cards whenever the library changes).
    if (!visible) return;
    api
      .libraryGet(media.id, media.media_type)
      .then((result) => {
        libraryEntry = result?.entry ?? null;
      })
      .catch((err) => {
        // Backend unreachable (e.g. dev server restart) — keep the last known
        // entry rather than throwing an uncaught rejection.
        console.error("MediaCard: failed to load library entry", err);
      });
  });

  // ── Lazy art loading ──────────────────────────────────────────────────────
  // Fetch poster/logo art only once the card is (near) visible, then reveal it.
  let imagesRequested = false;
  $effect(() => {
    if (!visible || imagesRequested) return;
    imagesRequested = true;
    api
      .getImages(media)
      .then((d) => {
        images = d;
      })
      .catch((err) => {
        // Network/server hiccup: fall back to the plain poster (the template's
        // poster branch) instead of leaving the card un-rendered.
        console.error("MediaCard: failed to load images", err);
        images = { backdrops: [], logos: [], posters: [] };
      })
      .finally(() => {
        // Reveal + animate in regardless of whether images loaded, so a failed
        // fetch can't strand the card invisible.
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

  // Flip `visible` when the card scrolls within 300px of the viewport, which
  // triggers the two effects above. Loads eagerly if IntersectionObserver is
  // unavailable. Library state still updates on $libraryChanged afterwards.
  onMount(() => {
    if (!buttonEl || typeof IntersectionObserver === "undefined") {
      visible = true;
      return;
    }
    const io = new IntersectionObserver(
      (entries) => {
        if (entries.some((e) => e.isIntersecting)) {
          visible = true;
          io.disconnect(); // load once; no need to track further
        }
      },
      { rootMargin: "300px" },
    );
    io.observe(buttonEl);
    return () => io.disconnect();
  });
</script>

<ContextMenu.Root>
  <ContextMenu.Trigger>
    <div
      bind:this={buttonEl}
      onclick={openOverlay}
      onmouseenter={onHover}
      onmouseleave={onLeave}
      class={`relative cursor-pointer ${hovered ? "z-50" : "z-0"}`}
      role="button"
      tabindex="0"
      onkeydown={(e) => e.key === "Enter" && openOverlay()}
    >
      <div bind:this={posterEl} class="relative">
        {#if logoLoaded && images.posters.length > 0}
          <img
            src={getImageOpt(images, "posters", {
              iso: "en",
              voteAverage: 5,
              randomize: true,
            })}
            alt={title}
            class="block aspect-2/3 w-full rounded-md object-cover transition-all duration-300 {isWatched
              ? 'opacity-35'
              : 'opacity-100'} {isDropped ? 'opacity-10 grayscale' : ''}"
          />
        {:else if logoLoaded && media.poster_path}
          <img
            src={media.poster_path}
            alt={title}
            class="block aspect-2/3 w-full rounded-md object-cover transition-all duration-300 {isWatched
              ? 'opacity-35'
              : 'opacity-100'} {isDropped ? 'opacity-10 grayscale' : ''}"
          />
        {:else}
          <div
            class="flex aspect-2/3 w-full items-center justify-center rounded-md"
          >
            <Spinner class="size-10" />
          </div>
        {/if}
        {#if newEpisodes}
          <div
            class="absolute inset-x-0 bottom-0 rounded-b-md"
            style="background: linear-gradient(to top, rgba(0,0,0,0.85) 0%, rgba(0,0,0,0.5) 55%, transparent 100%)"
          >
            <p
              class="px-2 pt-6 pb-2 text-[11px] font-semibold tracking-wide text-white"
            >
              New Episodes
            </p>
          </div>
        {/if}
        {#if isWatched}
          <div
            class="absolute inset-0 flex items-center justify-center rounded-md"
            style="background: linear-gradient(to top, rgba(0,0,0,0.7) 0%, rgba(0,0,0,0.3) 60%, transparent 100%)"
          >
            <CircleCheckBig class="size-12 text-white/80" />
          </div>
        {/if}
        {#if isDropped}
          <div
            class="absolute inset-0 flex items-center justify-center rounded-md"
            style="background: linear-gradient(to top, rgba(0,0,0,0.7) 0%, rgba(0,0,0,0.3) 60%, transparent 100%)"
          >
            <HeartOff class="size-12 text-red-600/80" />
          </div>
        {/if}
      </div>
    </div>
  </ContextMenu.Trigger>
  <ContextMenu.Content>
    <LibraryContextMenuContent {libraryEntry} {media} />
  </ContextMenu.Content>
</ContextMenu.Root>

{#if hovered}
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
    {lastAiredSeason}
    {lastAiredEpisode}
    {quality}
    onmouseleave={onLeave}
    onwatch={(season, episode) =>
      onwatch ? onwatch(media, season, episode) : openOverlay()}
    onexpand={openOverlay}
    onpopoverchange={(open) => (popoverOpen = open)}
  />
{/if}
