package nuvio

import (
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// githubURLPattern accepts github.com/owner/repo, with or without a scheme,
// trailing slash, .git suffix, or an explicit /tree/<branch>.
var githubURLPattern = regexp.MustCompile(`^(?:https?://)?(?:www\.)?github\.com/([^/]+)/([^/]+?)(?:\.git)?(?:/tree/([^/]+))?/?$`)

// rawGithubUsercontentPattern accepts a direct link to a file in a repo on
// raw.githubusercontent.com — the form some community plugin directories hand
// users directly via a "copy manifest URL" button, rather than a
// github.com/owner/repo URL. Matches both the short branch form
// (.../owner/repo/main/manifest.json) and the long ref form GitHub also
// serves (.../owner/repo/refs/heads/main/manifest.json).
var rawGithubUsercontentPattern = regexp.MustCompile(`^(?:https?://)?raw\.githubusercontent\.com/([^/]+)/([^/]+)/(?:refs/heads/)?([^/]+)/(.+)$`)

func parseGitHubURL(raw string) (owner, name, branch string, err error) {
	m := githubURLPattern.FindStringSubmatch(strings.TrimSpace(raw))
	if m == nil {
		return "", "", "", fmt.Errorf("not a github.com/owner/repo URL")
	}
	return m[1], m[2], m[3], nil
}

// parseRawGithubUsercontentURL extracts owner/repo/branch/path from a direct
// raw.githubusercontent.com file URL. The returned path lets AddRepo fetch
// exactly what the user pasted instead of assuming "manifest.json" — most
// links do point at manifest.json, but there's no reason to require it.
func parseRawGithubUsercontentURL(raw string) (owner, name, branch, path string, ok bool) {
	m := rawGithubUsercontentPattern.FindStringSubmatch(strings.TrimSpace(raw))
	if m == nil {
		return "", "", "", "", false
	}
	return m[1], m[2], m[3], m[4], true
}

func (m *Manager) fetchRaw(owner, name, branch, path string) ([]byte, error) {
	url := fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s/%s", owner, name, branch, path)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	res, err := m.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d fetching %s", res.StatusCode, path)
	}
	return io.ReadAll(res.Body)
}

// resolveBranchAndManifest tries the given branch (if any), falling back to
// main then master, and returns whichever branch actually had a manifest.json.
func (m *Manager) resolveBranchAndManifest(owner, name, branch string) (string, []byte, error) {
	candidates := []string{branch}
	if branch == "" {
		candidates = []string{"main", "master"}
	}
	var lastErr error
	for _, b := range candidates {
		data, err := m.fetchRaw(owner, name, b, "manifest.json")
		if err == nil {
			return b, data, nil
		}
		lastErr = err
	}
	return "", nil, lastErr
}

// AddRepo fetches and parses a repo's manifest.json and persists it with every
// scraper Enabled=false — nothing is downloaded or executed until the user
// opts in per-scraper. Accepts either a github.com/owner/repo URL or a direct
// raw.githubusercontent.com link to the manifest file itself (the form some
// community plugin directories hand users via a "copy" button).
func (m *Manager) AddRepo(rawURL string) (Repo, error) {
	var owner, name, resolvedBranch string
	var data []byte

	if o, n, b, path, ok := parseRawGithubUsercontentURL(rawURL); ok {
		owner, name, resolvedBranch = o, n, b
		var err error
		data, err = m.fetchRaw(owner, name, resolvedBranch, path)
		if err != nil {
			return Repo{}, fmt.Errorf("could not fetch manifest: %w", err)
		}
	} else {
		var branch string
		var err error
		owner, name, branch, err = parseGitHubURL(rawURL)
		if err != nil {
			return Repo{}, fmt.Errorf("not a github.com/owner/repo URL or a raw.githubusercontent.com manifest link")
		}
		resolvedBranch, data, err = m.resolveBranchAndManifest(owner, name, branch)
		if err != nil {
			return Repo{}, fmt.Errorf("could not fetch manifest.json: %w", err)
		}
	}

	entries, err := parseManifest(data)
	if err != nil {
		return Repo{}, fmt.Errorf("could not parse manifest.json: %w", err)
	}

	scrapers := make([]Scraper, 0, len(entries))
	for _, e := range entries {
		s := newScraper(e)
		s.Enabled = false
		scrapers = append(scrapers, s)
	}

	repo := Repo{
		ID:        owner + "/" + name,
		Owner:     owner,
		Name:      name,
		Branch:    resolvedBranch,
		URL:       rawURL,
		Enabled:   false,
		Scrapers:  scrapers,
		FetchedAt: time.Now(),
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	for i, r := range m.repos {
		if r.ID == repo.ID {
			m.repos[i] = repo
			return repo, m.saveL()
		}
	}
	m.repos = append(m.repos, repo)
	return repo, m.saveL()
}

// RemoveRepo deletes a repo and all its cached scraper code.
func (m *Manager) RemoveRepo(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, r := range m.repos {
		if r.ID == id {
			m.repos = append(m.repos[:i], m.repos[i+1:]...)
			return m.saveL()
		}
	}
	return fmt.Errorf("repo not found")
}

// SetRepoEnabled is the master on/off switch for a repo. It does not itself
// fetch any scraper code — per-scraper enable does that lazily.
func (m *Manager) SetRepoEnabled(id string, enabled bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, r := range m.repos {
		if r.ID == id {
			m.repos[i].Enabled = enabled
			return m.saveL()
		}
	}
	return fmt.Errorf("repo not found")
}

// SetScraperEnabled toggles one scraper within a repo. On first enable it
// lazily fetches and caches the scraper's JS; a fetch/parse failure is stored
// in CodeErr and the scraper is refused enable rather than silently switched
// on with no code behind it.
func (m *Manager) SetScraperEnabled(repoID, scraperID string, enabled bool) error {
	m.mu.Lock()
	var repoIdx, scraperIdx = -1, -1
	for i, r := range m.repos {
		if r.ID != repoID {
			continue
		}
		repoIdx = i
		for j, s := range r.Scrapers {
			if s.ID == scraperID {
				scraperIdx = j
				break
			}
		}
		break
	}
	if repoIdx == -1 || scraperIdx == -1 {
		m.mu.Unlock()
		return fmt.Errorf("scraper not found")
	}

	if !enabled {
		m.repos[repoIdx].Scrapers[scraperIdx].Enabled = false
		err := m.saveL()
		m.mu.Unlock()
		return err
	}

	repo := m.repos[repoIdx]
	scraper := repo.Scrapers[scraperIdx]
	needsFetch := scraper.Code == "" || scraper.CodeErr != ""
	m.mu.Unlock()

	if needsFetch {
		code, fetchErr := m.fetchRaw(repo.Owner, repo.Name, repo.Branch, scraper.Filename)

		m.mu.Lock()
		defer m.mu.Unlock()
		// Re-find in case the slice changed while the network call was in flight.
		for i, r := range m.repos {
			if r.ID != repoID {
				continue
			}
			for j, s := range r.Scrapers {
				if s.ID != scraperID {
					continue
				}
				if fetchErr != nil {
					m.repos[i].Scrapers[j].CodeErr = fetchErr.Error()
					return fmt.Errorf("could not fetch scraper code: %w", fetchErr)
				}
				now := time.Now()
				m.repos[i].Scrapers[j].Code = string(code)
				m.repos[i].Scrapers[j].CodeFetchedAt = &now
				m.repos[i].Scrapers[j].CodeErr = ""
				m.repos[i].Scrapers[j].Enabled = true
				return m.saveL()
			}
		}
		return fmt.Errorf("scraper not found")
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.repos[repoIdx].Scrapers[scraperIdx].Enabled = true
	return m.saveL()
}

// RefreshRepo refetches manifest.json and, for any currently-enabled scraper,
// its JS too — matching Nuvio's own repo-refresh semantics.
func (m *Manager) RefreshRepo(id string) error {
	m.mu.RLock()
	var repo Repo
	found := false
	for _, r := range m.repos {
		if r.ID == id {
			repo = r
			found = true
			break
		}
	}
	m.mu.RUnlock()
	if !found {
		return fmt.Errorf("repo not found")
	}

	data, err := m.fetchRaw(repo.Owner, repo.Name, repo.Branch, "manifest.json")
	if err != nil {
		m.mu.Lock()
		for i, r := range m.repos {
			if r.ID == id {
				m.repos[i].FetchErr = err.Error()
			}
		}
		m.mu.Unlock()
		return err
	}
	entries, err := parseManifest(data)
	if err != nil {
		return err
	}

	// Preserve Enabled/Code for scrapers that still exist; refetch code for
	// ones that were enabled.
	prevByID := make(map[string]Scraper, len(repo.Scrapers))
	for _, s := range repo.Scrapers {
		prevByID[s.ID] = s
	}
	newScrapers := make([]Scraper, 0, len(entries))
	for _, e := range entries {
		s := newScraper(e)
		if prev, ok := prevByID[e.ID]; ok && prev.Enabled {
			code, fetchErr := m.fetchRaw(repo.Owner, repo.Name, repo.Branch, e.Filename)
			if fetchErr != nil {
				s.CodeErr = fetchErr.Error()
				s.Enabled = false
			} else {
				now := time.Now()
				s.Code = string(code)
				s.CodeFetchedAt = &now
				s.Enabled = true
			}
		}
		newScrapers = append(newScrapers, s)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	for i, r := range m.repos {
		if r.ID == id {
			m.repos[i].Scrapers = newScrapers
			m.repos[i].FetchedAt = time.Now()
			m.repos[i].FetchErr = ""
		}
	}
	return m.saveL()
}
