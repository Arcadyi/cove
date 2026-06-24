// Bridge client for the Qt/libmpv shell.
//
// Inside the Cove desktop shell, QtWebEngine injects `qt.webChannelTransport`
// and (via a document-creation user script) the global `QWebChannel`. This
// module connects to the `mpv` object registered on the C++ side exactly once
// and exposes it as reactive Svelte state plus a typed control API.
//
// In a plain browser (e.g. `vite dev` outside the shell) neither global exists;
// `available` stays false and every control is a no-op, so UI can branch on it.

export interface MpvTrack {
  id: number;
  type: "video" | "audio" | "sub";
  title: string;
  lang: string;
  selected: boolean;
}

// The injected globals have no shipped types; describe just what we touch.
interface QtSignal<A extends unknown[]> {
  connect(cb: (...args: A) => void): void;
  disconnect(cb: (...args: A) => void): void;
}

interface MpvBridge {
  positionChanged: QtSignal<[number]>;
  durationChanged: QtSignal<[number]>;
  pausedChanged: QtSignal<[boolean]>;
  volumeChanged: QtSignal<[number]>;
  fileLoaded: QtSignal<[]>;
  endReached: QtSignal<[]>;
  tracksChanged: QtSignal<[MpvTrack[]]>;

  play(url: string): void;
  pause(): void;
  resume(): void;
  stop(): void;
  seek(seconds: number): void;
  setAudioTrack(id: number): void;
  setSubtitleTrack(id: number): void;
  addSubtitle(url: string, title: string, lang: string): void;
  setVolume(volume: number): void;
}

declare global {
  interface Window {
    qt?: { webChannelTransport: unknown };
    QWebChannel?: new (
      transport: unknown,
      cb: (channel: { objects: { mpv: MpvBridge } }) => void,
    ) => void;
  }
}

class MpvPlayer {
  /** Running inside the Cove shell (the bridge globals are present). */
  available = $state(false);
  /** Channel handshake finished; controls are live. */
  ready = $state(false);

  position = $state(0); // seconds
  duration = $state(0); // seconds
  paused = $state(true);
  volume = $state(100); // 0–100
  ended = $state(false);

  audioTracks = $state<MpvTrack[]>([]);
  subtitleTracks = $state<MpvTrack[]>([]);

  #mpv: MpvBridge | null = null;
  #resolveReady!: () => void;
  /** Resolves once the bridge is connected; never resolves outside the shell. */
  readonly whenReady: Promise<void> = new Promise((r) => {
    this.#resolveReady = r;
  });

  constructor() {
    this.#connect();
  }

  #connect(): void {
    const transport = window.qt?.webChannelTransport;
    const Channel = window.QWebChannel;
    if (!transport || !Channel) return; // not inside the shell
    this.available = true;

    new Channel(transport, (channel) => {
      const mpv = channel.objects.mpv;
      this.#mpv = mpv;

      mpv.positionChanged.connect((s) => (this.position = s));
      mpv.durationChanged.connect((s) => (this.duration = s));
      mpv.pausedChanged.connect((p) => (this.paused = p));
      mpv.volumeChanged.connect((v) => (this.volume = v));
      mpv.fileLoaded.connect(() => (this.ended = false));
      mpv.endReached.connect(() => (this.ended = true));
      mpv.tracksChanged.connect((tracks) => this.#applyTracks(tracks));

      this.ready = true;
      this.#resolveReady();
    });
  }

  #applyTracks(tracks: MpvTrack[]): void {
    this.audioTracks = tracks.filter((t) => t.type === "audio");
    this.subtitleTracks = tracks.filter((t) => t.type === "sub");
  }

  // ─── Control API ────────────────────────────────────────────────────────────
  // Slots are fire-and-forget over the channel; reactive state updates arrive
  // back via the observed-property signals. Where it makes the UI feel instant
  // we also set the local state optimistically (it's overwritten by the signal).

  play(url: string): void {
    this.ended = false;
    this.position = 0;
    this.#mpv?.play(url);
  }

  pause(): void {
    this.#mpv?.pause();
  }

  resume(): void {
    this.#mpv?.resume();
  }

  togglePause(): void {
    if (this.paused) this.resume();
    else this.pause();
  }

  stop(): void {
    this.#mpv?.stop();
  }

  seek(seconds: number): void {
    const clamped = this.duration
      ? Math.max(0, Math.min(seconds, this.duration))
      : Math.max(0, seconds);
    this.position = clamped; // optimistic; positionChanged confirms
    this.#mpv?.seek(clamped);
  }

  setVolume(volume: number): void {
    const clamped = Math.max(0, Math.min(volume, 100));
    this.volume = clamped;
    this.#mpv?.setVolume(clamped);
  }

  /** mpv audio track id (from `audioTracks[].id`). */
  setAudioTrack(id: number): void {
    this.#mpv?.setAudioTrack(id);
  }

  /** mpv subtitle track id; pass a negative id to turn subtitles off. */
  setSubtitleTrack(id: number): void {
    this.#mpv?.setSubtitleTrack(id);
  }

  /** Load an external subtitle (e.g. OpenSubtitles URL) and select it. */
  addSubtitle(url: string, title = "", lang = ""): void {
    this.#mpv?.addSubtitle(url, title, lang);
  }
}

export const Player = new MpvPlayer();
