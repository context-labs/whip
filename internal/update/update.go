// Package update checks GitHub for a newer whipcode release and leaves a
// notice in ~/.whipcode for the next startup report.
//
// The check is best-effort by design: any failure (offline, rate-limited,
// corrupt state) is silent — a version check must never break startup.
// Auth mirrors install.sh: the `gh` CLI token or GH_TOKEN while the repo
// is private, anonymous once public.
package update

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/context-labs/whip/internal/buildinfo"
	"github.com/context-labs/whip/internal/config"
	"golang.org/x/mod/semver"
)

const (
	noticeFile = "update.json"
	// checkTTL caps the network check at once per day; a pending notice for
	// a release not yet installed suppresses re-checks entirely.
	checkTTL = 24 * time.Hour
)

// fetchTimeout bounds the update check. A var so tests can relax it: the
// hardcoded 2s is fine for a fire-and-forget startup check against GitHub, but
// races under `-race -shuffle=on` on a loaded CI runner when the test server
// is on the same box. Tests set this to a generous value.
var fetchTimeout = 2 * time.Second

// Notice is ~/.whipcode/update.json: the last check's outcome. Latest is set
// only when a newer release exists; Acknowledged is flipped by `whipcode
// update` so an installed release stops nagging.
type Notice struct {
	Channel      string    `json:"channel"`
	CheckedAt    time.Time `json:"checkedAt"`
	Latest       string    `json:"latest,omitempty"`
	Acknowledged bool      `json:"acknowledged,omitempty"`
}

// fetchLatest returns the newest release tag. A var so tests can stub the
// network.
var fetchLatest = fetchLatestGitHub

// Check runs the startup check: if a newer release than current exists, its
// tag is recorded in ~/.whipcode/update.json and returned. "" means nothing to
// say (up to date, dev build, already noted, checked recently, or the check
// failed). Never errors.
func Check(current string) string {
	if buildinfo.UpdateOwner == "desktop" || !validRelease(current) {
		return ""
	}
	dir, err := config.Dir()
	if err != nil {
		return ""
	}
	channel, err := Channel(current)
	if err != nil {
		return ""
	}
	return check(current, channel, filepath.Join(dir, noticeFile), func() (string, error) {
		return fetchLatest(channel)
	}, time.Now())
}

// Pending reads a recorded notice: the latest tag if a newer-than-current
// release is waiting to be acknowledged, else "".
func Pending(current string) string {
	if buildinfo.UpdateOwner == "desktop" || !validRelease(current) {
		return ""
	}
	channel, err := Channel(current)
	if err != nil {
		return ""
	}
	dir, err := config.Dir()
	if err != nil {
		return ""
	}
	n, err := readNotice(filepath.Join(dir, noticeFile))
	if err != nil || n.Channel != channel || !inChannel(n.Latest, channel) || n.Acknowledged || !Newer(current, n.Latest) {
		return ""
	}
	return n.Latest
}

// Acknowledge marks any pending notice as acted on (called by `whipcode update`
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
func check(current, channel, noticePath string, fetch func() (string, error), now time.Time) string {
	if !validRelease(current) || (channel != "stable" && channel != "prerelease") {
		return "" // dev builds never nag
	}
	n, err := readNotice(noticePath)
	if err == nil && n.Channel == channel && (n.Latest == "" || inChannel(n.Latest, channel)) {
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
	if err != nil || !inChannel(latest, channel) || !Newer(current, latest) {
		// Record the check itself (even when current/unknown) so the TTL
		// applies and an offline stretch doesn't retry every launch.
		_ = writeNotice(noticePath, Notice{Channel: channel, CheckedAt: now})
		return ""
	}
	_ = writeNotice(noticePath, Notice{Channel: channel, CheckedAt: now, Latest: latest})
	return latest
}

// Newer compares canonical post-reset releases using semantic version precedence.
// Development builds and pre-reset release namespaces never produce notices.
func Newer(current, latest string) bool {
	return validRelease(current) && validRelease(latest) && semver.Compare(latest, current) > 0
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
func fetchLatestGitHub(channel string) (string, error) {
	// The startup check is detached from any request; this timeout bounds it.
	ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
	defer cancel()
	return fetchReleases(ctx, ghToken(), channel)
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
