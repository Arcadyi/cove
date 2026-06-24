<script lang="ts">
  import type { Media } from "$lib/types/tmdb";
  import { Spinner } from "$lib/components/ui/spinner";
  import * as Popover from "$lib/components/ui/popover";
  import {
    Settings,
    Headphones,
    ChevronLeft,
    ChevronRight,
    Play,
    Pause,
    Volume2,
    VolumeOff,
    Maximize,
    Minimize,
  } from "lucide-svelte";
  import "vidstack/bundle";
  import "vidstack/player/styles/base.css";
  import "vidstack/player/styles/default/theme.css";
  import "vidstack/player/styles/default/layouts/video.css";
  import Hls from "hls.js";
  import type { MediaPlayerElement } from "vidstack/elements";
  import { onDestroy } from "svelte";
  import { api } from "$lib/api";
  import { settings } from "$lib/stores/settings";
  import { TorrentProgress } from "$lib/player/torrentProgress.svelte";
  import { ProgressSaver } from "$lib/player/progressSaver.svelte";
  import { SubtitleCues } from "$lib/player/subtitleCues.svelte";
  import { HlsSession } from "$lib/player/hlsSession.svelte";
  import { SvelteMap } from "svelte/reactivity";

  // ─── Types ─────────────────────────────────────────────────────────────────

  type AudioTrackInfo = {
    index: number;
    language: string;
    title: string;
    codec: string;
  };

  type SubtitleTrackInfo = {
    index: number;
    language: string;
    title: string;
    codec: string;
  };

  type SubtitleSettings = {
    size: number;
    line: number;
    background: boolean;
    offset: number;
  };

  const UNSUPPORTED_AUDIO_CODECS = new Set([
    "ac3",
    "eac3",
    "dts",
    "truehd",
    "mlp",
  ]);

  // ─── Props ──────────────────────────────────────────────────────────────────

  let {
    src,
    media,
    externalSubtitles = [],
    season = undefined,
    episode = undefined,
    compact = false,
  }: {
    src: string;
    media?: Media;
    externalSubtitles?: { id: string; url: string; lang: string }[];
    season?: number;
    episode?: number;
    // True while shown in the small floating PiP box — subtitles get
    // clipped/illegible at that size and just eat into the limited screen
    // real estate, so we skip rendering them (the underlying cue-tracking
    // logic keeps running, so they reappear instantly back in full mode).
    compact?: boolean;
  } = $props();

  // Ensure settings are populated even if the user never visited the
  // settings page this session. Safe to call repeatedly — it's just a GET.
  settings.load().catch(() => {});

  // ─── Audio tracks (from probe, used to decide HLS vs direct) ────────────────

  let audioTracks = $state<AudioTrackInfo[]>([]);
  let videoCodec = $state("");

  // ─── Built-in subtitle tracks (from probe) ───────────────────────────────────

  let builtInSubtitles = $state<
    { id: string; url: string; lang: string; label: string }[]
  >([]);

  // True only once the probe (which produces builtInSubtitles) has settled —
  // success or failure. externalSubtitles is a static prop already resolved
  // before mount, so the probe finishing is the one async source we still
  // need to wait on before trusting the combined track list is complete.
  let subtitleProbeReady = $state(false);

  // ─── Vidstack audio track list (populated from HLS manifest) ────────────────

  interface VidstackAudioTrack {
    id: string;
    label: string;
    language: string;
    selected: boolean;
  }

  let vidstackAudioTracks = $state<VidstackAudioTrack[]>([]);

  // ─── DOM refs ───────────────────────────────────────────────────────────────

  let playerEl = $state<MediaPlayerElement | null>(null);

  // ─── Source derivations ─────────────────────────────────────────────────────

  const isHash = $derived(!src.startsWith("http"));

  const baseInput = $derived(api.playUrl(src));

  const needsHLS = $derived(
    ($settings?.preferHLS ?? false) ||
      audioTracks.length > 1 ||
      audioTracks.some((t) =>
        UNSUPPORTED_AUDIO_CODECS.has(t.codec.toLowerCase()),
      ),
  );

  // ─── Playback state (driven by Vidstack events) ─────────────────────────────

  let canPlay = $state(false);
  let waiting = $state(false);
  let error = $state<string | null>(null);
  let fakeProgress = $state(0);
  let probedDuration = $state<number | null>(null);

  // ─── HLS session ────────────────────────────────────────────────────────────

  const hlsSession = new HlsSession();

  const activeStreamURL = $derived.by(() => {
    if (needsHLS && hlsSession.sessionID) {
      return api.hlsMasterUrl(hlsSession.sessionID);
    }
    if (!needsHLS && audioTracks.length > 0) {
      // No ffmpeg needed, stream directly
      return api.playUrl(src);
    }
    return null; // not ready yet (probe still running)
  });

  // ─── Subtitle fine-tuning ───────────────────────────────────────────────────

  let subtitleSettings = $state<SubtitleSettings>({
    size: 100,
    line: 8,
    background: true,
    offset: 0,
  });
  let subtitleSettingsOpen = $state(false);
  let subtitleView = $state<"tracks" | "settings">("tracks");

  // ─── Seed subtitle style from global settings ───────────────────────────────
  //
  // Applied once, then left alone — if the user tweaks size/position/background
  // for this session via the in-player panel, we don't want global settings
  // (or a later save from the Settings page) silently overriding that mid-watch.
  // Sync offset is intentionally excluded: drift is per-file, not a style
  // preference, so it has no global default to seed from.

  let appliedSubtitleDefaults = false;
  $effect(() => {
    if (appliedSubtitleDefaults || !$settings) return;
    appliedSubtitleDefaults = true;
    subtitleSettings.size = $settings.subtitleSize ?? subtitleSettings.size;
    subtitleSettings.line = $settings.subtitlePosition ?? subtitleSettings.line;
    subtitleSettings.background =
      $settings.subtitleBackground ?? subtitleSettings.background;
  });

  // ─── Other state ────────────────────────────────────────────────────────────

  let logoUrl = $state<string | null>(null);
  const torrent = new TorrentProgress();

  const title = $derived(
    media ? (media.media_type === "tv" ? media.name : media.title) : "",
  );

  // ─── Watch-progress state ────────────────────────────────────────────────────

  const progress = new ProgressSaver();

  // ─── Probe audio tracks ─────────────────────────────────────────────────────

  $effect(() => {
    if (!src) return () => {};
    audioTracks = [];
    videoCodec = "";
    builtInSubtitles = [];
    hlsSession.sessionID = null;
    canPlay = false;
    error = null;
    appliedAudioAutoSelect = false;
    subtitleProbeReady = false;

    const controller = new AbortController();
    api
      .probe<{
        audio: AudioTrackInfo[];
        subtitles: SubtitleTrackInfo[];
        videoCodec: string;
        duration: number;
      }>(src, controller.signal)
      .then((data) => {
        audioTracks = data.audio ?? [];
        videoCodec = data.videoCodec ?? "";
        probedDuration = data.duration ?? null;

        builtInSubtitles = (data.subtitles ?? []).map((t) => ({
          id: `builtin-${t.index}`,
          url: api.subtitleExtractUrl(src, t.index),
          lang: t.language || `track${t.index}`,
          label:
            t.title ||
            (t.language ? langName(t.language) : `Track ${t.index + 1}`),
        }));
      })
      .catch(() => {
        audioTracks = [];
        builtInSubtitles = [];
        probedDuration = null;
      })
      .finally(() => {
        // Only NOW is the full track list (built-in + external) settled.
        // Setting this any earlier risks the auto-select effect locking onto
        // whichever subtitle source happened to resolve first.
        subtitleProbeReady = true;
      });

    return () => controller.abort();
  });

  // ─── Load saved watch position (runs whenever src/season/episode changes) ────

  $effect(() => {
    if (!media || !src) return;
    progress.reset();

    // Default true (matches the Go-side default) — only explicit false opts out.
    // Using "=== false" rather than a falsy check means we still try to
    // remember position during the brief window before settings finish loading.
    if ($settings?.rememberPosition === false) return;

    progress.load(media.id, media.media_type, season ?? null, episode ?? null);
  });

  // ─── Start HLS session when needed ──────────────────────────────────────────

  $effect(() => {
    if (!needsHLS || audioTracks.length === 0 || probedDuration === null)
      return () => {};

    return hlsSession.start(
      {
        input: baseInput,
        tracks: audioTracks,
        duration: probedDuration,
        videoCodec: videoCodec,
      },
      (msg) => {
        error = msg;
      },
    );
  });

  // ─── Update Vidstack source when activeStreamURL changes ────────────────────

  $effect(() => {
    if (!playerEl) return () => {};

    function onProviderChange(e: CustomEvent): void {
      if (e.detail?.type === "hls") {
        e.detail.library = Hls;
        e.detail.config = {
          maxBufferLength: 60,
          maxMaxBufferLength: 120,
          maxBufferSize: 60000000,
          appendErrorMaxRetry: 10,
          fragLoadingMaxRetry: 10,
          fragLoadingTimeOut: 60000,
          manifestLoadingTimeOut: 60000,
          levelLoadingTimeOut: 60000,
        };

        e.detail.addEventListener?.("hls-instance", (ev: CustomEvent) => {
          const hls = ev.detail as Hls;
          let mediaRecoveryAttempts = 0;

          hls.on(Hls.Events.ERROR, (_, data) => {
            if (!data.fatal) return; // hls.js handles non-fatal errors itself

            if (
              data.type === Hls.ErrorTypes.MEDIA_ERROR &&
              mediaRecoveryAttempts < 3
            ) {
              mediaRecoveryAttempts++;
              if (mediaRecoveryAttempts === 1) {
                console.warn(
                  "[hls] media error — attempting recoverMediaError",
                );
                hls.recoverMediaError();
              } else {
                console.warn(
                  "[hls] media error — attempting swapAudioCodec + recoverMediaError",
                );
                hls.swapAudioCodec();
                hls.recoverMediaError();
              }
            } else if (data.type === Hls.ErrorTypes.NETWORK_ERROR) {
              console.warn("[hls] network error — resuming load");
              hls.startLoad();
            } else {
              console.error("[hls] unrecoverable error", data);
              error = data.details ?? "Stream error.";
            }
          });
        });
      }
    }

    playerEl.addEventListener("provider-change", onProviderChange);
    playerEl.addEventListener("can-play", onCanPlay);
    playerEl.addEventListener("waiting", onWaiting);
    playerEl.addEventListener("playing", onPlaying);
    playerEl.addEventListener("media-error", onError);
    playerEl.addEventListener("audio-track-change", onAudioTrackChange);

    return () => {
      playerEl.removeEventListener("provider-change", onProviderChange);
      playerEl.removeEventListener("can-play", onCanPlay);
      playerEl.removeEventListener("waiting", onWaiting);
      playerEl.removeEventListener("playing", onPlaying);
      playerEl.removeEventListener("media-error", onError);
      playerEl.removeEventListener("audio-track-change", onAudioTrackChange);
    };
  });

  // ─── Set player source ────────────────────────────────────────────────────────

  $effect(() => {
    if (!playerEl || !activeStreamURL) return;
    canPlay = false;
    fakeProgress = 0;
    error = null;
    playerEl.src = activeStreamURL;
  });

  // ─── Apply initial volume / mute from settings ──────────────────────────────
  //
  // Applied once, imperatively, rather than as a bound prop — otherwise every
  // settings change mid-playback would fight the user's own mute/volume
  // adjustments. Gated on settings actually being loaded so we don't briefly
  // apply stale defaults before the GET resolves.

  let appliedInitialAV = false;
  $effect(() => {
    if (!playerEl || appliedInitialAV || !$settings) return;
    appliedInitialAV = true;
    playerEl.muted = $settings.openOnMute ?? false;
    playerEl.volume = $settings.defaultVolume ?? 1;
  });

  // ─── Vidstack event handlers ─────────────────────────────────────────────────

  function onCanPlay(): void {
    canPlay = true;
    waiting = false;
    fakeProgress = 100;
    syncAudioTracks();

    // Seek to the saved position the first time playback becomes ready.
    progress.resume(playerEl?.querySelector<HTMLVideoElement>("video"));
  }

  function onWaiting(): void {
    if (canPlay) waiting = true;
  }

  function onPlaying(): void {
    waiting = false;
    canPlay = true;
  }

  function onError(e: CustomEvent): void {
    error = e.detail?.message ?? "Failed to load stream.";
    canPlay = false;
    console.error("Vidstack error:", e.detail);
  }

  function onAudioTrackChange(): void {
    syncAudioTracks();
  }

  function syncAudioTracks(): void {
    if (!playerEl) return;
    const tracks = playerEl.audioTracks as
      | Iterable<VidstackAudioTrack>
      | undefined;
    if (!tracks) return;
    vidstackAudioTracks = Array.from(tracks).map((t) => ({
      id: t.id,
      label: t.label,
      language: t.language,
      selected: t.selected,
    }));
  }

  function switchAudioTrack(id: string): void {
    if (!playerEl) return;
    const tracks = playerEl.audioTracks as
      | Iterable<VidstackAudioTrack>
      | undefined;
    if (!tracks) return;
    for (const t of Array.from(tracks)) {
      if (t.id === id) t.selected = true;
    }
    syncAudioTracks();
  }

  // ─── Auto-select preferred audio track ──────────────────────────────────────
  //
  // Only meaningful once there's more than one track to choose between.
  // Applied once per video so it doesn't override a manual switch later.

  let appliedAudioAutoSelect = false;
  $effect(() => {
    if (appliedAudioAutoSelect || vidstackAudioTracks.length <= 1) return;
    const lang = $settings?.defaultAudioLang;
    if (!lang) return;
    appliedAudioAutoSelect = true;
    const match = vidstackAudioTracks.find((t) => t.language === lang);
    if (match && !match.selected) switchAudioTrack(match.id);
  });

  // ─── Logo fetch ──────────────────────────────────────────────────────────────

  $effect(() => {
    if (!media) return;
    api.getLogos(media.id, media.media_type).then((logos: string[]) => {
      if (logos?.length) logoUrl = logos[0];
    });
  });

  // ─── Fake loading bar animation ──────────────────────────────────────────────

  $effect(() => {
    if (canPlay) {
      fakeProgress = 100;
      return () => {};
    }
    fakeProgress = 0;
    const id = setInterval(() => {
      fakeProgress += (85 - fakeProgress) * (fakeProgress < 40 ? 0.03 : 0.01);
    }, 100);
    return () => clearInterval(id);
  });

  // ─── Torrent progress (SSE) ──────────────────────────────────────────────────

  $effect(() => {
    if (!isHash) return () => {};
    return torrent.start(src);
  });

  // ─── Watch-progress saving ────────────────────────────────────────────────────
  //
  // Saves position every 10 seconds while playing, and marks completed on "ended".
  // Also auto-adds the title to the library as "watching" if not already there
  // (handled server-side in the progress POST handler).

  $effect(() => {
    if (!playerEl || !canPlay || !media) return () => {};
    const video = playerEl.querySelector<HTMLVideoElement>("video");
    if (!video) return () => {};

    return progress.track(video, () => ({
      tmdbId: media!.id,
      mediaType: media!.media_type,
      title: title,
      posterPath: media!.poster_path ?? "",
      voteAverage: media!.vote_average ?? 0,
      lastAirDate: (media as { last_air_date?: string }).last_air_date ?? "",
      season: season ?? null,
      episode: episode ?? null,
      probedDuration: probedDuration,
    }));
  });

  // ─── Helpers ──────────────────────────────────────────────────────────────────

  function langName(code: string): string {
    try {
      return (
        new Intl.DisplayNames(["en"], { type: "language" }).of(code) ?? code
      );
    } catch {
      return code;
    }
  }

  const loadingMessage = $derived.by(() => {
    if (hlsSession.loading) return "Preparing tracks…";
    if (needsHLS && !hlsSession.sessionID) return "Starting stream…";
    if (isHash)
      return torrent.peers > 0
        ? `Connecting · ${torrent.peers} peers · ${torrent.speed}`
        : "Connecting to peers…";
    return "Buffering…";
  });

  // ─── Custom subtitle renderer ─────────────────────────────────────────────────

  let selectedTrackId = $state<string | null>(null);
  const subtitles = new SubtitleCues();

  // ─── Pre-warm subtitle cache (sequential) ────────────────────────────────────

  $effect(() => {
    if (builtInSubtitles.length === 0) return () => {};
    let cancelled = false;

    (async () => {
      for (const sub of builtInSubtitles) {
        if (cancelled) break;
        await fetch(sub.url).catch(() => {});
      }
    })();

    return () => {
      cancelled = true;
    };
  });

  const textTracks = $derived([
    ...builtInSubtitles.map((s) => ({
      id: s.id,
      label: s.label,
      language: s.lang,
      mode: s.id === selectedTrackId ? "showing" : "disabled",
    })),
    ...externalSubtitles.map((s) => ({
      id: s.id,
      label: s.lang.toUpperCase(),
      language: s.lang,
      mode: s.id === selectedTrackId ? "showing" : "disabled",
    })),
  ]);

  // ─── Auto-select preferred subtitle track ────────────────────────────────────
  //
  // Only fires if "Enable subtitles by default" is on. Applied once per video
  // so a manual "Off" selection later in the same session sticks.
  //
  // Gated on subtitleProbeReady rather than just "textTracks is non-empty":
  // externalSubtitles (a static prop) is often available immediately, while
  // builtInSubtitles only populates once the probe resolves. Matching against
  // defaultSubtitleLang too early can lock onto whichever source happened to
  // be ready first — e.g. an external Portuguese track — before the file's
  // own English track has even loaded.

  let appliedSubtitleAutoSelect = false;
  $effect(() => {
    if (appliedSubtitleAutoSelect) return;
    if (!$settings?.subtitlesEnabled) return;
    if (!subtitleProbeReady) return; // wait for both sources to settle
    if (textTracks.length === 0) return; // nothing to select at all
    appliedSubtitleAutoSelect = true;
    const lang = $settings.defaultSubtitleLang;
    const match = textTracks.find((t) => t.language === lang) ?? textTracks[0];
    if (match) selectTextTrack(match.id);
  });

  let selectedLang = $state<string | null>(null);
  const tracksByLang = $derived.by(() => {
    const groups = new SvelteMap<
      string,
      { id: string; label: string; language: string; mode: string }[]
    >();
    for (const t of textTracks) {
      const key = t.language || "und";
      if (!groups.has(key)) groups.set(key, []);
      groups.get(key)!.push(t);
    }
    return groups;
  });

  $effect(() => {
    if (!playerEl || !canPlay) return () => {};
    const video = playerEl.querySelector<HTMLVideoElement>("video");
    if (!video) return () => {};
    return subtitles.track(video, () => subtitleSettings.offset);
  });

  $effect(() => {
    let s = src; // track dependency
    selectedTrackId = null;
    subtitles.clear();
    appliedSubtitleAutoSelect = false;
  });

  function selectTextTrack(id: string): void {
    selectedTrackId = id || null;
    if (!id) {
      subtitles.clear();
      return;
    }
    const builtin = builtInSubtitles.find((s) => s.id === id);
    if (builtin) {
      subtitles.load(builtin.url);
      return;
    }
    const ext = externalSubtitles.find((s) => s.id === id);
    if (ext) {
      subtitles.load(api.subtitleProxyUrl(ext.url));
    }
  }

  onDestroy(() => {
    hlsSession.stop();
  });
</script>

<!-- ─── Template ──────────────────────────────────────────────────────────────── -->

<div class="relative h-full w-full bg-black">
  {#if activeStreamURL}
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <media-player
      bind:this={playerEl}
      autoplay
      playsinline
      streamType="on-demand"
      class="h-full w-full"
    >
      <media-provider class="h-full w-full"></media-provider>

      <!-- ── Custom subtitle overlay ────────────────────────────────────────── -->
      {#if subtitles.loading && !compact}
        <div
          class="pointer-events-none absolute inset-x-0 bottom-16 z-20 flex justify-center"
        >
          <span class="rounded bg-black/70 px-3 py-1 text-sm text-white/70">
            Loading subtitles…
          </span>
        </div>
      {/if}
      {#if subtitles.currentText && !compact}
        <div
          class="pointer-events-none absolute inset-x-0 z-20 flex flex-col items-center gap-0.5 px-6"
          style="bottom: {subtitleSettings.line}%"
        >
          {#each subtitles.currentText.split("\n") as line (line)}
            <span
              class="block max-w-prose text-center leading-snug text-white drop-shadow-[0_1px_2px_rgba(0,0,0,0.9)]"
              style="font-size: {subtitleSettings.size}%; {subtitleSettings.background
                ? 'background-color: rgba(0,0,0,0.72); padding: 0.1em 0.4em; border-radius: 2px;'
                : ''}">{line}</span
            >
          {/each}
        </div>
      {/if}

      <media-controls
        class="absolute inset-0 z-10 flex flex-col justify-end bg-linear-to-t from-black/80 via-black/10 to-transparent opacity-0 transition-opacity duration-200 data-visible:opacity-100"
      >
        <div
          class="pointer-events-auto flex w-full items-center gap-2 px-3 pb-3 text-white"
        >
          <media-play-button
            class="group flex size-8 shrink-0 cursor-pointer items-center justify-center rounded transition hover:bg-white/20"
          >
            <Pause class="block size-4 group-data-paused:hidden" />
            <Play class="hidden size-4 group-data-paused:block" />
          </media-play-button>

          <media-mute-button
            class="group flex size-8 shrink-0 cursor-pointer items-center justify-center rounded transition hover:bg-white/20"
          >
            <VolumeOff class="hidden size-4 group-data-muted:block" />
            <Volume2 class="block size-4 group-data-muted:hidden" />
          </media-mute-button>

          <media-volume-slider
            class="group relative flex h-6 w-20 cursor-pointer touch-none items-center outline-none select-none"
          >
            <div
              class="relative h-1 w-full rounded-sm bg-white/30 transition-[height] group-data-focus:h-1.5"
            >
              <div
                class="absolute h-full w-(--slider-fill) rounded-sm bg-white"
              ></div>
            </div>
          </media-volume-slider>

          <media-time class="text-sm tabular-nums" type="current"></media-time>
          <span class="text-sm text-white/50">/</span>
          <media-time class="text-sm text-white/70 tabular-nums" type="duration"
          ></media-time>

          <media-time-slider
            class="group relative flex h-6 flex-1 cursor-pointer touch-none items-center outline-none select-none"
          >
            <div
              class="relative h-1 w-full rounded-sm bg-white/30 transition-[height] group-data-focus:h-1.5"
            >
              <div
                class="absolute h-full w-(--slider-fill) rounded-sm bg-white"
              ></div>
              <div
                class="absolute h-full w-(--slider-buffered) rounded-sm bg-white/40"
              ></div>
            </div>
          </media-time-slider>

          <!-- ── Custom: audio track selector ── -->
          {#if vidstackAudioTracks.length > 1}
            <Popover.Root>
              <Popover.Trigger
                onclick={(e) => e.stopPropagation()}
                class="flex items-center gap-1.5 rounded-md px-2 py-1 text-white/70 transition hover:bg-white/10 hover:text-white"
              >
                <Headphones class="size-4" />
                <span class="text-xs">
                  {vidstackAudioTracks.find((t) => t.selected)?.label ??
                    "Audio"}
                </span>
              </Popover.Trigger>
              <Popover.Content side="top" class="w-52 p-0">
                <div class="border-b border-border px-3 py-2">
                  <span class="text-xs font-medium text-muted-foreground"
                    >Audio Track</span
                  >
                </div>
                <div class="p-1">
                  {#each vidstackAudioTracks as track (track.id)}
                    <button
                      onclick={(e) => {
                        e.stopPropagation();
                        switchAudioTrack(track.id);
                      }}
                      class="flex w-full items-center gap-2 rounded px-3 py-1.5 text-left text-sm transition hover:bg-secondary {track.selected
                        ? 'font-semibold'
                        : ''}"
                    >
                      <span class="flex-1 truncate">
                        {track.label || langName(track.language) || "Unknown"}
                      </span>
                      {#if track.selected}
                        <span class="size-1.5 shrink-0 rounded-full bg-white"
                        ></span>
                      {/if}
                    </button>
                  {/each}
                </div>
              </Popover.Content>
            </Popover.Root>
          {/if}

          <!-- ── Custom: subtitle panel ── -->
          <Popover.Root bind:open={subtitleSettingsOpen}>
            <Popover.Trigger
              onclick={(e) => {
                e.stopPropagation();
                if (!subtitleSettingsOpen) {
                  subtitleView = "tracks";
                  selectedLang = null;
                }
              }}
              class="flex size-8 items-center justify-center rounded transition hover:bg-white/10 {subtitleSettingsOpen
                ? 'text-white'
                : 'text-white/50'}"
              aria-label="Subtitle settings"
            >
              <Settings class="size-4" />
            </Popover.Trigger>

            <Popover.Content
              side="top"
              class="w-52 p-0"
              onInteractOutside={() => {
                subtitleSettingsOpen = false;
                subtitleView = "tracks";
                selectedLang = null;
              }}
            >
              <!-- header -->
              <div
                class="flex items-center justify-between border-b border-border px-3 py-2"
              >
                <span class="text-xs font-medium text-muted-foreground">
                  {#if subtitleView === "settings"}
                    Subtitle Settings
                  {:else if selectedLang !== null}
                    {langName(selectedLang)}
                  {:else}
                    Subtitles
                  {/if}
                </span>
                {#if subtitleView === "tracks" && selectedLang === null}
                  <button
                    onclick={(e) => {
                      e.stopPropagation();
                      subtitleView = "settings";
                    }}
                    class="rounded p-0.5 text-muted-foreground transition hover:bg-secondary hover:text-foreground"
                  >
                    <Settings class="size-3.5" />
                  </button>
                {:else}
                  <button
                    onclick={(e) => {
                      e.stopPropagation();
                      if (subtitleView === "settings") {
                        subtitleView = "tracks";
                      } else {
                        selectedLang = null;
                      }
                    }}
                    class="rounded p-0.5 text-muted-foreground transition hover:bg-secondary hover:text-foreground"
                  >
                    <ChevronLeft class="size-3.5" />
                  </button>
                {/if}
              </div>

              <!-- body -->
              <div class="max-h-80 overflow-y-auto">
                {#if subtitleView === "settings"}
                  <div class="space-y-4 p-3">
                    <!-- Font size -->
                    <div class="space-y-1.5">
                      <div class="flex justify-between text-xs">
                        <span class="text-muted-foreground">Size</span>
                        <span>{subtitleSettings.size}%</span>
                      </div>
                      <input
                        type="range"
                        min="50"
                        max="200"
                        step="10"
                        bind:value={subtitleSettings.size}
                        onclick={(e) => e.stopPropagation()}
                        class="w-full accent-white"
                      />
                    </div>

                    <!-- Position -->
                    <div class="space-y-1.5">
                      <div class="flex justify-between text-xs">
                        <span class="text-muted-foreground">Position</span>
                        <span>{subtitleSettings.line}%</span>
                      </div>
                      <input
                        type="range"
                        min="2"
                        max="90"
                        step="1"
                        bind:value={subtitleSettings.line}
                        onclick={(e) => e.stopPropagation()}
                        class="w-full accent-white"
                      />
                      <div
                        class="flex justify-between text-[10px] text-muted-foreground"
                      >
                        <span>Bottom</span>
                        <span>Top</span>
                      </div>
                    </div>

                    <!-- Background toggle -->
                    <div class="flex items-center justify-between text-xs">
                      <span class="text-muted-foreground">Background</span>
                      <button
                        aria-label="Toggle background"
                        onclick={(e) => {
                          e.stopPropagation();
                          subtitleSettings.background =
                            !subtitleSettings.background;
                        }}
                        class="h-5 w-9 rounded-full transition-colors {subtitleSettings.background
                          ? 'bg-white'
                          : 'bg-white/20'}"
                      >
                        <span
                          class="block size-4 translate-x-0.5 rounded-full bg-black transition-transform {subtitleSettings.background
                            ? 'translate-x-4'
                            : ''}"
                        ></span>
                      </button>
                    </div>

                    <!-- Sync offset -->
                    <div class="space-y-1.5">
                      <div class="flex justify-between text-xs">
                        <span class="text-muted-foreground">Sync offset</span>
                        <span
                          >{subtitleSettings.offset > 0 ? "+" : ""}{(
                            subtitleSettings.offset / 1000
                          ).toFixed(1)}s</span
                        >
                      </div>
                      <div class="flex gap-1">
                        <button
                          onclick={(e) => {
                            e.stopPropagation();
                            subtitleSettings.offset = Math.max(
                              -10000,
                              subtitleSettings.offset - 500,
                            );
                          }}
                          class="flex h-7 flex-1 items-center justify-center rounded bg-secondary text-sm hover:bg-secondary/80"
                          >−500ms</button
                        >
                        <button
                          onclick={(e) => {
                            e.stopPropagation();
                            subtitleSettings.offset = Math.min(
                              10000,
                              subtitleSettings.offset + 500,
                            );
                          }}
                          class="flex h-7 flex-1 items-center justify-center rounded bg-secondary text-sm hover:bg-secondary/80"
                          >+500ms</button
                        >
                      </div>
                      {#if subtitleSettings.offset !== 0}
                        <button
                          onclick={(e) => {
                            e.stopPropagation();
                            subtitleSettings.offset = 0;
                          }}
                          class="w-full rounded py-0.5 text-center text-xs text-muted-foreground hover:text-foreground"
                          >Reset</button
                        >
                      {/if}
                    </div>
                  </div>
                {:else}
                  <div class="p-1">
                    <!-- Off -->
                    <button
                      onclick={(e) => {
                        e.stopPropagation();
                        selectTextTrack("");
                        subtitleSettingsOpen = false;
                        selectedLang = null;
                      }}
                      class="flex w-full items-center rounded px-3 py-1.5 text-left text-sm transition hover:bg-secondary {textTracks.every(
                        (t) => t.mode !== 'showing',
                      )
                        ? 'font-semibold'
                        : ''}"
                    >
                      Off
                    </button>

                    {#if selectedLang === null}
                      <!-- Level 1: one row per language -->
                      {#each [...tracksByLang.entries()] as [lang, tracks] (lang)}
                        {@const active = tracks.some(
                          (t) => t.mode === "showing",
                        )}
                        <button
                          onclick={(e) => {
                            e.stopPropagation();
                            if (tracks.length === 1) {
                              selectTextTrack(tracks[0].id);
                              subtitleSettingsOpen = false;
                            } else {
                              selectedLang = lang;
                            }
                          }}
                          class="flex w-full items-center rounded px-3 py-1.5 text-left text-sm transition hover:bg-secondary {active
                            ? 'font-semibold'
                            : ''}"
                        >
                          <span class="flex-1 truncate">{langName(lang)}</span>
                          {#if active}
                            <span
                              class="mr-1.5 size-1.5 shrink-0 rounded-full bg-white"
                            ></span>
                          {/if}
                          {#if tracks.length > 1}
                            <ChevronRight
                              class="size-3.5 shrink-0 text-muted-foreground"
                            />
                          {/if}
                        </button>
                      {/each}
                    {:else}
                      <!-- Level 2: versions within the chosen language -->
                      {#each tracksByLang.get(selectedLang) ?? [] as track (track.id)}
                        <button
                          onclick={(e) => {
                            e.stopPropagation();
                            selectTextTrack(track.id);
                            subtitleSettingsOpen = false;
                            selectedLang = null;
                          }}
                          class="flex w-full items-center rounded px-3 py-1.5 text-left text-sm transition hover:bg-secondary {track.mode ===
                          'showing'
                            ? 'font-semibold'
                            : ''}"
                        >
                          <span class="flex-1 truncate">{track.label}</span>
                          {#if track.mode === "showing"}
                            <span
                              class="size-1.5 shrink-0 rounded-full bg-white"
                            ></span>
                          {/if}
                        </button>
                      {/each}
                    {/if}
                  </div>
                {/if}
              </div>
            </Popover.Content>
          </Popover.Root>

          <!-- Torrent progress indicator -->
          {#if isHash && torrent.progress > 0 && torrent.progress < 100}
            <Popover.Root>
              <Popover.Trigger
                onclick={(e) => e.stopPropagation()}
                class="flex items-center gap-1.5 rounded-md px-2 py-1 text-white/70 transition hover:bg-white/10 hover:text-white"
              >
                <svg viewBox="0 0 12 12" class="size-3.5">
                  <circle
                    cx="6"
                    cy="6"
                    r="5"
                    fill="none"
                    stroke="currentColor"
                    stroke-width="1.5"
                    class="text-white/20"
                  />
                  <circle
                    cx="6"
                    cy="6"
                    r="5"
                    fill="none"
                    stroke="currentColor"
                    stroke-width="1.5"
                    stroke-dasharray="{(torrent.progress / 100) * 31.4} 31.4"
                    stroke-linecap="round"
                    transform="rotate(-90 6 6)"
                    class="text-green-400 transition-all duration-500"
                  />
                </svg>
                <span class="text-xs">{torrent.progress.toFixed(0)}%</span>
              </Popover.Trigger>
              <Popover.Content class="w-52" side="top">
                <p class="mb-2 text-sm font-medium">Download Progress</p>
                <div class="mb-1 h-1.5 w-full rounded-full bg-secondary">
                  <div
                    class="h-full rounded-full bg-green-500 transition-all"
                    style="width: {torrent.progress}%"
                  ></div>
                </div>
                <div class="flex justify-between text-xs text-muted-foreground">
                  <span>{torrent.progress.toFixed(1)}%</span>
                  {#if torrent.peers > 0}<span>{torrent.peers} peers</span>{/if}
                </div>
                <div class="mt-1 text-xs text-muted-foreground">
                  ↓ {torrent.speed}
                </div>
              </Popover.Content>
            </Popover.Root>
          {/if}

          <media-fullscreen-button
            class="group flex size-8 shrink-0 cursor-pointer items-center justify-center rounded transition hover:bg-white/20"
          >
            <Minimize class="hidden size-4 group-data-active:block" />
            <Maximize class="block size-4 group-data-active:hidden" />
          </media-fullscreen-button>
        </div>
      </media-controls>
    </media-player>
  {/if}

  <!-- ── Error ─────────────────────────────────────────────────────────────── -->
  {#if error}
    <div
      class="pointer-events-none absolute inset-0 z-30 flex items-center justify-center"
    >
      <p class="rounded bg-black/60 px-4 py-2 text-sm text-red-400">{error}</p>
    </div>
  {/if}

  <!-- ── Buffering spinner (mid-playback) ──────────────────────────────────── -->
  {#if waiting && canPlay}
    <div
      class="pointer-events-none absolute inset-0 z-20 flex items-center justify-center"
    >
      <Spinner class="size-14" />
    </div>
  {/if}

  <!-- ── Loading screen ────────────────────────────────────────────────────── -->
  {#if !canPlay}
    <div
      class="pointer-events-none absolute inset-0 z-20 flex flex-col items-center justify-center transition-opacity duration-700"
    >
      {#if media?.poster_path}
        <div
          class="absolute inset-0 scale-110 bg-cover bg-center"
          style="background-image: url('{media.poster_path}'); filter: blur(40px); opacity: 0.4;"
        ></div>
      {/if}
      <div class="absolute inset-0 bg-black/60"></div>

      {#if logoUrl}
        <div class="relative z-10 grid place-items-center px-8 select-none">
          <img
            src={logoUrl}
            alt={title}
            class="col-start-1 row-start-1 max-h-32 max-w-sm object-contain opacity-20"
          />
          <img
            src={logoUrl}
            alt={title}
            class="col-start-1 row-start-1 max-h-32 max-w-sm object-contain transition-all duration-500"
            style="clip-path: inset(0 {100 - fakeProgress}% 0 0)"
          />
        </div>
      {:else if title}
        <div
          class="relative z-10 grid place-items-center px-8 text-center select-none"
        >
          <span
            class="col-start-1 row-start-1 block text-4xl font-bold tracking-widest text-white/20 md:text-6xl"
            >{title}</span
          >
          <span
            class="col-start-1 row-start-1 block overflow-hidden text-4xl font-bold tracking-widest text-white transition-all duration-500 md:text-6xl"
            style="clip-path: inset(0 {100 - fakeProgress}% 0 0)">{title}</span
          >
        </div>
      {:else}
        <Spinner class="relative z-10 size-14" />
      {/if}

      <p class="relative z-10 mt-6 text-sm text-white/50">{loadingMessage}</p>
    </div>
  {/if}
</div>

<style>
  /* Without this, the wrapper's own aspect ratio (which on an ultrawide
     monitor in full-screen mode can be much wider than 16:9) ends up
     dictating how the video gets scaled, cropping anything that doesn't
     happen to match it. Forcing `contain` makes it always letterbox /
     pillarbox to the source's real aspect ratio instead. !important
     because vidstack's own bundled theme CSS may otherwise win. */
  :global(media-player video) {
    object-fit: contain !important;
  }
</style>
