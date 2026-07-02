<script lang="ts">
  import { onMount } from "svelte";
  import { CheckCircle, Check, Loader2 } from "lucide-svelte";
  import { Button } from "$lib/components/ui/button/index.js";
  import * as Select from "$lib/components/ui/select/index.js";
  import { Separator } from "$lib/components/ui/separator/index.js";
  import { Switch } from "$lib/components/ui/switch/index.js";
  import { Input } from "$lib/components/ui/input/index.js";
  import AuthDialog from "./AuthDialog.svelte";
  import StarRating from "./StarRating.svelte";
  import { settings } from "$lib/stores/settings";
  import { auth } from "$lib/stores/auth.svelte";
  import { api } from "$lib/api";
  import type { Media } from "$lib/types/tmdb";
  import type { LibraryEntry } from "$lib/types/library";
  import CoveIcon from "../assets/CoveIcon.svelte";
  import {SvelteSet} from "svelte/reactivity";

  let { onclose }: { onclose: () => void } = $props();

  type StepMeta = { id: string; title: string; skippable: boolean };
  const STEPS: StepMeta[] = [
    { id: "welcome",     title: "Welcome to Cove",         skippable: false },
    { id: "account",     title: "Your Account",             skippable: false  },
    { id: "genres",      title: "Your Taste",               skippable: false  },
    { id: "seen",        title: "What Have You Seen?",      skippable: true  },
    { id: "rate",        title: "Rate What You've Seen",    skippable: true  },
    { id: "preferences", title: "Playback Preferences",     skippable: true  },
    { id: "done",        title: "You're All Set!",          skippable: false },
  ];

  const LANGUAGES = [
    { value: "en", label: "English" },
    { value: "es", label: "Spanish" },
    { value: "fr", label: "French" },
    { value: "de", label: "German" },
    { value: "pt", label: "Portuguese" },
    { value: "it", label: "Italian" },
    { value: "ja", label: "Japanese" },
    { value: "ko", label: "Korean" },
    { value: "zh", label: "Chinese" },
    { value: "ar", label: "Arabic" },
    { value: "ru", label: "Russian" },
  ];

  function langLabel(v: string): string {
    return LANGUAGES.find((l) => l.value === v)?.label ?? v;
  }

  let selectedMovieGenreIds = new SvelteSet<number>();
  let selectedTvGenreIds    = new SvelteSet<number>();

  // ── Navigation ────────────────────────────────────────────────────────────────
  let stepIndex = $state(0);
  const step = $derived(STEPS[stepIndex]);
  const isFirst = $derived(stepIndex === 0);
  const isLast = $derived(stepIndex === STEPS.length - 1);
  const canProceed = $derived(
    stepIndex === 2
      ? selectedMovieGenreIds.size > 0 || selectedTvGenreIds.size > 0
      : true,
  );

  // ── Account step ──────────────────────────────────────────────────────────────
  let authOpen = $state(false);

  // ── Genre step ────────────────────────────────────────────────────────────────
  let movieGenres = $state<{ id: number; name: string }[]>([]);
  let tvGenres    = $state<{ id: number; name: string }[]>([]);


  let genreQuery = $state("");
  let loadingGenres = $state(false);

  const filteredMovieGenres = $derived(
    genreQuery.trim()
      ? movieGenres.filter((g) => g.name.toLowerCase().includes(genreQuery.toLowerCase()))
      : movieGenres,
  );
  const filteredTvGenres = $derived(
    genreQuery.trim()
      ? tvGenres.filter((g) => g.name.toLowerCase().includes(genreQuery.toLowerCase()))
      : tvGenres,
  );

  function toggleMovieGenre(id: number): void {
    if (selectedMovieGenreIds.has(id)) selectedMovieGenreIds.delete(id);
    else selectedMovieGenreIds.add(id);
  }
  function toggleTvGenre(id: number): void {
    if (selectedTvGenreIds.has(id)) selectedTvGenreIds.delete(id);
    else selectedTvGenreIds.add(id);
  }

  // ── Seen step ─────────────────────────────────────────────────────────────────
  let browseMedia   = $state<Media[]>([]);
  let mediaQuery    = $state("");
  let searchResults = $state<Media[]>([]);
  let loadingMedia  = $state(false);
  let seenMedia     = $state<Media[]>([]);

  const seenIds = $derived(new Set(seenMedia.map((m) => `${m.media_type}-${m.id}`)));
  const displayMedia = $derived(mediaQuery.trim() ? searchResults : browseMedia);

  function toggleSeenMedia(m: Media): void {
    const key = `${m.media_type}-${m.id}`;
    if (seenIds.has(key)) {
      seenMedia = seenMedia.filter((x) => `${x.media_type}-${x.id}` !== key);
    } else {
      seenMedia = [...seenMedia, m];
    }
  }

  let searchTimer: ReturnType<typeof setTimeout> | null = null;
  function onMediaQueryChange(q: string): void {
    mediaQuery = q;
    if (searchTimer) clearTimeout(searchTimer);
    if (!q.trim()) { searchResults = []; return; }
    searchTimer = setTimeout(async () => {
      searchResults = await api.search(q).catch(() => []);
    }, 350);
  }

  async function loadBrowseMedia(): Promise<void> {
    loadingMedia = true;
    try {
      const movieIds = [...selectedMovieGenreIds];
      const tvIds    = [...selectedTvGenreIds];
      if (movieIds.length === 0 && tvIds.length === 0) {
        const [movies, tv] = await Promise.all([
          api.discover("movie", { limit: 15 }),
          api.discover("tv",    { limit: 15 }),
        ]);
        browseMedia = [...movies, ...tv];
      } else {
        const results = await Promise.all([
          ...movieIds.map((id) => api.discoverByGenre("movie", id, { limit: 12 })),
          ...tvIds.map((id)    => api.discoverByGenre("tv",    id, { limit: 12 })),
        ]);
        const seen = new SvelteSet<string>();
        browseMedia = results.flat().filter((m) => {
          const k = `${m.media_type}-${m.id}`;
          if (seen.has(k)) return false;
          seen.add(k);
          return true;
        });
      }
    } catch {
      browseMedia = [];
    }
    loadingMedia = false;
  }

  // ── Rate step ─────────────────────────────────────────────────────────────────
  let seenEntries      = $state<(LibraryEntry | null)[]>([]);
  let preparingEntries = $state(false);

  // ── Preferences step ─────────────────────────────────────────────────────────
  let subtitleLang     = $state($settings?.defaultSubtitleLang ?? "en");
  let audioLang        = $state($settings?.defaultAudioLang    ?? "en");
  let autoPlay         = $state($settings?.autoPlay            ?? false);
  let rememberPosition = $state($settings?.rememberPosition    ?? true);
  let autoSkipIntro    = $state($settings?.autoSkipIntro       ?? false);
  let autoSkipRecap    = $state($settings?.autoSkipRecap       ?? false);
  let autoSkipCredits  = $state($settings?.autoSkipCredits     ?? false);
  let autoSkipPreview  = $state($settings?.autoSkipPreview     ?? false);

  // ── Navigation handlers ───────────────────────────────────────────────────────
  async function next(): Promise<void> {
    if (stepIndex === 2) {
      await loadBrowseMedia();
    }
    if (stepIndex === 3) {
      if (seenMedia.length === 0) {
        stepIndex = 5;
        return;
      }
      preparingEntries = true;
      seenEntries = await Promise.all(
        seenMedia.map((m) =>
          api
            .libraryUpsert({
              tmdb_id: m.id,
              media_type: m.media_type,
              title: m.media_type === "movie" ? m.title : m.name,
              poster_path: m.poster_path,
              vote_average: m.vote_average,
              status: "finished",
            })
            .catch(() => null),
        ),
      );
      preparingEntries = false;
    }
    if (stepIndex === 5) {
      settings.save({
        defaultSubtitleLang: subtitleLang,
        defaultAudioLang: audioLang,
        autoPlay,
        rememberPosition,
        autoSkipIntro,
        autoSkipRecap,
        autoSkipCredits,
        autoSkipPreview,
      });
    }
    if (isLast) {
      settings.save({ onboardingDone: true });
      onclose();
    } else {
      stepIndex += 1;
    }
  }

  function back(): void {
    if (stepIndex === 5 && seenMedia.length === 0) {
      // Skipped rating step on the way in — go back to seen step
      stepIndex = 3;
    } else {
      stepIndex -= 1;
    }
  }

  function skip(): void {
    // Skipping "Seen" with nothing selected also skips "Rate" since it'd be empty
    if (stepIndex === 3 && seenMedia.length === 0) {
      stepIndex = 5;
    } else {
      stepIndex += 1;
    }
  }

  let nextLoading = $state(false);
  async function handleNext(): Promise<void> {
    nextLoading = true;
    await next();
    nextLoading = false;
  }

  // ── Init ──────────────────────────────────────────────────────────────────────
  onMount(async () => {
    loadingGenres = true;
    [movieGenres, tvGenres] = await Promise.all([
      api.genreList("movie").catch(() => []),
      api.genreList("tv").catch(() => []),
    ]);
    loadingGenres = false;
  });
</script>

<div class="flex w-[90%]! h-[50%]! flex-col rounded-2xl border border-border bg-card shadow-2xl">
  <!-- Progress pills -->
  <div class="flex items-center justify-center gap-2 px-8 pt-8">
    {#each STEPS as _, i}
      <div
        class="h-1.5 rounded-full transition-all duration-300 {i === stepIndex
          ? 'w-6 bg-accent'
          : i < stepIndex
            ? 'w-3 bg-accent/40'
            : 'w-3 bg-muted'}"
      ></div>
    {/each}
  </div>

  <!-- Step content -->
  <div class="flex min-h-10/12 flex-col px-8 py-6">
    {#if stepIndex === 0}
      <!-- Welcome -->
      <div class="flex flex-1 flex-col items-center justify-center gap-4 text-center">
        <div class="flex items-center justify-center rounded-2xl">
          <CoveIcon size={96} />
        </div>
        <h1 class="text-3xl font-bold tracking-tight">Welcome to Cove</h1>
        <p class="max-w-sm text-base text-muted-foreground">
          Your personal media streaming hub. Let's get you set up in a few quick steps.
        </p>
      </div>

    {:else if stepIndex === 1}
      <!-- Account -->
      <h2 class="mb-4 text-xl font-semibold">{step.title}</h2>
      {#if auth.isGuest}
        <p class="text-sm text-muted-foreground">
          You're browsing as a guest. You can optionally sign in to sync your library and preferences across devices.
        </p>
        <Button onclick={() => (authOpen = true)} class="mt-4 w-full">
          Sign in / Create account
        </Button>
        <p class="mt-3 text-center text-xs text-muted-foreground">
          You can always sign in later from the account menu.
        </p>
      {:else}
        <div class="flex items-center gap-3 rounded-lg border border-border p-4">
          <div
            class="flex size-10 shrink-0 items-center justify-center rounded-full bg-accent font-semibold text-accent-foreground"
          >
            {auth.activeProfile?.name?.charAt(0).toUpperCase() ?? "?"}
          </div>
          <div class="min-w-0 flex-1">
            <p class="truncate text-sm font-medium">{auth.activeProfile?.name ?? "Profile"}</p>
            <p class="truncate text-xs text-muted-foreground">{auth.session?.email}</p>
          </div>
          <CheckCircle class="size-5 shrink-0 text-green-500" />
        </div>
        <p class="mt-3 text-sm text-muted-foreground">
          Your account is connected. Your library syncs automatically.
        </p>
      {/if}

    {:else if stepIndex === 2}
      <!-- Genres -->
      <h2 class="mb-1 text-xl font-semibold">{step.title}</h2>
      <p class="mb-4 text-sm text-muted-foreground">Pick genres you enjoy watching.</p>
      <Input
        placeholder="Filter genres..."
        bind:value={genreQuery}
        class="mb-4"
      />
      <div class="overflow-y-auto" style="max-height: 18rem;">
        {#if loadingGenres}
          <div class="flex items-center justify-center py-8">
            <Loader2 class="size-6 animate-spin text-muted-foreground" />
          </div>
        {:else}
          {#if filteredMovieGenres.length > 0}
            <p class="mb-2 text-xs font-medium uppercase tracking-wider text-muted-foreground">Movies</p>
            <div class="mb-4 flex flex-wrap gap-2">
              {#each filteredMovieGenres as g (g.id)}
                <button
                  onclick={() => toggleMovieGenre(g.id)}
                  class="rounded-full border px-3 py-1 text-sm transition-colors
                    {selectedMovieGenreIds.has(g.id)
                      ? 'border-accent bg-accent text-accent-foreground'
                      : 'border-border hover:border-accent/60 hover:text-foreground'}"
                >
                  {g.name}
                </button>
              {/each}
            </div>
          {/if}
          {#if filteredTvGenres.length > 0}
            <p class="mb-2 text-xs font-medium uppercase tracking-wider text-muted-foreground">TV Shows</p>
            <div class="flex flex-wrap gap-2">
              {#each filteredTvGenres as g (g.id)}
                <button
                  onclick={() => toggleTvGenre(g.id)}
                  class="rounded-full border px-3 py-1 text-sm transition-colors
                    {selectedTvGenreIds.has(g.id)
                      ? 'border-accent bg-accent text-accent-foreground'
                      : 'border-border hover:border-accent/60 hover:text-foreground'}"
                >
                  {g.name}
                </button>
              {/each}
            </div>
          {/if}
        {/if}
      </div>

    {:else if stepIndex === 3}
      <!-- Seen Before -->
      <div class="flex items-center justify-between">
        <h2 class="text-xl font-semibold">{step.title}</h2>
        {#if seenMedia.length > 0}
          <span class="rounded-full bg-accent/20 px-2 py-0.5 text-xs font-medium text-accent">
            {seenMedia.length} selected
          </span>
        {/if}
      </div>
      <p class="mb-4 mt-1 text-sm text-muted-foreground">Select movies and shows you've already watched.</p>
      <Input
        placeholder="Search for a movie or show..."
        value={mediaQuery}
        oninput={(e) => onMediaQueryChange(e.currentTarget.value)}
        class="mb-3"
      />
      <div class="overflow-y-auto max-h-full">
        {#if loadingMedia}
          <div class="flex items-center justify-center py-8">
            <Loader2 class="size-6 animate-spin text-muted-foreground" />
          </div>
        {:else if displayMedia.length === 0}
          <p class="py-6 text-center text-sm text-muted-foreground">
            {mediaQuery.trim() ? "No results found." : "No media to show — try selecting some genres first."}
          </p>
        {:else}
          <div class="grid grid-cols-4 gap-2">
            {#each displayMedia as m (`${m.media_type}-${m.id}`)}
              {@const title = m.media_type === "movie" ? m.title : m.name}
              {@const selected = seenIds.has(`${m.media_type}-${m.id}`)}
              <button
                onclick={() => toggleSeenMedia(m)}
                title={title}
                class="group relative overflow-hidden rounded-lg border-2 transition-colors
                  {selected ? 'border-accent' : 'border-transparent'}"
              >
                {#if m.poster_path}
                  <img
                    src={m.poster_path}
                    alt={title}
                    loading="lazy"
                    decoding="async"
                    class="aspect-2/3 w-full object-cover"
                  />
                {:else}
                  <div class="flex aspect-2/3 w-full items-center justify-center bg-muted text-xs text-muted-foreground">
                    {title}
                  </div>
                {/if}
                {#if selected}
                  <div class="absolute inset-0 flex items-center justify-center bg-accent/30">
                    <Check class="size-6 text-white drop-shadow" />
                  </div>
                {/if}
              </button>
            {/each}
          </div>
        {/if}
      </div>

    {:else if stepIndex === 4}
      <!-- Rate Them -->
      <h2 class="mb-1 text-xl font-semibold">{step.title}</h2>
      <p class="mb-4 text-sm text-muted-foreground">
        Rate the ones you remember — this helps personalize your recommendations.
      </p>
      {#if preparingEntries}
        <div class="flex flex-1 items-center justify-center">
          <Loader2 class="size-6 animate-spin text-muted-foreground" />
        </div>
      {:else}
        <div class="overflow-y-auto overflow-x-clip max-h-full">
          <div class="flex flex-col gap-3">
            {#each seenMedia as m, i (`${m.media_type}-${m.id}`)}
              {@const title = m.media_type === "movie" ? m.title : m.name}
              <div class="flex items-center gap-3">
                {#if m.poster_path}
                  <img
                    src={m.poster_path}
                    alt={title}
                    loading="lazy"
                    decoding="async"
                    class="h-12 w-8 shrink-0 rounded object-cover"
                  />
                {:else}
                  <div class="h-12 w-8 shrink-0 rounded bg-muted"></div>
                {/if}
                <p class="min-w-0 flex-1 truncate text-sm font-medium">{title}</p>
                <StarRating libraryEntry={seenEntries[i] ?? null} media={m} />
              </div>
            {/each}
          </div>
        </div>
      {/if}

    {:else if stepIndex === 5}
      <!-- Preferences -->
      <h2 class="mb-4 text-xl font-semibold">{step.title}</h2>
      <div class="overflow-y-auto overflow-x-clip max-h-full">
        <!-- Language selectors -->
        <div class="flex items-center justify-between py-2">
          <div>
            <p class="text-sm font-medium">Subtitle language</p>
            <p class="text-xs text-muted-foreground">Auto-selected when subtitles are available</p>
          </div>
          <Select.Root type="single" bind:value={subtitleLang}>
            <Select.Trigger class="w-36">{langLabel(subtitleLang)}</Select.Trigger>
            <Select.Content>
              {#each LANGUAGES as l}
                <Select.Item value={l.value}>{l.label}</Select.Item>
              {/each}
            </Select.Content>
          </Select.Root>
        </div>
        <div class="flex items-center justify-between py-2">
          <div>
            <p class="text-sm font-medium">Audio language</p>
            <p class="text-xs text-muted-foreground">Auto-selected when multiple tracks are available</p>
          </div>
          <Select.Root type="single" bind:value={audioLang}>
            <Select.Trigger class="w-36">{langLabel(audioLang)}</Select.Trigger>
            <Select.Content>
              {#each LANGUAGES as l}
                <Select.Item value={l.value}>{l.label}</Select.Item>
              {/each}
            </Select.Content>
          </Select.Root>
        </div>

        <Separator class="my-3" />

        <!-- Playback toggles -->
        <div class="flex items-center justify-between py-2">
          <div>
            <p class="text-sm font-medium">Auto-play next episode</p>
            <p class="text-xs text-muted-foreground">Automatically start the next episode</p>
          </div>
          <Switch checked={autoPlay} onCheckedChange={(v) => (autoPlay = v)} />
        </div>
        <div class="flex items-center justify-between py-2">
          <div>
            <p class="text-sm font-medium">Remember playback position</p>
            <p class="text-xs text-muted-foreground">Resume from where you left off</p>
          </div>
          <Switch checked={rememberPosition} onCheckedChange={(v) => (rememberPosition = v)} />
        </div>

        <Separator class="my-3" />

        <div class="flex items-center justify-between py-2">
          <div>
            <p class="text-sm font-medium">Auto-skip intros</p>
          </div>
          <Switch checked={autoSkipIntro} onCheckedChange={(v) => (autoSkipIntro = v)} />
        </div>
        <div class="flex items-center justify-between py-2">
          <div>
            <p class="text-sm font-medium">Auto-skip recaps</p>
          </div>
          <Switch checked={autoSkipRecap} onCheckedChange={(v) => (autoSkipRecap = v)} />
        </div>
        <div class="flex items-center justify-between py-2">
          <div>
            <p class="text-sm font-medium">Auto-skip credits</p>
          </div>
          <Switch checked={autoSkipCredits} onCheckedChange={(v) => (autoSkipCredits = v)} />
        </div>
        <div class="flex items-center justify-between py-2">
          <div>
            <p class="text-sm font-medium">Auto-skip previews</p>
          </div>
          <Switch checked={autoSkipPreview} onCheckedChange={(v) => (autoSkipPreview = v)} />
        </div>
      </div>

    {:else if stepIndex === 6}
      <!-- Done -->
      <div class="flex flex-1 flex-col items-center justify-center gap-4 text-center">
        <div class="flex size-16 items-center justify-center rounded-full bg-accent/20">
          <CheckCircle class="size-8 text-accent" />
        </div>
        <h2 class="text-2xl font-semibold">You're all set!</h2>
        <p class="max-w-sm text-sm text-muted-foreground">
          Cove is ready to use. Add streaming addons from Settings to start watching.
        </p>
      </div>
    {/if}
  </div>

  <!-- Navigation bar -->
  <div class="flex items-center justify-between border-t border-border px-8 py-4">
    <div class="w-24">
      {#if !isFirst}
        <Button variant="ghost" onclick={back} disabled={nextLoading}>← Back</Button>
      {/if}
    </div>
    <div class="flex items-center gap-2">
      {#if step.skippable && !nextLoading}
        <Button variant="ghost" class="text-muted-foreground" onclick={skip}>Skip</Button>
      {/if}
      <Button onclick={handleNext} disabled={nextLoading || !canProceed} class="min-w-28">
        {#if nextLoading}
          <Loader2 class="size-4 animate-spin" />
        {:else}
          {isFirst ? "Get Started" : isLast ? "Start Exploring" : "Next →"}
        {/if}
      </Button>
    </div>
  </div>
</div>

{#if authOpen}
  <AuthDialog onclose={() => (authOpen = false)} />
{/if}
