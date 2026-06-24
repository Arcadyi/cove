# Cove build orchestration.
#
#   make            # build everything (Go backend, web frontend, Qt shell)
#   make run        # build everything, then launch the shell
#   make dev        # regenerate TS types, build everything, launch the shell
#   make go|web|qt  # build a single component
#   make web-dev    # Vite dev server (browser only — no mpv bridge)
#   make clean      # remove build artifacts

GO_BIN    := cove
WEB_DIR   := web
QT_DIR    := qt
QT_BUILD  := $(QT_DIR)/build
SHELL_BIN := $(QT_BUILD)/cove_shell

.PHONY: all build run dev go web qt qt-configure generate web-dev shell clean

all: build

## Build all three components.
build: go web qt

## Go backend binary (repo root). Static build — no cgo.
go:
	CGO_ENABLED=0 go build -o $(GO_BIN) .

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
## NOTE: update tygo.yaml's output path from electron/src/... to web/src/...
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

## Remove build artifacts.
clean:
	rm -f $(GO_BIN)
	rm -rf $(WEB_DIR)/dist
	rm -rf $(QT_BUILD)
