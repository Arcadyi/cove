<script lang="ts">
  import { Button } from "$lib/components/ui/button/index.js";
  import * as ButtonGroup from "$lib/components/ui/button-group/index.js";
  import {
    ChevronLeft,
    ChevronRight,
    Pause,
    Play,
    Star,
    ThumbsDown,
    Volume2,
    VolumeOff,
  } from "lucide-svelte";
  import type { Details, Media, MediaImages } from "$lib/types/tmdb";
  import { Badge } from "$lib/components/ui/badge/index.js";
  import { api } from "$lib/api";
  import {
    formatRating,
    formatRuntime,
    getImageOpt,
    getVideoOpt,
  } from "$lib/utils";
  import { animate, splitText, stagger } from "animejs";
  import { getContext, onDestroy } from "svelte";
  import { fly } from "svelte/transition";
  import LibraryStatusPanel from "./LibraryStatusPanel.svelte";
  import type { LibraryEntry } from "$lib/types/library";
  import PlayerSimple from "./PlayerSimple.svelte";

  let mediaIndex = $state<number>(0);
  let medias = $state<Media[]>([]);
  let isMuted = $state(true);
  let isPaused = $state(true);

  // Starts playback (auto-picking the best stream) for the current item.
  // Provided by App.svelte via context, so no prop threading through HomePage.
  const watchMedia = getContext<
    ((m: Media, season?: number, episode?: number) => void) | undefined
  >("watchMedia");

  function watchCurrent(): void {
    const media = medias[mediaIndex];
    if (!media) return;
    const entry = libraryEntries[mediaIndex];
    watchMedia?.(
      media,
      entry?.last_watched_season ?? undefined,
      entry?.last_watched_episode ?? undefined,
    );
  }

  // "Continue S2E4" for a resumable show, otherwise "Watch" — mirrors the modal.
  const watchLabel = $derived.by(() => {
    const media = medias[mediaIndex];
    const entry = libraryEntries[mediaIndex];
    if (
      media?.media_type === "tv" &&
      entry?.last_watched_season != null &&
      entry?.last_watched_episode != null
    ) {
      return `Continue S${entry.last_watched_season}E${entry.last_watched_episode}`;
    }
    return "Watch";
  });

  let backdropUrls = $state<string[]>([]);
  let videoClips = $state<string[]>([]);
  let logoUrls = $state<string[]>([]);
  let genres = $state<string[][]>([]);
  let runtimes = $state<string[]>([]);
  let tmdbRatings = $state<string[]>([]);
  let libraryEntries = $state<LibraryEntry[]>([]);

  $effect(() => {
    api.discover("all", { limit: 10 }).then((d) => (medias = d));
  });

  $effect(() => {
    backdropUrls = new Array(medias.length).fill("");
    videoClips = new Array(medias.length).fill("");
    logoUrls = new Array(medias.length).fill("");
    genres = new Array(medias.length).fill([]);
    runtimes = new Array(medias.length).fill("");
    tmdbRatings = new Array(medias.length).fill("");
    libraryEntries = new Array(medias.length).fill(null);

    for (let i = 0; i < medias.length; i++) {
      api.getImages(medias[i]).then((d: MediaImages) => {
        backdropUrls[i] = getImageOpt(d, "backdrops", {
          iso: "",
          randomize: true,
        });
        logoUrls[i] = getImageOpt(d, "logos", { iso: "en" });
      });
      api.getDetails(medias[i]).then((d: Details) => {
        genres[i] = d.genres.map((g) => g.name);
        runtimes[i] = formatRuntime(d);
        tmdbRatings[i] = formatRating(d);
      });
      api
        .getVideos(medias[i])
        .then((d) => {
          videoClips[i] = getVideoOpt(d, "Clip", {
            iso: "en",
            official: true,
            randomize: true,
          });
        })
        .catch(() => {
          videoClips[i] = null;
        });
      api
        .libraryGet(medias[i].id, medias[i].media_type)
        .then((d) => {
          libraryEntries[i] = d?.entry ? d.entry : null;
        })
        .catch(() => {
          libraryEntries[i] = null;
        });
    }
  });

  function next(): void {
    if (medias.length === 0) return;
    mediaIndex = (mediaIndex + 1) % medias.length;
  }

  const DURATION = 60000;
  let progress = $state(0);
  let currentAnimation: ReturnType<typeof animate> | null = null;

  function startTimer(durationMs = DURATION): void {
    currentAnimation?.pause();
    const obj = { value: 0 };
    progress = 0;
    currentAnimation = animate(obj, {
      value: 100,
      duration: durationMs,
      ease: "linear",
      onUpdate: () => (progress = obj.value),
      onComplete: next,
    });
  }

  $effect(() => {
    const idx = mediaIndex;
    currentAnimation?.pause();
    currentAnimation = null;
    progress = 0;
    if (medias.length > 0 && !videoClips[idx]) startTimer();
  });

  let overviewEl = $state<HTMLElement | null>(null);

  $effect(() => {
    const idx = mediaIndex;
    const el = overviewEl;
    const media = medias[idx];
    if (!el || !media) return;

    el.textContent = media.overview;

    const { words } = splitText(el, { words: true });
    animate(words, {
      opacity: ["0", "1"],
      duration: 2000,
      ease: "out(3)",
      delay: stagger(50),
    });
  });

  function dismissCurrent(): void {
    const media = medias[mediaIndex];
    if (!media) return;
    api.notInterested(media).catch(() => {}); // fire-and-forget; UI updates optimistically
    medias = medias.filter(
      (m) => !(m.id === media.id && m.media_type === media.media_type),
    );
    if (mediaIndex >= medias.length) mediaIndex = 0;
  }

  function moveToNext(): void {
    if (mediaIndex === medias.length - 1) {
      mediaIndex = 0;
    } else {
      mediaIndex += 1;
    }
  }

  function moveToPrevious(): void {
    if (mediaIndex === 0) {
      mediaIndex = medias.length -1;
    } else {
      mediaIndex -= 1;
    }
  }

  onDestroy(() => currentAnimation?.pause());
</script>

<div class="relative aspect-video max-h-[75vh] w-full overflow-hidden">
  <!-- {#each backdropUrls as url, i (i)} -->
  <!--   {#if url} -->
  <!--     <img -->
  <!--       class="absolute inset-0 z-0 h-full w-full rounded-b-2xl border-b object-cover transition-opacity duration-1000" -->
  <!--       class:opacity-0={i !== mediaIndex} -->
  <!--       class:opacity-100={i === mediaIndex} -->
  <!--       src={url} -->
  <!--       alt="backdrop" -->
  <!--     /> -->
  <!--   {/if} -->
  <!-- {/each} -->
  {#if videoClips[mediaIndex]}
    <div
      class="absolute inset-0 z-0 overflow-clip rounded-b-2xl border-b"
      in:fly|local={{ duration: 1000 }}
    >
      {#key mediaIndex}
        <PlayerSimple
          src={videoClips[mediaIndex]}
          loop={false}
          controls={false}
          bg={backdropUrls[mediaIndex]}
          bind:muted={isMuted}
          bind:paused={isPaused}
          onProgress={(cur, dur) => (progress = (cur / dur) * 100)}
          onEnded={() => (mediaIndex = (mediaIndex + 1) % medias.length)}
        />
      {/key}
    </div>
  {/if}

  <div
    class="absolute inset-0 z-10 rounded-b-2xl mask-[linear-gradient(to_right,black,transparent)] [backdrop-filter:blur(120px)] [-webkit-mask-image:linear-gradient(to_right,black,transparent)]"
  ></div>

  <div
    class="absolute inset-0 z-20 bg-linear-to-r from-black/80 from-0% to-transparent to-50% xl:to-40%"
  ></div>

  <div
    class="absolute inset-0 z-20 bg-linear-to-t from-background from-0% to-transparent to-20% xl:to-40%"
  ></div>

  <div
    class="absolute top-[25%] bottom-[5%] left-[5%] z-30 flex min-h-0 w-[40%] flex-col items-start gap-4 xl:w-[30%] 2xl:w-[25%]"
  >
    <div class="relative h-[14vw] max-h-36 w-full shrink-0 self-center">
      {#each logoUrls as url, i (i)}
        {#if url}
          <img
            class="absolute inset-0 h-full max-w-full object-contain transition-opacity duration-1000"
            class:opacity-0={i !== mediaIndex}
            class:opacity-100={i === mediaIndex}
            src={url}
            alt="logo"
          />
        {/if}
      {/each}
    </div>

    {#key mediaIndex}
      <div
        class="flex w-full flex-row flex-wrap gap-1 px-4 text-xs font-medium text-muted-foreground"
        in:fly={{ x: -20, duration: 400, delay: 200 }}
      >
        <Badge variant="outline">{tmdbRatings[mediaIndex]}</Badge>
        {#each genres[mediaIndex] ?? [] as genre (genre)}
          <Badge variant="outline">{genre}</Badge>
        {/each}
        <Badge variant="outline">{runtimes[mediaIndex]}</Badge>
        <Badge variant="outline">
          <span
            class="flex flex-row items-center gap-1 align-middle text-yellow-500"
          >
            <Star size="12" />
            {medias[mediaIndex]?.vote_average}
          </span>
        </Badge>
      </div>
    {/key}

    <p
      bind:this={overviewEl}
      class="line-clamp-3 min-h-0 overflow-hidden px-4 font-medium xl:line-clamp-5"
    ></p>

    <div class="mt-auto flex w-full flex-col gap-4">
      <div class="flex w-full flex-row gap-4 px-4">
        <Button
          variant="outline"
          size="icon"
          onclick={() => (isMuted = !isMuted)}
        >
          {#if isMuted}<VolumeOff />{:else}<Volume2 />{/if}
        </Button>
        <Button
          variant="outline"
          size="icon"
          onclick={() => (isPaused = !isPaused)}
        >
          {#if isPaused}<Play />{:else}<Pause />{/if}
        </Button>
        <ButtonGroup.Root>
          <Button
            variant="outline"
            size="icon"
            onclick={moveToPrevious}
            aria-label="Next"
          >
            <ChevronLeft />
          </Button>
          <Button
            variant="outline"
            size="icon"
            onclick={moveToNext}
            aria-label="Next"
          >
            <ChevronRight />
          </Button>
        </ButtonGroup.Root>

        <Button
          variant="outline"
          size="icon"
          onclick={dismissCurrent}
          aria-label="Not interested"
        >
          <ThumbsDown />
        </Button>
      </div>

      <div class="flex w-full flex-row px-4">
        <Button
          variant="default"
          size="lg"
          class="flex-1 rounded-r-none bg-accent/75 hover:bg-accent"
          onclick={watchCurrent}
        >
          <Play class="fill-current" />
          {watchLabel}
        </Button>
        <LibraryStatusPanel
          libraryEntry={libraryEntries[mediaIndex]}
          media={medias[mediaIndex]}
          size="icon-lg"
          class="rounded-l-none"
        />
      </div>
    </div>

    <div class="flex w-full flex-row gap-1 px-4">
      {#each medias as _, i (i)}
        <div class="h-1 flex-1 overflow-hidden rounded-full border">
          <div
            class="h-full bg-primary"
            style="width: {i < mediaIndex
              ? 100
              : i === mediaIndex
                ? progress
                : 0}%"
          ></div>
        </div>
      {/each}
    </div>
  </div>
</div>
