<script lang="ts">
  import { api, type LibraryStatus, STATUS_LABELS } from "$lib/api";
  import type { LibraryEntry } from "$lib/types/library";
  import type { Media } from "$lib/types/tmdb";
  import * as Popover from "$lib/components/ui/popover/index.js";
  import { Button } from "$lib/components/ui/button/index.js";
  import { BookmarkIcon, BookmarkPlus } from "lucide-svelte";
  import { animate } from "animejs";
  import * as ButtonGroup from "$lib/components/ui/button-group/index.js";
  import { libraryChanged } from "$lib/stores/library";

  let {
    libraryEntry,
    media,
    size = "icon",
    lastAiredSeason = null,
    lastAiredEpisode = null,
    onpopoverchange,
    class: className = "",
  } = $props<{
    libraryEntry: LibraryEntry | null;
    media: Media;
    size: string | null;
    lastAiredSeason?: number | null;
    lastAiredEpisode?: number | null;
    onpopoverchange?: (open: boolean) => void;
    class?: string;
  }>();

  const title = $derived(media.media_type === "tv" ? media.name : media.title);

  let popoverOpen = $state(false);

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
      // Set directly rather than relying solely on onOpenChange: mutating a
      // bind:open value doesn't reliably re-fire the component's own
      // onOpenChange callback, so the parent's popoverOpen tracking (and the
      // hover card's close check tied to it) would otherwise never hear that
      // this closed.
      popoverOpen = false;
      onpopoverchange?.(false);
    } catch (e) {
      console.error("library status:", e);
    }
  }

  const inLibrary = $derived(!!libraryEntry);
</script>

<Popover.Root
  bind:open={popoverOpen}
  onOpenChange={(o) => onpopoverchange?.(o)}
>
  <Popover.Trigger>
    <Button variant="default" {size} class={className}>
      {#if inLibrary}
        <BookmarkIcon class="size-4" />
      {:else}
        <BookmarkPlus />
      {/if}
    </Button>
  </Popover.Trigger>
  <Popover.Content class="rounded-3xl p-0">
    <ButtonGroup.Root orientation="vertical" class="w-full">
      {#each Object.entries(STATUS_LABELS) as [value, label] (value)}
        {@const isActive = libraryEntry?.status === value}
        <Button
          onclick={(e: { stopPropagation: () => void }) => {
            e.stopPropagation();
            handleStatus(value as LibraryStatus);
          }}
          variant={isActive ? "default" : "outline"}
          class="w-full"
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
        </Button>
      {/each}
    </ButtonGroup.Root>
  </Popover.Content>
</Popover.Root>
