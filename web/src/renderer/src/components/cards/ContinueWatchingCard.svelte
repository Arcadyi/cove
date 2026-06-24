<script module lang="ts">
  import type { Media } from "$lib/types/tmdb";

  // Normalized, render-ready shape the carousel hands each card. Kept here (and
  // exported) so the carousel and card agree on one type.
  export interface ContinueItem {
    key: string; // `${tmdb_id}-${media_type}` — stable {#each} key
    media: Media; // enough of a Media to resume + open details
    title: string;
    image: string; // episode still (tv) or poster (movie / fallback)
    mediaType: "movie" | "tv";
    season: number | null; // null for movies
    episode: number | null; // null for movies
    upNext: boolean; // true => next episode "Up Next"; false => resume in progress
    position: number; // seconds watched (0 when upNext)
    duration: number; // total seconds (0 when upNext)
    watchedAt: string; // ISO; drives most-recent ordering
    progress: number; // 0..1, clamped — drives the bar (0 when upNext)
  }
</script>

<script lang="ts">
  import { Play, Film, Tv } from "lucide-svelte";
  import { formatPosition } from "$lib/api";

  let { item, onResume } = $props<{
    item: ContinueItem;
    onResume: (item: ContinueItem) => void;
  }>();

  // Movies show time left; a resumed episode shows S/E; a rolled-forward
  // episode shows S/E + "Up Next".
  const subtitle = $derived.by(() => {
    if (item.mediaType !== "tv") {
      return `${formatPosition(Math.max(0, item.duration - item.position))} left`;
    }
    const tag = `S${item.season}E${item.episode}`;
    return item.upNext ? `${tag} · Up Next` : tag;
  });

  const pct = $derived(
    Math.round(Math.min(1, Math.max(0, item.progress)) * 100),
  );
</script>

<button
  onclick={() => onResume(item)}
  class="group flex w-70 shrink-0 flex-col gap-2 rounded-2xl text-left"
  style="scroll-snap-align: start;"
  aria-label={item.upNext ? `Play ${item.title}` : `Resume ${item.title}`}
>
  <span class="relative block overflow-hidden rounded-md">
    {#if item.image}
      <img
        src={item.image}
        alt={item.title}
        class="aspect-video w-full object-cover transition-transform duration-200 group-hover:scale-105"
      />
    {:else}
      {@const Icon = item.mediaType === "tv" ? Tv : Film}
      <div
        class="flex aspect-video w-full items-center justify-center bg-secondary"
      >
        <Icon class="size-8 text-muted-foreground/40" />
      </div>
    {/if}

    <!-- Hover play affordance -->
    <span
      class="absolute inset-0 flex items-center justify-center bg-black/0 transition-colors duration-200 group-hover:bg-black/30"
    >
      <span
        class="flex size-12 items-center justify-center rounded-full bg-white/90 opacity-0 transition-opacity duration-200 group-hover:opacity-100"
      >
        <Play class="size-6 translate-x-0.5 fill-current text-black" />
      </span>
    </span>

    <!-- Title + episode/remaining label -->
    <span
      class="absolute inset-x-0 bottom-0 block px-2 pt-24 pb-2.5"
      style="background: linear-gradient(to top, rgba(0,0,0,0.85) 0%, transparent 100%)"
    >
      <span
        class="block truncate text-sm leading-tight font-semibold text-white"
      >
        {item.title}
      </span>
      <span class="block truncate text-xs text-white/70">{subtitle}</span>
    </span>

    <!-- Progress line — only for a resume (a fresh "Up Next" episode has none) -->
    {#if !item.upNext && pct > 0}
      <span class="absolute inset-x-0 bottom-0 block h-1 bg-white/25">
        <span class="block h-full bg-accent" style="width: {pct}%"></span>
      </span>
    {/if}
  </span>
</button>
