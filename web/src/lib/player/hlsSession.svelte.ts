// $lib/player/hlsSession.svelte.ts
//
// Owns the HLS transcoding-session lifecycle: POST /api/hls/start, hold the
// resulting session id and a loading flag, and tear the session down (abort the
// in-flight request + POST /api/hls/stop) on cleanup. Extracted from
// Player.svelte. The decision of *whether* HLS is needed (probe results,
// needsHLS) stays in the component — this just manages the session once asked.

import { api } from "$lib/api";

export interface HlsStartRequest {
  input: string;
  tracks: unknown[];
  duration: number;
  videoCodec: string;
}

export class HlsSession {
  sessionID = $state<string | null>(null);
  loading = $state(false); // true while waiting for POST /api/hls/start

  // The id actually created on the server, tracked separately from the public
  // sessionID so teardown can stop it even after sessionID is cleared.
  #createdID: string | null = null;

  /**
   * Start a session for the given request. Returns a cleanup function that
   * aborts the in-flight request and stops the created session — return it
   * straight from an $effect. onError is invoked (with a message) on a
   * non-abort failure so the component can surface it.
   */
  start(req: HlsStartRequest, onError?: (msg: string) => void): () => void {
    this.loading = true;
    this.sessionID = null;

    const controller = new AbortController();

    api
      .hlsStart(req, controller.signal)
      .then((d) => {
        this.#createdID = d.sessionID;
        this.sessionID = d.sessionID;
      })
      .catch((e) => {
        if ((e as DOMException).name === "AbortError") return;
        onError?.("Failed to start HLS session.");
        console.error(e);
      })
      .finally(() => {
        this.loading = false;
      });

    return () => {
      controller.abort();
      this.stop();
    };
  }

  /** Stop the created session (fire-and-forget). Safe to call repeatedly. */
  stop(): void {
    if (this.#createdID) {
      api.hlsStop(this.#createdID);
      this.#createdID = null;
    }
  }
}
