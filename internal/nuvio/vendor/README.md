# Vendored JS dependencies

Real-world Nuvio scrapers frequently `require()` a small set of npm packages
that assume a JS environment (Node, or React Native via Metro) rather than
the sandboxed goja runtime Cove embeds. Roughly half of the scrapers in
real-world community plugin collections depend on one or both of these two
packages, so they're vendored here as pre-bundled, dependency-free CommonJS
files and registered as native `require()` modules in `runtime.go`.

- `crypto-js.js` — vendored from `crypto-js@4.2.0`. Falls back to the
  `crypto` global for randomness (see `bindWebGlobals` in `weburl.go`); its
  `require("crypto")` fallback is unreachable in practice since that global is
  always present, but is left as-is (harmless, guarded by try/catch upstream).
- `cheerio-without-node-native.js` — vendored from
  `cheerio-without-node-native@0.20.2` (a cheerio build with its HTML-parsing
  dependency tree — `css-select`, `dom-serializer`, `entities`,
  `htmlparser2-without-node-native` — bundled in, built specifically for
  environments without Node's native modules, e.g. React Native/Hermes).
  Its `require("util")` fallback is similarly guarded by try/catch upstream
  and never actually needed.

## Regenerating

Both were produced with esbuild, bundling each package's own dependency tree
into one file with no external `require()`s left except the two Node
built-ins noted above (both already handled gracefully by the package's own
try/catch fallback code — verified empirically, not just assumed):

```sh
mkdir /tmp/vendor-bundle && cd /tmp/vendor-bundle
npm init -y
npm install crypto-js@4.2.0 cheerio-without-node-native@0.20.2
npx esbuild node_modules/crypto-js/index.js --bundle --platform=node --format=cjs --minify --outfile=crypto-js.js
npx esbuild node_modules/cheerio-without-node-native/index.js --bundle --platform=node --format=cjs --minify --outfile=cheerio-without-node-native.js
```

Then copy the two output files here, replacing the existing ones.
