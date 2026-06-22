<script lang="ts">
  import { api, STATUS_LABELS, type LibraryStatus } from "$lib/api";
  import type { LibraryEntry } from "$lib/types/library";
  import type { Media } from "$lib/types/tmdb";
  import { ScrollArea } from "$lib/components/ui/scroll-area/index.js";
  import { Spinner } from "$lib/components/ui/spinner/index.js";
  import MediaCard from "./MediaCard.svelte";
  import { BookMarked, Star, ArrowDownUp, Filter, Film } from "lucide-svelte";
  import { onMount } from "svelte";
  import { libraryChanged } from "$lib/stores/library";
  import { flip } from "svelte/animate";
  import { cubicOut } from "svelte/easing";
  import Upcoming from "./Upcoming.svelte";
  import ReadyToWatch from "./cards/ReadyToWatch.svelte";
  import ComingSoon from "./ComingSoon.svelte";
  import { Button } from "$lib/components/ui/button/index.js";
  import * as ButtonGroup from "$lib/components/ui/button-group/index.js";
  import * as Select from "$lib/components/ui/select/index.js";
  import InsightsPage from "./InsightsPage.svelte";
  import {SvelteSet} from "svelte/reactivity";

  let {
    onSelectMedia,
    onWatch,
  }: {
    onSelectMedia: (m: Media) => void;
    onWatch?: (m: Media, season?: number, episode?: number) => void;
  } = $props();

  // ── State ────────────────────────────────────────────────────────────────────

  let entries = $state<LibraryEntry[]>([]);
  let loading = $state(true);
  let activeType = $state<"all" | "movie" | "tv">("all");
  let activeStatus = $state<LibraryStatus | "all">("all");

  // ── Sort & genre filter ───────────────────────────────────────────────────────

  type SortKey =
    | "default"
    | "watched_desc"
    | "added_desc"
    | "added_asc"
    | "release_desc"
    | "tmdb_desc"
    | "personal_desc"
    | "title_asc";

  const SORT_OPTIONS: { value: SortKey; label: string }[] = [
    { value: "default", label: "Recommended" },
    { value: "watched_desc", label: "Recently watched" },
    { value: "added_desc", label: "Recently added" },
    { value: "added_asc", label: "Oldest added" },
    { value: "release_desc", label: "Release date" },
    { value: "tmdb_desc", label: "TMDB rating" },
    { value: "personal_desc", label: "Your rating" },
    { value: "title_asc", label: "Title (A–Z)" },
  ];

  let sortKey = $state<SortKey>("default");
  let activeGenre = $state<string>("all"); // genre name, or "all"

  // TMDB genre id→name, kept per media_type because the movie and tv id spaces
  // overlap with different meanings (e.g. 10759 is tv-only Action & Adventure).
  let genreNames = $state<{
    movie: Record<number, string>;
    tv: Record<number, string>;
  }>({ movie: {}, tv: {} });

  // Trigger labels for the selects (shadcn Select renders the label ourselves).
  const typeLabel = $derived(
    activeType === "movie"
      ? "Movies"
      : activeType === "tv"
        ? "TV Shows"
        : "All",
  );
  const sortLabel = $derived(
    SORT_OPTIONS.find((o) => o.value === sortKey)?.label ?? "Sort",
  );
  const genreLabel = $derived(
    activeGenre === "all" ? "All genres" : activeGenre,
  );

  // ── Data ─────────────────────────────────────────────────────────────────────

  async function loadEntries(showSpinner = true): Promise<void> {
    if (showSpinner) loading = true;
    try {
      entries = await api.libraryList();
    } finally {
      if (showSpinner) loading = false;
    }
  }

  // TMDB genre lists are static, so fetch the id→name maps once. Used to label
  // the genre filter and to resolve an entry's genres when the loaded Media
  // only carries numeric genre_ids.
  async function loadGenreNames(): Promise<void> {
    try {
      const [movie, tv] = await Promise.all([
        api.genreList("movie"),
        api.genreList("tv"),
      ]);
      genreNames = {
        movie: Object.fromEntries(movie.map((g) => [g.id, g.name])) as Record<
          number,
          string
        >,
        tv: Object.fromEntries(tv.map((g) => [g.id, g.name])) as Record<
          number,
          string
        >,
      };
    } catch (e) {
      console.error("Failed to load genre names", e);
    }
  }

  onMount(() => {
    loadEntries(true);
    loadGenreNames();
  });

  let initialized = $state(false);
  $effect(() => {
    $libraryChanged;
    if (!initialized) {
      initialized = true;
      return;
    }
    loadEntries(false); // silent — no spinner, no re-render
  });

  // ── Derived ──────────────────────────────────────────────────────────────────

  const TAB_ORDER: (LibraryStatus | "all")[] = [
    "all",
    "watching",
    "watch_later",
    "finished",
    "dropped",
  ];

  const TAB_LABELS: Record<string, string> = { all: "All", ...STATUS_LABELS };

  const counts = $derived(
    Object.fromEntries(
      TAB_ORDER.map((s) => {
        const typeFiltered = entries.filter(
          (e) => activeType === "all" || e.media_type === activeType,
        );
        return [
          s,
          s === "all"
            ? typeFiltered.length
            : typeFiltered.filter((e) => e.status === s).length,
        ];
      }),
    ),
  );

  // Section order for the grouped layout: each status becomes its own headed
  // list, rendered top-to-bottom in this order.
  const SECTION_ORDER: LibraryStatus[] = [
    "watching",
    "watch_later",
    "finished",
    "dropped",
  ];

  // Genre names present across the current type view — drives the genre filter
  // dropdown. Populates as Media objects finish loading.
  const availableGenres = $derived.by(() => {
    const set = new SvelteSet<string>();
    for (const e of entries) {
      if (activeType !== "all" && e.media_type !== activeType) continue;
      for (const g of genresFor(e)) set.add(g);
    }
    return [...set].sort((a, b) => a.localeCompare(b));
  });

  // Grouped, headed lists. On the "all" tab every non-empty status gets its own
  // section; on a specific tab there's a single section for that status. Within
  // each section, entries honor the active genre filter and the chosen sort.
  // Empty groups are dropped so no bare header shows.
  const sections = $derived(
    (activeStatus === "all" ? SECTION_ORDER : [activeStatus])
      .map((status) => ({
        status,
        label: STATUS_LABELS[status],
        entries: entries
          .filter(
            (e) =>
              e.status === status &&
              (activeType === "all" || e.media_type === activeType) &&
              matchesGenre(e),
          )
          .toSorted(compareEntries),
      }))
      .filter((section) => section.entries.length > 0),
  );

  // If the selected genre stops existing (e.g. after switching type), reset it
  // so the view doesn't get stuck showing nothing.
  $effect(() => {
    if (activeGenre !== "all" && !availableGenres.includes(activeGenre)) {
      activeGenre = "all";
    }
  });

  // ── Helpers ──────────────────────────────────────────────────────────────────

  // A show has unwatched new episodes when the latest aired episode is
  // numerically ahead of the user's last-watched episode. Comparing season
  // and episode numbers (not dates) avoids the bug where a recently-watched
  // older episode looks "newer" than an unwatched episode that aired weeks
  // ago — timestamps don't reflect watch order, episode numbers do.
  function hasNewEpisodes(entry: LibraryEntry): boolean {
    if (entry.media_type !== "tv" || entry.status !== "watching") return false;

    const airedS = entry.last_aired_season;
    const airedE = entry.last_aired_episode;
    if (airedS == null || airedE == null) return false;

    const watchedS = entry.last_watched_season ?? 0;
    const watchedE = entry.last_watched_episode ?? 0;

    if (airedS > watchedS) return true;
    return airedS === watchedS && airedE > watchedE;
  }

  // ── Sort & filter accessors ───────────────────────────────────────────────────

  function ts(d?: string | null): number {
    if (!d) return 0;
    const t = new Date(d).getTime();
    return Number.isNaN(t) ? 0 : t;
  }

  // Genre names for an entry. Prefers names already on the fetched Media; falls
  // back to mapping numeric genre_ids through the per-type TMDB genre list.
  // Empty until the Media for this entry has loaded.
  function genresFor(entry: LibraryEntry): string[] {
    const media = mediaByKey[toMediaKey(entry)] as
      | (Media & {
          genres?: { id: number; name: string }[];
          genre_ids?: number[];
        })
      | undefined;
    if (!media) return [];
    if (Array.isArray(media.genres) && media.genres.length) {
      return media.genres.map((g) => g.name).filter(Boolean);
    }
    const ids = media.genre_ids ?? [];
    const map = genreNames[entry.media_type as "movie" | "tv"] ?? {};
    return ids.map((id) => map[id]).filter(Boolean);
  }

  // TMDB community score. Stored on the entry in newer libraries (libraryUpsert
  // persists vote_average); otherwise read off the loaded Media.
  function tmdbRating(entry: LibraryEntry): number {
    const onEntry = (entry as LibraryEntry & { vote_average?: number })
      .vote_average;
    if (typeof onEntry === "number" && onEntry > 0) return onEntry;
    const media = mediaByKey[toMediaKey(entry)] as
      | (Media & { vote_average?: number })
      | undefined;
    return media?.vote_average ?? 0;
  }

  function personalRating(entry: LibraryEntry): number {
    return entry.rating ?? -1; // unrated sinks to the bottom
  }

  // Best-available "last watched" recency. NOTE: this assumes a watch-timestamp
  // field on the entry and falls back to added_at when none is present — see the
  // note in chat to confirm/correct the real field name.
  function lastWatchedAt(entry: LibraryEntry): number {
    const e = entry as LibraryEntry & {
      last_watched_at?: string;
      watched_at?: string;
      updated_at?: string;
    };
    return ts(
      e.last_watched_at ?? e.watched_at ?? e.updated_at ?? entry.added_at,
    );
  }

  function releaseDate(entry: LibraryEntry): number {
    const media = mediaByKey[toMediaKey(entry)] as
      | (Media & {
          release_date?: string;
          first_air_date?: string;
          last_air_date?: string;
        })
      | undefined;
    return ts(
      entry.last_air_date ??
        media?.release_date ??
        media?.first_air_date ??
        media?.last_air_date,
    );
  }

  function titleOf(entry: LibraryEntry): string {
    return (entry.title ?? "").toLowerCase();
  }

  // The previous "smart" ordering — new episodes first, then most recent —
  // preserved as the default sort.
  function defaultCompare(a: LibraryEntry, b: LibraryEntry): number {
    const aNew = hasNewEpisodes(a) ? 1 : 0;
    const bNew = hasNewEpisodes(b) ? 1 : 0;
    if (bNew !== aNew) return bNew - aNew;
    return (
      ts(b.last_air_date || b.added_at) - ts(a.last_air_date || a.added_at)
    );
  }

  function compareEntries(a: LibraryEntry, b: LibraryEntry): number {
    switch (sortKey) {
      case "added_desc":
        return ts(b.added_at) - ts(a.added_at);
      case "added_asc":
        return ts(a.added_at) - ts(b.added_at);
      case "title_asc":
        return titleOf(a).localeCompare(titleOf(b));
      case "tmdb_desc":
        return tmdbRating(b) - tmdbRating(a);
      case "personal_desc":
        return personalRating(b) - personalRating(a);
      case "watched_desc":
        return lastWatchedAt(b) - lastWatchedAt(a);
      case "release_desc":
        return releaseDate(b) - releaseDate(a);
      default:
        return defaultCompare(a, b);
    }
  }

  function matchesGenre(entry: LibraryEntry): boolean {
    if (activeGenre === "all") return true;
    return genresFor(entry).includes(activeGenre);
  }

  function toMediaKey(entry: LibraryEntry): string {
    return `${entry.tmdb_id}-${entry.media_type}`;
  }

  // Real, fully-populated Media objects fetched per entry — replaces the old
  // toMedia() stub that hand-built a partial Media client-side (hardcoding
  // overview: "" among other things). LibraryEntry intentionally doesn't
  // persist a full copy of TMDB's metadata, so this is the genuine source
  // instead of a lossy stand-in. Keyed so re-fetching on every libraryChanged
  // tick doesn't re-request titles already loaded.
  let mediaByKey = $state<Record<string, Media>>({});

  async function ensureMediaLoaded(entry: LibraryEntry): Promise<void> {
    const key = toMediaKey(entry);
    if (mediaByKey[key]) return;
    try {
      mediaByKey[key] = await api.getMediaByID(entry.tmdb_id, entry.media_type);
    } catch (e) {
      console.error("Failed to load media for", key, e);
    }
  }

  $effect(() => {
    if (entries?.length === 0){
      return
    }
    for (const entry of entries) {
      ensureMediaLoaded(entry); // no-op if cached; fires in parallel otherwise
    }
  });

  const EMPTY_MESSAGES: Record<string, { heading: string; sub: string }> = {
    all: {
      heading: "Your list is empty",
      sub: "Open any title and use the status buttons to start tracking.",
    },
    watching: {
      heading: "Nothing in progress",
      sub: "Mark something as Watching to see it here.",
    },
    watch_later: {
      heading: "Nothing saved for later",
      sub: "Found something you want to watch? Hit Watch Later.",
    },
    finished: {
      heading: "Nothing finished yet",
      sub: "Mark a title as Finished once you're done.",
    },
    dropped: {
      heading: "Nothing dropped",
      sub: "Titles you give up on will appear here.",
    },
  };
</script>

<div class="relative h-full p-6 pt-18">
  <!-- ── Sticky header with gradient fade ───────────────────────────────────── -->
  <div
    class="absolute top-18 right-6 left-6 z-10 p-4 pb-6"
    style="
      background: linear-gradient(to bottom, var(--background) 0%, var(--background) 70%, rgba(0,0,0,0) 100%);
      pointer-events: none;
    "
  >
    <div class="pointer-events-auto">
      <!-- Title row -->
      <div class="mb-4 flex items-baseline gap-3">
        <h1 class="text-2xl font-semibold">My List</h1>
        {#if !loading && entries.length > 0}
          <span class="text-sm text-muted-foreground">
            {entries.length} title{entries.length !== 1 ? "s" : ""}
          </span>
        {/if}
      </div>

      <div class="flex flex-row justify-center gap-1">
        <!-- Status tabs -->
        {#if !loading && entries.length > 0}
          <div class="flex flex-wrap gap-1.5">
            <!-- Type -->
            <Select.Root
              type="single"
              value={activeType}
              onValueChange={(v) => (activeType = v as typeof activeType)}
            >
              <Select.Trigger
                class="h-8 w-auto gap-1.5 rounded-full border-0 bg-secondary px-3 text-xs font-medium text-foreground hover:bg-secondary/70"
              >
                <Film class="size-3.5 text-muted-foreground" />
                {typeLabel}
              </Select.Trigger>
              <Select.Content>
                <Select.Item value="all" label="All">All</Select.Item>
                <Select.Item value="movie" label="Movies">Movies</Select.Item>
                <Select.Item value="tv" label="TV Shows">TV Shows</Select.Item>
              </Select.Content>
            </Select.Root>

            <ButtonGroup.Root>
              {#each TAB_ORDER as tab (tab)}
                {@const count = counts[tab]}
                {#if count > 0 || tab === "all"}
                  <Button
                    onclick={() => (activeStatus = tab)}
                    size="default"
                    class="flex items-center gap-1.5 rounded-full px-3 py-1 text-xs font-medium transition-colors
                  {activeStatus === tab
                      ? 'bg-foreground text-background'
                      : 'bg-secondary text-muted-foreground hover:bg-secondary/70 hover:text-foreground'}"
                  >
                    {TAB_LABELS[tab]}
                    <span
                      class="tabular-nums {activeStatus === tab
                        ? 'text-background/70'
                        : 'text-muted-foreground/60'}"
                    >
                      {count}
                    </span>
                  </Button>
                {/if}
              {/each}
            </ButtonGroup.Root>

            <!-- Sort -->
            <Select.Root
              type="single"
              value={sortKey}
              onValueChange={(v) => (sortKey = v as SortKey)}
            >
              <Select.Trigger
                class="h-8 w-auto gap-1.5 rounded-full border-0 bg-secondary px-3 text-xs font-medium text-foreground hover:bg-secondary/70"
              >
                <ArrowDownUp class="size-3.5 text-muted-foreground" />
                {sortLabel}
              </Select.Trigger>
              <Select.Content>
                {#each SORT_OPTIONS as opt (opt.value)}
                  <Select.Item value={opt.value} label={opt.label}>
                    {opt.label}
                  </Select.Item>
                {/each}
              </Select.Content>
            </Select.Root>

            <!-- Genre -->
            {#if availableGenres.length > 0}
              <Select.Root
                type="single"
                value={activeGenre}
                onValueChange={(v) => (activeGenre = v)}
              >
                <Select.Trigger
                  class="h-8 w-auto gap-1.5 rounded-full border-0 bg-secondary px-3 text-xs font-medium text-foreground hover:bg-secondary/70
                  {activeGenre !== 'all' ? 'ring-1 ring-foreground/30' : ''}"
                >
                  <Filter class="size-3.5 text-muted-foreground" />
                  {genreLabel}
                </Select.Trigger>
                <Select.Content>
                  <Select.Item value="all" label="All genres">
                    All genres
                  </Select.Item>
                  {#each availableGenres as g (g)}
                    <Select.Item value={g} label={g}>{g}</Select.Item>
                  {/each}
                </Select.Content>
              </Select.Root>
            {/if}
          </div>
        {/if}
      </div>
    </div>
  </div>

  <!-- ── Loading ────────────────────────────────────────────────────────────── -->
  {#if loading}
    <div class="flex h-full items-center justify-center">
      <Spinner class="size-8" />
    </div>

    <!-- ── Empty: no entries at all ──────────────────────────────────────────── -->
  {:else if entries.length === 0}
    <div class="flex h-full flex-col items-center justify-center gap-3">
      <BookMarked class="size-12 text-muted-foreground/30" />
      <p class="text-base font-medium">{EMPTY_MESSAGES.all.heading}</p>
      <p class="text-sm text-muted-foreground">{EMPTY_MESSAGES.all.sub}</p>
    </div>

    <!-- ── Content ────────────────────────────────────────────────────────────── -->
  {:else}
    <ScrollArea class="h-full">
      <div class="mt-28 flex flex-col gap-4 p-4">
        <div class="rounded-2xl border p-4">
          <ComingSoon {onSelectMedia} />
        </div>
        <div class="rounded-2xl border p-4">
          <Upcoming {onSelectMedia} />
        </div>
        <div class="rounded-2xl border p-4">
          <ReadyToWatch {onSelectMedia} />
        </div>
      </div>

      {#if sections.length === 0}
        <div class="flex h-[60vh] flex-col items-center justify-center gap-4">
          {#if activeGenre !== "all"}
            <p class="text-base font-medium">No titles in this genre</p>
            <p class="text-sm text-muted-foreground">
              Try a different genre or clear the filter.
            </p>
          {:else}
            <p class="text-base font-medium">
              {EMPTY_MESSAGES[activeStatus]?.heading}
            </p>
            <p class="text-sm text-muted-foreground">
              {EMPTY_MESSAGES[activeStatus]?.sub}
            </p>
          {/if}
        </div>
      {:else}
        {#each sections as section (section.status)}
          <section class="mt-8 first:mt-5">
            <!-- List header -->
            <div class="mb-3 flex items-baseline gap-2 pr-4">
              <h2 class="text-lg font-semibold">{section.label}</h2>
              <span class="text-sm text-muted-foreground tabular-nums">
                {section.entries.length}
              </span>
            </div>

            <div
              class="grid gap-4 pr-4"
              style="grid-template-columns: repeat(auto-fill, minmax(150px, 1fr))"
            >
              {#each section.entries as entry (entry.id)}
                {@const media = mediaByKey[toMediaKey(entry)]}
                <div
                  class="relative"
                  animate:flip={{ duration: 300, easing: cubicOut }}
                >
                  {#if media}
                    <MediaCard
                      {media}
                      onclick={() => onSelectMedia(media)}
                      newEpisodes={hasNewEpisodes(entry)}
                      onwatch={onWatch}
                    />
                  {:else}
                    <!-- Real Media object still loading — show the poster we
                         already have stored so the grid doesn't look broken,
                         swap to the interactive MediaCard once it resolves. -->
                    <img
                      src={entry.poster_path}
                      alt={entry.title}
                      class="aspect-2/3 w-full rounded-md object-cover opacity-60"
                    />
                  {/if}

                  {#if entry.rating !== null && entry.rating !== undefined}
                    <div
                      class="pointer-events-none absolute top-1.5 left-1.5 z-10 flex items-center gap-0.5 rounded border border-yellow-400/40 bg-black/65 px-1.5 py-0.5 text-[10px] font-semibold text-yellow-400 backdrop-blur-sm"
                    >
                      <Star class="size-2.5 fill-current" />
                      {entry.rating}
                    </div>
                  {/if}
                </div>
              {/each}
            </div>
          </section>
        {/each}
      {/if}
      <InsightsPage />
    </ScrollArea>
  {/if}
</div>
