package addons

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNormalizeAddonURL(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"https://addon.example/manifest.json", "https://addon.example"},
		{"https://addon.example/", "https://addon.example"},
		{"https://addon.example", "https://addon.example"},
		{"https://example.com/some/path/manifest.json", "https://example.com/some/path"},
		{"https://example.com/some/path/", "https://example.com/some/path"},
		{"https://example.com/some/path", "https://example.com/some/path"},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			assert.Equal(t, tc.want, normalizeAddonURL(tc.input))
		})
	}
}
