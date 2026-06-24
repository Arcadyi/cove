<script lang="ts">
  import type { Media } from "$lib/types/tmdb";
  import { Spinner } from "$lib/components/ui/spinner";
  import * as Popover from "$lib/components/ui/popover";
  import {
    Play,
    Pause,
    Volume2,
    VolumeOff,
    Headphones,
    Captions,
  } from "lucide-svelte";
  import { onDestroy } from "svelte";
  import { api } from "$lib/api";
  import { settings } from "$lib/stores/settings";
  import { Player } from "$lib/player/player.svelte";
  import {
    ProgressSaver,
    type ProgressContext,
  } from "$lib/player/progressSaver.svelte.js";
  import { TorrentProgress } from "$lib/player/torrentProgress.svelte.js";

  // ─── Props (unchanged from the old Player) ──────────────────────────────────

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
    // NOTE: in the Qt shell mpv renders to the whole window behind the web UI,
    // so true PiP confinement isn't wired yet — `compact` currently only affects
    // chrome. Proper compact playback is a follow-up (resize the mpv surface).
    compact?: boolean;
  } = $props();

  settings.load().catch(() => {});

  // ─── Playback lifecycle ─────────────────────────────────────────────────────

  // mpv plays the backend stream URL directly — no probe, no HLS, no transcode.
  // The Go backend still serves it (so torrent streaming keeps working); mpv
  // just consumes it over http with range requests for seeking.
  let appliedAudioDefault = false;
  let appliedSubDefault = false;
  const addedExternal = new Set<string>(); // external sub ids already sub-add'd

  $effect(() => {
    if (!src || !Player.available) return;
    appliedAudioDefault = false;
    appliedSubDefault = false;
    addedExternal.clear();
    subSelection = { kind: "off" };
    Player.play(api.playUrl(src));
  });

  // Stop playback when the player closes so video/audio don't keep running
  // behind the rest of the UI — and persist where we got to. Saving must never
  // prevent the stop, so it's guarded.
  onDestroy(() => {
    if (!Player.available) return;
    try {
      if (media && Player.duration > 0)
        progress.saveNow(
          Player.position,
          Player.duration,
          progressCtx,
          false,
        );
    } catch (e) {
      console.error(e);
    }
    Player.stop();
  });

  const canPlay = $derived(Player.ready && Player.duration > 0);

  // ─── Watch progress (mpv-driven) ─────────────────────────────────────────────

  const progress = new ProgressSaver();

  function progressCtx(): ProgressContext {
    return {
      tmdbId: media!.id,
      mediaType: media!.media_type,
      title,
      posterPath: media!.poster_path ?? "",
      voteAverage: media!.vote_average ?? 0,
      lastAirDate: (media as { last_air_date?: string }).last_air_date ?? "",
      season: season ?? null,
      episode: episode ?? null,
      probedDuration: null, // mpv reports the real duration
    };
  }

  // Load any saved position when the source changes.
  $effect(() => {
    if (!media || !src) return;
    progress.reset();
    if ($settings?.rememberPosition === false) return;
    progress.load(media.id, media.media_type, season ?? null, episode ?? null);
  });

  // Seek to it once, the first time playback is ready.
  $effect(() => {
    if (!canPlay) return;
    progress.resume((t) => Player.seek(t));
  });

  // Throttled save while playing (re-runs as position ticks).
  $effect(() => {
    const pos = Player.position;
    if (!canPlay || !media || Player.paused) return;
    progress.maybeSave(pos, Player.duration, progressCtx);
  });

  // Mark complete at end of file.
  $effect(() => {
    if (Player.ended && media)
      progress.saveNow(
        Player.duration,
        Player.duration,
        progressCtx,
        true,
      );
  });

  // ─── Torrent download progress (SSE, hash sources only) ──────────────────────

  const isHash = $derived(!src.startsWith("http"));
  const torrent = new TorrentProgress();

  $effect(() => {
    if (!isHash) return;
    return torrent.start(src);
  });

  const loadingMessage = $derived(
    isHash
      ? torrent.peers > 0
        ? `Connecting · ${torrent.peers} peers · ${torrent.speed}`
        : "Connecting to peers…"
      : "Buffering…",
  );

  // ─── Auto-select preferred audio track ──────────────────────────────────────

  $effect(() => {
    if (appliedAudioDefault || Player.audioTracks.length <= 1) return;
    const lang = $settings?.defaultAudioLang;
    if (!lang) return;
    appliedAudioDefault = true;
    const match = Player.audioTracks.find((t) => t.lang === lang);
    if (match && !match.selected) Player.setAudioTrack(match.id);
  });

  // ─── Auto-select preferred subtitle track ───────────────────────────────────
  // Gated on the file being loaded (duration > 0) so embedded tracks have had a
  // chance to populate before we choose between them and the external list.

  $effect(() => {
    if (appliedSubDefault || !canPlay) return;
    if (!$settings?.subtitlesEnabled) return;
    appliedSubDefault = true;
    const lang = $settings.defaultSubtitleLang;

    const embedded = Player.subtitleTracks.find((t) => t.lang === lang);
    if (embedded) {
      selectSubtitle({ kind: "embedded", id: embedded.id });
      return;
    }
    const ext =
      externalSubtitles.find((s) => s.lang === lang) ?? externalSubtitles[0];
    if (ext) selectSubtitle({ kind: "external", id: ext.id });
  });

  // ─── Controls state ─────────────────────────────────────────────────────────

  let lastVolume = $state(100);

  function toggleMute(): void {
    if (Player.volume > 0) {
      lastVolume = Player.volume;
      Player.setVolume(0);
    } else {
      Player.setVolume(lastVolume || 100);
    }
  }

  function onSeek(e: Event): void {
    const v = Number((e.target as HTMLInputElement).value);
    Player.seek(v);
  }

  function onVolume(e: Event): void {
    Player.setVolume(Number((e.target as HTMLInputElement).value));
  }

  // ─── Subtitle selection (embedded mpv tracks + lazy external) ────────────────

  type SubSel =
    | { kind: "off" }
    | { kind: "embedded"; id: number }
    | { kind: "external"; id: string };

  let subSelection = $state<SubSel>({ kind: "off" });

  function selectSubtitle(sel: SubSel): void {
    subSelection = sel;
    if (sel.kind === "off") {
      Player.setSubtitleTrack(-1);
      return;
    }
    if (sel.kind === "embedded") {
      Player.setSubtitleTrack(sel.id);
      return;
    }
    // External: add once (mpv selects it on add), then it lives as a track.
    const ext = externalSubtitles.find((s) => s.id === sel.id);
    if (!ext) return;
    if (addedExternal.has(ext.id)) {
      // already loaded — find the matching mpv track by language and select it
      const t = Player.subtitleTracks.find((x) => x.lang === ext.lang);
      if (t) Player.setSubtitleTrack(t.id);
    } else {
      addedExternal.add(ext.id);
      Player.addSubtitle(
        api.subtitleProxyUrl(ext.url),
        ext.lang.toUpperCase(),
        ext.lang,
      );
    }
  }

  // ─── Helpers ────────────────────────────────────────────────────────────────

  function langName(code: string): string {
    try {
      return (
        new Intl.DisplayNames(["en"], { type: "language" }).of(code) ?? code
      );
    } catch {
      return code;
    }
  }

  function fmt(t: number): string {
    if (!isFinite(t) || t < 0) t = 0;
    const h = Math.floor(t / 3600);
    const m = Math.floor((t % 3600) / 60);
    const s = Math.floor(t % 60);
    const mm = h ? String(m).padStart(2, "0") : String(m);
    return `${h ? h + ":" : ""}${mm}:${String(s).padStart(2, "0")}`;
  }

  // Best available human label for a track. mpv exposes whatever the container
  // tagged: prefer an explicit title, else the language name, else a numbered
  // fallback (some files ship untagged tracks — nothing to name them by).
  function trackLabel(
    t: { id: number; title: string; lang: string },
    kind: "Audio" | "Subtitle",
  ): string {
    if (t.title) return t.title;
    if (t.lang) return langName(t.lang);
    return `${kind} ${t.id}`;
  }

  // Sorted for stable, language-grouped menus (untagged → bottom by number).
  const sortedAudio = $derived(
    [...Player.audioTracks].sort((a, b) =>
      trackLabel(a, "Audio").localeCompare(trackLabel(b, "Audio")),
    ),
  );

  // Subtitle menu grouped by language: embedded mpv tracks + external
  // (OpenSubtitles) entries fall under their language; tracks with no language
  // tag land in "Other". Groups are sorted alphabetically with "Other" last.
  type SubMenuItem =
    | { kind: "embedded"; key: string; id: number; label: string }
    | { kind: "external"; key: string; id: string; label: string };

  const OTHER = "Other";

  const subtitleGroups = $derived.by(() => {
    const groups = new Map<string, SubMenuItem[]>();
    const push = (g: string, item: SubMenuItem) => {
      if (!groups.has(g)) groups.set(g, []);
      groups.get(g)!.push(item);
    };

    for (const t of Player.subtitleTracks) {
      const g = t.lang ? langName(t.lang) : t.title || OTHER;
      push(g, {
        kind: "embedded",
        key: `e${t.id}`,
        id: t.id,
        label: trackLabel(t, "Subtitle"),
      });
    }
    for (const s of externalSubtitles) {
      const g = s.lang ? langName(s.lang) : OTHER;
      push(g, {
        kind: "external",
        key: `x${s.id}`,
        id: s.id,
        label: `${langName(s.lang)} · OpenSubtitles`,
      });
    }

    return [...groups.entries()]
      .sort((a, b) =>
        a[0] === OTHER ? 1 : b[0] === OTHER ? -1 : a[0].localeCompare(b[0]),
      )
      .map(([label, items]) => ({ label, items }));
  });

  const title = $derived(
    media ? (media.media_type === "tv" ? media.name : media.title) : "",
  );

  const selectedAudio = $derived(
    Player.audioTracks.find((t) => t.selected),
  );

  // ─── Controls auto-hide ──────────────────────────────────────────────────────

  let controlsVisible = $state(true);
  let hideTimer: ReturnType<typeof setTimeout> | undefined;

  function showControls(): void {
    controlsVisible = true;
    clearTimeout(hideTimer);
    if (!Player.paused)
      hideTimer = setTimeout(() => (controlsVisible = false), 3000);
  }

  onDestroy(() => clearTimeout(hideTimer));
</script>

<!-- Root is transparent so mpv (rendered behind the WebEngineView) shows through.
     For this to reveal video, the page background and every ancestor down to the
     video region must also be transparent — see integration notes. -->
<!-- svelte-ignore a11y_no_static_element_interactions -->
<div
  class="relative h-full w-full overflow-hidden"
  onmousemove={showControls}
  onclick={() => Player.togglePause()}
>
  <!-- ── Bridge unavailable (running outside the Cove shell) ─────────────────── -->
  {#if !Player.available}
    <div class="absolute inset-0 z-30 grid place-items-center bg-black">
      <p class="rounded bg-black/60 px-4 py-2 text-sm text-red-400">
        Native player unavailable — run inside the Cove desktop app.
      </p>
    </div>
  {/if}

  <!-- ── Controls ───────────────────────────────────────────────────────────── -->
  {#if canPlay && !compact}
    <div
      class="absolute inset-0 z-10 flex flex-col justify-end bg-linear-to-t from-black/80 via-black/10 to-transparent transition-opacity duration-200 {controlsVisible
        ? 'opacity-100'
        : 'opacity-0'}"
    >
      <!-- svelte-ignore a11y_no_static_element_interactions -->
      <div
        class="flex w-full items-center gap-2 px-3 pb-3 text-white"
        onclick={(e) => e.stopPropagation()}
      >
        <button
          onclick={() => Player.togglePause()}
          class="flex size-8 shrink-0 cursor-pointer items-center justify-center rounded transition hover:bg-white/20"
        >
          {#if Player.paused}
            <Play class="size-4" />
          {:else}
            <Pause class="size-4" />
          {/if}
        </button>

        <button
          onclick={toggleMute}
          class="flex size-8 shrink-0 cursor-pointer items-center justify-center rounded transition hover:bg-white/20"
        >
          {#if Player.volume === 0}
            <VolumeOff class="size-4" />
          {:else}
            <Volume2 class="size-4" />
          {/if}
        </button>

        <input
          type="range"
          min="0"
          max="100"
          step="1"
          value={Player.volume}
          oninput={onVolume}
          class="h-1 w-20 cursor-pointer accent-white"
          aria-label="Volume"
        />

        <span class="text-sm tabular-nums">{fmt(Player.position)}</span>
        <span class="text-sm text-white/50">/</span>
        <span class="text-sm text-white/70 tabular-nums"
          >{fmt(Player.duration)}</span
        >

        <input
          type="range"
          min="0"
          max={Player.duration || 0}
          step="0.1"
          value={Player.position}
          oninput={onSeek}
          class="h-1 flex-1 cursor-pointer accent-white"
          aria-label="Seek"
        />

        <!-- Audio tracks -->
        {#if Player.audioTracks.length > 1}
          <Popover.Root>
            <Popover.Trigger
              onclick={(e) => e.stopPropagation()}
              class="flex items-center gap-1.5 rounded-md px-2 py-1 text-white/70 transition hover:bg-white/10 hover:text-white"
            >
              <Headphones class="size-4" />
              <span class="text-xs">
                {selectedAudio?.title ||
                  langName(selectedAudio?.lang ?? "") ||
                  "Audio"}
              </span>
            </Popover.Trigger>
            <Popover.Content side="top" class="w-52 p-1">
              {#each sortedAudio as track (track.id)}
                <button
                  onclick={() => Player.setAudioTrack(track.id)}
                  class="flex w-full items-center gap-2 rounded px-3 py-1.5 text-left text-sm transition hover:bg-secondary {track.selected
                    ? 'font-semibold'
                    : ''}"
                >
                  <span class="flex-1 truncate">
                    {trackLabel(track, "Audio")}
                  </span>
                  {#if track.selected}
                    <span class="size-1.5 shrink-0 rounded-full bg-white"></span>
                  {/if}
                </button>
              {/each}
            </Popover.Content>
          </Popover.Root>
        {/if}

        <!-- Subtitles -->
        {#if Player.subtitleTracks.length > 0 || externalSubtitles.length > 0}
          <Popover.Root>
            <Popover.Trigger
              onclick={(e) => e.stopPropagation()}
              class="flex items-center gap-1.5 rounded-md px-2 py-1 text-white/70 transition hover:bg-white/10 hover:text-white"
            >
              <Captions class="size-4" />
            </Popover.Trigger>
            <Popover.Content side="top" class="max-h-80 w-56 overflow-y-auto p-1">
              <button
                onclick={() => selectSubtitle({ kind: "off" })}
                class="flex w-full items-center rounded px-3 py-1.5 text-left text-sm transition hover:bg-secondary {subSelection.kind ===
                'off'
                  ? 'font-semibold'
                  : ''}"
              >
                Off
              </button>

              {#each subtitleGroups as group (group.label)}
                <div
                  class="px-3 pt-2 pb-1 text-xs font-medium text-muted-foreground"
                >
                  {group.label}
                </div>
                {#each group.items as item (item.key)}
                  <button
                    onclick={() =>
                      item.kind === "embedded"
                        ? selectSubtitle({ kind: "embedded", id: item.id })
                        : selectSubtitle({ kind: "external", id: item.id })}
                    class="flex w-full items-center gap-2 rounded px-3 py-1.5 text-left text-sm transition hover:bg-secondary {(subSelection.kind ===
                      'embedded' &&
                      item.kind === 'embedded' &&
                      subSelection.id === item.id) ||
                    (subSelection.kind === 'external' &&
                      item.kind === 'external' &&
                      subSelection.id === item.id)
                      ? 'font-semibold'
                      : ''}"
                  >
                    <span class="flex-1 truncate">{item.label}</span>
                  </button>
                {/each}
              {/each}
            </Popover.Content>
          </Popover.Root>
        {/if}

        <!-- Torrent download progress (hash sources, mid-download) -->
        {#if isHash && torrent.progress > 0 && torrent.progress < 100}
          <span class="text-xs text-white/60 tabular-nums">
            ↓ {torrent.progress.toFixed(0)}%
          </span>
        {/if}
      </div>
    </div>
  {/if}

  <!-- ── Compact / PiP: mpv fills the window behind the UI, so a small box can't
       confine the video yet. Show an opaque poster instead of a wrong slice. ── -->
  {#if Player.available && compact}
    <div class="absolute inset-0 z-10 bg-black">
      {#if media?.poster_path}
        <img
          src={media.poster_path}
          alt={title}
          class="h-full w-full object-cover opacity-50"
        />
      {/if}
      <div class="absolute inset-0 grid place-items-center">
        {#if Player.paused}
          <Play class="size-8 text-white/80" />
        {:else}
          <Pause class="size-8 text-white/80" />
        {/if}
      </div>
    </div>
  {/if}

  <!-- ── Loading screen ─────────────────────────────────────────────────────── -->
  {#if Player.available && !canPlay && !compact}
    <div class="absolute inset-0 z-20 flex flex-col items-center justify-center">
      {#if media?.poster_path}
        <div
          class="absolute inset-0 scale-110 bg-cover bg-center"
          style="background-image: url('{media.poster_path}'); filter: blur(40px); opacity: 0.4;"
        ></div>
      {/if}
      <div class="absolute inset-0 bg-black/70"></div>
      {#if title}
        <span
          class="relative z-10 px-8 text-center text-3xl font-bold tracking-widest text-white md:text-5xl"
          >{title}</span
        >
      {/if}
      <Spinner class="relative z-10 mt-6 size-10" />
      <p class="relative z-10 mt-4 text-sm text-white/50">{loadingMessage}</p>
    </div>
  {/if}
</div>
