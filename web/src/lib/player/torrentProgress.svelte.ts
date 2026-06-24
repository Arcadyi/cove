// $lib/player/torrentProgress.svelte.ts
//
// Tracks live torrent download stats (progress %, peer count, download speed)
// for a hash source via the backend's SSE progress stream. Extracted from
// Player.svelte so the component no longer owns the EventSource plumbing — it
// just instantiates this and reads the reactive fields.
//
// This is a .svelte.ts module so it can use runes ($state) in a plain class;
// the fields stay reactive when read from a component's template or $derived.

import { api } from "$lib/api";

export class TorrentProgress {
  progress = $state(0);
  peers = $state(0);
  speed = $state("0 B/s");

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
    es.onmessage = (e) => {
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
    return () => es.close();
  }
}
