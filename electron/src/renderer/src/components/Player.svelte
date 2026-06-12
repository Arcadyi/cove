<script lang="ts">
  import type { Media } from "$lib/types/tmdb";
  import { Spinner } from "$lib/components/ui/spinner";
  import VideoControls from "./VideoControls.svelte";

  let {
    src,
    media,
    externalSubtitles = [],
  }: {
    src: string;
    media?: Media;
    externalSubtitles?: { id: string; url: string; lang: string }[];
  } = $props();

  const isHash = $derived(!src.startsWith("http"));
  const streamURL = $derived(
    isHash
      ? `http://localhost:6969/api/play?hash=${src}`
      : `http://localhost:6969/api/play?url=${encodeURIComponent(src)}`,
  );

  export type SubtitleSettings = {
    size: number; // 50–200, font size %
    line: number; // 5–95, vertical position % from top
    background: boolean;
    offset: number; // seconds, can be negative
  };
  let subtitleSettings = $state<SubtitleSettings>({
    size: 100,
    line: 85,
    background: true,
    offset: 0,
  });

  $effect(() => {
    const el = document.createElement("style");
    el.textContent = `::cue {
    font-size: ${subtitleSettings.size}%;
    background-color: ${subtitleSettings.background ? "rgba(0,0,0,0.75)" : "transparent"};
  }`;
    document.head.appendChild(el);
    return () => el.remove();
  });

  const cueOriginalTimes = new WeakMap<TextTrackCue, { start: number; end: number }>();
  function applySubtitleAdjustments(track: TextTrack): void {
    const run = (): void => {
      if (!track.cues) return;
      Array.from(track.cues).forEach((cue) => {
        const v = cue as VTTCue;
        // Position
        v.snapToLines = false;
        v.line = subtitleSettings.line;
        // Time offset — store originals first
        if (!cueOriginalTimes.has(cue)) {
          cueOriginalTimes.set(cue, { start: v.startTime, end: v.endTime });
        }
        const orig = cueOriginalTimes.get(cue)!;
        v.startTime = Math.max(0, orig.start + subtitleSettings.offset);
        v.endTime   = Math.max(0, orig.end   + subtitleSettings.offset);
      });
    };
    run();
    setTimeout(run, 600); // cues load async from proxy
  }

  const title = $derived(
    media ? (media.media_type === "tv" ? media.name : media.title) : "",
  );

  let videoEl = $state<HTMLVideoElement | null>(null);
  let containerEl = $state<HTMLElement | null>(null);

  let logoUrl = $state<string | null>(null);

  let pipSupported = $derived(
    typeof document !== "undefined" && "pictureInPictureEnabled" in document,
  );

  let playing = $state(false);
  let currentTime = $state(0);
  let duration = $state(0);
  let volume = $state(1);
  let muted = $state(false);
  let buffered = $state(0);
  let fullscreen = $state(false);
  let showControls = $state(false);
  let waiting = $state(false);
  let canPlay = $state(false);
  let error = $state<string | null>(null);

  let playbackRate = $state(1);

  let subtitleTracks = $state<TextTrack[]>([]);
  let activeSubtitle = $state<string>("-1"); // -1 = off

  function setPlaybackRate(rate: number): void {
    if (!videoEl) return;
    playbackRate = rate;
    videoEl.playbackRate = rate;
  }

  async function togglePip(): Promise<void> {
    if (!videoEl) return;
    if (document.pictureInPictureElement) {
      await document.exitPictureInPicture();
    } else {
      await videoEl.requestPictureInPicture();
    }
  }

  $effect(() => {
    if (!videoEl) return () => {};

    const onTracksChange = (): void => {
      subtitleTracks = Array.from(videoEl!.textTracks).filter(
        (t) => t.kind === "subtitles" || t.kind === "captions",
      );
    };
    videoEl.textTracks.addEventListener("change", onTracksChange);
    videoEl.textTracks.addEventListener("addtrack", onTracksChange);
    return () => {
      videoEl?.textTracks.removeEventListener("change", onTracksChange);
      videoEl?.textTracks.removeEventListener("addtrack", onTracksChange);
    };
  });

  $effect(() => {
    if (!videoEl) return;
    const idx = Number(activeSubtitle);
    const { line, offset } = subtitleSettings;
    subtitleTracks.forEach((track, i) => {
      track.mode = i === idx ? "showing" : "disabled";
      if (i === idx) applySubtitleAdjustments(track);
    });
  });

  let torrentProgress = $state(0);
  let peers = $state(0);
  let speed = $state("0 B/s");

  // For HTTP streams, simulate a fake loading progress so the bar animates
  let fakeProgress = $state(0);

  $effect(() => {
    if (canPlay) {
      fakeProgress = 100;
      showControls = true;
      return () => {};
    }

    showControls = false;

    const interval = setInterval(() => {
      const target = 85;
      const distance = target - fakeProgress;
      const speed = fakeProgress < 40 ? 0.03 : 0.01;

      fakeProgress += distance * speed;
    }, 100);

    return () => clearInterval(interval);
  });

  const loadingProgress = $derived(fakeProgress);

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

  let controlsTimeout: ReturnType<typeof setTimeout>;

  function resetControlsTimer(): void {
    if (!canPlay) {
      return;
    }
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

  $effect(() => {
    const onFullscreenChange = (): void => {
      fullscreen = !!document.fullscreenElement;
    };
    document.addEventListener("fullscreenchange", onFullscreenChange);
    return () =>
      document.removeEventListener("fullscreenchange", onFullscreenChange);
  });

  function adjustVolume(delta: number): void {
    if (!videoEl) return;
    volume = Math.max(0, Math.min(1, volume + delta));
    videoEl.volume = volume;
    muted = volume === 0;
  }

  function handleKeydown(e: KeyboardEvent): void {
    if (!canPlay) return;
    switch (e.key) {
      case " ":
      case "k":
        e.preventDefault();
        togglePlay();
        break;
      case "ArrowRight":
        e.preventDefault();
        skip(10);
        break;
      case "ArrowLeft":
        e.preventDefault();
        skip(-10);
        break;
      case "ArrowUp":
        e.preventDefault();
        adjustVolume(0.1);
        break;
      case "ArrowDown":
        e.preventDefault();
        adjustVolume(-0.1);
        break;
      case "f":
      case "F":
        e.preventDefault();
        toggleFullscreen();
        break;
      case "m":
      case "M":
        e.preventDefault();
        toggleMute();
        break;
    }
  }

  $effect(() => {
    if (!src) {
      return;
    }
    error = null;
    canPlay = false;
    fakeProgress = 0;
  });
</script>

<!-- svelte-ignore a11y_no_static_element_interactions -->
<div
  bind:this={containerEl}
  class="group relative h-full w-full overflow-hidden bg-black"
  onmousemove={resetControlsTimer}
  onclick={canPlay ? togglePlay : undefined}
  onkeydown={handleKeydown}
  role="button"
  tabindex="0"
>
  <!-- svelte-ignore a11y_media_has_caption -->
  <video
    bind:this={videoEl}
    src={streamURL}
    crossorigin="anonymous"
    class="h-full w-full object-contain transition-opacity duration-700"
    style="opacity: {canPlay ? 1 : 0}"
    autoplay
    onplay={() => (playing = true)}
    onpause={() => (playing = false)}
    onerror={() => {
      error = "Failed to load stream.";
      canPlay = false;
    }}
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
    {#each externalSubtitles as sub (sub.id)}
      <track
        kind="subtitles"
        src={`http://localhost:6969/api/subtitle-proxy?url=${encodeURIComponent(sub.url)}`}
        srclang={sub.lang}
        label={sub.lang.toUpperCase()}
      />
    {/each}
  </video>

  {#if error}
    <div class="absolute inset-0 flex items-center justify-center">
      <p class="text-sm text-red-400">{error}</p>
    </div>
  {/if}

  <!-- Loading screen -->
  {#if !canPlay}
    <div
      class="absolute inset-0 flex flex-col items-center justify-center transition-opacity duration-700"
      style="opacity: 1"
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
            class="col-start-1 row-start-1 max-h-32 max-w-sm object-contain transition-all duration-500 {fakeProgress >
              40 && !canPlay
              ? 'animate-pulse'
              : ''}"
            style="clip-path: inset(0 {100 - loadingProgress}% 0 0)"
          />
        </div>
      {:else if title}
        <div
          class="relative z-10 grid place-items-center px-8 text-center select-none"
        >
          <span
            class="col-start-1 row-start-1 block text-4xl font-bold tracking-widest text-white/20 md:text-6xl"
          >
            {title}
          </span>
          <span
            class="col-start-1 row-start-1 block overflow-hidden text-4xl font-bold tracking-widest text-white transition-all duration-500 md:text-6xl"
            style="clip-path: inset(0 {100 - loadingProgress}% 0 0)"
          >
            {title}
          </span>
        </div>
      {:else}
        <Spinner class="size-14" />
      {/if}

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
  {#if waiting}
    <div
      class="pointer-events-none absolute inset-0 flex items-center justify-center"
    >
      <Spinner class="size-14" />
    </div>
  {/if}

  <!-- Controls row -->
  <VideoControls
    {playing}
    {currentTime}
    {duration}
    {volume}
    {muted}
    {buffered}
    {torrentProgress}
    {peers}
    {speed}
    {isHash}
    {fullscreen}
    showControls={showControls && canPlay}
    onTogglePlay={togglePlay}
    onSeek={seek}
    onSkip={skip}
    onToggleMute={toggleMute}
    onSetVolume={setVolume}
    onToggleFullscreen={toggleFullscreen}
    onResetControlsTimer={resetControlsTimer}
    {subtitleTracks}
    bind:activeSubtitle
    bind:subtitleSettings
    {playbackRate}
    onSetPlaybackRate={setPlaybackRate}
    {pipSupported}
    onTogglePip={togglePip}
  />
</div>
