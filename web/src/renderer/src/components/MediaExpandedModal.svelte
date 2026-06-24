<script lang="ts">
  import type { Media, MediaImages } from "$lib/types/tmdb";
  import type { Stream } from "$lib/types/addons";
  import { Separator } from "$lib/components/ui/separator/index.js";
  import { animate } from "animejs";
  import { Badge } from "$lib/components/ui/badge/index.js";
  import { Button } from "$lib/components/ui/button";
  import { ChevronDown, ListVideo, Play, Star, X } from "lucide-svelte";
  import {
    countryName,
    formatRating,
    formatRuntime,
    getImageOpt,
    getVideoOpt,
    qualityClass,
  } from "$lib/utils";
  import PlayerSimple from "./PlayerSimple.svelte";
  import StreamsList from "./StreamsList.svelte";
  import { ScrollArea } from "$lib/components/ui/scroll-area/index.js";
  import { api, formatPosition } from "$lib/api";
  import type { LibraryEntry, WatchProgress } from "$lib/types/library";
  import StarRating from "./StarRating.svelte";
  import LibraryStatusPanel from "./LibraryStatusPanel.svelte";

  let {
    media,
    streamActive = false,
    activeSeason = undefined,
    activeEpisode = undefined,
    onwatch,
    onplaystream,
    onclose,
    onsimilar,
  }: {
    media: Media;
    streamActive?: boolean;
    activeSeason?: number;
    activeEpisode?: number;
    onwatch: (season?: number, episode?: number) => void;
    onplaystream?: (
      stream: Stream,
      season?: number,
      episode?: number,
      episodeName?: string,
    ) => void;
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

  // ── Detail data (fetched here now, the way MediaPage used to) ───────────────
  let detailsLoading = $state(true);
  let images = $state<MediaImages>();
  let videoUrl = $state<string | null>(null);
  let genres: string[] = $state([]);
  let runtime = $state("");
  let cast: string[] = $state([]);
  let ageRating = $state("");
  let keywords: string[] = $state([]);
  let similar: Media[] = $state([]);
  let originCountry: string[] = $state([]);
  let numberOfSeasons = $state<number | null>(null);
  let numberOfEpisodes = $state<number | null>(null);
  let lastAiredSeason = $state<number | null>(null);
  let lastAiredEpisode = $state<number | null>(null);
  // Captured once from Details and then left alone, so a later swap of the
  // live `media` prop (e.g. after a library status change) can't make the
  // overview text silently vanish.
  let detailsOverview = $state<string | null>(null);

  $effect(() => {
    detailsLoading = true;
    const type = media.media_type;
    Promise.all([
      api.getVideos(media),
      api.getSimilar(media),
      api.getDetails(media),
      api.getImages(media),
    ])
      .then(([vids, similarList, details, img]) => {
        images = img;
        videoUrl = getVideoOpt(vids, "Trailer", { randomize: true }) ?? null;
        similar = similarList;
        detailsOverview = details.overview ?? null;
        genres =
          details.genres?.map((g: { name: string }) => g.name).slice(0, 3) ??
          [];
        runtime = formatRuntime(details);
        cast =
          details.credits?.cast
            ?.slice(0, 5)
            .map((c: { name: string }) => c.name) ?? [];
        ageRating = formatRating(details);
        keywords =
          (type === "movie"
              ? details.keywords?.keywords
              : details.keywords?.results
          )
            ?.slice(0, 4)
            .map((k: { name: string }) => k.name) ?? [];
        originCountry = details.origin_country ?? [];
        if (type === "tv") {
          numberOfSeasons = details.number_of_seasons ?? null;
          numberOfEpisodes = details.number_of_episodes ?? null;
          lastAiredSeason = details.last_episode_to_air?.season_number ?? null;
          lastAiredEpisode =
            details.last_episode_to_air?.episode_number ?? null;
        }
        detailsLoading = false;
      })
      .catch((err) => {
        console.error("MediaExpandedModal details fetch failed", err);
        detailsLoading = false;
      });
  });

  const overviewParagraphs = $derived(
    (detailsOverview ?? media.overview)
      ?.split(". ")
      .map((s, i, arr) => (i < arr.length - 1 ? s + "." : s))
      .filter((s) => s.trim().length > 0) ?? [],
  );

  const logoUrl = $derived(
    images && images.logos?.length > 0
      ? getImageOpt(images, "logos", { iso: "en" })
      : null,
  );
  const backdropUrl = $derived(
    images && images.backdrops?.length > 0
      ? getImageOpt(images, "backdrops", { iso: "en", randomize: true })
      : null,
  );

  // ── Stream browser (the merged-in StreamsList) ─────────────────────────────
  let showStreams = $state(false);
  let streamMaxQuality = $state<string | null>(null);

  const streamsToggleLabel = $derived(
    media.media_type === "tv" ? "Episodes" : "Choose a source",
  );

  // ── Library state ──────────────────────────────────────────────────────────
  let libraryEntry = $state<LibraryEntry | null>(null);
  let movieProgress = $state<WatchProgress | null>(null);
  let tvProgressList = $state<WatchProgress[]>([]);

  $effect(() => {
    api
      .libraryGet(media.id, media.media_type)
      .then((result) => {
        if (!result) {
          libraryEntry = null;
          movieProgress = null;
          tvProgressList = [];
          return;
        }
        libraryEntry = result.entry;
        if (media.media_type === "movie") {
          movieProgress = result.progress[0] ?? null;
        } else {
          tvProgressList = result.progress;
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
  const episodesWatched = $derived(
    tvProgressList.filter((p) => p.completed).length,
  );
  const hasIncompleteMovieProgress = $derived(
    media.media_type === "movie" &&
    movieProgress !== null &&
    !movieProgress.completed &&
    movieProgressPct > 1,
  );
  const watchButtonLabel = $derived.by(() => {
    if (media.media_type === "tv") {
      const s = libraryEntry?.last_watched_season;
      const e = libraryEntry?.last_watched_episode;
      if (s != null && e != null) return `Continue S${s}E${e}`;
    }
    return hasIncompleteMovieProgress ? "Continue" : "Watch";
  });

  // ── Actions ─────────────────────────────────────────────────────────────────
  // Starting playback always dismisses the modal so the full player (which
  // sits below this overlay in the stack) isn't left hidden behind it.
  function watchNow(): void {
    onwatch(
      libraryEntry?.last_watched_season ?? undefined,
      libraryEntry?.last_watched_episode ?? undefined,
    );
    onclose();
  }
  function playStream(
    stream: Stream,
    season?: number,
    episode?: number,
    episodeName?: string,
  ): void {
    onplaystream?.(stream, season, episode, episodeName);
    onclose();
  }

  // ── Modal animation ───────────────────────────────────────────────────────────
  $effect(() => {
    if (!el) return;
    animate(el, {
      scale: [0.94, 1],
      opacity: [0, 1],
      duration: 220,
      easing: "easeOutQuart",
    });
  });
  function close(): void {
    if (!el) {
      onclose();
      return;
    }
    animate(el, {
      scale: [1, 0.94],
      opacity: [1, 0],
      duration: 180,
      easing: "easeInQuart",
      onComplete: onclose,
    });
  }
</script>

<!-- Backdrop (visual only) -->
<div
  class="pointer-events-none fixed inset-0 z-40 bg-black/70 backdrop-blur-sm"
></div>

<!-- Full-window layer hosting a shadcn ScrollArea, so the styled scrollbar sits
     at the window edge while the card grows to its natural height. The plain
     fixed wrapper gives the ScrollArea a definite size to bound its viewport —
     putting `fixed` on the ScrollArea root itself doesn't work, because bits-ui
     forces position:relative on it inline. -->
<div class="fixed inset-0 z-50 mt-18">
  <ScrollArea class="h-full w-full">
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div
      role="presentation"
      class="flex min-h-full items-start justify-center overscroll-contain p-4 sm:p-6 lg:p-10"
      onmousedown={close}
    >
      <!-- svelte-ignore a11y_no_static_element_interactions -->
      <div
        bind:this={el}
        role="presentation"
        class="relative my-auto flex w-[min(1080px,94vw)] cursor-default flex-col overflow-hidden rounded-xl border border-border bg-background shadow-2xl"
        onmousedown={(e) => e.stopPropagation()}
        onclick={(e) => e.stopPropagation()}
        onkeydown={(e) => e.stopPropagation()}
        style="opacity: 0; transform: scale(0.94);"
      >
        <!-- Close — pinned to the card's top-right -->
        <button
          class="absolute top-3 right-3 z-20 flex size-9 items-center justify-center rounded-full bg-black/60 text-white transition hover:bg-black/80"
          onclick={close}
          aria-label="Close"
        >
          <X class="size-5" />
        </button>

        <!-- ── Hero ───────────────────────────────────────────────────────────── -->
        <!-- `isolate` traps PlayerSimple's internal stacking inside this hero so
             it can never paint over the sibling close button above it. -->
        <div class="relative isolate w-full overflow-hidden bg-black">
          {#if videoUrl}
            <PlayerSimple
              src={videoUrl}
              controls={true}
              bg={media.poster_path}
            />
          {:else if backdropUrl}
            <img
              src={backdropUrl}
              alt={title}
              class="h-[clamp(200px,38vh,440px)] w-full object-cover"
            />
          {:else}
            <img
              src={media.poster_path}
              alt={title}
              class="h-[clamp(200px,38vh,440px)] w-full object-cover"
            />
          {/if}

          <!-- Gradient that fades the hero into the body below -->
          <div
            class="pointer-events-none absolute inset-x-0 bottom-0 h-2/3"
            style="background: linear-gradient(to top, var(--background) 2%, rgba(0,0,0,0.35) 55%, transparent 100%)"
          ></div>

          <!-- Title / logo overlaid bottom-left, Netflix-style -->
          <div
            class="pointer-events-none absolute bottom-0 left-0 flex max-w-[70%] flex-col gap-2 p-5 sm:p-7"
          >
            {#if logoUrl}
              <img
                src={logoUrl}
                alt={title}
                class="max-h-20 w-auto max-w-full object-contain drop-shadow-lg sm:max-h-28"
              />
            {:else}
              <h2
                class="text-2xl leading-tight font-bold text-white drop-shadow-lg sm:text-4xl"
              >
                {title}
              </h2>
            {/if}
          </div>
        </div>

        <!-- ── Body ───────────────────────────────────────────────────────────── -->
        <div class="flex flex-col gap-4 p-5 sm:p-7">
          <!-- Action row -->
          <div class="flex flex-wrap items-center gap-3">
            <Button
              class="h-11 grow rounded-md border-b border-accent bg-accent px-6 text-base font-semibold text-accent-foreground hover:bg-accent-foreground hover:text-accent sm:grow-0"
              variant="default"
              onclick={watchNow}
            >
              <Play class="size-4 fill-current" />
              {watchButtonLabel}
            </Button>

            <div class="ml-auto flex items-center gap-2">
              <StarRating
                {libraryEntry}
                {media}
                {lastAiredSeason}
                {lastAiredEpisode}
              />
              <LibraryStatusPanel
                {libraryEntry}
                {media}
                {lastAiredSeason}
                {lastAiredEpisode}
                size="icon"
              />
            </div>
          </div>

          <!-- Title + rating + metadata -->
          <div class="flex flex-wrap items-center gap-x-3 gap-y-2">
            <span class="text-xl font-semibold">{title}</span>
            {#if year}<Badge variant="outline">{year}</Badge>{/if}
            <Badge variant="outline" class="text-yellow-400">
              <Star class="size-4 fill-current" />
              {media.vote_average?.toFixed(1)}
              <span class="font-bold">TMDB</span>
            </Badge>
          </div>

          <div class="flex flex-wrap items-center gap-2 text-sm">
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
            {#if streamMaxQuality}
              <span
                class="rounded border px-1.5 py-0.5 text-xs font-medium {qualityClass(
                  streamMaxQuality,
                )}"
              >
                {streamMaxQuality.toUpperCase()}
              </span>
            {/if}
            {#if genres.length}
              {#each genres as genre (genre)}
                <span
                  class="rounded-full bg-secondary px-2 py-0.5 text-xs whitespace-nowrap text-secondary-foreground"
                >
                  {genre}
                </span>
              {/each}
            {/if}
          </div>

          <!-- Progress -->
          {#if hasIncompleteMovieProgress && movieProgress}
            <div
              class="flex items-center gap-2 rounded-md bg-secondary/40 px-3 py-2"
            >
              <div
                class="h-1.5 flex-1 overflow-hidden rounded-full bg-secondary"
              >
                <div
                  class="h-full rounded-full bg-accent transition-all"
                  style="width: {movieProgressPct}%"
                ></div>
              </div>
              <span class="shrink-0 text-xs text-muted-foreground tabular-nums">
                {formatPosition(movieProgress.position_seconds)} / {formatPosition(
                movieProgress.duration_seconds,
              )}
              </span>
            </div>
          {/if}

          {#if media.media_type === "tv" && episodesWatched > 0}
            <div
              class="flex items-center gap-2 rounded-md bg-secondary/40 px-3 py-2"
            >
              <span class="text-xs text-muted-foreground">
                {episodesWatched} episode{episodesWatched !== 1 ? "s" : ""} watched
                {#if numberOfEpisodes}· {Math.round(
                  (episodesWatched / numberOfEpisodes) * 100,
                )}%{/if}
              </span>
            </div>
          {/if}

          <!-- Overview + cast / keywords -->
          <div class="grid grid-cols-1 gap-x-6 gap-y-4 md:grid-cols-[1fr_auto]">
            <div class="flex flex-col gap-3">
              {#if detailsLoading && overviewParagraphs.length === 0}
                <p class="animate-pulse text-sm text-muted-foreground">
                  Loading details…
                </p>
              {:else}
                {#each overviewParagraphs as paragraph, i (i)}
                  <p class="text-sm leading-relaxed text-muted-foreground">
                    {paragraph}
                  </p>
                {/each}
              {/if}
            </div>

            <div class="flex w-full flex-col gap-3 md:w-56">
              {#if cast.length}
                <div class="text-sm">
                  <span class="text-muted-foreground">Cast: </span>
                  {cast.slice(0, 5).join(", ")}
                </div>
              {/if}
              {#if keywords.length}
                <div class="text-sm">
                  <span class="text-muted-foreground"
                  >This {media.media_type === "tv" ? "show" : "film"} is:
                  </span>
                  {keywords.join(", ")}
                </div>
              {/if}
            </div>
          </div>

          <Button
            class="h-11 rounded-md"
            variant="outline"
            onclick={() => (showStreams = !showStreams)}
          >
            <ListVideo class="size-4" />
            {streamsToggleLabel}
            <ChevronDown
              class="size-4 transition-transform duration-200 {showStreams
                ? 'rotate-180'
                : ''}"
            />
          </Button>

          <!-- Expandable stream / episode browser -->
          {#if showStreams}
            <StreamsList
              {media}
              onPlayStream={(
                s: Stream,
                season?: number,
                episode?: number,
                episodeName?: string,
              ) => playStream(s, season, episode, episodeName)}
              bind:maxQuality={streamMaxQuality}
              {streamActive}
              {activeSeason}
              {activeEpisode}
            />
          {/if}

          <!-- More like this -->
          {#if similar.length}
            <div class="space-y-3">
              <Separator />
              <h3 class="text-base font-semibold">More like this</h3>
              <div class="grid grid-cols-3 gap-3 sm:grid-cols-4 lg:grid-cols-6">
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
      </div>
    </div>
  </ScrollArea>
</div>
