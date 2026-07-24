// Package capture defines the provider interface that feeds raw activity into
// Tally, plus the signal-extraction helpers that turn noisy window titles and
// URLs into the structured fields (repo, domain) the rule engine matches on.
package capture

import (
	"net/url"
	"path"
	"regexp"
	"strings"
)

var (
	// dashRepoRe matches a leading repo-like token before a separator, as in
	// "tally — main", "secureai-backend: feature/x", or "project | notes".
	dashRepoRe = regexp.MustCompile(`^([A-Za-z0-9._-]{2,})\s*[\x{2014}\-|:]`)
	// pathTokenRe finds a path-like fragment ("~/code/tally/...") in a title.
	pathTokenRe = regexp.MustCompile(`[~.A-Za-z0-9_-]*/[~./A-Za-z0-9_-]+`)
)

// workspaceDirs are path segments that contain repos rather than being one;
// skipped when walking a path-like title.
var workspaceDirs = map[string]bool{
	"code": true, "src": true, "source": true, "projects": true, "project": true,
	"dev": true, "develop": true, "work": true, "workspace": true, "repos": true,
	"repo": true, "git": true, "go": true, "sites": true, "www": true, "tmp": true,
	"var": true, "opt": true, "home": true, "users": true, "user": true,
	"documents": true, "desktop": true, "downloads": true,
}

// Domain extracts the registrable host from a URL, lowercased and without a
// leading "www.". Returns "" when the input is not a usable URL.
func Domain(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	return strings.TrimPrefix(host, "www.")
}

// Repo attempts to pull a repository name out of a terminal/IDE window title.
// It is deliberately conservative: a wrong guess is worse than none because it
// would misclassify. Returns "" when nothing looks like a repo.
func Repo(title string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		return ""
	}
	// Case 1: a leading token before a separator — "repo — branch",
	// "repo: something", "repo | notes". The most reliable signal.
	if m := dashRepoRe.FindStringSubmatch(title); m != nil {
		cand := strings.TrimSpace(m[1])
		if looksLikeRepo(cand) {
			return strings.ToLower(cand)
		}
	}
	// Case 2: a path fragment like "~/code/tally/internal/store". Walk the
	// segments and take the first that looks like a repo and isn't a known
	// workspace/parent directory.
	if frag := pathTokenRe.FindString(title); frag != "" {
		for _, seg := range strings.Split(frag, "/") {
			seg = strings.TrimSpace(seg)
			if seg == "" || seg == "~" || strings.HasPrefix(seg, ".") {
				continue
			}
			if workspaceDirs[strings.ToLower(seg)] {
				continue
			}
			if looksLikeRepo(seg) {
				return strings.ToLower(seg)
			}
		}
	}
	return ""
}

func looksLikeRepo(s string) bool {
	if len(s) < 2 || len(s) > 64 {
		return false
	}
	if workspaceDirs[strings.ToLower(s)] {
		return false
	}
	switch strings.ToLower(s) {
	case "http", "https", "com", "org", "net":
		return false
	}
	// Must contain a letter; avoid pure version numbers.
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			return true
		}
	}
	return false
}

// AppBase normalizes an application name (drops ".app", trims whitespace).
func AppBase(app string) string {
	app = strings.TrimSpace(app)
	app = strings.TrimSuffix(app, ".app")
	return path.Base(app)
}
