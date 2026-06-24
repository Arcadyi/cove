<script lang="ts">
  import { Star } from "lucide-svelte";
  import { api } from "$lib/api";
  import type { LibraryEntry } from "$lib/types/library";
  import type { Media } from "$lib/types/tmdb";
  import { animate, stagger } from "animejs";
  import { libraryChanged } from "$lib/stores/library";

  let {
    libraryEntry,
    media,
    lastAiredSeason = null,
    lastAiredEpisode = null,
  } = $props<{
    libraryEntry: LibraryEntry;
    media: Media;
    lastAiredSeason?: number | null;
    lastAiredEpisode?: number | null;
  }>();

  const title = $derived(media.media_type === "tv" ? media.name : media.title);
  let hoverRating = $state<number>(0);
  const displayRating = $derived(
    hoverRating > 0 ? hoverRating : (libraryEntry?.rating ?? 0),
  );

  let starRefs = $state<HTMLButtonElement[]>([]);

  function getStarsUpTo(n: number): HTMLButtonElement[] {
    return starRefs.slice(0, n).filter(Boolean);
  }

  function animateHoverIn(star: number): void {
    const prev = hoverRating;
    hoverRating = star;

    if (star > prev) {
      const newTargets = starRefs.slice(prev, star).filter(Boolean);
      animate(newTargets, {
        scale: [1, 1.25, 1],
        duration: 300,
        ease: "outBack",
        delay: stagger(40),
      });
    } else if (star < prev) {
      const removedTargets = starRefs.slice(star, prev).filter(Boolean);
      animate(removedTargets, {
        scale: 1,
        duration: 200,
        ease: "outQuad",
      });
    }
  }

  function animateHoverOut(): void {
    hoverRating = 0;
  }

  function animateRatingSet(star: number): void {
    const targets = getStarsUpTo(star);
    if (!targets.length) return;
    animate(targets, {
      translateY: [0, -8, 2, -4, 0],
      duration: 500,
      ease: "outQuad",
      delay: stagger(70),
    });
  }

  function animateClear(): void {
    if (!starRefs.length) return;
    animate(starRefs.filter(Boolean), {
      translateX: [0, -4, 4, -3, 3, 0],
      duration: 350,
      ease: "outQuad",
    });
  }

  async function handleRating(star: number): Promise<void> {
    try {
      const newRating = libraryEntry?.rating === star ? null : star;

      if (newRating === null) {
        animateClear();
      } else {
        animateRatingSet(star);
      }

      if (!libraryEntry) {
        libraryEntry = await api.libraryUpsert({
          tmdb_id: media.id,
          media_type: media.media_type,
          title,
          poster_path: media.poster_path ?? "",
          vote_average: media.vote_average ?? 0,
          last_air_date: media.last_air_date ?? "",
          last_aired_season: lastAiredSeason,
          last_aired_episode: lastAiredEpisode,
        });
      }

      libraryEntry = await api.librarySetRating(
        media.id,
        media.media_type,
        newRating,
      );

      libraryChanged.update((n) => n + 1);
    } catch (e) {
      console.error("library rating:", e);
    }
  }
</script>

<div
  class="flex shrink-0 items-center gap-0.5"
  onmouseleave={animateHoverOut}
  role="group"
  aria-label="Rating"
>
  {#each [1, 2, 3, 4, 5] as star, i (star)}
    <button
      bind:this={starRefs[i]}
      onclick={(e) => {
        e.stopPropagation();
        handleRating(star);
      }}
      onmouseenter={() => animateHoverIn(star)}
      aria-label="Rate {star} star{star !== 1 ? 's' : ''}"
      class="rounded p-0.5"
      style="will-change: transform;"
    >
      <Star
        class="size-4 transition-colors duration-150 {displayRating >= star
          ? 'fill-yellow-400 text-yellow-400'
          : 'text-muted-foreground/40'}"
      />
    </button>
  {/each}
</div>
