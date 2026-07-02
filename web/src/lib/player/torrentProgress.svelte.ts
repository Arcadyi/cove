// $lib/player/torrentProgress.svelte.ts
//
// Tracks live torrent download stats (progress %, peer count, download speed)
// for a hash source via the backend's SSE progress stream.
//
// This is a .svelte.ts module so it can use runes ($state) in a plain class;
// the fields stay reactive when read from a component's template or $derived.

import { api } from "$lib/api";

export class TorrentProgress {
  progress = $state(0);
  peers = $state(0);
  speed = $state("0 B/s");
  // True once the stream has been given up on after repeated errors.
  // Consumers may read this to adjust their loading message; no UI does
  // today, so it's kept simple.
  stalled = $state(false);

  /**
   * Opens the SSE progress stream for a hash source and returns a cleanup
   * function that closes it. Designed to be returned straight from an $effect:
   *
   *   $effect(() => {
   *     if (!isHash) return () => {};
   *     return torrent.start(src);
   *   });
   */
  start(src: string): () => void {
    const es = new EventSource(api.progressStreamUrl(src));
    let consecutiveErrors = 0;

    es.onmessage = (e) => {
      consecutiveErrors = 0;
      try {
        const d = JSON.parse(e.data);
        if (d.found) {
          this.progress = d.progress ?? 0;
          this.peers = d.peers ?? 0;
          this.speed = d.speed ?? "0 B/s";
        }
      } catch {
        // ignore malformed frames
      }
    };
    // EventSource auto-reconnects through transient errors on its own, so a
    // single onerror isn't fatal — only give up (closing for good) after
    // several in a row, so a genuinely dead backend doesn't retry forever.
    es.onerror = () => {
      consecutiveErrors++;
      if (consecutiveErrors >= 5) {
        this.stalled = true;
        es.close();
      }
    };
    return () => es.close();
  }
}
