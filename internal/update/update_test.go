package update

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func fetchOK(tag string) func() (string, error) {
	return func() (string, error) { return tag, nil }
}

func fetchErr() func() (string, error) {
	return func() (string, error) { return "", errors.New("offline") }
}

// TestCheckNewerRelease: a newer tag is recorded in the notice file and
// returned so the startup report can name it.
func TestCheckNewerRelease(t *testing.T) {
	p := filepath.Join(t.TempDir(), noticeFile)
	got := check("v0.3.0", p, fetchOK("v0.4.0"), time.Now())
	if got != "v0.4.0" {
		t.Fatalf("check = %q, want v0.4.0", got)
	}
	n, err := readNotice(p)
	if err != nil {
		t.Fatal(err)
	}
	if n.Latest != "v0.4.0" || n.Acknowledged {
		t.Errorf("notice = %+v, want Latest v0.4.0 unacknowledged", n)
	}
}

// TestCheckSkips: the network stays untouched when a notice is pending, the
// TTL is fresh, or the build is dev.
func TestCheckSkips(t *testing.T) {
	called := false
	spy := func() (string, error) { called = true; return "v9.9.9", nil }
	now := time.Now()

	// Pending unacknowledged notice for a release not yet installed.
	p := filepath.Join(t.TempDir(), noticeFile)
	if err := writeNotice(p, Notice{CheckedAt: now.Add(-365 * 24 * time.Hour), Latest: "v0.4.0"}); err != nil {
		t.Fatal(err)
	}
	if got := check("v0.3.0", p, spy, now); got != "" {
		t.Errorf("pending notice: check = %q, want \"\"", got)
	}
	if called {
		t.Error("pending notice: fetch was called")
	}

	// Fresh TTL, nothing pending.
	p2 := filepath.Join(t.TempDir(), noticeFile)
	if err := writeNotice(p2, Notice{CheckedAt: now.Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if got := check("v0.3.0", p2, spy, now); got != "" {
		t.Errorf("fresh TTL: check = %q, want \"\"", got)
	}
	if called {
		t.Error("fresh TTL: fetch was called")
	}

	// Dev builds never nag.
	p3 := filepath.Join(t.TempDir(), noticeFile)
	if got := check("dev", p3, spy, now); got != "" {
		t.Errorf("dev build: check = %q, want \"\"", got)
	}
	if called {
		t.Error("dev build: fetch was called")
	}
}

// TestCheckStaleTTLRefetches: past the TTL the check runs again; a still-
// current release records only the check time (clears any stale Latest).
func TestCheckStaleTTLRefetches(t *testing.T) {
	p := filepath.Join(t.TempDir(), noticeFile)
	old := time.Now().Add(-48 * time.Hour)
	if err := writeNotice(p, Notice{CheckedAt: old, Latest: "v0.3.0", Acknowledged: true}); err != nil {
		t.Fatal(err)
	}
	if got := check("v0.3.0", p, fetchOK("v0.3.0"), time.Now()); got != "" {
		t.Fatalf("check = %q, want \"\" (already on latest)", got)
	}
	n, err := readNotice(p)
	if err != nil {
		t.Fatal(err)
	}
	if n.Latest != "" {
		t.Errorf("stale Latest %q not cleared", n.Latest)
	}
	if !n.CheckedAt.After(old) {
		t.Error("CheckedAt not refreshed")
	}
}

// TestCheckFetchFailure: an offline check is silent but still records the
// attempt, so every launch doesn't hammer the API.
func TestCheckFetchFailure(t *testing.T) {
	p := filepath.Join(t.TempDir(), noticeFile)
	if got := check("v0.3.0", p, fetchErr(), time.Now()); got != "" {
		t.Fatalf("check = %q, want \"\"", got)
	}
	n, err := readNotice(p)
	if err != nil {
		t.Fatal("fetch failure should still record the check")
	}
	if n.Latest != "" {
		t.Errorf("Latest = %q, want \"\"", n.Latest)
	}
}

// TestCheckOutOfBandUpdate: the user updated via curl|sh (never ran
// `whip update`), so the notice for the now-installed release is stale and
// unacknowledged. check clears it and resumes checking — otherwise they'd
// never hear about a new release again.
func TestCheckOutOfBandUpdate(t *testing.T) {
	p := filepath.Join(t.TempDir(), noticeFile)
	// The notice was written days ago; the user has since installed v0.4.0.
	old := time.Now().Add(-48 * time.Hour)
	if err := writeNotice(p, Notice{CheckedAt: old, Latest: "v0.4.0"}); err != nil {
		t.Fatal(err)
	}
	called := false
	spy := func() (string, error) { called = true; return "v0.5.0", nil }
	// Now ON v0.4.0: the stale notice must not suppress the check.
	if got := check("v0.4.0", p, spy, time.Now()); got != "v0.5.0" {
		t.Fatalf("check = %q, want v0.5.0", got)
	}
	if !called {
		t.Error("stale notice suppressed the fetch")
	}
	n, err := readNotice(p)
	if err != nil {
		t.Fatal(err)
	}
	if n.Latest != "v0.5.0" {
		t.Errorf("notice Latest = %q, want v0.5.0", n.Latest)
	}
}

// TestCheckCorruptNotice: a clobbered notice file is treated as "never
// checked" — the check runs and rewrites it.
func TestCheckCorruptNotice(t *testing.T) {
	p := filepath.Join(t.TempDir(), noticeFile)
	if err := os.WriteFile(p, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := check("v0.3.0", p, fetchOK("v0.4.0"), time.Now())
	if got != "v0.4.0" {
		t.Fatalf("check = %q, want v0.4.0", got)
	}
	if _, err := readNotice(p); err != nil {
		t.Fatal("corrupt notice was not rewritten:", err)
	}
}

// TestNewer: semver-ish comparison.
func TestNewer(t *testing.T) {
	cases := []struct {
		current, latest string
		want            bool
	}{
		{"v0.3.0", "v0.4.0", true},
		{"v0.3.0", "v0.3.1", true},
		{"v0.3.0", "v1.0.0", true},
		{"0.3.0", "0.4.0", true}, // missing v prefix tolerated
		{"v0.3.0", "v0.3.0", false},
		{"v0.4.0", "v0.3.0", false},
		{"v0.3.0", "v0.3.0-rc.1", false}, // prerelease of the same version
		{"v0.3.0-rc.1", "v0.3.0", true},
		{"dev", "v0.4.0", false},
		{"v0.3.0", "", false},
		{"v0.3.0", "garbage", false},
		{"v0.3", "v0.4.0", false},
	}
	for _, c := range cases {
		if got := Newer(c.current, c.latest); got != c.want {
			t.Errorf("Newer(%q, %q) = %v, want %v", c.current, c.latest, got, c.want)
		}
	}
}

// TestPendingAndAcknowledge: Pending reports only unacknowledged newer
// notices; Acknowledge silences them.
func TestPendingAndAcknowledge(t *testing.T) {
	home := t.TempDir()
	t.Setenv("WHIP_HOME", home)

	if got := Pending("v0.3.0"); got != "" {
		t.Fatalf("no notice: Pending = %q, want \"\"", got)
	}
	if err := writeNotice(filepath.Join(home, noticeFile), Notice{CheckedAt: time.Now(), Latest: "v0.4.0"}); err != nil {
		t.Fatal(err)
	}
	if got := Pending("v0.3.0"); got != "v0.4.0" {
		t.Fatalf("Pending = %q, want v0.4.0", got)
	}
	if got := Pending("v0.4.0"); got != "" {
		t.Errorf("already on latest: Pending = %q, want \"\"", got)
	}
	Acknowledge()
	if got := Pending("v0.3.0"); got != "" {
		t.Errorf("after Acknowledge: Pending = %q, want \"\"", got)
	}
	// Acknowledge is a no-op with nothing pending, and notice JSON stays valid.
	Acknowledge()
	data, err := os.ReadFile(filepath.Join(home, noticeFile))
	if err != nil {
		t.Fatal(err)
	}
	var n Notice
	if err := json.Unmarshal(data, &n); err != nil {
		t.Fatal(err)
	}
	if !n.Acknowledged {
		t.Error("notice lost its acknowledgement")
	}
}

// fetchLatestGitHub parses the tag from a GitHub releases response and errors
// on non-200 / empty tag. latestURL is swapped for a mock server.
func TestFetchLatestGitHub(t *testing.T) {
	orig := latestURL
	defer func() { latestURL = orig }()
	// Relax the fetch timeout: the 2s production bound races under
	// `-race -shuffle=on` on a loaded CI runner (a localhost request took
	// 2.1s and flaked the suite).
	origTimeout := fetchTimeout
	fetchTimeout = 30 * time.Second
	defer func() { fetchTimeout = origTimeout }()

	t.Run("ok", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{"tag_name": "v9.9.9"})
		}))
		defer srv.Close()
		latestURL = srv.URL
		tag, err := fetchLatestGitHub()
		if err != nil || tag != "v9.9.9" {
			t.Fatalf("tag=%q err=%v", tag, err)
		}
	})
	t.Run("non-200", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer srv.Close()
		latestURL = srv.URL
		if _, err := fetchLatestGitHub(); err == nil {
			t.Fatal("expected error on 404")
		}
	})
	t.Run("empty tag", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]string{"tag_name": ""})
		}))
		defer srv.Close()
		latestURL = srv.URL
		if _, err := fetchLatestGitHub(); err == nil {
			t.Fatal("expected error on empty tag_name")
		}
	})
}
