<script lang="ts">
  import {
    api,
    type LibraryStats,
    type DiscoverInsights,
    type Taste,
  } from "$lib/api";
  import * as Card from "$lib/components/ui/card/index.js";
  import { Badge } from "$lib/components/ui/badge/index.js";
  import {
    Film,
    Tv,
    ThumbsDown,
    Sparkles,
    Tag,
    Info,
    Activity,
  } from "lucide-svelte";

  let stats = $state<LibraryStats | null>(null);
  let insights = $state<DiscoverInsights | null>(null);
  let loading = $state(true);
  let loadError = $state<string | null>(null);

  $effect(() => {
    Promise.all([api.libraryStats(), api.discoverInsights()])
      .then(([s, i]) => {
        stats = s;
        insights = i;
      })
      .catch((e) => (loadError = e instanceof Error ? e.message : String(e)))
      .finally(() => (loading = false));
  });

  let hasProfile = $derived((insights?.signals_used ?? 0) > 0);

  // ── Chart plumbing ────────────────────────────────────────────────────────
  type Slice = { label: string; value: number; color: string; count?: number };

  // Fixed palette keeps the donuts readable on light and dark. Swap any entry
  // for `var(--chart-1)` etc. if you'd rather pull from your theme tokens.
  const palette = [
    "#6366f1", // indigo
    "#ec4899", // pink
    "#22c55e", // green
    "#f59e0b", // amber
    "#06b6d4", // cyan
    "#94a3b8", // slate (used for "Other")
  ];

  function conic(slices: Slice[]): string {
    const total = slices.reduce((s, x) => s + x.value, 0) || 1;
    let acc = 0;
    const stops = slices.map((s) => {
      const start = (acc / total) * 100;
      acc += s.value;
      const end = (acc / total) * 100;
      return `${s.color} ${start}% ${end}%`;
    });
    return `conic-gradient(${stops.join(", ")})`;
  }

  function genreSlices(list: Taste[]): Slice[] {
    const top = list.slice(0, 5).map((g, i) => ({
      label: g.name,
      value: Math.abs(g.score),
      color: palette[i],
    }));
    const rest = list.slice(5);
    if (rest.length > 0) {
      top.push({
        label: "Other",
        value: rest.reduce((a, g) => a + Math.abs(g.score), 0),
        color: palette[5],
      });
    }
    return top;
  }

  function mvSlices(s: LibraryStats): Slice[] {
    return [
      {
        label: "Movies",
        value: s.movie_share,
        color: palette[0],
        count: s.by_type.movie ?? 0,
      },
      {
        label: "TV",
        value: s.tv_share,
        color: palette[1],
        count: s.by_type.tv ?? 0,
      },
    ];
  }

  const statusMeta = [
    { key: "watching", label: "Watching" },
    { key: "finished", label: "Finished" },
    { key: "watch_later", label: "Watch later" },
    { key: "dropped", label: "Dropped" },
  ];

  function statusSlices(s: LibraryStats): Slice[] {
    return statusMeta
      .map((m, i) => ({
        label: m.label,
        value: s.by_status[m.key] ?? 0,
        color: palette[i],
        count: s.by_status[m.key] ?? 0,
      }))
      .filter((x) => x.value > 0);
  }

  // Mirror of the backend signalWeight model.
  const weights = [
    { label: "Finished a title", value: "+1.5" },
    { label: "Watched to the end", value: "+1.0" },
    { label: "Currently watching", value: "+0.5" },
    { label: "Saved to watch later", value: "+0.5" },
    { label: "Each ★ above / below 3", value: "±1.5" },
    { label: "Dropped", value: "−2.0" },
    { label: "Not interested", value: "−2.0" },
  ];
</script>

{#snippet stat(value: string, label: string)}
  <Card.Root>
    <Card.Content class="p-4">
      <div class="text-2xl font-semibold">{value}</div>
      <div class="text-xs text-muted-foreground">{label}</div>
    </Card.Content>
  </Card.Root>
{/snippet}

{#snippet donut(slices: Slice[])}
  {#if slices.length === 0}
    <p class="text-xs text-muted-foreground">Not enough signal yet.</p>
  {:else}
    {@const total = slices.reduce((s, x) => s + x.value, 0) || 1}
    <div class="flex items-center gap-5">
      <div
        class="relative size-28 shrink-0 rounded-full"
        style="background: {conic(slices)};"
      >
        <div class="absolute inset-[24%] rounded-full bg-card"></div>
      </div>
      <ul class="flex min-w-0 flex-1 flex-col gap-1.5 text-sm">
        {#each slices as s (s.label)}
          <li class="flex items-center gap-2">
            <span
              class="size-2.5 shrink-0 rounded-full"
              style="background: {s.color};"
            ></span>
            <span class="truncate">{s.label}</span>
            <span class="ml-auto shrink-0 text-muted-foreground">
              {Math.round((s.value / total) * 100)}%{s.count != null
              ? ` · ${s.count}`
              : ""}
            </span>
          </li>
        {/each}
      </ul>
    </div>
  {/if}
{/snippet}

{#snippet chartCard(
  title: string,
  Icon: typeof Film,
  slices: Slice[],
  description?: string,
)}
  <Card.Root>
    <Card.Header>
      <Card.Title class="flex items-center gap-2 text-sm">
        <Icon class="size-4" />
        {title}
      </Card.Title>
      {#if description}
        <Card.Description>{description}</Card.Description>
      {/if}
    </Card.Header>
    <Card.Content>
      {@render donut(slices)}
    </Card.Content>
  </Card.Root>
{/snippet}

<div class="mx-auto mt-24 flex w-full max-w-4xl flex-col gap-6 p-6">
  <header class="flex flex-col gap-1">
    <h1 class="text-2xl font-semibold">Your taste profile</h1>
    <p class="text-sm text-muted-foreground">All that makes you unique.</p>
  </header>

  {#if loading}
    <div class="grid gap-4 sm:grid-cols-2">
      {#each Array(4) as _, i (i)}
        <div class="h-40 animate-pulse rounded-xl border bg-muted/40"></div>
      {/each}
    </div>
  {:else if loadError}
    <Card.Root class="border-destructive/40 bg-destructive/10">
      <Card.Content class="p-4 text-sm">
        Couldn't load your profile: {loadError}
      </Card.Content>
    </Card.Root>
  {:else if !hasProfile}
    <Card.Root>
      <Card.Content class="flex flex-col items-center gap-2 p-10 text-center">
        <Sparkles class="size-6 text-muted-foreground" />
        <p class="font-medium">Nothing to analyze yet</p>
        <p class="max-w-sm text-sm text-muted-foreground">
          Finish, rate, or drop a few titles and your taste profile will start
          to take shape here.
        </p>
      </Card.Content>
    </Card.Root>
  {:else if stats && insights}
    <!-- ── Library at a glance ─────────────────────────────────────────────── -->
    <section class="grid grid-cols-2 gap-3 sm:grid-cols-4">
      {@render stat(String(stats.total), "in library")}
      {@render stat(String(stats.by_status.finished ?? 0), "finished")}
      {@render stat(
        stats.rated ? stats.avg_rating.toFixed(1) : "—",
        `avg rating (${stats.rated} rated)`,
      )}
      {@render stat(String(stats.dismissed), "not interested")}
    </section>

    <!-- ── Composition donuts ──────────────────────────────────────────────── -->
    <section class="grid gap-4 md:grid-cols-2">
      {@render chartCard(
        "What You Enjoy Most",
        Film,
        mvSlices(stats),
        "Share of what you've finished or are watching.",
      )}
      {@render chartCard(
        "Watch activity",
        Activity,
        statusSlices(stats),
        "Where your titles sit",
      )}
    </section>

    <!-- ── Top genres (per type) ────────────────────────────────────────────── -->
    <section class="grid gap-4 md:grid-cols-2">
      {@render chartCard(
        "Top Movie Genres",
        Film,
        genreSlices(insights.top_movie_genres),
      )}
      {@render chartCard(
        "Top TV Genres",
        Tv,
        genreSlices(insights.top_tv_genres),
      )}
    </section>

    <!-- ── Keywords + what it avoids ────────────────────────────────────────── -->
    <section class="grid gap-4 md:grid-cols-2">
      <Card.Root>
        <Card.Header>
          <Card.Title class="flex items-center gap-2 text-sm">
            <Tag class="size-4" />
            Themes you gravitate to
          </Card.Title>
        </Card.Header>
        <Card.Content class="flex flex-wrap gap-1.5">
          {#if insights.top_keywords.length === 0}
            <p class="text-xs text-muted-foreground">Not enough signal yet.</p>
          {:else}
            {#each insights.top_keywords as k (k.id)}
              <Badge variant="secondary">{k.name}</Badge>
            {/each}
          {/if}
        </Card.Content>
      </Card.Root>

      <Card.Root>
        <Card.Header>
          <Card.Title class="flex items-center gap-2 text-sm">
            <ThumbsDown class="size-4" />
            Steered away from
          </Card.Title>
        </Card.Header>
        <Card.Content class="flex flex-wrap gap-1.5">
          {#if insights.disliked_genres.length === 0}
            <p class="text-xs text-muted-foreground">
              Nothing strongly disliked yet.
            </p>
          {:else}
            {#each insights.disliked_genres as g (g.id)}
              <Badge variant="destructive">{g.name}</Badge>
            {/each}
          {/if}
        </Card.Content>
      </Card.Root>
    </section>

    <Card.Root class="bg-muted/20">
      <Card.Header>
        <Card.Title class="flex items-center gap-2 text-sm">
          <Info class="size-4" />
          How your recommendations are built
        </Card.Title>
      </Card.Header>
      <Card.Content class="flex flex-col gap-4 text-sm text-muted-foreground">
        <p>
          Your profile is built from
          <span class="font-medium text-foreground"
          >{insights.signals_used}</span
          >
          titles you've actively engaged with. Each becomes a like/dislike weight,
          which is spread across that title's genres and keywords:
        </p>

        <div class="grid grid-cols-2 gap-x-6 gap-y-1.5 sm:grid-cols-3">
          {#each weights as wgt (wgt.label)}
            <div class="flex items-center justify-between gap-2">
              <span>{wgt.label}</span>
              <span class="font-mono text-xs text-foreground">{wgt.value}</span>
            </div>
          {/each}
        </div>
      </Card.Content>
    </Card.Root>
  {/if}
</div>
