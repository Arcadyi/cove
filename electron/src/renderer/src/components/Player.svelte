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
    Loader,
  } from "lucide-svelte";
  import { Button } from "$lib/components/ui/button";
  import * as Tooltip from "$lib/components/ui/tooltip";
  import * as Popover from "$lib/components/ui/popover";
  import type { Media } from "$lib/types/tmdb";

  let { src, media, imdbId }: { src: string; media?: Media; imdbId?: string } =
    $props();

  const isHash = $derived(!src.startsWith("http"));
  const streamURL = $derived(
    isHash
      ? `http://localhost:6969/api/play?hash=${src}`
      : `http://localhost:6969/api/play?url=${encodeURIComponent(src)}`,
  );

  const title = $derived(
    media ? (media.media_type === "tv" ? media.name : media.title) : "",
  );

  let videoEl = $state<HTMLVideoElement | null>(null);
  let containerEl = $state<HTMLElement | null>(null);

  let logoUrl = $state<string | null>(null);

  let playing = $state(false);
  let currentTime = $state(0);
  let duration = $state(0);
  let volume = $state(1);
  let muted = $state(false);
  let buffered = $state(0);
  let fullscreen = $state(false);
  let showControls = $state(true);
  let waiting = $state(false);
  let hasStarted = $state(false); // goes true once video has enough data to play
  let canPlay = $state(false);

  let torrentProgress = $state(0);
  let peers = $state(0);
  let speed = $state("0 B/s");

  $effect(() => {
    if (!imdbId) return;
    fetch(`http://localhost:6969/api/subtitles?imdb_id=${imdbId}`)
      .then((r) => r.json())
      .then(() => {})
      .catch((e) => console.error("Subtitles API error:", e));
  });

  // For HTTP streams, simulate a fake loading progress so the bar animates
  let fakeProgress = $state(0);

  $effect(() => {
    if (hasStarted) return;
    const progressReady = isHash
      ? torrentProgress >= 0.5
      : loadingProgress >= 100;
    if (canPlay && progressReady) {
      fakeProgress = 100; // make sure bar fills before transitioning
      setTimeout(() => {
        hasStarted = true;
      }, 800);
    }
  });

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

  const loadingProgress = $derived(isHash ? torrentProgress : fakeProgress);

  let controlsTimeout: ReturnType<typeof setTimeout>;

  function resetControlsTimer(): void {
    showControls = true;
    clearTimeout(controlsTimeout);
    controlsTimeout = setTimeout(() => {
      if (playing) showControls = false;
    }, 3000);
  }

  function togglePlay(): void {
    if (!videoEl) return;
    videoEl.paused ? videoEl.play() : videoEl.pause();
  }

  function seek(e: MouseEvent): void {
    if (!videoEl || !duration) return;
    const bar = e.currentTarget as HTMLElement;
    const rect = bar.getBoundingClientRect();
    videoEl.currentTime = ((e.clientX - rect.left) / rect.width) * duration;
  }

  function setVolume(e: MouseEvent): void {
    if (!videoEl) return;
    const bar = e.currentTarget as HTMLElement;
    const rect = bar.getBoundingClientRect();
    const pct = Math.max(0, Math.min(1, (e.clientX - rect.left) / rect.width));
    volume = pct;
    videoEl.volume = pct;
    muted = pct === 0;
  }

  function toggleMute(): void {
    if (!videoEl) return;
    muted = !muted;
    videoEl.muted = muted;
  }

  function skip(seconds: number): void {
    if (!videoEl) return;
    videoEl.currentTime = Math.max(
      0,
      Math.min(duration, currentTime + seconds),
    );
  }

  function toggleFullscreen(): void {
    if (!containerEl) return;
    if (!document.fullscreenElement) {
      containerEl.requestFullscreen();
      fullscreen = true;
    } else {
      document.exitFullscreen();
      fullscreen = false;
    }
  }

  function formatTime(s: number): string {
    const h = Math.floor(s / 3600);
    const m = Math.floor((s % 3600) / 60);
    const sec = Math.floor(s % 60);
    if (h > 0)
      return `${h}:${String(m).padStart(2, "0")}:${String(sec).padStart(2, "0")}`;
    return `${m}:${String(sec).padStart(2, "0")}`;
  }

  function onTimeUpdate(): void {
    if (!videoEl) return;
    currentTime = videoEl.currentTime;
    if (videoEl.buffered.length > 0) {
      buffered =
        (videoEl.buffered.end(videoEl.buffered.length - 1) / duration) * 100;
    }
  }

  $effect(() => {
    if (!isHash) return () => {};
    const interval = setInterval(async () => {
      const res = await fetch(`http://localhost:6969/api/progress?hash=${src}`);
      const d = await res.json();
      if (d.found) {
        torrentProgress = d.progress ?? 0;
        peers = d.peers ?? 0;
        speed = d.speed ?? "0 B/s";
      }
    }, 2000);
    return () => clearInterval(interval);
  });
</script>

<!-- svelte-ignore a11y_no_static_element_interactions -->
<div
  bind:this={containerEl}
  class="group relative h-full w-full overflow-hidden bg-black"
  onmousemove={resetControlsTimer}
  onclick={hasStarted ? togglePlay : undefined}
  onkeydown={(e) => hasStarted && e.key === " " && togglePlay()}
  role="button"
  tabindex="0"
>
  <video
    bind:this={videoEl}
    src={streamURL}
    crossorigin="anonymous"
    class="h-full w-full object-contain transition-opacity duration-700"
    style="opacity: {hasStarted ? 1 : 0}"
    autoplay
    onplay={() => (playing = true)}
    onpause={() => (playing = false)}
    ontimeupdate={onTimeUpdate}
    onloadedmetadata={() => {
      if (videoEl) duration = videoEl.duration;
    }}
    onwaiting={() => (waiting = true)}
    oncanplay={() => {
      waiting = false;
      canPlay = true;
    }}
  >
    <track kind="captions" src="" />
  </video>

  <!-- Loading screen -->
  {#if !hasStarted}
    <div
      class="absolute inset-0 flex flex-col items-center justify-center transition-opacity duration-700"
      style="opacity: {hasStarted ? 0 : 1}"
    >
      <!-- Blurred poster background -->
      {#if media?.poster_path}
        <div
          class="absolute inset-0 scale-110 bg-cover bg-center"
          style="background-image: url('{media.poster_path}'); filter: blur(40px); opacity: 0.4;"
        ></div>
      {/if}
      <div class="absolute inset-0 bg-black/60"></div>

      <!-- Title as progress bar -->
      {#if logoUrl}
        <div class="relative z-10 px-8 select-none">
          <!-- Grey unfilled logo -->
          <img
            src={logoUrl}
            alt={title}
            class="max-h-32 max-w-sm object-contain opacity-20"
          />
          <!-- Colored filled logo clipped left-to-right -->
          <img
            src={logoUrl}
            alt={title}
            class="absolute inset-0 max-h-32 max-w-sm object-contain transition-all duration-500"
            style="clip-path: inset(0 {100 - loadingProgress}% 0 0)"
          />
        </div>
      {:else if title}
        <!-- fallback to text if no logo found -->
        <div class="relative z-10 px-8 text-center select-none">
          <span
            class="block text-4xl font-bold tracking-widest text-white/20 md:text-6xl"
          >
            {title}
          </span>
          <span
            class="absolute inset-0 block overflow-hidden text-4xl font-bold tracking-widest text-white transition-all duration-500 md:text-6xl"
            style="clip-path: inset(0 {100 - loadingProgress}% 0 0)"
          >
            {title}
          </span>
        </div>
      {:else}
        <Loader class="relative z-10 size-12 animate-spin text-white/70" />
      {/if}

      <!-- Status text -->
      <div class="relative z-10 mt-6 text-sm text-white/50">
        {#if isHash}
          {#if peers > 0}
            Connecting · {peers} peers · {speed}
          {:else}
            Connecting to peers...
          {/if}
        {:else}
          Buffering...
        {/if}
      </div>
    </div>
  {/if}

  <!-- Buffering spinner (shown after initial load, when seeking etc) -->
  {#if waiting && hasStarted}
    <div
      class="pointer-events-none absolute inset-0 flex items-center justify-center"
    >
      <Loader class="size-12 animate-spin text-white/70" />
    </div>
  {/if}

  <!-- Controls overlay -->
  <div
    class="pointer-events-none absolute inset-0 flex flex-col justify-end bg-linear-to-t from-black/80 via-transparent to-transparent transition-opacity duration-300"
    style="opacity: {showControls && hasStarted ? 1 : 0}"
  >
    <!-- Seek bar -->
    <div class="pointer-events-auto px-4 pb-2">
      <button
        class="relative block h-1 w-full cursor-pointer rounded-full bg-white/20 transition-all hover:h-2"
        onclick={(e) => {
          e.stopPropagation();
          seek(e);
        }}
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
              skip(-10);
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
              togglePlay();
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
              skip(10);
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

      <Tooltip.Root>
        <Tooltip.Trigger>
          <Button
            variant="ghost"
            size="icon"
            class="text-white hover:bg-white/10 hover:text-white"
            onclick={(e) => {
              e.stopPropagation();
              toggleMute();
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
          setVolume(e);
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
              toggleFullscreen();
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
</div>
