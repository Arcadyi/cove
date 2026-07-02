# Contributing to Cove

Thanks for taking a look. This covers dev setup beyond the
[README](README.md)'s quick-start, what to expect around the proprietary
submodules, and the conventions this codebase already follows.

## Dev setup

Follow the README's "Build from source" section for prerequisites and the
first `make run`. A few things specific to iterating on the code:

- `make hot` is the tightest loop: it builds the Go backend and Qt shell, then
  runs Vite with HMR served in-window via the shell's `--dev` mode — frontend
  changes apply live without a full rebuild. `make hot-debug` adds QtWebEngine
  remote devtools on `:9222`.
- `make web-dev` runs a browser-only Vite dev server with no Qt shell at all.
  The player will show "unavailable" (no `QWebChannel`/mpv bridge exists in a
  plain browser), but everything else — search, library, settings, addons —
  works against the real Go backend.
- After changing any Go struct in `internal/tmdb`, `internal/addons`,
  `internal/player`, `internal/settings`, `internal/library`, or
  `internal/nuvio`, run `make generate` (wraps `tygo generate`) to regenerate
  the mirrored TypeScript types in `web/src/lib/types/*.ts`. **Never hand-edit
  those generated files** — they're overwritten on the next `make generate`
  and the files themselves say so at the top.
- `internal/nuvio` embeds pure-Go dependencies (`goja`, `goja_nodejs`,
  `andybalholm/brotli`) for its sandboxed JS runtime, so the OSS build stays
  `CGO_ENABLED=0` with no Node/V8 dependency. Keep any new dependency for that
  package pure Go for the same reason.

## Working without submodule access

`internal/discover` and `internal/supabase` each have two implementations,
switched at compile time by build tag: an open-source stub (`noop.go`,
compiled by default) and a proprietary implementation pulled from a private
git submodule via `make inject-private`. See
[ARCHITECTURE.md](ARCHITECTURE.md#the-ossproprietary-split) for the full
mechanism.

If you don't have access to `_private/cove-auth`/`_private/cove-discover`,
that's fine — `git submodule update --init` will simply fail or leave those
directories empty, and a plain `make run`/`make go` (no `inject-private`
step) builds and runs the full OSS experience: no personalization beyond a
user-configured custom algorithm URL, and `/api/auth/*` returns `503`. This
is the environment to develop and test against for any change to shared code
(`internal/library`, `internal/settings`, `internal/tmdb`,
`internal/player`, `internal/addons`, the frontend). If a change needs to
touch the proprietary side (e.g. a new field the discovery engine should
read), coordinate on making the corresponding `noop.go` change in the same
PR so both builds keep working.

## Code style

- Comments should explain **why**, not what — the codebase leans on
  descriptive naming for the "what" and reserves comments for non-obvious
  constraints, workarounds, or the reasoning behind a magic number. Look at
  `internal/player/player.go` or `internal/tmdb/tmdb.go` for the tone to
  match.
- Every backend package that registers HTTP routes does so via a
  `SetupHandlers(mux *http.ServeMux)` method (or, for a couple of simpler
  packages, a package-level `SetupHandlers(mux, ...)` function) called once
  from `main.go`. Keep new routes consistent with that pattern rather than
  wiring `http.HandleFunc` calls elsewhere.
- `web/src/lib/api.ts` is the single point of contact with the backend from
  the frontend — never construct a backend URL anywhere else. If you're
  adding a new endpoint, add its method there alongside the existing ones for
  that package's routes.
- Go doc comments: every package should have a `// Package x ...` comment
  explaining its purpose and any non-obvious constraint (see
  `internal/clientsession/clientsession.go` for the bar to hit — a couple of
  sentences that explain *why* the package exists, not just what it's
  called).

## Testing

```sh
go build ./...                      # OSS build
go build -tags discover ./...       # with the proprietary discovery engine, if you have submodule access
go test ./...                       # OSS-build tests
go test -tags discover ./...        # proprietary-build tests only run under this tag
```

```sh
cd web
npm run check   # svelte-check (types)
npm run lint    # eslint
npm run format  # prettier — run before committing frontend changes
```

There's no CI lint/test gate on pull requests yet (`.github/workflows/release.yml`
only handles tagged releases), so these are on you to run locally before
opening a PR.

## Before opening a PR

- Run the build/test/lint commands above for whatever you touched.
- If you changed a Go struct consumed by the frontend, confirm you also ran
  `make generate` and committed the regenerated `.ts` files.
- Keep the scope focused — this repo doesn't have issue/PR templates yet, so
  a clear description of *why* the change is needed (not just what changed)
  in the PR body goes a long way.
