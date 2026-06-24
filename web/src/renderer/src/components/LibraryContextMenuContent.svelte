<script lang="ts">
  import { api, type LibraryStatus, STATUS_LABELS } from "$lib/api";
  import type { LibraryEntry } from "$lib/types/library";
  import type { Media } from "$lib/types/tmdb";
  import { BookmarkIcon, List } from "lucide-svelte";
  import { animate } from "animejs";
  import { libraryChanged } from "$lib/stores/library";
  import * as ContextMenu from "$lib/components/ui/context-menu/index.js";

  let {
    libraryEntry,
    media,
    lastAiredSeason = null,
    lastAiredEpisode = null,
    onpopoverchange,
  } = $props<{
    libraryEntry: LibraryEntry;
    media: Media;
    lastAiredSeason?: number | null;
    lastAiredEpisode?: number | null;
    onpopoverchange?: (open: boolean) => void;
  }>();

  const title = $derived(media.media_type === "tv" ? media.name : media.title);

  function animateBookmarkIn(el: HTMLElement): void {
    animate(el, {
      scale: [0, 1.3, 1],
      opacity: [0, 1],
      duration: 300,
      ease: "outBack",
    });
  }

  async function handleStatus(status: LibraryStatus): Promise<void> {
    try {
      if (libraryEntry?.status === status) {
        await api.libraryRemove(media.id, media.media_type);
        libraryEntry = null;
      } else if (libraryEntry) {
        libraryEntry = await api.librarySetStatus(
          media.id,
          media.media_type,
          status,
        );
      } else {
        libraryEntry = await api.libraryUpsert({
          tmdb_id: media.id,
          media_type: media.media_type,
          title,
          poster_path: media.poster_path ?? "",
          vote_average: media.vote_average ?? 0,
          last_air_date: media.last_air_date ?? "",
          last_aired_season: lastAiredSeason,
          last_aired_episode: lastAiredEpisode,
          status,
        });
      }

      libraryChanged.update((n) => n + 1);
      onpopoverchange?.(false);
    } catch (e) {
      console.error("library status:", e);
    }
  }
</script>

<ContextMenu.Sub>
  <ContextMenu.SubTrigger>
    <span class="flex items-center justify-start gap-4 align-middle">
      <List />
      Add to List
    </span>
  </ContextMenu.SubTrigger>
  <ContextMenu.SubContent class="w-48">
    <p class="px-2 py-1.5 text-center text-xs text-muted-foreground">
      Click to add or remove
    </p>
    {#each Object.entries(STATUS_LABELS) as [value, label] (value)}
      {@const isActive = libraryEntry?.status === value}
      <ContextMenu.Item
        onclick={(e) => {
          e.stopPropagation();
          handleStatus(value as LibraryStatus);
        }}
      >
        <span class="flex w-full items-center gap-3">
          <span class="size-4 shrink-0">
            {#if isActive}
              <span use:animateBookmarkIn>
                <BookmarkIcon class="size-4" />
              </span>
            {/if}
          </span>
          {label}
        </span>
      </ContextMenu.Item>
    {/each}
  </ContextMenu.SubContent>
</ContextMenu.Sub>
