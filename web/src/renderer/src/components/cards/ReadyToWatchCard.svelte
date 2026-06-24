<script lang="ts" module>
  // Shape produced by ReadyToWatch.svelte for each show that has aired-but-
  // unwatched episodes. `season`/`episode` point at the NEXT episode to watch
  // (the first unwatched one), while `waiting` is how many aired episodes are
  // queued up from that point onward.
  export interface ReadyItem {
    tmdbId: number;
    title: string;
    posterPath: string;
    stillPath: string;
    season: number;
    episode: number;
    episodeName: string;
    airDate: string; // when the next-to-watch episode aired
    waiting: number; // count of aired-but-unwatched episodes
  }
</script>

<script lang="ts">
  import { EyeOff, Tv } from "lucide-svelte";
  import type { Media } from "$lib/types/tmdb";
  import { Badge } from "$lib/components/ui/badge/index.js";
  import { settings } from "$lib/stores/settings";
  import ScrambledText from "../ScrambledText.svelte";

  let {
    item,
    onSelectMedia,
  }: { item: ReadyItem; onSelectMedia: (m: Media) => void } = $props();

  // Partial Media stand-in — same approach as UpcomingMediaCard. The detail
  // view it opens into re-fetches the full object, so a thin shell is enough.
  function toMedia(it: ReadyItem): Media {
    return {
      id: it.tmdbId,
      media_type: "tv",
      name: it.title,
      poster_path: it.posterPath,
      overview: "",
      vote_average: 0,
    } as unknown as Media;
  }

  function formatAired(dateStr: string): string {
    if (!dateStr) return "";
    const date = new Date(dateStr + "T00:00:00");
    if (Number.isNaN(date.getTime())) return "";
    return date.toLocaleDateString(undefined, {
      month: "short",
      day: "numeric",
      year: "numeric",
    });
  }

  const media = $derived(toMedia(item));

  // Prefer the episode still over the show poster, like the Upcoming card.
  const imageSrc = $derived(item.stillPath || item.posterPath || null);

  const isSpoilerHidden = $derived(!!$settings?.hideSpoilers);

  // "1 waiting" / "5 waiting"
  const waitingLabel = $derived(`${item.waiting} waiting`);
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
        class="aspect-video w-full object-cover transition-transform duration-200 group-hover:scale-105 {isSpoilerHidden
          ? 'scale-110 blur-md'
          : ''}"
      />
    {:else}
      <div
        class="flex aspect-video w-full items-center justify-center bg-secondary"
      >
        <Tv class="size-8 text-muted-foreground/40" />
      </div>
    {/if}

    {#if isSpoilerHidden && imageSrc}
      <span class="absolute inset-0 flex items-center justify-center">
        <EyeOff class="size-7 text-white drop-shadow-md" />
      </span>
    {/if}

    <span
      class="absolute inset-x-0 top-0 flex flex-row justify-between px-2 pt-1.5"
    >
      <!-- How many aired episodes are queued up for this show -->
      <Badge
        class="block bg-accent/90 text-[11px] font-semibold tracking-wide text-accent-foreground"
      >
        {waitingLabel}
      </Badge>
      {#if isSpoilerHidden}
        <Badge variant="outline">Spoilers Hidden</Badge>
      {/if}
    </span>

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
          S{item.season}E{item.episode}
          {#if formatAired(item.airDate)}
            · {formatAired(item.airDate)}
          {/if}
          {#if item.episodeName}
            · <ScrambledText text={item.episodeName} active={isSpoilerHidden} />
          {/if}
        </span>
      </span>
    </span>
  </span>
</button>
