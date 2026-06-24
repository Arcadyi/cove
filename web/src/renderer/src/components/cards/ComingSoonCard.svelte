<script lang="ts" module>
  // Shape produced by ComingSoon.svelte for each upcoming release in the user's
  // watch_later / watching lists. Movies use release_date; TV entries are season
  // premieres only (episode_number === 1) so they don't overlap with Upcoming.
  export interface ComingSoonItem {
    tmdbId: number;
    title: string;
    mediaType: "movie" | "tv";
    posterPath: string;
    backdropPath: string;
    releaseDate: string; // ISO date string (YYYY-MM-DD)
    seasonNumber?: number; // TV season premieres only
    daysUntil: number;
  }
</script>

<script lang="ts">
  import { Film, Tv } from "lucide-svelte";
  import type { Media } from "$lib/types/tmdb";
  import { Badge } from "$lib/components/ui/badge/index.js";

  let {
    item,
    onSelectMedia,
  }: { item: ComingSoonItem; onSelectMedia: (m: Media) => void } = $props();

  function toMedia(it: ComingSoonItem): Media {
    return {
      id: it.tmdbId,
      media_type: it.mediaType,
      ...(it.mediaType === "movie" ? { title: it.title } : { name: it.title }),
      poster_path: it.posterPath,
      overview: "",
      vote_average: 0,
    } as unknown as Media;
  }

  function formatDate(dateStr: string): string {
    if (!dateStr) return "";
    const d = new Date(dateStr + "T00:00:00");
    if (Number.isNaN(d.getTime())) return "";
    return d.toLocaleDateString(undefined, {
      month: "short",
      day: "numeric",
      year: "numeric",
    });
  }

  // "Today" / "Tomorrow" / "in X days"
  function countdownLabel(days: number): string {
    if (days <= 0) return "Today";
    if (days === 1) return "Tomorrow";
    return `In ${days} days`;
  }

  const media = $derived(toMedia(item));
  const imageSrc = $derived(item.backdropPath || item.posterPath || null);

  // "Season 3" for TV, "Movie" for films
  const typeLabel = $derived(
    item.mediaType === "tv" && item.seasonNumber != null
      ? `Season ${item.seasonNumber}`
      : "Movie",
  );
</script>

<button
  onclick={() => onSelectMedia(media)}
  class="group flex w-70 shrink-0 flex-col gap-2 rounded-2xl text-left"
  style="scroll-snap-align: start;"
>
  <span class="relative block overflow-hidden rounded-md">
    {#if imageSrc}
      <img
        src={imageSrc}
        alt={item.title}
        class="aspect-video w-full object-cover transition-transform duration-200 group-hover:scale-105"
      />
    {:else}
      <div
        class="flex aspect-video w-full items-center justify-center bg-secondary"
      >
        {#if item.mediaType === "tv"}
          <Tv class="size-8 text-muted-foreground/40" />
        {:else}
          <Film class="size-8 text-muted-foreground/40" />
        {/if}
      </div>
    {/if}

    <!-- Top badges: type label + countdown -->
    <span
      class="absolute inset-x-0 top-0 flex flex-row justify-between px-2 pt-1.5"
    >
      <Badge
        class="bg-primary/90 text-[11px] font-semibold tracking-wide text-primary-foreground"
      >
        {typeLabel}
      </Badge>
      <Badge
        class="text-[11px] font-semibold tracking-wide
          {item.daysUntil <= 7
          ? 'bg-orange-500/90 text-white'
          : 'bg-accent/90 text-accent-foreground'}"
      >
        {countdownLabel(item.daysUntil)}
      </Badge>
    </span>

    <!-- Bottom gradient: title + date -->
    <span
      class="absolute inset-x-0 bottom-0 block px-2 pt-24 pb-1.5"
      style="background: linear-gradient(to top, rgba(0,0,0,0.85) 0%, transparent 100%)"
    >
      <span class="block">
        <span
          class="block truncate text-sm leading-tight font-semibold text-white"
        >
          {item.title}
        </span>
        <span class="block truncate text-xs text-muted-foreground">
          {formatDate(item.releaseDate)}
        </span>
      </span>
    </span>
  </span>
</button>
