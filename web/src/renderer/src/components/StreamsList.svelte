<script lang="ts">
  import {
    epKey,
    epProgress,
    getMaxQuality,
    inferQuality,
    progressPct,
  } from "$lib/utils";
  import { ScrollArea } from "$lib/components/ui/scroll-area/index.js";
  import type { Stream } from "$lib/types/addons";
  import * as Select from "$lib/components/ui/select/index.js";
  import {
    ListFilter,
    Play,
    Settings2,
    ChevronLeft,
    Check,
  } from "lucide-svelte";
  import { Button } from "$lib/components/ui/button/index.js";
  import { Spinner } from "$lib/components/ui/spinner";
  import { SvelteMap } from "svelte/reactivity";
  import { api, formatPosition } from "$lib/api";
  import type { WatchProgress } from "$lib/types/library";
  import { settings } from "$lib/stores/settings";
  import {
    getSeeders,
    getSizeBytes,
    pickBestStream,
    formatStreamSummary,
    type StreamSelectionMode,
  } from "$lib/streamSelection";
  import { Skeleton } from "$lib/components/ui/skeleton";
  import type { TVEpisode } from "$lib/types/tmdb";
  import EpisodeCard from "./EpisodeCard.svelte";

  let loadingStreams = $state(false);
  let sortMode = $state<"seeders" | "size">("seeders");
  let qualityFilter = $state("all");

  let {
    media,
    onPlayStream,
    maxQuality = $bindable<string | null>(),
    streamActive = false,
    activeSeason = undefined,
    activeEpisode = undefined,
  } = $props();

  // TV types

  type TVSeason = {
    season_number: number;
    episode_count: number;
    name: string;
    poster_path: string;
  };

  // State

  const isTV = $derived(media.media_type === "tv");

  // TV browsing state
  let seasons = $state<TVSeason[]>([]);
  let episodes = $state<TVEpisode[]>([]);
  let selectedSeason = $state<number | null>(null);
  let selectedEpisode = $state<TVEpisode | null>(null);
  let loadingSeasons = $state(false);
  let loadingEpisodes = $state(false);

  // Stream state
  let streams = $state<Stream[]>([]);

  let pollInterval: ReturnType<typeof setInterval> | null = null;

  // ── Auto stream selection ─────────────────────────────────────────────────────

  let autoPicking = $state(false);
  let autoPickCancelled = $state(false);
  // Whether to show the picker at all when something's already playing for
  // this exact selection — keeps the panel from defaulting to "here's a
  // full list to pick from" when there's nothing to actually decide yet.
  let showAlternatives = $state(false);

  // True when the season/episode currently browsed here is the exact thing
  // already playing (full or minimized to PiP). Prevents auto-select from
  // firing again and silently swapping out the stream you're watching —
  // this list keeps polling/rendering in the background now that the
  // player no longer unmounts it while a stream is active.
  const alreadyPlayingThisSelection = $derived(
    streamActive &&
      (!isTV ||
        (selectedSeason === activeSeason &&
          selectedEpisode?.episode_number === activeEpisode)),
  );

  // fetchStreams sets autoPicking = true right before kicking off playback,
  // but nothing ever flips it back once that stream actually starts — it
  // used to not matter because this whole component got unmounted the
  // instant playback began. It no longer does, so clear it explicitly once
  // we can see the pick succeeded.
  $effect(() => {
    if (alreadyPlayingThisSelection && autoPicking) {
      autoPicking = false;
    }
  });

  // ── Watch progress ────────────────────────────────────────────────────────────

  // TV: keyed by "season:episode"
  let progressMap = new SvelteMap<string, WatchProgress>();
  // Movie: single record
  let movieProgress = $state<WatchProgress | null>(null);

  // Fetch all episode progress for this show whenever the media changes
  $effect(() => {
    if (!isTV) return;
    api
      .libraryGet(media.id, "tv")
      .then((result) => {
        progressMap.clear();
        for (const p of result?.progress ?? []) {
          if (p.season != null && p.episode != null) {
            progressMap.set(epKey(p.season, p.episode), p);
          }
        }
      })
      .catch(console.error);
  });

  // Fetch movie progress
  $effect(() => {
    if (isTV) return;
    api
      .progressGet(media.id, "movie")
      .then((p) => {
        movieProgress = p;
      })
      .catch(console.error);
  });

  // Data fetching

  $effect(() => {
    if (!isTV) return;
    loadingSeasons = true;
    api
      .tvSeasons<TVSeason>(media.id)
      .then((data) => {
        seasons = data ?? [];
        if (seasons.length > 0 && selectedSeason === null) {
          // Land on whatever's already playing (full or minimized to PiP)
          // instead of always defaulting to season 1.
          const preferred =
            activeSeason != null &&
            seasons.some((s) => s.season_number === activeSeason)
              ? activeSeason
              : seasons[0].season_number;
          selectedSeason = preferred;
        }
      })
      .finally(() => (loadingSeasons = false));
  });

  $effect(() => {
    if (!isTV || selectedSeason === null) return;
    loadingEpisodes = true;
    episodes = [];
    selectedEpisode = null;
    streams = [];
    api
      .tvEpisodes(media.id, selectedSeason!)
      .then((data) => {
        episodes = data ?? [];
        // Same idea, one level deeper: jump straight to the episode that's
        // already playing rather than leaving the user on the episode
        // browser, having to find and re-click it themselves.
        if (selectedSeason === activeSeason && activeEpisode != null) {
          const match = episodes.find(
            (e) => e.episode_number === activeEpisode,
          );
          if (match) selectedEpisode = match;
        }
      })
      .finally(() => (loadingEpisodes = false));
  });

  $effect(() => {
    if (isTV) {
      if (!selectedEpisode || selectedSeason === null) return () => {};
      clearPoll();
      loadingStreams = true;
      streams = [];
      autoPickCancelled = false;
      autoPicking = false;
      showAlternatives = false;
      fetchStreams().then(() => {
        loadingStreams = false;
        if (streams.length === 0)
          pollInterval = setInterval(fetchStreams, 1000);
      });
    } else {
      clearPoll();
      loadingStreams = true;
      streams = [];
      autoPickCancelled = false;
      autoPicking = false;
      showAlternatives = false;
      fetchStreams().then(() => {
        loadingStreams = false;
        if (streams.length === 0)
          pollInterval = setInterval(fetchStreams, 1000);
      });
    }

    return () => clearPoll();
  });

  // Stream helpers

  const availableQualities = $derived.by(() => {
    const qs = [
      ...new Set(streams.map((s) => inferQuality(s)).filter(Boolean)),
    ];
    qs.sort(
      (a, b) =>
        ["4k dv", "4k hdr", "4k", "1080p", "720p", "480p", "ts", "cam"].indexOf(
          a!,
        ) -
        ["4k dv", "4k hdr", "4k", "1080p", "720p", "480p", "ts", "cam"].indexOf(
          b!,
        ),
    );
    return ["all", ...qs];
  });

  const filteredStreams = $derived.by(() => {
    const list = streams.map((s) => ({
      ...s,
      seeders: getSeeders(s),
      sizeBytes: getSizeBytes(s),
      quality: inferQuality(s),
    }));
    const filtered = list.filter(
      (s) => qualityFilter === "all" || s.quality === qualityFilter,
    );
    filtered.sort((a, b) =>
      sortMode === "seeders"
        ? b.seeders - a.seeders
        : b.sizeBytes - a.sizeBytes,
    );
    return filtered;
  });

  const selectedSeasonLabel = $derived(
    seasons.find((s) => s.season_number === selectedSeason)?.name ??
      (selectedSeason !== null ? `Season ${selectedSeason}` : "Season"),
  );

  function clearPoll(): void {
    if (pollInterval) {
      clearInterval(pollInterval);
      pollInterval = null;
    }
  }

  async function fetchStreams(): Promise<void> {
    return await api
      .getStreams(
        media.id,
        isTV
          ? {
              type: "tv",
              season: selectedSeason!,
              episode: selectedEpisode!.episode_number,
            }
          : {},
      )
      .then((res: Stream[]) => {
        streams = res;
        maxQuality = getMaxQuality(streams);
        if (streams.length > 0) clearPoll();

        if (
          $settings?.autoSelectStream &&
          !autoPickCancelled &&
          !autoPicking &&
          !alreadyPlayingThisSelection &&
          streams.length > 0
        ) {
          const best = pickBestStream(
            streams,
            ($settings.streamSelectionMode as StreamSelectionMode) ??
              "balanced",
            { measuredBandwidthMbps: $settings.measuredBandwidthMbps },
          );
          if (best) {
            const mode = $settings.streamSelectionMode ?? "balanced";
            console.log(
              `[stream-select] auto (${mode}): "${best.name}" — ${formatStreamSummary(best)}`,
              best,
            );
            autoPicking = true;
            // Small delay so the "Auto-selecting…" message and its cancel
            // button actually get a moment on screen before playback starts.
            setTimeout(() => {
              if (!autoPickCancelled) {
                onPlayStream(
                  best,
                  selectedSeason ?? undefined,
                  selectedEpisode?.episode_number,
                  selectedEpisode?.name,
                );
              }
            }, 500);
          }
        }
      });
  }
</script>

<div
  class="flex h-full w-full flex-col rounded-2xl border border-border bg-background/60 backdrop-blur-xl"
>
  <!-- TV: episode browser -->
  {#if isTV && !selectedEpisode}
    <!-- Season picker header -->
    <div class="flex-none border-b border-border p-4">
      {#if loadingSeasons}
        <span class="animate-pulse text-sm text-muted-foreground"
          >Loading seasons…</span
        >
      {:else}
        <Select.Root
          type="single"
          value={selectedSeason?.toString()}
          onValueChange={(v) => {
            selectedSeason = v ? Number(v) : null;
          }}
        >
          <Select.Trigger class="w-full">
            {selectedSeasonLabel}
          </Select.Trigger>
          <Select.Content>
            <Select.Group>
              {#each seasons as s (s.season_number)}
                <Select.Item
                  value={s.season_number.toString()}
                  label="{s.name} ({s.episode_count} eps)"
                />
              {/each}
            </Select.Group>
          </Select.Content>
        </Select.Root>
      {/if}
    </div>

    <!-- Episode rows -->
    <ScrollArea class="min-h-0 flex-1">
      <div class="flex flex-col divide-y divide-border">
        {#if loadingEpisodes}
          <div class="flex items-center justify-center py-12">
            <span class="animate-pulse text-sm text-muted-foreground"
              >Loading episodes…</span
            >
          </div>
        {:else}
          {#each episodes as ep (ep.episode_number)}
            <EpisodeCard
              {media}
              {ep}
              {selectedSeason}
              bind:selectedEpisode
              {progressMap}
            />
          {/each}
        {/if}
      </div>
    </ScrollArea>

    <!-- Stream list (movies always, TV after episode picked) -->
  {:else}
    <!-- Header: back button for TV, or plain title for movies -->
    <div class="flex-none space-y-3 border-b border-border p-5">
      {#if isTV && selectedEpisode}
        <Button
          variant="outline"
          onclick={() => {
            selectedEpisode = null;
            streams = [];
          }}
        >
          <ChevronLeft class="size-4" />
          Back to episodes
        </Button>

        <!-- Selected episode summary -->
        <div
          class="flex items-start gap-3 rounded-lg border border-border bg-secondary/40 p-2.5"
        >
          {#if selectedEpisode.still_path}
            <img
              src={selectedEpisode.still_path}
              alt={selectedEpisode.name}
              class="aspect-video w-24 shrink-0 rounded-md object-cover"
            />
          {:else}
            <Skeleton
              class="aspect-video w-24 shrink-0 rounded-md object-cover"
            />
          {/if}
          <div class="min-w-0 flex-1">
            <p class="text-[11px] text-muted-foreground">
              S{selectedSeason} · E{selectedEpisode.episode_number}
            </p>
            <p class="text-sm leading-snug font-semibold">
              {selectedEpisode.name}
            </p>
            <!-- Episode progress -->
            {#if selectedSeason != null}
              {@const prog = epProgress(
                selectedSeason,
                selectedEpisode.episode_number,
                progressMap,
              )}
              {#if prog}
                {@const pct = progressPct(prog)}
                {#if prog.completed}
                  <p
                    class="mt-1.5 flex items-center gap-1 text-[11px] text-green-500"
                  >
                    <Check class="size-3" /> Watched
                  </p>
                {:else if pct > 1}
                  <div class="mt-2 space-y-1">
                    <div
                      class="h-1 w-full overflow-hidden rounded-full bg-secondary"
                    >
                      <div
                        class="h-full rounded-full bg-accent transition-all"
                        style="width: {pct}%"
                      ></div>
                    </div>
                    <p class="text-[10px] text-muted-foreground">
                      {formatPosition(prog.position_seconds)} / {formatPosition(
                        prog.duration_seconds,
                      )}
                    </p>
                  </div>
                {/if}
              {/if}
            {/if}
          </div>
        </div>
      {:else}
        <!-- Movie: "Available Streams" header with progress -->
        <div class="flex items-start justify-between gap-3">
          <h3 class="text-lg font-semibold">Available Streams</h3>
        </div>
        {#if movieProgress}
          {#if movieProgress.completed}
            <p class="flex items-center gap-1.5 text-xs text-green-500">
              <Check class="size-3.5" /> Watched
            </p>
          {:else if progressPct(movieProgress) > 1}
            <div class="space-y-1">
              <div class="h-1 w-full overflow-hidden rounded-full bg-secondary">
                <div
                  class="h-full rounded-full bg-accent transition-all"
                  style="width: {progressPct(movieProgress)}%"
                ></div>
              </div>
              <p class="text-[11px] text-muted-foreground">
                {formatPosition(movieProgress.position_seconds)} / {formatPosition(
                  movieProgress.duration_seconds,
                )}
              </p>
            </div>
          {/if}
        {/if}
      {/if}

      <!-- Quality + sort filters -->
      {#if !alreadyPlayingThisSelection || showAlternatives}
        <div class="grid grid-cols-2 gap-2">
          <Select.Root type="single" bind:value={qualityFilter}>
            <Select.Trigger class="flex w-full">
              <span class="flex flex-row items-center justify-center gap-1">
                <Settings2 class="size-4" />
                {qualityFilter.toUpperCase()}
              </span>
            </Select.Trigger>
            <Select.Content>
              <Select.Group>
                {#each availableQualities as q (q)}
                  <Select.Item value={q} label={q.toUpperCase()} />
                {/each}
              </Select.Group>
            </Select.Content>
          </Select.Root>

          <Select.Root type="single" bind:value={sortMode}>
            <Select.Trigger class="flex w-full">
              <span class="flex flex-row items-center justify-center gap-1">
                <ListFilter class="size-4" />
                {sortMode.toUpperCase()}
              </span>
            </Select.Trigger>
            <Select.Content>
              <Select.Group>
                <Select.Item value="seeders" label="Seeders" />
                <Select.Item value="size" label="Size" />
              </Select.Group>
            </Select.Content>
          </Select.Root>
        </div>
      {/if}
    </div>

    <!-- Stream rows -->
    <ScrollArea class="min-h-0 flex-1">
      <div class="p-4">
        {#if alreadyPlayingThisSelection}
          <div
            class="flex items-center justify-between gap-2 rounded-lg border border-accent/30 bg-accent/10 px-3 py-2 text-sm text-accent"
            class:mb-3={showAlternatives}
          >
            <span class="flex items-center gap-2">
              <Play class="size-4 fill-current" />
              Playing this stream
            </span>
            <Button
              variant="ghost"
              size="sm"
              onclick={() => (showAlternatives = !showAlternatives)}
            >
              {showAlternatives ? "Hide alternatives" : "See alternatives"}
            </Button>
          </div>
        {/if}
        {#if !alreadyPlayingThisSelection || showAlternatives}
          {#if autoPicking && !autoPickCancelled && !alreadyPlayingThisSelection}
            <div class="flex flex-col items-center justify-center gap-3 py-12">
              <Spinner class="size-8" />
              <span class="text-sm text-muted-foreground">
                Auto-selecting the best stream…
              </span>
              <Button
                variant="outline"
                size="sm"
                onclick={() => {
                  autoPickCancelled = true;
                  autoPicking = false;
                }}
              >
                Choose manually instead
              </Button>
            </div>
          {:else if loadingStreams}
            <div class="flex flex-col items-center justify-center gap-2 py-12">
              <Spinner class="size-8" />
              <span class="animate-pulse text-sm text-muted-foreground">
                Finding streams…
              </span>
            </div>
          {:else if streams.length === 0}
            <div class="flex flex-col items-center justify-center gap-2 py-12">
              <Spinner class="size-8" />
              <span class="animate-pulse text-sm text-muted-foreground">
                No streams found — retrying…
              </span>
            </div>
          {:else if filteredStreams.length === 0}
            <div class="flex items-center justify-center py-12">
              <span class="text-sm text-muted-foreground"
                >No streams match this filter.</span
              >
            </div>
          {:else}
            <div class="flex flex-col gap-3">
              {#each filteredStreams as stream (stream)}
                <button
                  class="group flex w-full flex-col gap-1 rounded-lg border border-border/50 bg-secondary/50 p-3 text-left transition-colors hover:border-border hover:bg-secondary"
                  onclick={() => {
                    console.log(
                      `[stream-select] manual: "${stream.name}" — ${formatStreamSummary(stream)}`,
                      stream,
                    );
                    onPlayStream(
                      stream,
                      selectedSeason ?? undefined,
                      selectedEpisode?.episode_number,
                      selectedEpisode?.name,
                    );
                  }}
                >
                  <span class="flex items-center justify-between gap-2">
                    <span class="text-sm font-medium text-foreground"
                      >{stream.name}</span
                    >
                    <Play
                      class="size-3 text-foreground opacity-0 transition-opacity group-hover:opacity-100"
                    />
                  </span>

                  <span
                    class="line-clamp-2 text-xs whitespace-pre-line text-muted-foreground"
                  >
                    {stream.title}
                  </span>

                  <span
                    class="mt-1 flex flex-wrap gap-1.5 text-[11px] text-muted-foreground"
                  >
                    <span class="rounded bg-background/70 px-1.5 py-0.5">
                      👤 {getSeeders(stream)}
                    </span>
                    <span class="rounded bg-background/70 px-1.5 py-0.5">
                      💾 {getSizeBytes(stream) / 1024 ** 3 >= 1
                        ? `${(getSizeBytes(stream) / 1024 ** 3).toFixed(2)} GB`
                        : `${(getSizeBytes(stream) / 1024 ** 2).toFixed(0)} MB`}
                    </span>
                    <span class="rounded bg-background/70 px-1.5 py-0.5">
                      {inferQuality(stream)}
                    </span>
                  </span>
                </button>
              {/each}
            </div>
          {/if}
        {/if}
      </div>
    </ScrollArea>
  {/if}
</div>
