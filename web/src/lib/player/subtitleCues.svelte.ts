// $lib/player/subtitleCues.svelte.ts
//
// Owns the subtitle *cue pipeline*: fetching a WebVTT track, parsing it, and
// tracking which cue is current as playback advances. Extracted from
// Player.svelte. Track-selection UI (which track is chosen, the language menu)
// and styling stay in the component — this controller only answers "what text
// should be on screen right now".

export interface Cue {
  start: number;
  end: number;
  text: string;
}

function parseVTTTime(ts: string): number {
  const parts = ts.trim().replace(/\r/g, "").replace(",", ".").split(":");
  if (parts.length === 3)
    return +parts[0] * 3600 + +parts[1] * 60 + parseFloat(parts[2]);
  return +parts[0] * 60 + parseFloat(parts[1]);
}

function parseVTT(raw: string): Cue[] {
  const normalized = raw.replace(/\r\n/g, "\n").replace(/\r/g, "\n");
  const cues: Cue[] = [];
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

export class SubtitleCues {
  cues = $state<Cue[]>([]);
  currentText = $state<string | null>(null);
  loading = $state(false);

  /** Reset to no track. */
  clear(): void {
    this.cues = [];
    this.currentText = null;
  }

  /** Fetch and parse a WebVTT track from a URL. */
  async load(url: string): Promise<void> {
    this.loading = true;
    this.cues = [];
    try {
      const res = await fetch(url);
      const raw = await res.text();
      this.cues = parseVTT(raw);
    } catch (e) {
      console.error("[subs] failed to load subtitles:", e);
      this.cues = [];
    } finally {
      this.loading = false;
    }
  }

  /**
   * Track the current cue against a video element. getOffsetMs supplies the
   * live timing offset (ms) so the user's subtitle-delay setting applies
   * without re-attaching the listener. Returns a cleanup function.
   */
  track(video: HTMLVideoElement, getOffsetMs: () => number): () => void {
    const onTimeUpdate = (): void => {
      if (!this.cues.length) {
        this.currentText = null;
        return;
      }
      const t = video.currentTime + getOffsetMs() / 1000;
      const cue = this.cues.find((c) => t >= c.start && t < c.end);
      this.currentText = cue?.text ?? null;
    };
    video.addEventListener("timeupdate", onTimeUpdate);
    return () => video.removeEventListener("timeupdate", onTimeUpdate);
  }
}
