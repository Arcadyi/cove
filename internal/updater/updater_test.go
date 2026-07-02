package updater

import (
	"fmt"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewerThan(t *testing.T) {
	tests := []struct {
		latest  string
		current string
		want    bool
	}{
		{"v0.10.0", "v0.9.0", true},    // minor bump, double-digit vs single
		{"v1.0.0", "v0.9.9", true},     // major bump
		{"v0.9.1", "v0.9.0", true},     // patch bump
		{"v0.9.0", "v0.9.0", false},    // equal
		{"v0.9.0", "v0.10.0", false},   // older
		{"v1.0.0", "v1.0.0", false},    // exact equal with v prefix
		{"v0.9.0-rc1", "v0.8.0", true}, // pre-release suffix stripped from latest
		{"v1.2.3", "v1.2.4", false},    // patch regression
	}
	for _, tc := range tests {
		t.Run(fmt.Sprintf("%s_vs_%s", tc.latest, tc.current), func(t *testing.T) {
			assert.Equal(t, tc.want, newerThan(tc.latest, tc.current))
		})
	}
}

func TestIsCleanSemver(t *testing.T) {
	tests := []struct {
		v    string
		want bool
	}{
		{"v1.2.3", true},
		{"v0.0.0", true},
		{"v10.20.30", true},
		{"dev", false},
		{"", false},
		{"v0.7.2-3-gabc1234", false}, // git describe output
		{"v1.2", false},              // only two parts
		{"v1.2.3.4", false},          // four parts
	}
	for _, tc := range tests {
		t.Run(tc.v, func(t *testing.T) {
			assert.Equal(t, tc.want, isCleanSemver(tc.v))
		})
	}
}

func TestCheckSkipsOnLinux(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux-only behavior")
	}
	// No network call should happen: the linux skip returns before fetchLatest.
	result, err := check("v1.0.0")
	assert.NoError(t, err)
	assert.False(t, result.Available)
	assert.Empty(t, result.LatestVersion)
}

func TestAssetName(t *testing.T) {
	name := assetName()
	assert.NotEmpty(t, name)
	assert.True(t, strings.HasPrefix(name, "cove-"), "should start with cove-")
	assert.Contains(t, name, runtime.GOOS)
	assert.Contains(t, name, runtime.GOARCH)
	if runtime.GOOS == "windows" {
		assert.True(t, strings.HasSuffix(name, ".zip"))
	} else {
		assert.True(t, strings.HasSuffix(name, ".tar.gz"))
	}
}

func TestSecurePath(t *testing.T) {
	dest := "/tmp/covedest"
	ok := []string{"cove", "web/index.html", "./web/app.js"}
	for _, name := range ok {
		if _, err := securePath(dest, name); err != nil {
			t.Errorf("securePath(%q) unexpectedly rejected: %v", name, err)
		}
	}
	// A leading "/" is cleaned by filepath.Join into a path relative to dest,
	// so "/abs/path" lands safely inside — only real ".." escapes are rejected.
	bad := []string{"../evil", "../../etc/passwd", "web/../../escape"}
	for _, name := range bad {
		if _, err := securePath(dest, name); err == nil {
			t.Errorf("securePath(%q) should have been rejected as escaping", name)
		}
	}
}
