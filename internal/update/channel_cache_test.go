package update

import (
	"path/filepath"
	"testing"
	"time"
)

func TestCachedNoticeFollowsChannel(t *testing.T) {
	t.Setenv("WHIPCODE_HOME", t.TempDir())
	t.Setenv("WHIPCODE_CHANNEL", "prerelease")
	previous := fetchLatest
	t.Cleanup(func() { fetchLatest = previous })
	calls := 0
	fetchLatest = func(channel string) (string, error) {
		calls++
		if channel == "prerelease" {
			return "v1.1.0-alpha.1", nil
		}
		return "v1.0.1", nil
	}
	if got := Check("v1.0.0"); got != "v1.1.0-alpha.1" {
		t.Fatalf("alpha check: %q", got)
	}
	if got := Pending("v1.0.0"); got != "v1.1.0-alpha.1" {
		t.Fatalf("alpha pending: %q", got)
	}
	t.Setenv("WHIPCODE_CHANNEL", "stable")
	if got := Pending("v1.0.0"); got != "" {
		t.Fatalf("stable offered alpha: %q", got)
	}
	if got := Check("v1.0.0"); got != "v1.0.1" || calls != 2 {
		t.Fatalf("stable did not refetch: %q, calls %d", got, calls)
	}
	if got := Pending("v1.0.0"); got != "v1.0.1" {
		t.Fatalf("stable pending: %q", got)
	}
	t.Setenv("WHIPCODE_CHANNEL", "invalid")
	if got := Pending("v1.0.0"); got != "" {
		t.Fatalf("invalid channel pending: %q", got)
	}
	if got := Check("v1.0.0"); got != "" || calls != 2 {
		t.Fatalf("invalid channel fetched: %q, calls %d", got, calls)
	}
}

func TestCheckRefreshesNoticeFromAnotherChannel(t *testing.T) {
	for _, notice := range []Notice{
		{Channel: "prerelease", Latest: "v1.1.0-alpha.1"},
		{Channel: "prerelease"},                       // A fresh negative lookup must not suppress the other channel.
		{Latest: "v1.1.0-alpha.1"},                    // Notices predating channel tracking are discarded.
		{Channel: "stable", Latest: "v1.1.0-alpha.1"}, // Malformed cache cannot advertise alpha.
	} {
		now := time.Now()
		notice.CheckedAt = now
		path := filepath.Join(t.TempDir(), noticeFile)
		if err := writeNotice(path, notice); err != nil {
			t.Fatal(err)
		}
		if got := check("v1.0.0", "stable", path, fetchOK("v1.0.1"), now); got != "v1.0.1" {
			t.Fatalf("notice %+v blocked stable fetch: %q", notice, got)
		}
	}
}
