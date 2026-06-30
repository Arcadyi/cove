package updater

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coveninja/cove/internal/utils"
)

// RestartExitCode is the exit code the Go backend uses to signal the Qt shell
// to restart (picking up new binaries and web assets). The shell connects to
// the backend's finished signal and calls QProcess::startDetached on this code.
const RestartExitCode = 42

// assetName is the filename this build looks for in GitHub release assets.
// The CI packaging job MUST emit an asset with exactly this name or check()
// silently returns available=false. Both sides are pinned here as the source
// of truth — keep in sync with the "Package" jobs in release.yml.
func assetName() string {
	var ext string
	switch runtime.GOOS {
	case "windows":
		ext = ".zip"
	default:
		ext = ".tar.gz"
	}
	return fmt.Sprintf("cove-%s-%s%s", runtime.GOOS, runtime.GOARCH, ext)
}

// pendingURL is set by check() and consumed by applyUpdate(). Storing it
// server-side avoids accepting a client-supplied download URL (SSRF risk).
var (
	mu         sync.Mutex
	pendingURL string
)

// CheckResult is the JSON payload returned by GET /api/update/check.
type CheckResult struct {
	Available      bool   `json:"available"`
	CurrentVersion string `json:"current_version"`
	LatestVersion  string `json:"latest_version"`
	ReleaseName    string `json:"release_name"`
}

type ghRelease struct {
	TagName string    `json:"tag_name"`
	Name    string    `json:"name"`
	Assets  []ghAsset `json:"assets"`
}

type ghAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// newerThan returns true when latest is strictly greater than current using
// numeric field comparison so "v0.10.0" > "v0.9.0" resolves correctly.
func newerThan(latest, current string) bool {
	parse := func(v string) [3]int {
		v = strings.TrimPrefix(v, "v")
		parts := strings.SplitN(v, ".", 3)
		var n [3]int
		for i, p := range parts {
			if i >= 3 {
				break
			}
			// Strip any pre-release suffix (e.g. "0-rc1" → 0).
			n[i], _ = strconv.Atoi(strings.SplitN(p, "-", 2)[0])
		}
		return n
	}
	l, c := parse(latest), parse(current)
	for i := range l {
		if l[i] != c[i] {
			return l[i] > c[i]
		}
	}
	return false
}

// isCleanSemver returns true for "vX.Y.Z" with no extra suffixes.
// Only clean release builds should ever prompt for updates.
func isCleanSemver(v string) bool {
	v = strings.TrimPrefix(v, "v")
	parts := strings.SplitN(v, ".", 3)
	if len(parts) != 3 {
		return false
	}
	for _, p := range parts {
		if _, err := strconv.Atoi(p); err != nil {
			return false
		}
	}
	return true
}

func fetchLatest() (*ghRelease, error) {
	url := "https://api.github.com/repos/coveninja/cove/releases/latest"
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil // no releases published yet
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned %d", resp.StatusCode)
	}

	var rel ghRelease
	return &rel, json.NewDecoder(resp.Body).Decode(&rel)
}

func check(currentVersion string) (*CheckResult, error) {
	result := &CheckResult{CurrentVersion: currentVersion}

	// Managed distributions (AppImage, Flatpak) handle updates outside the app.
	// AppImage runtime sets APPIMAGE; Flatpak sets FLATPAK_ID.
	if os.Getenv("APPIMAGE") != "" || os.Getenv("FLATPAK_ID") != "" {
		return result, nil
	}

	// Only clean release builds ("vX.Y.Z") can be compared against releases.
	// Dev builds, git-describe strings ("v0.7.2-3-gabc1234"), dirty builds, etc.
	// are all excluded so developers don't get spurious update prompts.
	if !isCleanSemver(currentVersion) {
		return result, nil
	}

	rel, err := fetchLatest()
	if err != nil {
		return nil, err
	}
	if rel == nil {
		return result, nil
	}

	result.LatestVersion = rel.TagName
	result.ReleaseName = rel.Name

	if !newerThan(rel.TagName, currentVersion) {
		return result, nil
	}

	want := assetName()
	for _, a := range rel.Assets {
		if a.Name == want {
			result.Available = true
			mu.Lock()
			pendingURL = a.BrowserDownloadURL
			mu.Unlock()
			break
		}
	}
	return result, nil
}

func applyUpdate() error {
	mu.Lock()
	url := pendingURL
	mu.Unlock()

	if url == "" {
		return fmt.Errorf("no pending update (call /api/update/check first)")
	}

	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("cannot determine executable path: %w", err)
	}
	destDir := filepath.Dir(execPath)

	log.Printf("[updater] downloading %s", url)
	resp, err := http.Get(url) //nolint:noctx
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download: HTTP %d", resp.StatusCode)
	}

	log.Printf("[updater] extracting to %s", destDir)
	if runtime.GOOS == "windows" {
		// archive/zip requires io.ReaderAt, so download to a temp file first.
		tmp, err := os.CreateTemp("", "cove-update-*.zip")
		if err != nil {
			return fmt.Errorf("temp file: %w", err)
		}
		tmpName := tmp.Name()
		defer os.Remove(tmpName)
		if _, err := io.Copy(tmp, resp.Body); err != nil {
			tmp.Close()
			return fmt.Errorf("download: %w", err)
		}
		tmp.Close()
		if err := extractZip(tmpName, destDir, filepath.Base(execPath)); err != nil {
			return fmt.Errorf("extract: %w", err)
		}
	} else {
		if err := extractTarGz(resp.Body, destDir); err != nil {
			return fmt.Errorf("extract: %w", err)
		}
	}

	log.Println("[updater] done — restarting")
	// Delay exit slightly so the HTTP response is flushed to the client before
	// the process disappears. The Qt shell detects RestartExitCode and re-execs
	// itself; on Windows it also renames cove.exe.new → cove.exe first.
	go func() {
		time.Sleep(250 * time.Millisecond)
		os.Exit(RestartExitCode)
	}()
	return nil
}

// extractZip extracts a .zip archive into destDir. selfName is the base name
// of the currently-running binary; on Windows a running .exe cannot be renamed
// over itself, so it is written as selfName+".new" and the Qt shell performs
// the final rename after exit code 42 causes it to restart.
func extractZip(zipPath, destDir, selfName string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer r.Close()

	for _, f := range r.File {
		if strings.Contains(filepath.ToSlash(f.Name), "..") {
			log.Printf("[updater] skipping suspicious path: %s", f.Name)
			continue
		}

		target := filepath.Join(destDir, filepath.FromSlash(f.Name))

		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}

		// The running binary cannot be replaced in-place on Windows; write a
		// .new sidecar so the Qt shell can rename it after we exit with 42.
		dest := target
		if filepath.Base(target) == selfName {
			dest = target + ".new"
		}

		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
		if err != nil {
			rc.Close()
			return err
		}
		_, copyErr := io.Copy(out, rc)
		rc.Close()
		out.Close()
		if copyErr != nil {
			os.Remove(dest)
			return copyErr
		}
	}
	return nil
}

// extractTarGz extracts a .tar.gz archive into destDir, writing each entry
// atomically (temp file + rename) to avoid half-written files on failure.
func extractTarGz(r io.Reader, destDir string) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return err
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		// Path traversal guard: reject entries with ".." components.
		if strings.Contains(filepath.ToSlash(hdr.Name), "..") {
			log.Printf("[updater] skipping suspicious path: %s", hdr.Name)
			continue
		}

		target := filepath.Join(destDir, filepath.FromSlash(hdr.Name))

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			tmp := target + ".new"
			f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(hdr.Mode))
			if err != nil {
				return err
			}
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				os.Remove(tmp)
				return err
			}
			if err := f.Close(); err != nil {
				return err
			}
			if err := os.Rename(tmp, target); err != nil {
				return err
			}
		}
	}
	return nil
}

// SetupHandlers registers /api/update/check and /api/update/apply on mux.
func SetupHandlers(mux *http.ServeMux, currentVersion string) {
	mux.HandleFunc("/api/update/check", utils.CorsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		result, err := check(currentVersion)
		if err != nil {
			log.Println("[updater] check error:", err)
			http.Error(w, "update check failed", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	}))

	mux.HandleFunc("/api/update/apply", utils.CorsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		// applyUpdate blocks for the duration of the download + extraction,
		// then spawns a goroutine to exit with RestartExitCode.
		if err := applyUpdate(); err != nil {
			log.Println("[updater] apply error:", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
}
