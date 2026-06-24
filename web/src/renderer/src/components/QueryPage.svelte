<script lang="ts">
  import type { Media } from "$lib/types/tmdb";
  import type { SearchResults } from "$lib/api";
  import * as Select from "$lib/components/ui/select/index.js";
  import * as ToggleGroup from "$lib/components/ui/toggle-group/index.js";
  import { ScrollArea } from "$lib/components/ui/scroll-area/index.js";
  import MediaCard from "./MediaCard.svelte";
  import PersonCard from "./cards/PersonCard.svelte";
  import ProviderCard from "./cards/ProviderCard.svelte";
  import { SvelteMap, SvelteSet } from "svelte/reactivity";
  import { api } from "$lib/api";
  import { Button } from "$lib/components/ui/button";
  import { animate, splitText, stagger } from "animejs";
  import { onMount, tick } from "svelte";

  let {
    query = $bindable(""),
    loading = $bindable(false),
    onSelectMedia,
    onSuggested,
    onWatch,
    onSelectPerson = () => {},
    onSelectProvider = () => {},
  } = $props();

  // ── Search state ────────────────────────────────────────────────────────────
  const empty = (): SearchResults => ({
    movies: [],
    tv: [],
    people: [],
    providers: [],
  });

  let data = $state<SearchResults>(empty());
  let keywords: { id: number; name: string }[] = $state([]);
  let qualityMap = new SvelteMap<number, string>();

  // ── Controls ──────────────────────────────────────────────────────────────────
  const sortOptions = [
    { value: "relevance", label: "Relevance" },
    { value: "rating", label: "Rating" },
    { value: "popularity", label: "Popularity" },
    { value: "recommended", label: "Recommended for you" },
    { value: "personal", label: "My rating" },
  ] as const;

  // string (not a strict union) so it can bind cleanly to shadcn Select.
  let sortKey = $state<string>("relevance");
  const sortLabel = $derived(
    sortOptions.find((o) => o.value === sortKey)?.label ?? "Relevance",
  );

  const typeOptions = [
    { key: "movie", label: "Movies" },
    { key: "tv", label: "TV" },
    { key: "person", label: "People" },
    { key: "provider", label: "Providers" },
  ] as const;

  // ToggleGroup (multiple) binds to a string[] of the active type keys.
  let selectedTypes = $state<string[]>(["movie", "tv", "person", "provider"]);
  const showMovie = $derived(selectedTypes.includes("movie"));
  const showTV = $derived(selectedTypes.includes("tv"));
  const showPerson = $derived(selectedTypes.includes("person"));
  const showProvider = $derived(selectedTypes.includes("provider"));

  // ── Personalization (loaded once, used for the "recommended" / "my rating"
  // sort options). Genre scores approximate how recommended a title is from its
  // genres alone — no per-result detail fetch needed. ──────────────────────────
  let movieGenreScore = new SvelteMap<number, number>();
  let tvGenreScore = new SvelteMap<number, number>();
  let ratingByKey = new SvelteMap<string, number>(); // `${id}-${type}` -> rating

  onMount(async () => {
    const [insights, entries] = await Promise.all([
      api.discoverInsights().catch(() => null),
      api.libraryList().catch(() => []),
    ]);
    if (insights) {
      for (const g of insights.top_movie_genres)
        movieGenreScore.set(g.id, g.score);
      for (const g of insights.top_tv_genres) tvGenreScore.set(g.id, g.score);
    }
    for (const e of entries ?? []) {
      if (e.rating != null)
        ratingByKey.set(`${e.tmdb_id}-${e.media_type}`, e.rating);
    }
  });

  function recScore(m: Media): number {
    const map = m.media_type === "tv" ? tvGenreScore : movieGenreScore;
    let s = 0;
    for (const id of m.genre_ids ?? []) s += map.get(id) ?? 0;
    return s;
  }

  function ratingOf(m: Media): number {
    return ratingByKey.get(`${m.id}-${m.media_type}`) ?? -1;
  }

  // ── Derived display lists ───────────────────────────────────────────────────
  // Fold each matched person's known-for titles into the title sections, so a
  // search for "Jackie Chan" also surfaces his films under Movies/TV.
  function withKnownFor(list: Media[], type: "movie" | "tv"): Media[] {
    const seen = new SvelteSet(list.map((m) => m.id));
    const out = [...list];
    for (const p of data.people) {
      for (const m of p.known_for ?? []) {
        if (m.media_type === type && !seen.has(m.id)) {
          seen.add(m.id);
          out.push(m);
        }
      }
    }
    return out;
  }

  // Sort a copy, keeping the original (relevance) index as the tiebreak so the
  // order is deterministic.
  function sortMedia(list: Media[]): Media[] {
    const arr = list.map((m, i) => ({ m, i }));
    arr.sort((a, b) => {
      let primary = 0;
      switch (sortKey) {
        case "rating":
          primary = (b.m.vote_average ?? 0) - (a.m.vote_average ?? 0);
          break;
        case "popularity":
          primary = (b.m.popularity ?? 0) - (a.m.popularity ?? 0);
          break;
        case "recommended":
          primary = recScore(b.m) - recScore(a.m);
          break;
        case "personal":
          primary = ratingOf(b.m) - ratingOf(a.m);
          break;
        default:
          primary = 0; // relevance == original order
      }
      return primary !== 0 ? primary : a.i - b.i;
    });
    return arr.map((x) => x.m);
  }

  let movies = $derived(sortMedia(withKnownFor(data.movies, "movie")));
  let tv = $derived(sortMedia(withKnownFor(data.tv, "tv")));
  let people = $derived(
    [...data.people].sort((a, b) => b.popularity - a.popularity),
  );
  let providers = $derived(data.providers);

  let anyVisible = $derived(
    (showMovie && movies.length > 0) ||
      (showTV && tv.length > 0) ||
      (showPerson && people.length > 0) ||
      (showProvider && providers.length > 0),
  );

  // ── "Results for" animation ──────────────────────────────────────────────────
  let resultsTextEl = $state<HTMLElement>();
  let displayQuery = $state("");
  let hasAnimated = $state(false);

  async function animateText(text: string): Promise<void> {
    if (!resultsTextEl) return;
    displayQuery = text;
    await tick();

    const { chars } = splitText(resultsTextEl, { chars: { wrap: "clip" } });
    animate(chars, {
      y: [{ to: ["100%", "0%"] }],
      duration: 750,
      ease: "out(3)",
      delay: stagger(50),
    });
    hasAnimated = true;
  }

  // ── Best-effort stream of cached download qualities for the title results ─────
  function streamQuality(ids: number[]): void {
    if (ids.length === 0) return;
    fetch(`http://localhost:6969/api/quality/batch?ids=${ids.join(",")}`)
      .then(async (r) => {
        const reader = r.body!.getReader();
        const decoder = new TextDecoder();
        let buffer = "";
        while (true) {
          const { done, value } = await reader.read();
          if (done) break;
          buffer += decoder.decode(value, { stream: true });
          const lines = buffer.split("\n");
          buffer = lines.pop() ?? "";
          for (const line of lines) {
            if (!line.trim()) continue;
            try {
              const { id, quality } = JSON.parse(line);
              qualityMap.set(Number(id), quality);
            } catch {
              /* empty */
            }
          }
        }
      })
      .catch(() => {});
  }

  // ── Debounced search ──────────────────────────────────────────────────────────
  $effect(() => {
    const q = query.trim();
    const timeout = setTimeout(async () => {
      if (!q) {
        data = empty();
        keywords = [];
        qualityMap = new SvelteMap();
        return;
      }
      await animateText(query);
      loading = true;
      const [res, kw] = await Promise.all([
        api.searchMulti(q).catch(() => empty()),
        api.getKeywords(q).catch(() => []),
      ]);
      // Guard against null sections (e.g. an empty array serialized as null).
      data = {
        movies: res.movies ?? [],
        tv: res.tv ?? [],
        people: res.people ?? [],
        providers: res.providers ?? [],
      };
      keywords = kw ?? [];
      loading = false;

      streamQuality([...data.movies, ...data.tv].map((m) => m.id));
    }, 400);
    return () => clearTimeout(timeout);
  });
</script>

<div class="flex h-full flex-col p-6 pt-18">
  {#if query.length > 0}
    <div class="mb-4 shrink-0 space-y-3">
      <div class="flex text-2xl font-semibold" class:invisible={!hasAnimated}>
        Results for
        <span class="size-1.5"></span>
        {#key displayQuery}
          <span class="text-accent" bind:this={resultsTextEl}
            >{displayQuery}</span
          >
        {/key}
      </div>

      <!-- Type filters + sort -->
      <div class="flex flex-wrap items-center gap-2">
        <ToggleGroup.Root
          type="multiple"
          variant="outline"
          size="sm"
          bind:value={selectedTypes}
        >
          {#each typeOptions as opt (opt.key)}
            <ToggleGroup.Item value={opt.key} aria-label={opt.label}>
              {opt.label}
            </ToggleGroup.Item>
          {/each}
        </ToggleGroup.Root>

        <div class="ml-auto flex items-center gap-2">
          <span class="text-xs text-muted-foreground">Sort titles by</span>
          <Select.Root type="single" bind:value={sortKey}>
            <Select.Trigger size="sm" class="w-45 text-xs">
              {sortLabel}
            </Select.Trigger>
            <Select.Content>
              {#each sortOptions as opt (opt.value)}
                <Select.Item value={opt.value} label={opt.label}>
                  {opt.label}
                </Select.Item>
              {/each}
            </Select.Content>
          </Select.Root>
        </div>
      </div>

      {#if !loading && keywords.length > 1}
        <div class="flex flex-col gap-2">
          <span class="text-xs font-medium text-muted-foreground">
            More to Explore:
          </span>
          <ScrollArea orientation="horizontal" class="overflow-clip rounded-sm">
            <div class="flex gap-2 pb-2">
              {#each keywords as kw (kw.id)}
                <Button
                  variant="ghost"
                  size="xs"
                  class="text-muted-foreground"
                  onclick={() => onSuggested(kw.name)}
                >
                  {kw.name}
                </Button>
              {/each}
            </div>
          </ScrollArea>
        </div>
      {/if}
    </div>
  {/if}

  {#if !loading}
    <ScrollArea class="flex min-h-0 flex-1 gap-4 p-4">
      {#if showPerson && people.length > 0}
        <section class="mb-8 space-y-3">
          <h2 class="text-lg font-semibold">People</h2>
          <div
            class="grid gap-4"
            style="grid-template-columns: repeat(auto-fill, minmax(120px, 1fr))"
          >
            {#each people as person (person.id)}
              <PersonCard {person} onclick={(p) => onSelectPerson(p)} />
            {/each}
          </div>
        </section>
      {/if}

      {#if showProvider && providers.length > 0}
        <section class="space-y-3 p-4">
          <h2 class="text-lg font-semibold">Providers</h2>
          <div
            class="grid gap-4"
            style="grid-template-columns: repeat(auto-fill, minmax(110px, 1fr))"
          >
            {#each providers as provider (provider.provider_id)}
              <ProviderCard {provider} onclick={(p) => onSelectProvider(p)} />
            {/each}
          </div>
        </section>
      {/if}

      <div class="space-y-8 pr-4 pb-8">
        {#if showMovie && movies.length > 0}
          <section class="space-y-3">
            <h2 class="text-lg font-semibold">Movies</h2>
            <div
              class="grid gap-4"
              style="grid-template-columns: repeat(auto-fill, minmax(150px, 1fr))"
            >
              {#each movies as media (media.id)}
                <MediaCard
                  {media}
                  onclick={() => onSelectMedia(media)}
                  quality={qualityMap.get(media.id) ?? null}
                  onwatch={onWatch}
                />
              {/each}
            </div>
          </section>
        {/if}

        {#if showTV && tv.length > 0}
          <section class="space-y-3">
            <h2 class="text-lg font-semibold">TV Shows</h2>
            <div
              class="grid gap-4"
              style="grid-template-columns: repeat(auto-fill, minmax(150px, 1fr))"
            >
              {#each tv as media (media.id)}
                <MediaCard
                  {media}
                  onclick={() => onSelectMedia(media)}
                  quality={qualityMap.get(media.id) ?? null}
                  onwatch={onWatch}
                />
              {/each}
            </div>
          </section>
        {/if}

        {#if query.trim() && !anyVisible}
          <p class="pt-8 text-center text-sm text-muted-foreground">
            No results to show.
          </p>
        {/if}
      </div>
    </ScrollArea>
  {/if}
</div>
