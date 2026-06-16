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
  import { onDestroy } from "svelte";

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
  }: {
    src: string;
    media?: Media;
    externalSubtitles?: { id: string; url: string; lang: string }[];
  } = $props();

  let createdID = $state<string | null>(null);

  // ─── Audio tracks (from probe, used to decide HLS vs direct) ────────────────

  let audioTracks = $state<AudioTrackInfo[]>([]);
  let videoCodec = $state("");

  // ─── Built-in subtitle tracks (from probe) ───────────────────────────────────

  let builtInSubtitles = $state<
    { id: string; url: string; lang: string; label: string }[]
  >([]);

  // ─── Vidstack audio track list (populated from HLS manifest) ────────────────

  let vidstackAudioTracks = $state<
    { id: string; label: string; language: string; selected: boolean }[]
  >([]);

  // ─── DOM refs ───────────────────────────────────────────────────────────────

  let playerEl = $state<any>(null);

  // ─── Source derivations ─────────────────────────────────────────────────────

  const isHash = $derived(!src.startsWith("http"));

  const baseInput = $derived(
    isHash ? `http://localhost:6969/api/play?hash=${src}` : src,
  );

  const needsHLS = $derived(
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

  let hlsSessionID = $state<string | null>(null);
  let hlsLoading = $state(false); // true while waiting for POST /api/hls/start

  const activeStreamURL = $derived.by(() => {
    if (needsHLS && hlsSessionID) {
      return `http://localhost:6969/api/hls/${hlsSessionID}/master.m3u8`;
    }
    if (!needsHLS && audioTracks.length > 0) {
      // No ffmpeg needed, stream directly
      return isHash ? `http://localhost:6969/api/play?hash=${src}` : src;
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

  // ─── Other state ────────────────────────────────────────────────────────────

  let logoUrl = $state<string | null>(null);
  let torrentProgress = $state(0);
  let peers = $state(0);
  let torrentSpeed = $state("0 B/s");

  const title = $derived(
    media ? (media.media_type === "tv" ? media.name : media.title) : "",
  );

  // ─── Probe audio tracks ─────────────────────────────────────────────────────

  $effect(() => {
    if (!src) return () => {};
    audioTracks = [];
    videoCodec = "";
    builtInSubtitles = [];
    hlsSessionID = null;
    canPlay = false;
    error = null;

    const probeURL = isHash
      ? `http://localhost:6969/api/probe?hash=${src}`
      : `http://localhost:6969/api/probe?url=${encodeURIComponent(src)}`;

    const controller = new AbortController();
    fetch(probeURL, { signal: controller.signal })
      .then((r) => r.json())
      .then(
        (data: {
          audio: AudioTrackInfo[];
          subtitles: SubtitleTrackInfo[];
          videoCodec: string;
          duration: number;
        }) => {
          audioTracks = data.audio ?? [];
          videoCodec = data.videoCodec ?? "";
          probedDuration = data.duration ?? null;

          const base = isHash
            ? `http://localhost:6969/api/subtitle/extract?hash=${src}`
            : `http://localhost:6969/api/subtitle/extract?url=${encodeURIComponent(src)}`;

          builtInSubtitles = (data.subtitles ?? []).map((t) => ({
            id: `builtin-${t.index}`,
            url: `${base}&index=${t.index}`,
            lang: t.language || `track${t.index}`,
            label:
              t.title ||
              (t.language ? langName(t.language) : `Track ${t.index + 1}`),
          }));
        },
      )
      .catch(() => {
        audioTracks = [];
        builtInSubtitles = [];
        probedDuration = null;
      });

    return () => controller.abort();
  });

  // ─── Start HLS session when needed ──────────────────────────────────────────

  $effect(() => {
    if (!needsHLS || audioTracks.length === 0 || probedDuration === null)
      return;

    hlsLoading = true;
    hlsSessionID = null;

    const controller = new AbortController();

    fetch("http://localhost:6969/api/hls/start", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        input: baseInput,
        tracks: audioTracks,
        duration: probedDuration,
        videoCodec: videoCodec,
      }),
      signal: controller.signal,
    })
      .then((r) => r.json())
      .then((d: { sessionID: string }) => {
        createdID = d.sessionID;
        hlsSessionID = d.sessionID;
      })
      .catch((e) => {
        if ((e as DOMException).name === "AbortError") return;
        error = "Failed to start HLS session.";
        console.error(e);
      })
      .finally(() => {
        hlsLoading = false;
      });

    return () => {
      controller.abort();
      if (createdID) {
        fetch(`http://localhost:6969/api/hls/stop/${createdID}`, {
          method: "POST",
          headers: {
            "Content-Type": "application/x-www-form-urlencoded",
          },
          keepalive: true,
        }).catch((e) => {
          console.error("Failed for ID: " + createdID, e);
        });
      }
    };
  });

  // ─── Update Vidstack source when activeStreamURL changes ────────────────────

  $effect(() => {
    if (!playerEl) return () => {};

    function onProviderChange(e: any): void {
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

        e.detail.addEventListener?.("hls-instance", (ev: any) => {
          const hls = ev.detail;
          let mediaRecoveryAttempts = 0;

          hls.on(Hls.Events.ERROR, (_: any, data: any) => {
            if (!data.fatal) return; // hls.js handles non-fatal errors itself

            if (data.type === Hls.ErrorTypes.MEDIA_ERROR && mediaRecoveryAttempts < 3) {
              // Media errors (codec/buffer issues) — try swapAudioCodec on
              // the second attempt, which forces hls.js to reinitialise the
              // SourceBuffer with a different codec string.
              mediaRecoveryAttempts++;
              if (mediaRecoveryAttempts === 1) {
                console.warn("[hls] media error — attempting recoverMediaError");
                hls.recoverMediaError();
              } else {
                console.warn("[hls] media error — attempting swapAudioCodec + recoverMediaError");
                hls.swapAudioCodec();
                hls.recoverMediaError();
              }
            } else if (data.type === Hls.ErrorTypes.NETWORK_ERROR) {
              // Network errors — just resume loading; the segment server will
              // keep the connection alive until the segment is ready.
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

  // ─── Set player source ────────────────────────────────────────────────────

  $effect(() => {
    if (!playerEl || !activeStreamURL) return;
    canPlay = false;
    fakeProgress = 0;
    error = null;
    playerEl.src = activeStreamURL;
  });

  // ─── Vidstack event handlers ─────────────────────────────────────────────────

  function onCanPlay(): void {
    canPlay = true;
    waiting = false;
    fakeProgress = 100;
    syncAudioTracks();
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
    const tracks = playerEl.audioTracks as any[];
    if (!tracks) return;
    vidstackAudioTracks = Array.from(tracks).map((t: any) => ({
      id: t.id,
      label: t.label,
      language: t.language,
      selected: t.selected,
    }));
  }

  function switchAudioTrack(id: string): void {
    if (!playerEl) return;
    const tracks = playerEl.audioTracks as any[];
    if (!tracks) return;
    for (const t of Array.from(tracks)) {
      if ((t as any).id === id) (t as any).selected = true;
    }
    syncAudioTracks();
  }

  // ─── Logo fetch ──────────────────────────────────────────────────────────────

  $effect(() => {
    if (!media) return;
    fetch(
      `http://localhost:6969/api/logos?id=${media.id}&type=${media.media_type}`,
    )
      .then((r) => r.json())
      .then((logos: string[]) => {
        if (logos?.length) logoUrl = logos[0];
      });
  });

  // ─── Fake loading bar animation ───────────────────────────────────────────────

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
  // One persistent connection replaces per-tick HTTP requests.
  // EventSource auto-reconnects on its own; we just close it on cleanup.

  $effect(() => {
    if (!isHash) return () => {};

    const es = new EventSource(
      `http://localhost:6969/api/progress/stream?hash=${src}`,
    );

    es.onmessage = (e) => {
      try {
        const d = JSON.parse(e.data);
        if (d.found) {
          torrentProgress = d.progress ?? 0;
          peers = d.peers ?? 0;
          torrentSpeed = d.speed ?? "0 B/s";
        }
      } catch {
        // ignore malformed frames
      }
    };

    return () => es.close();
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
    if (hlsLoading) return "Preparing tracks…";
    if (needsHLS && !hlsSessionID) return "Starting stream…";
    if (isHash)
      return peers > 0
        ? `Connecting · ${peers} peers · ${torrentSpeed}`
        : "Connecting to peers…";
    return "Buffering…";
  });

  // ─── Custom subtitle renderer ─────────────────────────────────────────────────

  let selectedTrackId = $state<string | null>(null);
  let subtitleCues = $state<{ start: number; end: number; text: string }[]>([]);
  let currentCueText = $state<string | null>(null);
  let subtitleLoading = $state(false);

  // ─── Pre-warm subtitle cache (sequential) ────────────────────────────────────
  // Fetch built-in subtitles one at a time so we don't fire N simultaneous
  // requests and exhaust the browser's ~6 connection slots to localhost:6969.

  $effect(() => {
    if (builtInSubtitles.length === 0) return;
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

  let selectedLang = $state<string | null>(null);
  const tracksByLang = $derived.by(() => {
    const groups = new Map<
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

  function parseVTTTime(ts: string): number {
    const parts = ts.trim().replace(/\r/g, "").replace(",", ".").split(":");
    if (parts.length === 3)
      return +parts[0] * 3600 + +parts[1] * 60 + parseFloat(parts[2]);
    return +parts[0] * 60 + parseFloat(parts[1]);
  }

  function parseVTT(
    raw: string,
  ): { start: number; end: number; text: string }[] {
    const normalized = raw.replace(/\r\n/g, "\n").replace(/\r/g, "\n");
    const cues: { start: number; end: number; text: string }[] = [];
    for (const block of normalized.split(/\n\n+/)) {
      const lines = block.trim().split("\n");
      const ti = lines.findIndex((l) => l.includes("-->"));
      if (ti === -1) continue;
      const [startStr, endAndRest] = lines[ti].split("-->");
      const endStr = endAndRest.trim().split(/\s+/)[0];
      const text = lines
        .slice(ti + 1)
        .join("\n")
        .replace(/<[^>]+>/g, "")
        .trim();
      if (!text) continue;
      const start = parseVTTTime(startStr);
      const end = parseVTTTime(endStr);
      if (isNaN(start) || isNaN(end)) {
        console.warn("[subs] skipping cue with bad timestamps:", lines[ti]);
        continue;
      }
      cues.push({ start, end, text });
    }
    return cues;
  }

  async function loadSubtitleCues(url: string): Promise<void> {
    subtitleLoading = true;
    subtitleCues = [];
    try {
      const res = await fetch(url);
      const raw = await res.text();
      subtitleCues = parseVTT(raw);
    } catch (e) {
      console.error("[subs] failed to load subtitles:", e);
      subtitleCues = [];
    } finally {
      subtitleLoading = false;
    }
  }

  $effect(() => {
    if (!playerEl || !canPlay) return () => {};
    const video = playerEl.querySelector<HTMLVideoElement>("video");
    if (!video) return () => {};

    function onTimeUpdate() {
      if (!subtitleCues.length) {
        currentCueText = null;
        return;
      }
      const t = video!.currentTime + subtitleSettings.offset / 1000;
      const cue = subtitleCues.find((c) => t >= c.start && t < c.end);
      currentCueText = cue?.text ?? null;
    }

    video.addEventListener("timeupdate", onTimeUpdate);
    return () => video.removeEventListener("timeupdate", onTimeUpdate);
  });

  $effect(() => {
    src; // track dependency
    selectedTrackId = null;
    subtitleCues = [];
    currentCueText = null;
  });

  function selectTextTrack(id: string): void {
    selectedTrackId = id || null;
    if (!id) {
      subtitleCues = [];
      currentCueText = null;
      return;
    }
    const builtin = builtInSubtitles.find((s) => s.id === id);
    if (builtin) {
      loadSubtitleCues(builtin.url);
      return;
    }
    const ext = externalSubtitles.find((s) => s.id === id);
    if (ext) {
      loadSubtitleCues(
        `http://localhost:6969/api/subtitle-proxy?url=${encodeURIComponent(ext.url)}`,
      );
    }
  }

  onDestroy(() => {
    if (createdID) {
      fetch(`http://localhost:6969/api/hls/stop/${createdID}`, {
        method: "POST",
        headers: {
          "Content-Type": "application/x-www-form-urlencoded",
        },
        keepalive: true,
      }).catch((e) => {
        console.error("Failed for ID: " + createdID, e);
      });
    }
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
      {#if subtitleLoading}
        <div
          class="pointer-events-none absolute inset-x-0 bottom-16 z-20 flex justify-center"
        >
          <span class="rounded bg-black/70 px-3 py-1 text-sm text-white/70">
            Loading subtitles…
          </span>
        </div>
      {/if}
      {#if currentCueText}
        <div
          class="pointer-events-none absolute inset-x-0 z-20 flex flex-col items-center gap-0.5 px-6"
          style="bottom: {subtitleSettings.line}%"
        >
          {#each currentCueText.split("\n") as line (line)}
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
                      {@const active = tracks.some((t) => t.mode === "showing")}
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
                          <span class="size-1.5 shrink-0 rounded-full bg-white"
                          ></span>
                        {/if}
                      </button>
                    {/each}
                  {/if}
                </div>
              {/if}
            </Popover.Content>
          </Popover.Root>

          <!-- Torrent progress indicator -->
          {#if isHash && torrentProgress > 0 && torrentProgress < 100}
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
                    stroke-dasharray="{(torrentProgress / 100) * 31.4} 31.4"
                    stroke-linecap="round"
                    transform="rotate(-90 6 6)"
                    class="text-green-400 transition-all duration-500"
                  />
                </svg>
                <span class="text-xs">{torrentProgress.toFixed(0)}%</span>
              </Popover.Trigger>
              <Popover.Content class="w-52" side="top">
                <p class="mb-2 text-sm font-medium">Download Progress</p>
                <div class="mb-1 h-1.5 w-full rounded-full bg-secondary">
                  <div
                    class="h-full rounded-full bg-green-500 transition-all"
                    style="width: {torrentProgress}%"
                  ></div>
                </div>
                <div class="flex justify-between text-xs text-muted-foreground">
                  <span>{torrentProgress.toFixed(1)}%</span>
                  {#if peers > 0}<span>{peers} peers</span>{/if}
                </div>
                <div class="mt-1 text-xs text-muted-foreground">
                  ↓ {torrentSpeed}
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
