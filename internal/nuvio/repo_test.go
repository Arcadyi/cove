package nuvio

import "testing"

func TestParseGitHubURL(t *testing.T) {
	cases := []struct {
		in                        string
		owner, name, branch, path string
		rawForm                   bool
	}{
		{in: "https://github.com/someowner/some-repo", owner: "someowner", name: "some-repo"},
		{in: "github.com/someowner/some-repo/", owner: "someowner", name: "some-repo"},
		{in: "https://github.com/someowner/some-repo.git", owner: "someowner", name: "some-repo"},
		{in: "https://github.com/someowner/some-repo/tree/dev", owner: "someowner", name: "some-repo", branch: "dev"},
	}
	for _, c := range cases {
		owner, name, branch, err := parseGitHubURL(c.in)
		if err != nil {
			t.Errorf("parseGitHubURL(%q) unexpected error: %v", c.in, err)
			continue
		}
		if owner != c.owner || name != c.name || branch != c.branch {
			t.Errorf("parseGitHubURL(%q) = (%q,%q,%q), want (%q,%q,%q)", c.in, owner, name, branch, c.owner, c.name, c.branch)
		}
	}
}

// TestParseRawGithubUsercontentURL covers the exact URL shape some community
// plugin directories hand users via a "copy manifest URL" button — this
// previously fell through to parseGitHubURL and failed with "not a
// github.com/owner/repo URL".
func TestParseRawGithubUsercontentURL(t *testing.T) {
	cases := []struct {
		in                        string
		owner, name, branch, path string
		ok                        bool
	}{
		{
			in:     "https://raw.githubusercontent.com/someowner/some-repo/refs/heads/main/manifest.json",
			owner:  "someowner",
			name:   "some-repo",
			branch: "main",
			path:   "manifest.json",
			ok:     true,
		},
		{
			in:     "https://raw.githubusercontent.com/someowner/some-repo/main/manifest.json",
			owner:  "someowner",
			name:   "some-repo",
			branch: "main",
			path:   "manifest.json",
			ok:     true,
		},
		{
			in:     "raw.githubusercontent.com/someowner/some-repo/refs/heads/dev/providers/manifest.json",
			owner:  "someowner",
			name:   "some-repo",
			branch: "dev",
			path:   "providers/manifest.json",
			ok:     true,
		},
		{
			in: "https://github.com/someowner/some-repo",
			ok: false,
		},
	}
	for _, c := range cases {
		owner, name, branch, path, ok := parseRawGithubUsercontentURL(c.in)
		if ok != c.ok {
			t.Errorf("parseRawGithubUsercontentURL(%q) ok = %v, want %v", c.in, ok, c.ok)
			continue
		}
		if !ok {
			continue
		}
		if owner != c.owner || name != c.name || branch != c.branch || path != c.path {
			t.Errorf("parseRawGithubUsercontentURL(%q) = (%q,%q,%q,%q), want (%q,%q,%q,%q)",
				c.in, owner, name, branch, path, c.owner, c.name, c.branch, c.path)
		}
	}
}
