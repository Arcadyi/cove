<script lang="ts">
  import {
    Play,
    Pause,
    Volume2,
    VolumeX,
    Maximize,
    Minimize,
    SkipBack,
    SkipForward,
    SubtitlesIcon,
    ChevronLeft,
    ChevronRight,
    PictureInPicture2,
    Settings,
  } from "lucide-svelte";
  import { Button } from "$lib/components/ui/button";
  import * as Tooltip from "$lib/components/ui/tooltip";
  import * as Popover from "$lib/components/ui/popover";
  import { SvelteMap } from "svelte/reactivity";
  import { ScrollArea } from "$lib/components/ui/scroll-area";
  import type { SubtitleSettings } from "./Player.svelte"; // or inline the type

  let {
    playing,
    currentTime,
    duration,
    volume,
    muted,
    buffered,
    torrentProgress = 0,
    peers = 0,
    speed = "0 B/s",
    isHash = false,
    fullscreen = false,
    showControls,
    onTogglePlay,
    onSeek,
    onSkip,
    onToggleMute,
    onSetVolume,
    onToggleFullscreen,
    onResetControlsTimer,
    subtitleTracks,
    activeSubtitle = $bindable<string>(),
    playbackRate,
    onSetPlaybackRate,
    pipSupported,
    onTogglePip,
    subtitleSettings = $bindable<SubtitleSettings>(),
  }: {
    playing: boolean;
    currentTime: number;
    duration: number;
    volume: number;
    muted: boolean;
    buffered: number;
    torrentProgress?: number;
    peers?: number;
    speed?: string;
    isHash?: boolean;
    fullscreen?: boolean;
    showControls: boolean;
    onTogglePlay: () => void;
    onSeek: (e: MouseEvent) => void;
    onSkip: (seconds: number) => void;
    onToggleMute: () => void;
    onSetVolume: (e: MouseEvent) => void;
    onToggleFullscreen: () => void;
    onResetControlsTimer: () => void;
    subtitleTracks: TextTrack[];
    activeSubtitle: string;
    playbackRate: number;
    onSetPlaybackRate: (rate: number) => void;
    pipSupported: boolean;
    onTogglePip: () => Promise<void>;
    subtitleSettings: SubtitleSettings;
  } = $props();

  function formatTime(s: number): string {
    const h = Math.floor(s / 3600);
    const m = Math.floor((s % 3600) / 60);
    const sec = Math.floor(s % 60);
    if (h > 0)
      return `${h}:${String(m).padStart(2, "0")}:${String(sec).padStart(2, "0")}`;
    return `${m}:${String(sec).padStart(2, "0")}`;
  }

  // group tracks by language for the two-level menu
  const tracksByLang = $derived.by(() => {
    const map = new SvelteMap<string, { track: TextTrack; index: number }[]>();
    subtitleTracks.forEach((track, i) => {
      const lang = track.language || track.label || "unknown";
      if (!map.has(lang)) map.set(lang, []);
      map.get(lang)!.push({ track, index: i });
    });
    return map;
  });

  let subtitleOpen = $state(false);
  let selectedLang = $state<string | null>(null);

  type SubtitleView = "languages" | "tracks" | "settings";
  let subtitleView = $state<SubtitleView>("languages");

  function langName(code: string): string {
    try {
      return (
        new Intl.DisplayNames(["en"], { type: "language" }).of(code) ?? code
      );
    } catch {
      return code;
    }
  }

  let hoverTime = $state<number | null>(null);
  let hoverPct = $state(0);

  function onSeekHover(e: MouseEvent): void {
    const bar = e.currentTarget as HTMLElement;
    const rect = bar.getBoundingClientRect();
    hoverPct = Math.max(0, Math.min(1, (e.clientX - rect.left) / rect.width));
    hoverTime = hoverPct * duration;
  }
</script>

<div
  class="pointer-events-none absolute inset-0 flex flex-col justify-end bg-linear-to-t from-black/80 via-transparent to-transparent transition-opacity duration-300"
  style="opacity: {showControls ? 1 : 0}"
  onmousemove={onResetControlsTimer}
  role="presentation"
>
  <!-- Seek bar -->
  <div class="pointer-events-auto px-4 pb-2">
    <button
      class="group/seek relative block h-1 w-full cursor-pointer rounded-full bg-white/20 transition-all hover:h-2"
      onclick={(e) => {
        e.stopPropagation();
        onSeek(e);
      }}
      onmousemove={onSeekHover}
      onmouseleave={() => (hoverTime = null)}
      aria-label="Seek"
    >
      <span
        class="absolute inset-y-0 left-0 rounded-full bg-white/30"
        style="width: {buffered}%"
      ></span>
      <span
        class="absolute inset-y-0 left-0 rounded-full bg-white"
        style="width: {duration ? (currentTime / duration) * 100 : 0}%"
      ></span>
      {#if hoverTime !== null}
        <span
          class="pointer-events-none absolute -top-7 -translate-x-1/2 rounded bg-black/80 px-1.5 py-0.5 text-xs text-white"
          style="left: {hoverPct * 100}%"
        >
          {formatTime(hoverTime)}
        </span>
      {/if}
    </button>
  </div>

  <!-- Controls row -->
  <div class="pointer-events-auto flex items-center gap-2 px-4 pb-4">
    <Tooltip.Root>
      <Tooltip.Trigger>
        <Button
          variant="ghost"
          size="icon"
          class="text-white hover:bg-white/10 hover:text-white"
          onclick={(e) => {
            e.stopPropagation();
            onSkip(-10);
          }}
        >
          <SkipBack class="size-5" />
        </Button>
      </Tooltip.Trigger>
      <Tooltip.Content>Back 10s</Tooltip.Content>
    </Tooltip.Root>

    <Tooltip.Root>
      <Tooltip.Trigger>
        <Button
          variant="ghost"
          size="icon"
          class="text-white hover:bg-white/10 hover:text-white"
          onclick={(e) => {
            e.stopPropagation();
            onTogglePlay();
          }}
        >
          {#if playing}<Pause class="size-6" />{:else}<Play
              class="size-6"
            />{/if}
        </Button>
      </Tooltip.Trigger>
      <Tooltip.Content>{playing ? "Pause" : "Play"}</Tooltip.Content>
    </Tooltip.Root>

    <Tooltip.Root>
      <Tooltip.Trigger>
        <Button
          variant="ghost"
          size="icon"
          class="text-white hover:bg-white/10 hover:text-white"
          onclick={(e) => {
            e.stopPropagation();
            onSkip(10);
          }}
        >
          <SkipForward class="size-5" />
        </Button>
      </Tooltip.Trigger>
      <Tooltip.Content>Forward 10s</Tooltip.Content>
    </Tooltip.Root>

    <span class="text-xs text-white/80 tabular-nums">
      {formatTime(currentTime)} / {formatTime(duration)}
    </span>

    <div class="flex-1"></div>

    {#if isHash}
      <Popover.Root>
        <Popover.Trigger
          class="flex items-center gap-1.5 rounded-md px-2 py-1 text-white/70 transition-colors hover:bg-white/10 hover:text-white"
          onclick={(e) => e.stopPropagation()}
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
              class="h-full rounded-full bg-green-500 transition-all duration-500"
              style="width: {torrentProgress}%"
            ></div>
          </div>
          <div class="flex justify-between text-xs text-muted-foreground">
            <span>{torrentProgress.toFixed(1)}%</span>
            {#if peers > 0}<span>{peers} peers</span>{/if}
          </div>
          <div class="mt-1 text-xs text-muted-foreground">↓ {speed}</div>
        </Popover.Content>
      </Popover.Root>
    {/if}

    {#if pipSupported}
      <Tooltip.Root>
        <Tooltip.Trigger>
          <Button
            variant="ghost"
            size="icon"
            class="text-white hover:bg-white/10 hover:text-white"
            onclick={(e) => {
              e.stopPropagation();
              onTogglePip();
            }}
          >
            <PictureInPicture2 class="size-5" />
          </Button>
        </Tooltip.Trigger>
        <Tooltip.Content>Picture in picture</Tooltip.Content>
      </Tooltip.Root>
    {/if}

    <Popover.Root>
      <Popover.Trigger
        onclick={(e) => e.stopPropagation()}
        class="flex items-center justify-center rounded-md px-2 py-1 text-xs font-medium transition-colors hover:bg-white/10 {playbackRate !==
        1
          ? 'text-white'
          : 'text-white/50'}"
      >
        {playbackRate}x
      </Popover.Trigger>
      <Popover.Content side="top" class="w-24 p-1">
        {#each [0.5, 0.75, 1, 1.25, 1.5, 2] as rate (rate)}
          <button
            onclick={(e) => {
              e.stopPropagation();
              onSetPlaybackRate(rate);
            }}
            class="flex w-full items-center rounded px-3 py-1.5 text-sm transition-colors hover:bg-secondary {playbackRate ===
            rate
              ? 'font-semibold'
              : ''}"
          >
            {rate}x
          </button>
        {/each}
      </Popover.Content>
    </Popover.Root>

    {#if subtitleTracks.length > 0}
      <Popover.Root
        bind:open={subtitleOpen}
        onOpenChange={() => (selectedLang = null)}
      >
        <Popover.Trigger
          onclick={(e) => e.stopPropagation()}
          class="flex items-center justify-center rounded-md p-2 transition-colors hover:bg-white/10"
        >
          <SubtitlesIcon
            class="size-5 {activeSubtitle !== '-1'
              ? 'text-white'
              : 'text-white/50'}"
          />
        </Popover.Trigger>

        <Popover.Content side="top" class="w-52 p-0">
          <!-- Header -->
          <div
            class="flex items-center justify-between border-b border-border px-3 py-2"
          >
            <span class="text-xs font-medium text-muted-foreground">
              {subtitleView === "settings"
                ? "Subtitle Settings"
                : subtitleView === "tracks"
                  ? langName(selectedLang ?? "")
                  : "Subtitles"}
            </span>
            <button
              onclick={(e) => {
                e.stopPropagation();
                subtitleView =
                  subtitleView === "settings" ? "languages" : "settings";
              }}
              class="rounded p-0.5 transition-colors hover:bg-secondary {subtitleView ===
              'settings'
                ? 'text-foreground'
                : 'text-muted-foreground'}"
            >
              <Settings class="size-3.5" />
            </button>
          </div>

          {#if subtitleView === "settings"}
            <!-- Settings panel -->
            <div class="space-y-4 p-3">
              <!-- Font size -->
              <div class="space-y-1.5">
                <div class="flex justify-between text-xs">
                  <span class="text-muted-foreground">Font size</span>
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

              <!-- Vertical position -->
              <div class="space-y-1.5">
                <div class="flex justify-between text-xs">
                  <span class="text-muted-foreground">Position</span>
                  <span>{subtitleSettings.line}%</span>
                </div>
                <input
                  type="range"
                  min="5"
                  max="95"
                  step="5"
                  bind:value={subtitleSettings.line}
                  onclick={(e) => e.stopPropagation()}
                  class="w-full accent-white"
                />
              </div>

              <!-- Background toggle -->
              <div class="flex items-center justify-between text-xs">
                <span class="text-muted-foreground">Background</span>
                <button
                  onclick={(e) => {
                    e.stopPropagation();
                    subtitleSettings.background = !subtitleSettings.background;
                  }}
                  class="h-5 w-9 rounded-full transition-colors {subtitleSettings.background
                    ? 'bg-white'
                    : 'bg-white/20'}"
                  aria-label="background_toggle"
                >
                  <span
                    class="block size-4 translate-x-0.5 rounded-full bg-black transition-transform {subtitleSettings.background
                      ? 'translate-x-4'
                      : ''}"
                  ></span>
                </button>
              </div>

              <!-- Time offset -->
              <div class="space-y-1.5">
                <div class="flex justify-between text-xs">
                  <span class="text-muted-foreground">Time offset</span>
                  <span
                    class={subtitleSettings.offset !== 0
                      ? "text-yellow-400"
                      : ""}
                    >{subtitleSettings.offset > 0
                      ? "+"
                      : ""}{subtitleSettings.offset.toFixed(1)}s</span
                  >
                </div>
                <div class="flex items-center gap-2">
                  <button
                    onclick={(e) => {
                      e.stopPropagation();
                      subtitleSettings.offset = Math.max(
                        -30,
                        +(subtitleSettings.offset - 0.5).toFixed(1),
                      );
                    }}
                    class="flex h-6 w-6 items-center justify-center rounded bg-secondary text-sm hover:bg-secondary/80"
                    >−</button
                  >
                  <div class="h-1 flex-1 rounded-full bg-white/20">
                    <div
                      class="h-full rounded-full bg-yellow-400 transition-all"
                      style="width: {((subtitleSettings.offset + 30) / 60) *
                        100}%"
                    ></div>
                  </div>
                  <button
                    onclick={(e) => {
                      e.stopPropagation();
                      subtitleSettings.offset = Math.min(
                        30,
                        +(subtitleSettings.offset + 0.5).toFixed(1),
                      );
                    }}
                    class="flex h-6 w-6 items-center justify-center rounded bg-secondary text-sm hover:bg-secondary/80"
                    >+</button
                  >
                </div>
                {#if subtitleSettings.offset !== 0}
                  <button
                    onclick={(e) => {
                      e.stopPropagation();
                      subtitleSettings.offset = 0;
                    }}
                    class="w-full rounded py-0.5 text-center text-xs text-muted-foreground transition-colors hover:text-foreground"
                    >Reset</button
                  >
                {/if}
              </div>
            </div>
          {:else if subtitleView === "languages"}
            <ScrollArea class="h-64">
              <div class="p-1">
                <button
                  onclick={() => {
                    activeSubtitle = "-1";
                    subtitleOpen = false;
                  }}
                  class="flex w-full items-center rounded px-3 py-1.5 text-left text-sm transition-colors hover:bg-secondary {activeSubtitle ===
                  '-1'
                    ? 'font-semibold'
                    : ''}">Off</button
                >
                {#each [...tracksByLang] as [lang, tracks] (lang)}
                  <button
                    onclick={(e) => {
                      e.stopPropagation();
                      if (tracks.length === 1) {
                        activeSubtitle = tracks[0].index.toString();
                        subtitleOpen = false;
                      } else {
                        selectedLang = lang;
                        subtitleView = "tracks";
                      }
                    }}
                    class="flex w-full items-center justify-between rounded px-3 py-1.5 text-left text-sm transition-colors hover:bg-secondary {tracks.some(
                      (t) => t.index.toString() === activeSubtitle,
                    )
                      ? 'font-semibold'
                      : ''}"
                  >
                    <span>{langName(lang)}</span>
                    {#if tracks.length > 1}
                      <span
                        class="flex items-center gap-1 text-xs text-muted-foreground"
                      >
                        {tracks.length}<ChevronRight class="size-3.5" />
                      </span>
                    {/if}
                  </button>
                {/each}
              </div>
            </ScrollArea>
          {:else if subtitleView === "tracks"}
            <ScrollArea class="h-64">
              <div class="p-1">
                <button
                  onclick={(e) => {
                    e.stopPropagation();
                    subtitleView = "languages";
                    selectedLang = null;
                  }}
                  class="flex w-full items-center gap-1.5 rounded px-3 py-1.5 text-left text-sm text-muted-foreground transition-colors hover:bg-secondary"
                >
                  <ChevronLeft class="size-3.5" />{langName(selectedLang ?? "")}
                </button>
                <div class="my-1 h-px bg-border"></div>
                {#each tracksByLang.get(selectedLang ?? "") ?? [] as { index }, i (index)}
                  <button
                    onclick={(e) => {
                      e.stopPropagation();
                      activeSubtitle = index.toString();
                      subtitleOpen = false;
                    }}
                    class="flex w-full items-center rounded px-3 py-1.5 text-left text-sm transition-colors hover:bg-secondary {activeSubtitle ===
                    index.toString()
                      ? 'font-semibold'
                      : ''}">Track {i + 1}</button
                  >
                {/each}
              </div>
            </ScrollArea>
          {/if}
        </Popover.Content>
      </Popover.Root>
    {/if}

    <Tooltip.Root>
      <Tooltip.Trigger>
        <Button
          variant="ghost"
          size="icon"
          class="text-white hover:bg-white/10 hover:text-white"
          onclick={(e) => {
            e.stopPropagation();
            onToggleMute();
          }}
        >
          {#if muted || volume === 0}<VolumeX class="size-5" />{:else}<Volume2
              class="size-5"
            />{/if}
        </Button>
      </Tooltip.Trigger>
      <Tooltip.Content>{muted ? "Unmute" : "Mute"}</Tooltip.Content>
    </Tooltip.Root>

    <button
      class="relative block h-1 w-20 cursor-pointer rounded-full bg-white/20 transition-all hover:h-2"
      onclick={(e) => {
        e.stopPropagation();
        onSetVolume(e);
      }}
      aria-label="Volume"
    >
      <span
        class="absolute inset-y-0 left-0 rounded-full bg-white"
        style="width: {muted ? 0 : volume * 100}%"
      ></span>
    </button>

    <Tooltip.Root>
      <Tooltip.Trigger>
        <Button
          variant="ghost"
          size="icon"
          class="text-white hover:bg-white/10 hover:text-white"
          onclick={(e) => {
            e.stopPropagation();
            onToggleFullscreen();
          }}
        >
          {#if fullscreen}<Minimize class="size-5" />{:else}<Maximize
              class="size-5"
            />{/if}
        </Button>
      </Tooltip.Trigger>
      <Tooltip.Content
        >{fullscreen ? "Exit fullscreen" : "Fullscreen"}</Tooltip.Content
      >
    </Tooltip.Root>
  </div>
</div>
