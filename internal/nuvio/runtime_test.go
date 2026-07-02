package nuvio

import (
	"context"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"
	"time"
)

// TestRunScraper_InfiniteLoop exercises the timeout/interrupt path against a
// scraper stuck in a synchronous infinite loop — the case a
// loop.RunOnLoop-queued interrupt could never break, since the loop's single
// goroutine never gets free to process its queue. This is what actually
// makes "run untrusted third-party JS safely" true, so it needs a real test,
// not just happy-path scrapers that happen to finish quickly.
func TestRunScraper_InfiniteLoop(t *testing.T) {
	code := `
		function getStreams(tmdbId, mediaType, season, episode) {
			while (true) {}
			return [];
		}
		module.exports = { getStreams };
	`
	start := time.Now()
	_, err := runScraper(context.Background(), "test-infinite-loop", code, 500*time.Millisecond, 1, "movie", "Test", 2020, "tt0000001", nil, nil)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected a timeout error, got nil")
	}
	if elapsed > 3*time.Second {
		t.Fatalf("runScraper took %s to return after a 500ms timeout — interrupt did not break the loop promptly", elapsed)
	}
	t.Logf("returned after %s with error: %v", elapsed, err)
}

// TestRunScraper_HangingFetch exercises the timeout path when the scraper is
// blocked on network I/O (a fetch() to an endpoint that never responds)
// rather than a synchronous loop — a different code path through the fetch
// shim's goroutine and the shared context.
func TestRunScraper_HangingFetch(t *testing.T) {
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-block // never respond within the test's lifetime
	}))
	// close(block) must run before srv.Close() (which blocks until in-flight
	// handlers return) — defers are LIFO, so declare this one second.
	defer func() {
		close(block)
		srv.Close()
	}()

	code := `
		async function getStreams(tmdbId, mediaType, season, episode) {
			const res = await fetch("` + srv.URL + `");
			const text = await res.text();
			return [{ name: "x", title: "x", url: "http://example.com/" + text }];
		}
		module.exports = { getStreams };
	`
	start := time.Now()
	_, err := runScraper(context.Background(), "test-hanging-fetch", code, 500*time.Millisecond, 1, "movie", "Test", 2020, "tt0000001", nil, nil)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected a timeout error, got nil")
	}
	if elapsed > 3*time.Second {
		t.Fatalf("runScraper took %s to return after a 500ms timeout — hanging fetch was not bounded", elapsed)
	}
	t.Logf("returned after %s with error: %v", elapsed, err)
}

// TestRunScraper_NoGoroutineLeak runs several timing-out scrapers back to
// back and confirms goroutine count settles back down — a leaked watcher
// goroutine or event loop per call would accumulate under sustained use.
func TestRunScraper_NoGoroutineLeak(t *testing.T) {
	code := `
		function getStreams() { while (true) {} }
		module.exports = { getStreams };
	`
	before := runtime.NumGoroutine()

	for i := 0; i < 10; i++ {
		_, err := runScraper(context.Background(), "test-leak", code, 200*time.Millisecond, 1, "movie", "Test", 2020, "tt0000001", nil, nil)
		if err == nil {
			t.Fatal("expected a timeout error, got nil")
		}
	}

	// Give any trailing goroutines a moment to actually exit.
	time.Sleep(300 * time.Millisecond)
	runtime.GC()
	after := runtime.NumGoroutine()

	if after > before+5 {
		t.Fatalf("goroutine count grew from %d to %d after 10 timed-out scrapers — likely a leak", before, after)
	}
	t.Logf("goroutines before=%d after=%d", before, after)
}

// TestRunScraper_HappyPath is a sanity check that a well-behaved
// synchronous-resolving scraper still works with the parameterized timeout.
func TestRunScraper_HappyPath(t *testing.T) {
	code := `
		function getStreams(tmdbId, mediaType, season, episode) {
			return Promise.resolve([{ name: "n", title: "t", url: "http://example.com/stream.m3u8" }]);
		}
		module.exports = { getStreams };
	`
	streams, err := runScraper(context.Background(), "test-happy", code, 5*time.Second, 1, "movie", "Test", 2020, "tt0000001", nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(streams) != 1 || streams[0].URL != "http://example.com/stream.m3u8" {
		t.Fatalf("unexpected streams: %+v", streams)
	}
}

// A hostile scraper returning a thenable whose then() invokes both callbacks
// (and twice) must not crash the process — send() is guarded by sync.Once.
// Before the fix the second resolution closed an already-closed channel and
// panicked the goja event-loop goroutine, taking down the whole backend.
func TestRunScraper_DoubleResolveDoesNotPanic(t *testing.T) {
	code := `
		var evil = {
			then: function (onF, onR) {
				onF([{ name: "a", title: "t", url: "http://example.com/a" }]);
				onF([{ name: "b", title: "t", url: "http://example.com/b" }]);
				onR("boom");
			}
		};
		function getStreams() { return evil; }
		module.exports = { getStreams };
	`
	// Must return without panicking; whichever resolution won is fine.
	if _, err := runScraper(context.Background(), "test-double", code, 5*time.Second, 1, "movie", "Test", 2020, "tt0000001", nil, nil); err != nil {
		_ = err // an error result is acceptable; only a panic would fail the test
	}
}

// fetch() to a loopback/private address must be refused by the safe dialer so
// scraper JS can't reach the local API or the user's LAN (SSRF).
func TestRunScraper_FetchLoopbackRefused(t *testing.T) {
	code := `
		function getStreams() {
			return fetch("http://127.0.0.1:6969/api/ping")
				.then(function () { return [{ name: "n", title: "t", url: "http://example.com/s" }]; })
				.catch(function () { return []; });
		}
		module.exports = { getStreams };
	`
	streams, err := runScraper(context.Background(), "test-ssrf", code, 5*time.Second, 1, "movie", "Test", 2020, "tt0000001", nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(streams) != 0 {
		t.Fatalf("expected loopback fetch to be refused (0 streams), got %+v", streams)
	}
}
