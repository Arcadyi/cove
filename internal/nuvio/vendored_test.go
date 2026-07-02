package nuvio

import (
	"context"
	"testing"
	"time"
)

// TestRunScraper_VendoredCryptoJS and TestRunScraper_VendoredCheerio cover
// the real-world gap reported against production: roughly half of scrapers
// in a real-world plugin collection require("crypto-js") and/or
// require("cheerio-without-node-native"), neither of which goja provides,
// so every such scraper failed with "Invalid module". These exercise the
// vendored bundles through the actual runScraper path (registry + globals
// wired up exactly as production does it), not a standalone harness.
func TestRunScraper_VendoredCryptoJS(t *testing.T) {
	code := `
		function getStreams() {
			var CryptoJS = require("crypto-js");
			var encrypted = CryptoJS.AES.encrypt("hello world", "secret").toString();
			var decrypted = CryptoJS.AES.decrypt(encrypted, "secret").toString(CryptoJS.enc.Utf8);
			var ok = decrypted === "hello world";
			return [{ name: ok ? "PASS" : "FAIL", title: decrypted, url: "http://example.com/x" }];
		}
		module.exports = { getStreams };
	`
	streams, err := runScraper(context.Background(), "test-cryptojs", code, 5*time.Second, 1, "movie", "Test", 2020, "tt0000001", nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(streams) != 1 || streams[0].Name != "PASS" {
		t.Fatalf("crypto-js round-trip failed: %+v", streams)
	}
}

func TestRunScraper_VendoredCheerio(t *testing.T) {
	code := `
		function getStreams() {
			var cheerio = require("cheerio-without-node-native");
			var $ = cheerio.load('<html><body><div class="foo">Hello Cheerio</div></body></html>');
			var text = $('.foo').text();
			var ok = text === "Hello Cheerio";
			return [{ name: ok ? "PASS" : "FAIL", title: text, url: "http://example.com/x" }];
		}
		module.exports = { getStreams };
	`
	streams, err := runScraper(context.Background(), "test-cheerio", code, 5*time.Second, 1, "movie", "Test", 2020, "tt0000001", nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(streams) != 1 || streams[0].Name != "PASS" {
		t.Fatalf("cheerio extraction failed: %+v", streams)
	}
}

// TestRunScraper_VendoredModulesIsolated confirms that requiring a vendored
// module from within a scraper doesn't clobber the scraper's own top-level
// module/exports globals — a real risk given both are wired up as function
// parameters in the same runtime rather than global state, but worth locking
// in given how easy it would be to regress into using vm.Set("module", ...)
// globally instead.
func TestRunScraper_VendoredModulesIsolated(t *testing.T) {
	code := `
		require("crypto-js");
		require("cheerio-without-node-native");
		function getStreams() {
			return [{ name: "n", title: "t", url: "http://example.com/after-requires" }];
		}
		module.exports = { getStreams };
	`
	streams, err := runScraper(context.Background(), "test-isolation", code, 5*time.Second, 1, "movie", "Test", 2020, "tt0000001", nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(streams) != 1 || streams[0].URL != "http://example.com/after-requires" {
		t.Fatalf("scraper's own module.exports was clobbered by vendored requires: %+v", streams)
	}
}
