<script lang="ts">
  import LargeRecommendationsCard from "./LargeRecommendationsCard.svelte";
  import SmallRecommendations from "./SmallRecommendations.svelte";
  import ContinueWatching from "./ContinueWatching.svelte";
  import type { Media } from "$lib/types/tmdb";
  import { api, type DiscoverInsights, type LibraryStats } from "$lib/api";
  import { ScrollArea } from "$lib/components/ui/scroll-area/index.js";
  import { onMount } from "svelte";

  // Same contract as MyListPage: parent hands down how to open a title and
  // (optionally) how to start watching it. We forward both into every row.
  let {
    onSelectMedia,
    onWatch,
  }: {
    onSelectMedia: (m: Media) => void;
    onWatch?: (m: Media, season?: number, episode?: number) => void;
  } = $props();

  // Rows aren't hardcoded. Beyond the blended "Based on your tastes" feed, the
  // page builds one row per top genre / theme the user actually engages with
  // (from /discover/insights), so it reshapes itself per profile.
  type Row = { key: string; header: string; medias: Media[]; loading: boolean };
  type RowSpec = { key: string; header: string; load: () => Promise<Media[]> };

  let rows = $state<Row[]>([]);

  const PER_BUCKET = 2; // rows drawn from each source: movie genres / tv genres / keywords
  const ROW_LIMIT = 20; // titles fetched per row

  const cap = (s: string) => (s ? s[0].toUpperCase() + s.slice(1) : s);

  function patchRow(key: string, patch: Partial<Row>): void {
    const i = rows.findIndex((r) => r.key === key);
    if (i !== -1) rows[i] = { ...rows[i], ...patch };
  }

  // Append a skeleton row immediately, then fill it when its fetch resolves.
  // Each row loads independently, so a slow genre never blocks the others, and
  // an empty result just hides itself (SmallRecommendations handles that).
  function startRow(spec: RowSpec): void {
    rows = [...rows, { key: spec.key, header: spec.header, medias: [], loading: true }];
    spec
      .load()
      .then((d) => patchRow(spec.key, { medias: d }))
      .catch(() => patchRow(spec.key, { medias: [] }))
      .finally(() => patchRow(spec.key, { loading: false }));
  }

  // Turn the taste profile into row specs. Genre IDs are namespaced per media
  // type on TMDB, so movie/tv genres drive separate rows; keywords are shared
  // across types, so we aim them at whichever type the user watches more.
  function tasteSpecs(
    insights: DiscoverInsights,
    primaryType: "movie" | "tv",
  ): RowSpec[] {
    const mg = insights.top_movie_genres.slice(0, PER_BUCKET);
    const tg = insights.top_tv_genres.slice(0, PER_BUCKET);
    const kw = insights.top_keywords.slice(0, PER_BUCKET);

    const specs: RowSpec[] = [];
    // Interleave so it's not all movies, then all shows, then all themes.
    for (let i = 0; i < Math.max(mg.length, tg.length, kw.length); i++) {
      const movieGenre = mg[i];
      if (movieGenre)
        specs.push({
          key: `mg-${movieGenre.id}`,
          header: `${movieGenre.name} movies`,
          load: () =>
            api.discoverByGenre("movie", movieGenre.id, { limit: ROW_LIMIT }),
        });

      const tvGenre = tg[i];
      if (tvGenre)
        specs.push({
          key: `tg-${tvGenre.id}`,
          header: `${tvGenre.name} shows`,
          load: () =>
            api.discoverByGenre("tv", tvGenre.id, { limit: ROW_LIMIT }),
        });

      const keyword = kw[i];
      if (keyword)
        specs.push({
          key: `kw-${keyword.id}`,
          header: cap(keyword.name),
          load: () =>
            api.discoverByKeyword(primaryType, keyword.id, { limit: ROW_LIMIT }),
        });
    }
    return specs;
  }

  onMount(() => {
    // Blended personalized row first — always present, even for a brand-new
    // library (the backend falls back to popular titles when signal is thin).
    startRow({
      key: "tastes",
      header: "Based on your tastes",
      load: () => api.discover("all", { limit: ROW_LIMIT }),
    });

    // Then the profile-driven rows. One insights call yields every genre/keyword
    // we need; stats just tells us which type to aim theme rows at.
    Promise.all([
      api.discoverInsights().catch(() => null),
      api.libraryStats().catch(() => null),
    ]).then(
      ([insights, stats]: [DiscoverInsights | null, LibraryStats | null]) => {
        if (!insights || insights.signals_used === 0) return; // not enough signal yet
        const primaryType =
          stats && stats.tv_share > stats.movie_share ? "tv" : "movie";
        for (const spec of tasteSpecs(insights, primaryType)) startRow(spec);
      },
    );
  });
</script>

<ScrollArea class="mb-24 h-full w-full">
  <div class="flex w-full flex-col justify-start gap-2 pb-8">
    <LargeRecommendationsCard />

    <ContinueWatching {onWatch} {onSelectMedia} />

    {#each rows as row (row.key)}
      <SmallRecommendations
        header={row.header}
        medias={row.medias}
        loading={row.loading}
        onSelect={onSelectMedia}
        {onWatch}
      />
    {/each}
  </div>
</ScrollArea>
