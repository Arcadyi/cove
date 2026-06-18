package utils

import (
	"os"
	"path/filepath"
)

// AtomicWriteFile writes data to a temporary file in the same directory as
// path, fsyncs it, then renames it over path. rename(2) is atomic on POSIX
// filesystems, so a concurrent reader — or a crash partway through — never
// observes a half-written file: it sees either the previous complete contents
// or the new complete contents.
//
// This matters for the JSON stores (library.json, settings.json): a plain
// os.WriteFile truncates the target first, so a crash mid-write would leave an
// empty or partial file and lose the entire store. The temp file must live in
// the same directory as the target so the rename stays within one filesystem
// (cross-device renames fail).
func AtomicWriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	// Best-effort cleanup: harmless no-op once the rename below has consumed
	// the temp file, and removes the stray temp if we return early on error.
	defer func() { _ = os.Remove(tmpName) }()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	// Flush to disk before the rename so the bytes are durable; without this a
	// crash just after rename could expose a zero-length file on some setups.
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
