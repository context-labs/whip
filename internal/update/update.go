// Package update checks GitHub for a newer whip release and leaves a
// notice in ~/.whip for the next startup report.
//
// The check is best-effort by design: any failure (offline, rate-limited,
// corrupt state) is silent — a version check must never break startup.
// Auth mirrors install.sh: the `gh` CLI token or GH_TOKEN while the repo
// is private, anonymous once public.
package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/context-labs/whip/internal/config"
)

const (
	noticeFile = "update.json"
	// checkTTL caps the network check at once per day; a pending notice for
	// a release not yet installed suppresses re-checks entirely.
	checkTTL = 24 * time.Hour
)

// latestURL is the GitHub releases endpoint. A var so tests can point it at a
// mock server.
var latestURL = "https://api.github.com/repos/context-labs/whip/releases/latest"

// fetchTimeout bounds the update check. A var so tests can relax it: the
// hardcoded 2s is fine for a fire-and-forget startup check against GitHub, but
// races under `-race -shuffle=on` on a loaded CI runner when the test server
// is on the same box. Tests set this to a generous value.
var fetchTimeout = 2 * time.Second

// Notice is ~/.whip/update.json: the last check's outcome. Latest is set
// only when a newer release exists; Acknowledged is flipped by `whip
// update` so an installed release stops nagging.
type Notice struct {
	CheckedAt    time.Time `json:"checkedAt"`
	Latest       string    `json:"latest,omitempty"`
	Acknowledged bool      `json:"acknowledged,omitempty"`
}

// fetchLatest returns the newest release tag. A var so tests can stub the
// network.
var fetchLatest = fetchLatestGitHub

// Check runs the startup check: if a newer release than current exists, its
// tag is recorded in ~/.whip/update.json and returned. "" means nothing to
// say (up to date, dev build, already noted, checked recently, or the check
// failed). Never errors.
func Check(current string) string {
	dir, err := config.Dir()
	if err != nil {
		return ""
	}
	return check(current, filepath.Join(dir, noticeFile), fetchLatest, time.Now())
}

// Pending reads a recorded notice: the latest tag if a newer-than-current
// release is waiting to be acknowledged, else "".
func Pending(current string) string {
	dir, err := config.Dir()
	if err != nil {
		return ""
	}
	n, err := readNotice(filepath.Join(dir, noticeFile))
	if err != nil || n.Acknowledged || !Newer(current, n.Latest) {
		return ""
	}
	return n.Latest
}

// Acknowledge marks any pending notice as acted on (called by `whip update`
// after a successful install). Best-effort.
func Acknowledge() {
	dir, err := config.Dir()
	if err != nil {
		return
	}
	p := filepath.Join(dir, noticeFile)
	n, err := readNotice(p)
	if err != nil || n.Latest == "" {
		return
	}
	n.Acknowledged = true
	_ = writeNotice(p, n)
}

// check is the pure core, I/O injected for tests.
func check(current, noticePath string, fetch func() (string, error), now time.Time) string {
	if current == "dev" || current == "" {
		return "" // dev builds never nag
	}
	n, err := readNotice(noticePath)
	if err == nil {
		if n.Latest != "" && !n.Acknowledged {
			if Newer(current, n.Latest) {
				return "" // a release is already noted; don't nag twice
			}
			// The pending release is installed (or jumped past) — the user
			// updated out of band (curl|sh, package manager), so
			// Acknowledge never ran. Clear the stale Latest but keep the
			// original CheckedAt: stamping now would trip the TTL below and
			// defer the refetch by a day, and the user is owed a check.
			n.Latest = ""
		}
		if now.Sub(n.CheckedAt) < checkTTL {
			return "" // checked recently
		}
	}
	latest, err := fetch()
	if err != nil || !Newer(current, latest) {
		// Record the check itself (even when current/unknown) so the TTL
		// applies and an offline stretch doesn't retry every launch.
		_ = writeNotice(noticePath, Notice{CheckedAt: now})
		return ""
	}
	_ = writeNotice(noticePath, Notice{CheckedAt: now, Latest: latest})
	return latest
}

// Newer reports whether latest is a strictly newer semver than current.
// A prerelease sorts before the same-numbered release ("v0.3.0-rc.1" <
// "v0.3.0"). Non-semver strings ("dev", "", "v0.3") never compare newer.
func Newer(current, latest string) bool {
	c, cpre, lok := parseSemver(current)
	l, lpre, rok := parseSemver(latest)
	if !lok || !rok {
		return false
	}
	for i := range c {
		if l[i] != c[i] {
			return l[i] > c[i]
		}
	}
	return cpre && !lpre
}

// parseSemver extracts the numeric major/minor/patch of a "v1.2.3" tag and
// reports a "-prerelease" suffix.
func parseSemver(v string) (nums [3]int, prerelease, ok bool) {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	base, suffix, _ := strings.Cut(v, "-")
	parts := strings.SplitN(base, ".", 3)
	if len(parts) != 3 {
		return nums, false, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return nums, false, false
		}
		nums[i] = n
	}
	return nums, suffix != "", true
}

func readNotice(path string) (Notice, error) {
	var n Notice
	data, err := os.ReadFile(path) //nolint:gosec // G703: path is the whip-owned notice file
	if err != nil {
		return n, err
	}
	if err := json.Unmarshal(data, &n); err != nil {
		return Notice{}, err // corrupt notice: treat as never checked
	}
	return n, nil
}

// writeNotice persists the notice atomically (tmp+rename) — persisted state
// never gets a bare WriteFile.
func writeNotice(path string, n Notice) error {
	data, err := json.Marshal(n)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".update-*.tmp")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// fetchLatestGitHub asks the releases API for the newest tag. Auth follows
// install.sh: gh token → GH_TOKEN → anonymous.
func fetchLatestGitHub() (string, error) {
	// context.Background is deliberate: the check is fire-and-forget (fired
	// from a detached goroutine in main), so there is no caller ctx to
	// thread — the 2s timeout is the bound that matters.
	ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, latestURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if tok := ghToken(); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("github: %s", resp.Status)
	}
	var rel struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return "", err
	}
	if rel.TagName == "" {
		return "", errors.New("github: empty tag_name")
	}
	return rel.TagName, nil
}

// ghToken mirrors install.sh's auth order. Best-effort: no gh, no token.
func ghToken() string {
	if out, err := exec.CommandContext(context.Background(), "gh", "auth", "token").Output(); err == nil {
		if tok := strings.TrimSpace(string(out)); tok != "" {
			return tok
		}
	}
	return os.Getenv("GH_TOKEN")
}
