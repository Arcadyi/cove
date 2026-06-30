package addons

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestInferQuality(t *testing.T) {
	tests := []struct {
		name   string
		stream Stream
		want   string
	}{
		{
			name:   "4K in name",
			stream: Stream{Name: "torrentio\n4K\nSome.Movie.4K.mkv"},
			want:   "4k",
		},
		{
			name:   "1080p in title",
			stream: Stream{Name: "addon", Title: "Some Movie 1080p BluRay"},
			want:   "1080p",
		},
		{
			name:   "720p in name",
			stream: Stream{Name: "Some Movie 720p WEB-DL"},
			want:   "720p",
		},
		{
			name:   "480p",
			stream: Stream{Name: "Some Movie 480p"},
			want:   "480p",
		},
		{
			name:   "2160 resolves to 4k",
			stream: Stream{Name: "Some Movie 2160p"},
			want:   "4k",
		},
		{
			name:   "dolby vision",
			stream: Stream{Name: "Some Movie Dolby Vision 4K"},
			want:   "4k dv",
		},
		{
			name:   "HDR",
			stream: Stream{Name: "Some Movie HDR"},
			want:   "4k hdr",
		},
		{
			name:   "telesync",
			stream: Stream{Name: "Some Movie TeleSync"},
			want:   "ts",
		},
		{
			name:   "cam",
			stream: Stream{Name: "Some Movie HDCAM"},
			want:   "cam",
		},
		{
			name:   "empty — no quality signal",
			stream: Stream{Name: "Unknown source"},
			want:   "",
		},
		{
			name:   "second line takes precedence",
			stream: Stream{Name: "addon\n1080p\nSome.Movie.mkv"},
			want:   "1080p",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, inferQuality(tc.stream))
		})
	}
}

func TestGetMaxQuality(t *testing.T) {
	streams := []Stream{
		{Name: "Some Movie 720p"},
		{Name: "Some Movie 1080p"},
		{Name: "Some Movie 4K"},
	}
	assert.Equal(t, "4k", GetMaxQuality(streams))

	// Only 720p available
	assert.Equal(t, "720p", GetMaxQuality([]Stream{{Name: "720p only"}}))

	// Empty list
	assert.Equal(t, "", GetMaxQuality(nil))
}
