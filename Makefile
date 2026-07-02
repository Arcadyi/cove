# Cove build orchestration.
#
#   make            # build everything (Go backend, web frontend, Qt shell)
#   make run        # build everything, then launch the shell
#   make dev        # regenerate TS types, build everything, launch the shell
#   make go|web|qt  # build a single component
#   make web-dev    # Vite dev server (browser only — no mpv bridge)
#   make patch      # bump patch version, stage all pending changes, commit, tag
#                   # (optionally: make patch TITLE="..." MSG="..." to override the
#                   # commit title / add a commit message body note)
#                   # (then: git push origin master v<ver>)
#   make clean      # remove build artifacts

VERSION   := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
GO_BIN    := cove
WEB_DIR   := web
QT_DIR    := qt
QT_BUILD  := $(QT_DIR)/build
SHELL_BIN := $(QT_BUILD)/cove_shell

# Auto-detect injected private implementations and compose build tags.
# After `make inject-private`, client.go and/or discover.go are present
# and the real implementations compile in automatically.
_empty :=
_space := $(_empty) $(_empty)
, := ,
_PRIVATE_TAGS := $(strip \
  $(if $(wildcard internal/supabase/client.go),supabase) \
  $(if $(wildcard internal/discover/discover.go),discover))
_BUILD_TAGS := $(subst $(_space),$(,),$(_PRIVATE_TAGS))
_TAG_FLAGS  := $(if $(_BUILD_TAGS),-tags $(_BUILD_TAGS))

.PHONY: all build run dev go web qt qt-configure generate web-dev shell patch clean

all: build

## Build all three components.
build: go web qt

## Go backend binary (repo root). Static build — no cgo.
## Private build tags (supabase, discover) are added automatically when the
## corresponding implementation files are present (run `make inject-private` first).
go:
	CGO_ENABLED=0 go build $(_TAG_FLAGS) -ldflags "-X main.Version=$(VERSION)" -o $(GO_BIN) .

## Frontend → web/dist (Vite).
web:
	cd $(WEB_DIR) && npm run build

## Configure the Qt build dir. Run once, or after CMakeLists.txt changes.
qt-configure:
	cmake -S $(QT_DIR) -B $(QT_BUILD)

## Build the Qt shell, configuring the build dir first if it's missing.
qt:
	@test -d $(QT_BUILD) || cmake -S $(QT_DIR) -B $(QT_BUILD)
	cmake --build $(QT_BUILD)

## Regenerate TypeScript types from Go structs (tygo).
generate:
	tygo generate

## Build everything, then run the shell: it serves web/dist and spawns ./cove.
run: build
	$(SHELL_BIN) --backend ./$(GO_BIN) --webroot ./$(WEB_DIR)/dist

## Rebuild only the frontend and relaunch the shell (fast frontend iteration).
shell: web
	$(SHELL_BIN) --backend ./$(GO_BIN) --webroot ./$(WEB_DIR)/dist

## Full dev cycle: regenerate types, build all, launch.
dev: generate run

## Vite dev server in a browser. The mpv bridge is absent here, so the player
## shows "unavailable", but the rest of the UI works against the Go backend.
web-dev:
	cd $(WEB_DIR) && npm run dev

run-debug: build
	QTWEBENGINE_REMOTE_DEBUGGING=9222 $(SHELL_BIN) --backend ./$(GO_BIN) --webroot ./$(WEB_DIR)/dist

## Hot-reload frontend dev: Vite serves the UI in-window with HMR via the
## shell's --dev mode. Builds the backend + shell but NOT the frontend (Vite
## serves it live). Requires the stripCspInDev() plugin in vite.config.ts.
hot: go qt
	bash scripts/dev-hot.sh

## Same as `hot`, with QtWebEngine remote devtools on :9222 (open in a browser).
hot-debug: go qt
	QTWEBENGINE_REMOTE_DEBUGGING=9222 bash scripts/dev-hot.sh

## Bump patch version in web/package.json, stage all pending changes, commit,
## and tag for release. Pass TITLE="..." to override the default commit title
## and/or MSG="..." to add a commit message body note, e.g.
## `make patch TITLE="fix quick-play loading state"`.
## Then push with: git push origin master v<version>
patch:
	cd $(WEB_DIR) && npm version patch --no-git-tag-version
	@NEW_VER=$$(node -p "require('./$(WEB_DIR)/package.json').version"); \
	TITLE="$(if $(TITLE),$(TITLE),chore: bump version to v$$NEW_VER)"; \
	git add -A && \
	if [ -n "$(MSG)" ]; then \
		git commit -m "$$TITLE" -m "$(MSG)"; \
	else \
		git commit -m "$$TITLE"; \
	fi && \
	git tag "v$$NEW_VER" && \
	echo "" && \
	echo "  Tagged v$$NEW_VER — push with: git push origin master v$$NEW_VER"

## Pull private submodules and inject implementation files into internal/.
inject-private:
	git submodule update --init
	cp _private/cove-auth/*.go internal/supabase/
	cp _private/cove-discover/*.go internal/discover/

## Remove build artifacts.
clean:
	rm -f $(GO_BIN)
	rm -rf $(WEB_DIR)/dist
	rm -rf $(QT_BUILD)
