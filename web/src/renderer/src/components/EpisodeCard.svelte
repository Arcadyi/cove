<script lang="ts">
  import { api, formatPosition } from "$lib/api";
  import * as ContextMenu from "$lib/components/ui/context-menu/index.js";
  import { Check, EyeOff, Play, RotateCcw } from "lucide-svelte";
  import type { Media, TVEpisode } from "$lib/types/tmdb";
  import { epKey, epProgress, progressPct, relativeDate } from "$lib/utils";
  import { SvelteDate, SvelteMap } from "svelte/reactivity";
  import type { WatchProgress } from "$lib/types/library";
  import { settings } from "$lib/stores/settings";
  import ScrambledText from "./ScrambledText.svelte";

  let {
    media,
    ep,
    selectedSeason,
    progressMap,
    selectedEpisode = $bindable<TVEpisode>(),
  } = $props<{
    media: Media;
    ep: TVEpisode;
    selectedSeason: number;
    progressMap: SvelteMap<string, WatchProgress>;
    selectedEpisode: TVEpisode;
  }>();

  let prog = $derived(
    selectedSeason != null
      ? epProgress(selectedSeason, ep.episode_number, progressMap)
      : undefined,
  );

  let pct = $derived(prog ? progressPct(prog) : 0);
  let completed = $derived(prog?.completed ?? false);
  let inProgress = $derived(!completed && pct > 1);
  let unreleased = $derived(
    ep.air_date && new SvelteDate(ep.air_date) > new SvelteDate(),
  );

  async function markWatched(ep: TVEpisode): Promise<void> {
    const p = await api.progressSave({
      tmdb_id: media.id,
      media_type: "tv",
      title: media.name,
      poster_path: media.poster_path ?? "",
      vote_average: media.vote_average ?? 0,
      season: selectedSeason!,
      episode: ep.episode_number,
      position_seconds: 1,
      duration_seconds: 1,
      completed: true,
    });
    progressMap.set(epKey(selectedSeason!, ep.episode_number), p);
  }

  async function markUnwatched(ep: TVEpisode): Promise<void> {
    const p = await api.progressSave({
      tmdb_id: media.id,
      media_type: "tv",
      title: media.name,
      poster_path: media.poster_path ?? "",
      vote_average: media.vote_average ?? 0,
      season: selectedSeason!,
      episode: ep.episode_number,
      position_seconds: 0,
      duration_seconds: 0,
      completed: false,
    });
    progressMap.set(epKey(selectedSeason!, ep.episode_number), p);
  }

  let hideSpoilers = $derived($settings?.hideSpoilers && !completed);
</script>

<ContextMenu.Root>
  <ContextMenu.Trigger class="w-full text-left">
    <button
      class="group relative flex w-full items-center gap-3 p-3 text-left transition-colors
                    {unreleased
        ? 'cursor-default opacity-40'
        : 'hover:bg-secondary/60'}
                    {completed ? 'opacity-70' : ''}"
      onclick={() => {
        if (!unreleased) selectedEpisode = ep;
      }}
      disabled={unreleased}
    >
      <!-- Thumbnail -->
      <span class="relative w-48 shrink-0 overflow-hidden rounded-md bg-muted">
        {#if ep.still_path}
          <img
            src={ep.still_path}
            alt={ep.name}
            class="aspect-video w-full object-cover {hideSpoilers
              ? 'scale-110 blur-md'
              : ''}"
          />
        {:else}
          <div
            class="flex aspect-video w-full items-center justify-center bg-secondary"
          >
            <Play class="size-5 text-muted-foreground/50" />
          </div>
        {/if}

        {#if hideSpoilers && ep.still_path}
          <span class="absolute inset-0 flex items-center justify-center">
            <EyeOff class="size-7 text-white drop-shadow-md" />
          </span>
        {/if}

        <!-- Completed checkmark -->
        {#if completed}
          <span
            class="absolute inset-0 flex items-center justify-center bg-black/50"
          >
            <span
              class="flex size-7 items-center justify-center rounded-full bg-green-500/90"
            >
              <Check class="size-4 text-white" />
            </span>
          </span>
        {:else}
          <!-- Hover play overlay (only when not completed) -->
          <span
            class="absolute inset-0 flex items-center justify-center bg-black/0 transition-colors group-hover:bg-black/40"
          >
            <Play
              class="size-5 text-white opacity-0 transition-opacity group-hover:opacity-100"
            />
          </span>
        {/if}
      </span>

      <!-- Info -->
      <span class="min-w-0 flex-1 flex-col py-0.5">
        <span class="flex flex-col">
          <span
            class="text-sm leading-snug font-medium {completed
              ? 'text-muted-foreground'
              : ''}">{ep.name}</span
          >
          <span
            class="flex items-center gap-1.5 text-xs font-semibold text-muted-foreground"
          >
            E{ep.episode_number}
            {#if ep.air_date}
              · <span class="font-normal"
                >{unreleased ? relativeDate(ep.air_date) : ep.air_date}</span
              >
            {/if}
            {#if inProgress}
              · <span class="font-normal text-accent"
                >{formatPosition(prog!.position_seconds)} watched</span
              >
            {/if}
          </span>
        </span>
        {#if ep.overview}
          <ScrambledText
            text={ep.overview}
            active={hideSpoilers}
            class="mt-1 line-clamp-2 text-xs leading-relaxed text-muted-foreground"
          />
        {/if}
      </span>

      <!-- In-progress bar (absolute bottom of row) -->
      {#if inProgress}
        <span
          class="absolute right-0 bottom-0 left-0 h-0.5 overflow-hidden bg-secondary"
        >
          <span
            class="block h-full bg-accent transition-all"
            style="width: {pct}%"
          ></span>
        </span>
      {/if}
    </button>
  </ContextMenu.Trigger>

  {#if !unreleased}
    <ContextMenu.Content>
      {#if !completed}
        <ContextMenu.Item
          onclick={() => markWatched(ep)}
          class="flex items-center gap-2"
        >
          <Check class="size-4" /> Mark as Watched
        </ContextMenu.Item>
      {:else}
        <ContextMenu.Item
          onclick={() => markUnwatched(ep)}
          class="flex items-center gap-2"
        >
          <RotateCcw class="size-4" /> Mark as Unwatched
        </ContextMenu.Item>
      {/if}
      <ContextMenu.Separator />
      <ContextMenu.Item
        onclick={() => {
          selectedEpisode = ep;
        }}
        class="flex items-center gap-2"
      >
        <Play class="size-4" /> View Streams
      </ContextMenu.Item>
    </ContextMenu.Content>
  {/if}
</ContextMenu.Root>
