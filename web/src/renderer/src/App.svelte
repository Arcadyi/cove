<script lang="ts">
  // Located in cove/src/renderer/App.svelte
  import TopBar from "./components/TopBar.svelte";
  import { ModeWatcher } from "mode-watcher";
  import MediaExpandedModal from "./components/MediaExpandedModal.svelte";
  import PersonExpandedModal from "./components/modals/PersonExpandedModal.svelte";
  import ProviderExpandedModal from "./components/modals/ProviderExpandedModal.svelte";
  import type { Media } from "$lib/types/tmdb";
  import type { Person, Provider } from "$lib/api";
  import type { Stream } from "$lib/types/addons";
  import Player from "./components/Player.svelte";
  import * as Tooltip from "$lib/components/ui/tooltip";
  import { Maximize2, X } from "lucide-svelte";
  import { setMode } from "mode-watcher";

  import type { Page } from "$lib/types/types";
  import QueryPage from "./components/QueryPage.svelte";
  import HomePage from "./components/HomePage.svelte";
  import SettingsPage from "./components/SettingsPage.svelte";
  import MyListPage from "./components/MyListPage.svelte";
  import { settings } from "$lib/stores/settings";
  import { onMount, setContext, tick } from "svelte";
  import { animate, JSAnimation } from "animejs";
  import { scale } from "svelte/transition";
  import { cubicOut } from "svelte/easing";
  import {
    pickBestStream,
    type StreamSelectionMode,
  } from "$lib/streamSelection";
  import { api } from "$lib/api";
  import InsightsPage from "./components/InsightsPage.svelte";

  let query = $state("");

  // The media whose detail overlay (the single app-level MediaExpandedModal)
  // is currently open. Opening one no longer navigates to a page — the overlay
  // floats over whatever page is underneath, Netflix-style. Cards request it
  // via the "openMediaDetail" context provided below.
  let selectedMedia: Media | null = $state(null);

  // Person / provider detail overlays, opened from search result cards. Same
  // floating-overlay model as selectedMedia.
  let selectedPerson: Person | null = $state(null);
  let selectedProvider: Provider | null = $state(null);

  let loading = $state(false);

  let currentPage = $state<Page>({ type: "home" });
  let pageHistory = $state<Page[]>([]);

  // ─── Player state (lifted out of MediaPage so it survives navigation) ──────
  // A stream started from a MediaPage is tracked here instead of inside
  // MediaPage itself, so switching pages/tabs no longer unmounts <Player> —
  // it just changes how it's displayed (full vs. floating "PiP").
  type PlayerSession = {
    media: Media;
    stream: Stream;
    season?: number;
    episode?: number;
    episodeName?: string;
    subtitles: { id: string; url: string; lang: string }[];
  };

  let playerSession = $state<PlayerSession | null>(null);
  let playerMode = $state<"full" | "pip" | null>(null);
  let playerWrapperEl = $state<HTMLDivElement | null>(null);
  let playerModeAnimation: JSAnimation | null = null;

  const canGoBack = $derived(playerMode === "full" || pageHistory.length > 0);

  // Whether the active/floating stream belongs to the media page currently
  // on screen — used to stop the trailer from playing underneath it, and to
  // stop StreamsList from re-triggering auto-select for the same episode.
  const streamActiveForSelectedMedia = $derived(
    !!playerSession &&
      !!selectedMedia &&
      playerSession.media.id === selectedMedia.id &&
      playerSession.media.media_type === selectedMedia.media_type,
  );

  const activePlaybackSeason = $derived(
    streamActiveForSelectedMedia ? playerSession?.season : undefined,
  );
  const activePlaybackEpisode = $derived(
    streamActiveForSelectedMedia ? playerSession?.episode : undefined,
  );

  // Drives the topbar's "now playing" title while the player is full-size —
  // replaces the logo so there's nothing left in the corner to collide with
  // the player's own controls.
  const fullscreenInfo = $derived(
    playerMode === "full" && playerSession
      ? {
          title:
            playerSession.media.media_type === "tv"
              ? playerSession.media.name
              : playerSession.media.title,
          subtitle:
            playerSession.media.media_type === "tv" &&
            playerSession.season != null &&
            playerSession.episode != null
              ? `S${playerSession.season}E${playerSession.episode}${
                  playerSession.episodeName
                    ? ` - ${playerSession.episodeName}`
                    : ""
                }`
              : undefined,
        }
      : null,
  );

  // Load settings once on startup so all components have values immediately.
  onMount(() => {
    setMode("dark");
    settings.load();

    // The media player (vidstack/maverick) aborts internal signals when its
    // element unmounts — closing the detail modal, re-keying it, or swapping it
    // for the person/provider overlay while a trailer is still loading. Those
    // surface as uncaught AbortErrors that are safe to ignore: an abort is an
    // intentional cancellation, not a failure. We match on name *and* message
    // (the dispose path can surface the abort without a clean name) and cover
    // both rejection and error events.
    const isAbort = (v: unknown): boolean => {
      const r = v as { name?: string; message?: string } | null | undefined;
      return (
        r?.name === "AbortError" ||
        (typeof r?.message === "string" && /abort/i.test(r.message))
      );
    };
    const onRejection = (e: PromiseRejectionEvent) => {
      if (isAbort(e.reason)) e.preventDefault();
    };
    const onError = (e: ErrorEvent) => {
      if (isAbort(e.error) || /aborted without reason/i.test(e.message ?? "")) {
        e.preventDefault();
      }
    };
    window.addEventListener("unhandledrejection", onRejection);
    window.addEventListener("error", onError);
    return () => {
      window.removeEventListener("unhandledrejection", onRejection);
      window.removeEventListener("error", onError);
    };
  });

  function changePage(page: Page): void {
    // Navigating away dismisses the detail overlay.
    selectedMedia = null;

    // Leaving the page a "full" player is overlaying — pop it out into PiP
    // instead of leaving it behind to be destroyed.
    if (playerMode === "full") {
      setPlayerMode("pip");
    }

    pageHistory.push(currentPage);
    currentPage = page;

    if (pageHistory.length > 25) {
      pageHistory.shift();
    }
  }

  function goBack(): void {
    // Navigating away dismisses the detail overlay.
    selectedMedia = null;

    // While the player is shown full-size, "back" means "leave the player
    // and reveal the media page underneath," not "go to whatever page was
    // open before the media page." The media page was never actually left.
    if (playerMode === "full") {
      setPlayerMode("pip");
      return;
    }

    const previousPage = pageHistory.pop();
    if (previousPage) {
      currentPage = previousPage;
      if (previousPage.type === "query") {
        query = previousPage.query;
      }
    }
  }

  function selectMedia(media: Media): void {
    selectedMedia = media;
  }

  // Any MediaCard, anywhere in the tree (including inside HomePage's
  // recommendation rows), opens the single detail overlay through this —
  // no prop drilling, no per-page wiring.
  setContext("openMediaDetail", selectMedia);

  // Same idea for "play now" (auto-pick best stream), so the hero card's
  // Watch button and similar entry points can start playback directly.
  setContext("watchMedia", quickPlay);

  // Short synthesized "thud" played whenever a stream actually starts —
  // the same kind of confirmation chime Netflix plays on play. No audio
  // asset needed; it's just a quick pitch-dropping tone through Web Audio.
  let audioCtx: AudioContext | null = null;

  function getAudioCtx(): AudioContext {
    if (!audioCtx) {
      audioCtx = new AudioContext();
    }
    return audioCtx;
  }

  // Browsers create a fresh AudioContext in "suspended" state until a real
  // user gesture unlocks it. Auto-select (StreamsList's setTimeout-fired
  // pick) isn't itself a gesture, so without this, the very first sound —
  // whichever path triggers it first — could end up scheduled against a
  // context that hadn't actually started ticking yet and just stay silent.
  // Unlocking eagerly on the first real interaction anywhere in the app
  // sidesteps that entirely; by the time anything calls playStartSound,
  // the context is already running.
  onMount(() => {
    const unlock = () => {
      getAudioCtx()
        .resume()
        .catch(() => {});
      window.removeEventListener("pointerdown", unlock);
      window.removeEventListener("keydown", unlock);
    };
    window.addEventListener("pointerdown", unlock);
    window.addEventListener("keydown", unlock);
    return () => {
      window.removeEventListener("pointerdown", unlock);
      window.removeEventListener("keydown", unlock);
    };
  });

  async function playStartSound(): Promise<void> {
    try {
      const ctx = getAudioCtx();
      if (ctx.state === "suspended") {
        await ctx.resume();
      }

      const now = ctx.currentTime;
      const osc = ctx.createOscillator();
      const gain = ctx.createGain();

      osc.type = "sine";
      osc.frequency.setValueAtTime(180, now);
      osc.frequency.exponentialRampToValueAtTime(70, now + 0.15);

      gain.gain.setValueAtTime(0.0001, now);
      gain.gain.exponentialRampToValueAtTime(0.35, now + 0.01);
      gain.gain.exponentialRampToValueAtTime(0.0001, now + 0.22);

      osc.connect(gain);
      gain.connect(ctx.destination);

      osc.start(now);
      osc.stop(now + 0.25);
    } catch (e) {
      console.error("playStartSound failed", e);
    }
  }

  function startPlayback(
    media: Media,
    stream: Stream,
    season?: number,
    episode?: number,
    episodeName?: string,
  ): void {
    playStartSound();

    playerSession = {
      media,
      stream,
      season,
      episode,
      episodeName,
      subtitles: [],
    };
    playerMode = "full";

    api
      .getSubtitles({
        id: media.id,
        type: media.media_type,
        season: media.media_type === "tv" ? (season ?? undefined) : undefined,
        episode: media.media_type === "tv" ? (episode ?? undefined) : undefined,
      })
      .then((subs) => {
        // Guard against a newer playStream call having superseded this one
        // while the fetch was in flight.
        if (playerSession && playerSession.stream === stream) {
          playerSession = {
            ...playerSession,
            subtitles: Array.isArray(subs) ? subs : [],
          };
        }
      })
      .catch(() => {});
  }

  // Streams often come back empty on the first hit while indexers are still
  // searching — StreamsList handles that by polling every second; quickPlay
  // does the same here rather than giving up after one empty response.
  async function fetchStreamsWithRetry(
    fetcher: () => Promise<Stream[]>,
  ): Promise<Stream[]> {
    for (let attempt = 0; attempt < 15; attempt++) {
      try {
        const res = await fetcher();
        if (Array.isArray(res) && res.length > 0) return res;
      } catch (e) {
        console.error("quickPlay: failed to fetch streams", e);
        return [];
      }
      await new Promise((r) => setTimeout(r, 1000));
    }
    return [];
  }

  // Used by "Watch"/"Continue" buttons on media cards: skip the media page
  // entirely and go straight to picking a stream and playing it, the same
  // way StreamsList's auto-select does. season/episode come from the
  // card's own library data (last_watched_season/episode); undefined for
  // movies, or for a TV show with no progress yet (in which case we start
  // from the first episode).
  async function quickPlay(
    media: Media,
    season?: number,
    episode?: number,
  ): Promise<void> {
    const isTV = media.media_type === "tv";
    const targetSeason = isTV ? (season ?? 1) : undefined;
    const targetEpisode = isTV ? (episode ?? 1) : undefined;

    const streams = await fetchStreamsWithRetry(() =>
      api.getStreams(
        media.id,
        isTV
          ? { type: "tv", season: targetSeason, episode: targetEpisode }
          : {},
      ),
    );
    if (streams.length === 0) return;

    const mode =
      ($settings?.streamSelectionMode as StreamSelectionMode) ?? "balanced";
    const best = pickBestStream(streams, mode, {
      measuredBandwidthMbps: $settings?.measuredBandwidthMbps,
    });
    if (!best) return;

    let episodeName: string | undefined;
    if (isTV) {
      try {
        const eps = await api.tvEpisodes(media.id, targetSeason!);
        episodeName = eps.find((e) => e.episode_number === targetEpisode)?.name;
      } catch (e) {
        // Non-critical — just means the topbar's subtitle line won't show
        // an episode title.
        console.error("quickPlay: failed to fetch episode name", e);
      }
    }

    startPlayback(media, best, targetSeason, targetEpisode, episodeName);
  }

  // Switches between "full" and "pip" with a quick FLIP-style animation —
  // measure the player's current box, apply the new layout, then animate
  // from the old box back to the new one. This sidesteps the fact that the
  // two modes use different CSS `position` values (absolute vs. fixed),
  // which plain CSS transitions can't animate between cleanly.
  async function setPlayerMode(mode: "full" | "pip"): Promise<void> {
    if (!playerSession || playerMode === mode) {
      playerMode = mode;
      return;
    }

    const el = playerWrapperEl;
    if (!el) {
      playerMode = mode;
      return;
    }

    const before = el.getBoundingClientRect();
    playerMode = mode;
    await tick();
    const after = el.getBoundingClientRect();

    if (after.width === 0 || after.height === 0) return;

    playerModeAnimation?.pause();

    const dx = before.left - after.left;
    const dy = before.top - after.top;
    const scaleX = before.width / after.width;
    const scaleY = before.height / after.height;

    el.style.transformOrigin = "top left";
    el.style.transform = `translateX(${dx}px) translateY(${dy}px) scaleX(${scaleX}) scaleY(${scaleY})`;

    playerModeAnimation = animate(el, {
      translateX: 0,
      translateY: 0,
      scaleX: 1,
      scaleY: 1,
      duration: 380,
      easing: "easeOutExpo",
      complete: () => {
        el.style.transform = "";
        el.style.transformOrigin = "";
      },
    });
  }

  function expandPlayer(): void {
    if (!playerSession) return;
    setPlayerMode("full");
  }

  // mpv's surface always fills the whole window behind the web UI. Make the app
  // background transparent (revealing it) ONLY while the player is full-size;
  // otherwise keep the app opaque so the full-window video stays hidden behind
  // the UI in pip mode and after the stream closes.
  $effect(() => {
    document.documentElement.classList.toggle(
      "cove-playing",
      playerMode === "full",
    );
  });

  function closePlayer(): void {
    playerSession = null;
    playerMode = null;
  }
</script>

<Tooltip.Provider>
  <!-- Any interaction with the top bar dismisses the open detail overlay.
       `contents` keeps this layout-neutral; the handler still fires via DOM
       event bubbling from the fixed TopBar inside it. -->
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div class="contents" onpointerdown={() => (selectedMedia = null)}>
    <TopBar
      bind:query
      bind:loading
      onSelectPage={changePage}
      {canGoBack}
      onGoBack={goBack}
      {fullscreenInfo}
      onMinimizePlayer={() => setPlayerMode("pip")}
      onCloseStream={closePlayer}
      {currentPage}
    />
  </div>
  {#if selectedMedia}
    {#key selectedMedia.id}
      <MediaExpandedModal
        media={selectedMedia}
        onwatch={(season, episode) => {
          const m = selectedMedia;
          if (m) quickPlay(m, season, episode);
        }}
        onplaystream={(stream, season, episode, episodeName) => {
          const m = selectedMedia;
          if (m) startPlayback(m, stream, season, episode, episodeName);
        }}
        onsimilar={(m) => selectMedia(m)}
        onclose={() => (selectedMedia = null)}
        streamActive={streamActiveForSelectedMedia}
        activeSeason={activePlaybackSeason}
        activeEpisode={activePlaybackEpisode}
      />
    {/key}
  {/if}

  {#if selectedPerson}
    {#key selectedPerson.id}
      <PersonExpandedModal
        person={selectedPerson}
        onclose={() => (selectedPerson = null)}
        onselect={(m) => {
          selectedPerson = null;
          selectMedia(m);
        }}
      />
    {/key}
  {/if}

  {#if selectedProvider}
    {#key selectedProvider.provider_id}
      <ProviderExpandedModal
        provider={selectedProvider}
        onclose={() => (selectedProvider = null)}
        onselect={(m) => {
          selectedProvider = null;
          selectMedia(m);
        }}
      />
    {/key}
  {/if}

  <div class="flex h-screen flex-col overflow-hidden">
    <main class="relative min-h-0 flex-1 overflow-hidden">
      <!-- Hidden (not unmounted) while the player is full, so its opaque page
           background doesn't block the transparent player from revealing mpv,
           and page state/scroll survive the full↔pip switch. -->
      <div class="h-full w-full" class:invisible={playerMode === "full"}>
        {#if currentPage.type === "settings"}
          <SettingsPage />
        {:else if currentPage.type === "query"}
          <QueryPage
            bind:query
            bind:loading
            onSelectMedia={selectMedia}
            onSuggested={(name: string) => {
              query = name;
              changePage({ type: "query", query: name });
            }}
            onWatch={quickPlay}
            onSelectPerson={(p: Person) => (selectedPerson = p)}
            onSelectProvider={(p: Provider) => (selectedProvider = p)}
          />
        {:else if currentPage.type === "home"}
          <HomePage onSelectMedia={selectMedia} onWatch={quickPlay} />
        {:else if currentPage.type === "insights"}
          <InsightsPage />
        {:else if currentPage.type === "myList"}
          <MyListPage onSelectMedia={selectMedia} onWatch={quickPlay} />
        {/if}
      </div>

      {#if playerSession}
        <!--
          One single, never-remounted <Player> instance. Only its container's
          position/size change between "full" (overlaying the media page,
          same as before) and "pip" (a small floating box that survives page
          navigation) — the component itself is never torn down by switching
          modes, so playback and the HLS session keep running.
        -->
        <div
          bind:this={playerWrapperEl}
          class="group z-30 overflow-hidden shadow-2xl transition-[border-radius] duration-300"
          class:absolute={playerMode === "full"}
          class:inset-0={playerMode === "full"}
          class:rounded-xl={playerMode === "full"}
          class:fixed={playerMode === "pip"}
          class:right-4={playerMode === "pip"}
          class:bottom-4={playerMode === "pip"}
          class:w-md={playerMode === "pip"}
          class:aspect-video={playerMode === "pip"}
          class:rounded-lg={playerMode === "pip"}
          class:ring-1={playerMode === "pip"}
          class:ring-border={playerMode === "pip"}
          transition:scale={{
            duration: 280,
            start: 0.92,
            opacity: 0,
            easing: cubicOut,
          }}
        >
          <Player
            src={playerSession.stream.infoHash || playerSession.stream.url}
            media={playerSession.media}
            externalSubtitles={playerSession.subtitles}
            season={playerSession.season}
            episode={playerSession.episode}
            compact={playerMode === "pip"}
          />

          {#if playerMode === "pip"}
            <div
              class="pointer-events-none absolute top-2 right-2 z-40 flex gap-1 opacity-0 transition-opacity group-hover:opacity-100"
            >
              <button
                class="pointer-events-auto flex size-7 items-center justify-center rounded bg-black/70 text-white transition hover:bg-black/90"
                onclick={expandPlayer}
                aria-label="Expand player"
              >
                <Maximize2 class="size-4" />
              </button>
              <button
                class="pointer-events-auto flex size-7 items-center justify-center rounded bg-black/70 text-white transition hover:bg-black/90"
                onclick={closePlayer}
                aria-label="Close stream"
              >
                <X class="size-4" />
              </button>
            </div>
          {/if}
        </div>
      {/if}
    </main>
  </div>
</Tooltip.Provider>
<ModeWatcher defaultMode="dark" />
