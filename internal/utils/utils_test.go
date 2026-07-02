package utils

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSrtToVTT(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "empty",
			input: "",
			want:  "WEBVTT\n\n",
		},
		{
			name:  "single cue — comma to dot",
			input: "1\n00:00:01,000 --> 00:00:03,500\nHello world\n",
			want:  "WEBVTT\n\n1\n00:00:01.000 --> 00:00:03.500\nHello world\n",
		},
		{
			name:  "multi-line cue",
			input: "1\n00:00:01,000 --> 00:00:03,000\nLine one\nLine two\n",
			want:  "WEBVTT\n\n1\n00:00:01.000 --> 00:00:03.000\nLine one\nLine two\n",
		},
		{
			name:  "replaces all commas including in text",
			input: "1\n00:00:01,000 --> 00:00:02,000\nHello, world\n",
			want:  "WEBVTT\n\n1\n00:00:01.000 --> 00:00:02.000\nHello. world\n",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, SrtToVTT(tc.input))
		})
	}
}

func TestAtomicWriteFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.json")
	data := []byte(`{"ok":true}`)

	require.NoError(t, AtomicWriteFile(path, data, 0o644))

	got, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, data, got)

	// Overwrite produces the new content
	data2 := []byte(`{"ok":false}`)
	require.NoError(t, AtomicWriteFile(path, data2, 0o644))
	got2, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, data2, got2)

	// No temp files left behind
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	for _, e := range entries {
		assert.False(t, strings.Contains(e.Name(), ".tmp-"), "stray temp file: %s", e.Name())
	}
}

const allowedOrigin = "http://127.0.0.1:5174"

func TestCorsMiddleware_Options_AllowedOrigin(t *testing.T) {
	handler := CorsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	req := httptest.NewRequest(http.MethodOptions, "/", nil)
	req.Header.Set("Origin", allowedOrigin)
	rr := httptest.NewRecorder()
	handler(rr, req)

	assert.Equal(t, http.StatusNoContent, rr.Code)
	assert.Equal(t, allowedOrigin, rr.Header().Get("Access-Control-Allow-Origin"))
}

func TestCorsMiddleware_AllowedOrigin_Reflected(t *testing.T) {
	handler := CorsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", allowedOrigin)
	rr := httptest.NewRecorder()
	handler(rr, req)

	assert.Equal(t, http.StatusTeapot, rr.Code)
	assert.Equal(t, allowedOrigin, rr.Header().Get("Access-Control-Allow-Origin"))
}

// No Origin header (curl, mpv, the shell probe) is not a browser CORS
// request — it passes through with no ACAO header and the handler runs.
func TestCorsMiddleware_NoOrigin_PassThrough(t *testing.T) {
	handler := CorsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	handler(rr, req)

	assert.Equal(t, http.StatusTeapot, rr.Code)
	assert.Empty(t, rr.Header().Get("Access-Control-Allow-Origin"))
}

// A disallowed origin gets no ACAO header on reads (browser blocks the read)
// but the handler still runs (GET is not state-changing).
func TestCorsMiddleware_DisallowedOrigin_GET(t *testing.T) {
	ran := false
	handler := CorsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		ran = true
		w.WriteHeader(http.StatusTeapot)
	})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "https://evil.example")
	rr := httptest.NewRecorder()
	handler(rr, req)

	assert.True(t, ran)
	assert.Empty(t, rr.Header().Get("Access-Control-Allow-Origin"))
}

// A disallowed origin is refused outright on state-changing methods, so a
// hostile page can't fire blind CSRF-style writes at the local API.
func TestCorsMiddleware_DisallowedOrigin_POST_Forbidden(t *testing.T) {
	ran := false
	handler := CorsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		ran = true
	})
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("Origin", "https://evil.example")
	rr := httptest.NewRecorder()
	handler(rr, req)

	assert.Equal(t, http.StatusForbidden, rr.Code)
	assert.False(t, ran, "handler must not run for a forbidden cross-origin write")
}
