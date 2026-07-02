package nuvio

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"compress/zlib"
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/andybalholm/brotli"
	"github.com/coveninja/cove/internal/utils"
	"github.com/dop251/goja"
	"github.com/dop251/goja_nodejs/console"
	"github.com/dop251/goja_nodejs/eventloop"
	"github.com/dop251/goja_nodejs/require"
)

// scraperConsolePrinter implements goja_nodejs/console.Printer. console.log
// (and its aliases info/debug) are scraper-side debug chatter that floods
// the app log with no operator value — only warn/error (something the
// scraper itself flagged as unexpected) are worth surfacing.
type scraperConsolePrinter struct {
	scraperID string
}

func (p scraperConsolePrinter) Log(string) {} // swallow console.log/info/debug
func (p scraperConsolePrinter) Warn(s string) {
	log.Println("[nuvio]", p.scraperID, "warn:", s)
}
func (p scraperConsolePrinter) Error(s string) {
	log.Println("[nuvio]", p.scraperID, "error:", s)
}

// scrapedStream is a stream as returned by scraper JS, before being mapped
// into addons.Stream.
type scrapedStream struct {
	Name    string            `json:"name"`
	Title   string            `json:"title"`
	Quality string            `json:"quality"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers"`
}

// invocationTimeout bounds a single scraper's total run time (script + all
// network calls it makes through the fetch shim).
const invocationTimeout = 15 * time.Second

// httpTimeout bounds a single fetch() call inside a scraper, independent of
// the overall invocation deadline, so a hung request is distinguishable in
// logs from a scraper that's just slow across many small requests.
const httpTimeout = 10 * time.Second

// maxFetchBody caps how much a scraper's fetch() will read, both raw and
// after decompression (a compressed body can inflate far past the raw cap).
// Scrapers fetch search pages and JSON APIs — 20 MiB is already generous.
const maxFetchBody = 20 << 20

// scraperTransport builds the HTTP transport backing scraper fetch(). In
// production it's the SSRF-safe transport, which refuses loopback/private
// addresses; tests that need to hit a local httptest server override it.
var scraperTransport = utils.SafeTransport

// runScraper executes one scraper's code against the given media info and
// returns whatever streams it produced. It never panics the caller: script
// errors, timeouts, and malformed output are all returned as an error.
// timeout is a parameter (rather than always the invocationTimeout constant)
// so tests can exercise the timeout/interrupt path without waiting out the
// real production timeout.
func runScraper(parent context.Context, scraperID, code string, timeout time.Duration, tmdbID int, mediaType, title string, year int, imdbID string, season, episode *int) ([]scrapedStream, error) {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	loop := eventloop.NewEventLoop()
	loop.Start()
	defer loop.Stop()

	type result struct {
		streams []scrapedStream
		err     error
	}
	done := make(chan result, 1)
	finished := make(chan struct{}) // closed once, right before the one send to done
	// sync.Once guards against a hostile scraper resolving twice — e.g. a
	// custom thenable whose then() invokes both callbacks. Without it the
	// second call would close(finished) twice and panic the process.
	var sendOnce sync.Once
	send := func(r result) {
		sendOnce.Do(func() {
			done <- r
			close(finished)
		})
	}

	// goja.Runtime.Interrupt is explicitly safe (and intended) to call from a
	// goroutine other than the one running the script — that's what lets this
	// watcher break a runaway/synchronous-infinite-loop scraper, which a
	// loop.RunOnLoop-queued interrupt could never do (the loop's single
	// goroutine would be stuck running that same script, never draining its
	// queue to reach the queued interrupt).
	vmReady := make(chan *goja.Runtime, 1)
	go func() {
		select {
		case vm := <-vmReady:
			select {
			case <-ctx.Done():
				vm.Interrupt("scraper timed out")
			case <-finished:
			}
		case <-finished:
		}
	}()

	loop.RunOnLoop(func(vm *goja.Runtime) {
		vmReady <- vm
		registry := new(require.Registry)
		registerVendoredModules(registry)
		// Scrapers log liberally via console.log for their own debugging —
		// that's noise at the level of an app that's supposed to just find a
		// stream, so only console.error/warn (something actually went wrong)
		// reach the process log. Registering our own printer as the native
		// "console" module takes precedence over goja_nodejs's default,
		// which would otherwise forward everything (including plain .log) to
		// Go's stdlib log.
		registry.RegisterNativeModule(console.ModuleName, console.RequireWithPrinter(scraperConsolePrinter{scraperID}))
		registry.Enable(vm)
		console.Enable(vm)
		bindGlobals(ctx, loop, vm, scraperID)

		module := vm.NewObject()
		exportsObj := vm.NewObject()
		module.Set("exports", exportsObj)
		vm.Set("module", module)
		vm.Set("exports", exportsObj)

		if _, err := vm.RunString(code); err != nil {
			send(result{err: fmt.Errorf("script error: %w", err)})
			return
		}

		exportsVal := module.Get("exports").ToObject(vm)
		entry, entryKind, err := findEntryPoint(exportsVal)
		if err != nil {
			send(result{err: err})
			return
		}

		var resultVal goja.Value
		if entryKind == "getStreams" {
			seasonArg, episodeArg := goja.Value(goja.Undefined()), goja.Value(goja.Undefined())
			if season != nil {
				seasonArg = vm.ToValue(*season)
			}
			if episode != nil {
				episodeArg = vm.ToValue(*episode)
			}
			resultVal, err = entry(exportsVal, vm.ToValue(tmdbID), vm.ToValue(mediaType), seasonArg, episodeArg)
		} else {
			metadata := vm.NewObject()
			metadata.Set("title", title)
			metadata.Set("year", year)
			metadata.Set("type", mediaType)
			metadata.Set("imdbId", imdbID)
			resultVal, err = entry(exportsVal, metadata, vm.NewObject())
		}
		if err != nil {
			send(result{err: fmt.Errorf("call error: %w", err)})
			return
		}

		thenFn, ok := goja.AssertFunction(resultVal.ToObject(vm).Get("then"))
		if !ok {
			// Some scrapers might resolve synchronously; accept that too.
			streams, err := exportStreams(resultVal)
			send(result{streams: streams, err: err})
			return
		}
		_, err = thenFn(resultVal,
			vm.ToValue(func(call goja.FunctionCall) goja.Value {
				streams, err := exportStreams(call.Argument(0))
				send(result{streams: streams, err: err})
				return goja.Undefined()
			}),
			vm.ToValue(func(call goja.FunctionCall) goja.Value {
				send(result{err: fmt.Errorf("scraper rejected: %v", call.Argument(0).Export())})
				return goja.Undefined()
			}),
		)
		if err != nil {
			send(result{err: fmt.Errorf("then error: %w", err)})
		}
	})

	select {
	case r := <-done:
		return r.streams, r.err
	case <-ctx.Done():
		return nil, fmt.Errorf("scraper %s timed out after %s", scraperID, timeout)
	}
}

// findEntryPoint tries getStreams first (the simple contract used by most
// real-world scrapers), then falls back to scrape (the fuller local-scraper
// spec). Neither present is a hard error, not a hang.
func findEntryPoint(exports *goja.Object) (goja.Callable, string, error) {
	if v := exports.Get("getStreams"); v != nil && !goja.IsUndefined(v) {
		if fn, ok := goja.AssertFunction(v); ok {
			return fn, "getStreams", nil
		}
	}
	if v := exports.Get("scrape"); v != nil && !goja.IsUndefined(v) {
		if fn, ok := goja.AssertFunction(v); ok {
			return fn, "scrape", nil
		}
	}
	return nil, "", fmt.Errorf("no getStreams or scrape export")
}

func exportStreams(v goja.Value) ([]scrapedStream, error) {
	if v == nil || goja.IsUndefined(v) || goja.IsNull(v) {
		return nil, nil
	}
	raw, ok := v.Export().([]interface{})
	if !ok {
		return nil, fmt.Errorf("unexpected result shape (not an array)")
	}
	streams := make([]scrapedStream, 0, len(raw))
	for _, item := range raw {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		s := scrapedStream{
			Name:    stringField(m, "name"),
			Title:   stringField(m, "title"),
			Quality: stringField(m, "quality"),
			URL:     stringField(m, "url"),
		}
		if s.URL == "" {
			continue // a stream with no URL is useless — drop it rather than surface a dead entry
		}
		if h, ok := m["headers"].(map[string]interface{}); ok {
			s.Headers = make(map[string]string, len(h))
			for k, v := range h {
				if sv, ok := v.(string); ok {
					s.Headers[k] = sv
				}
			}
		}
		streams = append(streams, s)
	}
	return streams, nil
}

func stringField(m map[string]interface{}, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

// bindGlobals installs the injected context real Nuvio scrapers expect:
// fetch/fetchWithTimeout, base64 helpers, logger, and a getRandomValues-backed
// crypto object (needed by scrapers that pull in crypto-js for AES work).
// Deliberately nothing else is bound — no filesystem, no process, no Go
// stdlib beyond what's shimmed here, which is the whole of this package's
// sandboxing story.
func bindGlobals(ctx context.Context, loop *eventloop.EventLoop, vm *goja.Runtime, scraperID string) {
	// The transport refuses loopback/private/link-local addresses so scraper
	// JS can't use fetch() to reach the local API or the user's LAN.
	client := &http.Client{Transport: scraperTransport(), Timeout: httpTimeout}
	bindFetch(ctx, loop, vm, client)
	bindWebGlobals(vm)

	vm.Set("logger", map[string]interface{}{
		// logger.log is scraper-side debug chatter, same as console.log —
		// silenced for the same reason (see scraperConsolePrinter).
		"log":   func(args ...interface{}) {},
		"error": func(args ...interface{}) { log.Println("[nuvio]", scraperID, "error:", args) },
	})
	vm.Set("base64Encode", stdBase64Encode)
	vm.Set("base64Decode", stdBase64Decode)

	cryptoObj := vm.NewObject()
	cryptoObj.Set("getRandomValues", func(call goja.FunctionCall) goja.Value {
		arr := call.Argument(0)
		obj := arr.ToObject(vm)
		length := obj.Get("length").ToInteger()
		buf := make([]byte, length)
		_, _ = rand.Read(buf)
		for i, b := range buf {
			obj.Set(fmt.Sprint(i), int(b))
		}
		return arr
	})
	vm.Set("crypto", cryptoObj)
}

// bindFetch installs a fetch() shim performing real HTTP requests on a
// goroutine, resolving/rejecting a goja Promise back on the event loop so
// goja values are only ever touched from the loop's own goroutine.
func bindFetch(ctx context.Context, loop *eventloop.EventLoop, vm *goja.Runtime, client *http.Client) {
	fetch := func(call goja.FunctionCall) goja.Value {
		url := call.Argument(0).String()
		method := "GET"
		var headers map[string]string
		var body string
		if len(call.Arguments) > 1 && !goja.IsUndefined(call.Argument(1)) && !goja.IsNull(call.Argument(1)) {
			opts := call.Argument(1).ToObject(vm)
			if m := opts.Get("method"); m != nil && !goja.IsUndefined(m) {
				method = m.String()
			}
			if h := opts.Get("headers"); h != nil && !goja.IsUndefined(h) && !goja.IsNull(h) {
				headers = map[string]string{}
				ho := h.ToObject(vm)
				for _, k := range ho.Keys() {
					headers[k] = ho.Get(k).String()
				}
			}
			if b := opts.Get("body"); b != nil && !goja.IsUndefined(b) {
				body = b.String()
			}
		}

		promise, resolve, reject := vm.NewPromise()

		if _, err := utils.ValidatePublicURL(url); err != nil {
			reject(newJSError(vm, err.Error()))
			return vm.ToValue(promise)
		}

		go func() {
			var reqBody io.Reader
			if body != "" {
				reqBody = bytes.NewBufferString(body)
			}
			req, err := http.NewRequestWithContext(ctx, method, url, reqBody)
			if err != nil {
				loop.RunOnLoop(func(vm *goja.Runtime) { reject(newJSError(vm, err.Error())) })
				return
			}
			for k, v := range headers {
				req.Header.Set(k, v)
			}
			resp, err := client.Do(req)
			if err != nil {
				loop.RunOnLoop(func(vm *goja.Runtime) { reject(newJSError(vm, err.Error())) })
				return
			}
			defer resp.Body.Close()

			// Go only auto-decompresses gzip when the request didn't set its own
			// Accept-Encoding — many scrapers set one explicitly (often offering
			// gzip/deflate/br), so decode by hand based on what the server chose.
			data, err := decodeBody(resp)
			if err != nil {
				loop.RunOnLoop(func(vm *goja.Runtime) { reject(newJSError(vm, err.Error())) })
				return
			}

			loop.RunOnLoop(func(vm *goja.Runtime) {
				resolve(newFetchResponse(vm, resp, data))
			})
		}()

		return vm.ToValue(promise)
	}
	vm.Set("fetch", fetch)
	vm.Set("fetchWithTimeout", func(call goja.FunctionCall) goja.Value {
		return fetch(call)
	})
}

// decodeBody reads and decompresses resp's body according to its
// Content-Encoding. Scrapers commonly set their own Accept-Encoding header
// (often "gzip, deflate, br"), which disables Go's automatic transparent
// decompression, so origins are free to respond with any of the three and
// this has to decode by hand. Buffering the raw body first (rather than
// wrapping resp.Body directly in a decoder) lets the ambiguous "deflate" case
// try zlib then fall back to raw flate against the same bytes, instead of
// corrupting the stream on a failed first attempt. A failed decode is
// returned as a real error rather than silently falling back to the raw
// (still-compressed) bytes — that previously surfaced as a confusing
// JSON.parse "unexpected character" error instead of the actual problem.
func decodeBody(resp *http.Response) ([]byte, error) {
	raw, err := readCapped(resp.Body)
	if err != nil {
		return nil, err
	}
	switch resp.Header.Get("Content-Encoding") {
	case "", "identity":
		return raw, nil
	case "gzip":
		gz, err := gzip.NewReader(bytes.NewReader(raw))
		if err != nil {
			return nil, fmt.Errorf("gzip decode: %w", err)
		}
		defer gz.Close()
		return readCapped(gz)
	case "br":
		data, err := readCapped(brotli.NewReader(bytes.NewReader(raw)))
		if err != nil {
			return nil, fmt.Errorf("brotli decode: %w", err)
		}
		return data, nil
	case "deflate":
		if zr, zerr := zlib.NewReader(bytes.NewReader(raw)); zerr == nil {
			defer zr.Close()
			if data, err := readCapped(zr); err == nil {
				return data, nil
			}
		}
		fr := flate.NewReader(bytes.NewReader(raw))
		defer fr.Close()
		data, err := readCapped(fr)
		if err != nil {
			return nil, fmt.Errorf("deflate decode: %w", err)
		}
		return data, nil
	default:
		return raw, nil
	}
}

// readCapped reads at most maxFetchBody bytes and errors (rather than
// silently truncating) when the source exceeds the cap — a truncated page
// would surface later as a baffling parse error inside the scraper.
func readCapped(r io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxFetchBody+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxFetchBody {
		return nil, fmt.Errorf("response exceeds %d byte limit", maxFetchBody)
	}
	return data, nil
}

func newFetchResponse(vm *goja.Runtime, resp *http.Response, data []byte) *goja.Object {
	respObj := vm.NewObject()
	respObj.Set("ok", resp.StatusCode >= 200 && resp.StatusCode < 300)
	respObj.Set("status", resp.StatusCode)
	text := string(data)
	respObj.Set("text", func(goja.FunctionCall) goja.Value {
		p, res, _ := vm.NewPromise()
		res(vm.ToValue(text))
		return vm.ToValue(p)
	})
	respObj.Set("json", func(goja.FunctionCall) goja.Value {
		p, res, rej := vm.NewPromise()
		// Reuse the runtime's own JSON.parse for identical parse semantics/errors
		// rather than hand-rolling a Go->goja marshal of arbitrary JSON.
		jsonObj := vm.Get("JSON").ToObject(vm)
		parseFn, _ := goja.AssertFunction(jsonObj.Get("parse"))
		v, err := parseFn(jsonObj, vm.ToValue(text))
		if err != nil {
			rej(newJSError(vm, err.Error()))
		} else {
			res(v)
		}
		return vm.ToValue(p)
	})
	headersObj := vm.NewObject()
	headersObj.Set("get", func(call goja.FunctionCall) goja.Value {
		return vm.ToValue(resp.Header.Get(call.Argument(0).String()))
	})
	respObj.Set("headers", headersObj)
	return respObj
}

func stdBase64Encode(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

func stdBase64Decode(s string) string {
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return ""
	}
	return string(b)
}

func newJSError(vm *goja.Runtime, msg string) goja.Value {
	errCtor, ok := goja.AssertFunction(vm.Get("Error"))
	if !ok {
		return vm.ToValue(msg)
	}
	v, err := errCtor(goja.Undefined(), vm.ToValue(msg))
	if err != nil {
		return vm.ToValue(msg)
	}
	return v
}
