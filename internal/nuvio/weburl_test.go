package nuvio

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"compress/zlib"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/andybalholm/brotli"
)

// TestRunScraper_CompressedResponses covers the exact real-world failure
// reported against production: a scraper hitting an origin that compresses
// its JSON response with gzip, brotli, or deflate despite the scraper
// setting its own Accept-Encoding header (which disables Go's transparent
// decompression). Previously only gzip was handled; brotli/deflate produced
// garbage bytes that broke JSON.parse with a confusing "invalid character"
// error instead of a clear decode failure or, ideally, a successful parse.
func TestRunScraper_CompressedResponses(t *testing.T) {
	// These exercise the decode path against a loopback httptest server, which
	// the production SSRF-safe transport refuses — swap in a plain transport
	// for the duration of the test.
	orig := scraperTransport
	scraperTransport = func() *http.Transport { return &http.Transport{} }
	t.Cleanup(func() { scraperTransport = orig })

	body := `{"ok":true,"streams":[{"name":"n","title":"t","url":"http://example.com/s.m3u8"}]}`

	cases := []struct {
		name     string
		encoding string
		encode   func(string) []byte
	}{
		{"gzip", "gzip", func(s string) []byte {
			var buf bytes.Buffer
			w := gzip.NewWriter(&buf)
			w.Write([]byte(s))
			w.Close()
			return buf.Bytes()
		}},
		{"brotli", "br", func(s string) []byte {
			var buf bytes.Buffer
			w := brotli.NewWriter(&buf)
			w.Write([]byte(s))
			w.Close()
			return buf.Bytes()
		}},
		{"deflate (zlib-wrapped)", "deflate", func(s string) []byte {
			var buf bytes.Buffer
			w := zlib.NewWriter(&buf)
			w.Write([]byte(s))
			w.Close()
			return buf.Bytes()
		}},
		{"deflate (raw)", "deflate", func(s string) []byte {
			var buf bytes.Buffer
			w, _ := flate.NewWriter(&buf, flate.DefaultCompression)
			w.Write([]byte(s))
			w.Close()
			return buf.Bytes()
		}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			encoded := c.encode(body)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Encoding", c.encoding)
				w.Write(encoded)
			}))
			defer srv.Close()

			code := `
				async function getStreams() {
					const res = await fetch("` + srv.URL + `", { headers: { "Accept-Encoding": "gzip, deflate, br" } });
					const data = await res.json();
					return data.streams;
				}
				module.exports = { getStreams };
			`
			streams, err := runScraper(context.Background(), "test-"+c.name, code, 5*time.Second, 1, "movie", "Test", 2020, "tt0000001", nil, nil)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(streams) != 1 || streams[0].URL != "http://example.com/s.m3u8" {
				t.Fatalf("unexpected streams: %+v", streams)
			}
		})
	}
}

// TestRunScraper_WebGlobals covers URL, URLSearchParams, and the `global`
// alias that real scrapers reference (e.g. Castle's `URLSearchParams is not
// defined`, mallumv/dvdplay's `global is not defined` from production logs)
// but which goja doesn't provide out of the box.
func TestRunScraper_WebGlobals(t *testing.T) {
	code := `
		function getStreams() {
			var results = [];

			var u = new URL("/path?a=1&b=2", "https://example.com");
			results.push(u.hostname === "example.com");
			results.push(u.pathname === "/path");
			results.push(u.searchParams.get("a") === "1");

			var p = new URLSearchParams("x=1&y=2");
			p.append("z", "3");
			results.push(p.get("x") === "1");
			results.push(p.get("z") === "3");
			results.push(p.has("y") === true);

			results.push(typeof global !== "undefined");

			results.push(btoa("hello") === "aGVsbG8=");
			results.push(atob("aGVsbG8=") === "hello");

			var ok = results.every(function (r) { return r === true; });
			return [{ name: ok ? "PASS" : "FAIL", title: JSON.stringify(results), url: "http://example.com/x" }];
		}
		module.exports = { getStreams };
	`
	streams, err := runScraper(context.Background(), "test-webglobals", code, 5*time.Second, 1, "movie", "Test", 2020, "tt0000001", nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(streams) != 1 {
		t.Fatalf("expected 1 result, got %d", len(streams))
	}
	if !strings.EqualFold(streams[0].Name, "PASS") {
		t.Fatalf("web globals check failed: %s", streams[0].Title)
	}
}
