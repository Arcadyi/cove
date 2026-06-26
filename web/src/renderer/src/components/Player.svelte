<script lang="ts">
  import type { Media } from "$lib/types/tmdb";
  import { Spinner } from "$lib/components/ui/spinner";
  import * as Popover from "$lib/components/ui/popover";
  import * as Tooltip from "$lib/components/ui/tooltip";
  import { Button } from "$lib/components/ui/button";
  import { Slider } from "$lib/components/ui/slider";
  import {
    Play,
    Pause,
    Volume2,
    Volume1,
    VolumeX,
    Headphones,
    Captions,
    Check,
    Keyboard,
  } from "lucide-svelte";
  import { onDestroy } from "svelte";
  import { fade } from "svelte/transition";
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
  }: {
    src: string;
    media?: Media;
    externalSubtitles?: { id: string; url: string; lang: string }[];
    season?: number;
    episode?: number;
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

  // Track-menu open state. While any picker is open, keyboard shortcuts stand
  // down so the menu's own arrow-key navigation isn't hijacked.
  let audioOpen = $state(false);
  let subsOpen = $state(false);
  let helpOpen = $state(false);
  const menuOpen = $derived(audioOpen || subsOpen || helpOpen);

  // Scrubbing: while dragging the seek bar, show the dragged time and only issue
  // the real seek on release, so we don't spam mpv (costly on torrent sources).
  let scrubbing = $state(false);
  let scrubValue = $state(0);
  const displayPos = $derived(scrubbing ? scrubValue : Player.position);

  function toggleMute(): void {
    if (Player.volume > 0) {
      lastVolume = Player.volume;
      Player.setVolume(0);
      flash("Muted");
    } else {
      const v = lastVolume || 100;
      Player.setVolume(v);
      flash(`Volume ${Math.round(v)}%`);
    }
  }

  function onSeekChange(v: number): void {
    scrubbing = true;
    scrubValue = v;
  }
  function onSeekCommit(v: number): void {
    Player.seek(v);
    scrubbing = false;
  }
  function onVolumeChange(v: number): void {
    Player.setVolume(v);
  }

  function nudgeVolume(delta: number): void {
    const v = Math.max(0, Math.min(100, Math.round(Player.volume + delta)));
    Player.setVolume(v);
    flash(`Volume ${v}%`);
  }
  function nudgeSeek(delta: number): void {
    const target = Math.max(
            0,
            Math.min(Player.duration || Infinity, Player.position + delta),
    );
    Player.seek(target);
    flash(`${delta > 0 ? "+" : "−"}${Math.abs(delta)}s`);
  }
  function seekToFraction(frac: number): void {
    if (Player.duration) Player.seek(Player.duration * frac);
  }

  function toggleCaptions(): void {
    if (subSelection.kind !== "off") {
      selectSubtitle({ kind: "off" });
      flash("Subtitles off");
      return;
    }
    const emb = Player.subtitleTracks[0];
    if (emb) {
      selectSubtitle({ kind: "embedded", id: emb.id });
      flash("Subtitles on");
      return;
    }
    const ext = externalSubtitles[0];
    if (ext) {
      selectSubtitle({ kind: "external", id: ext.id });
      flash("Subtitles on");
    }
  }

  // ─── On-screen feedback flash (so keyboard actions register even when the
  //     control bar is hidden) ─────────────────────────────────────────────────
  let feedback = $state<string | null>(null);
  let feedbackTimer: ReturnType<typeof setTimeout> | undefined;
  function flash(text: string): void {
    feedback = text;
    clearTimeout(feedbackTimer);
    feedbackTimer = setTimeout(() => (feedback = null), 700);
  }
  onDestroy(() => clearTimeout(feedbackTimer));

  // ─── Keyboard shortcuts ──────────────────────────────────────────────────────

  function isTypingTarget(t: EventTarget | null): boolean {
    const el = t as HTMLElement | null;
    if (!el || !el.tagName) return false;
    return (
            el.tagName === "INPUT" ||
            el.tagName === "TEXTAREA" ||
            el.tagName === "SELECT" ||
            el.isContentEditable
    );
  }

  function onKey(e: KeyboardEvent): void {
    if (!Player.available || !Player.ready) return;
    // Don't steal keys from a focused field or an open picker menu.
    if (menuOpen || isTypingTarget(e.target)) return;
    if (e.ctrlKey || e.metaKey || e.altKey) return;

    let handled = true;
    switch (e.key) {
      case " ":
      case "k": {
        const willPause = !Player.paused;
        Player.togglePause();
        flash(willPause ? "Paused" : "Playing");
        break;
      }
      case "ArrowRight":
        nudgeSeek(5);
        break;
      case "ArrowLeft":
        nudgeSeek(-5);
        break;
      case "l":
        nudgeSeek(10);
        break;
      case "j":
        nudgeSeek(-10);
        break;
      case "ArrowUp":
        nudgeVolume(5);
        break;
      case "ArrowDown":
        nudgeVolume(-5);
        break;
      case "m":
        toggleMute();
        break;
      case "c":
        toggleCaptions();
        break;
      case "Home":
        Player.seek(0);
        break;
      case "End":
        if (Player.duration) Player.seek(Player.duration - 1);
        break;
      default:
        if (e.key >= "0" && e.key <= "9") seekToFraction(Number(e.key) / 10);
        else handled = false;
    }

    if (handled) {
      e.preventDefault();
      showControls();
    }
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

  const subtitleLabel = $derived.by(() => {
    // Capture into a const so the discriminated-union narrowing survives into
    // the .find() callbacks below (TS drops narrowing of a reassignable `let`
    // inside nested closures, but keeps it for a const).
    const sel = subSelection;
    if (sel.kind === "off") return "Subtitles";
    if (sel.kind === "embedded") {
      const t = Player.subtitleTracks.find((x) => x.id === sel.id);
      return t ? trackLabel(t, "Subtitle") : "Subtitles";
    }
    const e = externalSubtitles.find((x) => x.id === sel.id);
    return e ? langName(e.lang) : "Subtitles";
  });

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

<svelte:window onkeydown={onKey} />

{#snippet menuItem(label: string, active: boolean, onSelect: () => void)}
  <button
          type="button"
          onclick={onSelect}
          class="flex w-full items-center gap-2 rounded-sm px-2 py-1.5 text-left text-sm outline-none transition-colors hover:bg-accent hover:text-accent-foreground focus-visible:bg-accent focus-visible:text-accent-foreground {active
      ? 'font-medium'
      : ''}"
  >
    <span class="flex-1 truncate">{label}</span>
    {#if active}<Check class="size-4 shrink-0" />{/if}
  </button>
{/snippet}

{#snippet shortcut(label: string, keys: string)}
  <div class="flex items-center justify-between gap-4">
    <dt class="text-muted-foreground">{label}</dt>
    <dd>
      <kbd
              class="rounded border border-border bg-muted px-1.5 py-0.5 font-mono text-[11px] text-muted-foreground"
      >{keys}</kbd
      >
    </dd>
  </div>
{/snippet}

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

  <!-- ── Keyboard/action feedback flash ──────────────────────────────────────── -->
  {#if feedback}
    <div class="pointer-events-none absolute inset-0 z-20 grid place-items-center">
      <div
              class="rounded-full bg-black/70 px-4 py-2 text-sm font-medium text-white backdrop-blur-sm"
              transition:fade={{ duration: 150 }}
      >
        {feedback}
      </div>
    </div>
  {/if}

  <!-- ── Controls ───────────────────────────────────────────────────────────── -->
  {#if canPlay}
    <div
            class="absolute inset-0 z-10 flex flex-col justify-end bg-linear-to-t from-black/85 via-black/15 to-transparent transition-opacity duration-200 {controlsVisible ||
      Player.paused
        ? 'opacity-100'
        : 'pointer-events-none opacity-0'}"
    >
      <!-- svelte-ignore a11y_no_static_element_interactions -->
      <div
              class="flex w-full flex-col gap-2 px-4 pb-4 text-white"
              onclick={(e) => e.stopPropagation()}
      >
        <!-- Seek bar (full width) -->
        <Slider
                type="single"
                value={displayPos}
                max={Player.duration || 0}
                step={0.1}
                onValueChange={onSeekChange}
                onValueCommit={onSeekCommit}
                aria-label="Seek"
                class="w-full"
        />

        <!-- Transport + tracks -->
        <div class="flex items-center gap-1">
          <!-- Play / pause -->
          <Tooltip.Root>
            <Tooltip.Trigger>
              {#snippet child({ props })}
                <Button
                        {...props}
                        variant="ghost"
                        size="icon"
                        class="text-white hover:bg-white/15 hover:text-white"
                        onclick={() => Player.togglePause()}
                >
                  {#if Player.paused}
                    <Play class="size-5" />
                  {:else}
                    <Pause class="size-5" />
                  {/if}
                </Button>
              {/snippet}
            </Tooltip.Trigger>
            <Tooltip.Content>
              {Player.paused ? "Play" : "Pause"} · Space
            </Tooltip.Content>
          </Tooltip.Root>

          <!-- Volume: button + slider that expands on hover/focus -->
          <div class="group/vol flex items-center">
            <Tooltip.Root>
              <Tooltip.Trigger>
                {#snippet child({ props })}
                  <Button
                          {...props}
                          variant="ghost"
                          size="icon"
                          class="text-white hover:bg-white/15 hover:text-white"
                          onclick={toggleMute}
                  >
                    {#if Player.volume === 0}
                      <VolumeX class="size-5" />
                    {:else if Player.volume < 50}
                      <Volume1 class="size-5" />
                    {:else}
                      <Volume2 class="size-5" />
                    {/if}
                  </Button>
                {/snippet}
              </Tooltip.Trigger>
              <Tooltip.Content>
                {Player.volume === 0 ? "Unmute" : "Mute"} · M
              </Tooltip.Content>
            </Tooltip.Root>
            <div
                    class="ml-1 w-0 overflow-hidden opacity-0 transition-all duration-200 group-hover/vol:w-24 group-hover/vol:opacity-100 group-focus-within/vol:w-24 group-focus-within/vol:opacity-100"
            >
              <Slider
                      type="single"
                      value={Player.volume}
                      max={100}
                      step={1}
                      onValueChange={onVolumeChange}
                      aria-label="Volume"
                      class="w-24"
              />
            </div>
          </div>

          <span class="ml-2 text-xs tabular-nums text-white/80">
            {fmt(displayPos)}<span class="mx-1 text-white/40">/</span>{fmt(
                  Player.duration,
          )}
          </span>

          <div class="flex-1"></div>

          <!-- Torrent download progress (hash sources, mid-download) -->
          {#if isHash && torrent.progress > 0 && torrent.progress < 100}
            <span class="mr-1 text-xs tabular-nums text-white/60">
              ↓ {torrent.progress.toFixed(0)}%
            </span>
          {/if}

          <!-- Audio tracks -->
          {#if Player.audioTracks.length > 0}
            <Popover.Root bind:open={audioOpen}>
              <Popover.Trigger>
                {#snippet child({ props })}
                  <Button
                          {...props}
                          variant="ghost"
                          size="sm"
                          class="gap-1.5 text-white hover:bg-white/15 hover:text-white"
                  >
                    <Headphones class="size-4" />
                    <span class="max-w-28 truncate text-xs">
                      {selectedAudio?.title ||
                      langName(selectedAudio?.lang ?? "") ||
                      "Audio"}
                    </span>
                  </Button>
                {/snippet}
              </Popover.Trigger>
              <Popover.Content side="top" align="end" class="w-56 p-1">
                <p class="px-2 py-1.5 text-xs font-medium text-muted-foreground">
                  Audio
                </p>
                <div class="max-h-72 overflow-y-auto">
                  {#each sortedAudio as track (track.id)}
                    {@render menuItem(
                            trackLabel(track, "Audio"),
                            !!track.selected,
                            () => Player.setAudioTrack(track.id),
                    )}
                  {/each}
                </div>
              </Popover.Content>
            </Popover.Root>
          {/if}

          <!-- Subtitles -->
          {#if Player.subtitleTracks.length > 0 || externalSubtitles.length > 0}
            <Popover.Root bind:open={subsOpen}>
              <Popover.Trigger>
                {#snippet child({ props })}
                  <Button
                          {...props}
                          variant="ghost"
                          size="sm"
                          class="gap-1.5 text-white hover:bg-white/15 hover:text-white"
                  >
                    <Captions class="size-4" />
                    <span class="max-w-28 truncate text-xs">{subtitleLabel}</span>
                  </Button>
                {/snippet}
              </Popover.Trigger>
              <Popover.Content side="top" align="end" class="w-60 p-1">
                <p class="px-2 py-1.5 text-xs font-medium text-muted-foreground">
                  Subtitles
                </p>
                <div class="max-h-72 overflow-y-auto">
                  {@render menuItem("Off", subSelection.kind === "off", () =>
                          selectSubtitle({ kind: "off" }),
                  )}
                  {#each subtitleGroups as group (group.label)}
                    <p
                            class="px-2 pt-2 pb-1 text-[11px] font-medium tracking-wide text-muted-foreground/70 uppercase"
                    >
                      {group.label}
                    </p>
                    {#each group.items as item (item.key)}
                      {@render menuItem(
                              item.label,
                              (subSelection.kind === "embedded" &&
                                      item.kind === "embedded" &&
                                      subSelection.id === item.id) ||
                              (subSelection.kind === "external" &&
                                      item.kind === "external" &&
                                      subSelection.id === item.id),
                              () =>
                                      item.kind === "embedded"
                                              ? selectSubtitle({ kind: "embedded", id: item.id })
                                              : selectSubtitle({ kind: "external", id: item.id }),
                      )}
                    {/each}
                  {/each}
                </div>
              </Popover.Content>
            </Popover.Root>
          {/if}

          <!-- Keyboard shortcuts -->
          <Popover.Root bind:open={helpOpen}>
            <Popover.Trigger>
              {#snippet child({ props })}
                <Button
                        {...props}
                        variant="ghost"
                        size="icon"
                        class="text-white hover:bg-white/15 hover:text-white"
                        aria-label="Keyboard shortcuts"
                >
                  <Keyboard class="size-4" />
                </Button>
              {/snippet}
            </Popover.Trigger>
            <Popover.Content side="top" align="end" class="w-64 p-3">
              <p class="mb-2 text-xs font-medium text-muted-foreground">
                Keyboard shortcuts
              </p>
              <dl class="space-y-1.5 text-sm">
                {@render shortcut("Play / pause", "Space")}
                {@render shortcut("Seek ±5s", "← →")}
                {@render shortcut("Seek ±10s", "J L")}
                {@render shortcut("Volume", "↑ ↓")}
                {@render shortcut("Mute", "M")}
                {@render shortcut("Subtitles", "C")}
                {@render shortcut("Jump to 0–90%", "0–9")}
              </dl>
            </Popover.Content>
          </Popover.Root>
        </div>
      </div>
    </div>
  {/if}

  <!-- ── Loading screen ─────────────────────────────────────────────────────── -->
  {#if Player.available && !canPlay}
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