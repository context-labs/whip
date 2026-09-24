package update

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestChannel(t *testing.T) {
	for _, test := range []struct{ version, override, want string }{
		{"v1.0.0-alpha.9", "", "prerelease"},
		{"v1.0.0-beta.1", "", "prerelease"},
		{"v1.0.0", "", "stable"},
		{"dev", "", "stable"},
		{"v1.0.0-alpha.9", "stable", "stable"},
		{"v1.0.0", "prerelease", "prerelease"},
	} {
		t.Setenv("WHIPCODE_CHANNEL", test.override)
		got, err := Channel(test.version)
		if err != nil || got != test.want {
			t.Fatalf("Channel(%q) = %q, %v", test.version, got, err)
		}
	}
	t.Setenv("WHIPCODE_CHANNEL", "legacy")
	if _, err := Channel("v1.0.0"); err == nil {
		t.Fatal("invalid channel accepted")
	}
}

func TestReleaseChannelsIgnoreHistoricalAndIncompleteReleases(t *testing.T) {
	previous := releasesURL
	t.Cleanup(func() { releasesURL = previous })
	releases := []githubRelease{
		releaseFixture("v0.6.5"), releaseFixture("whipcode-v0.0.999"),
		releaseFixture("desktop-v999.0.0"), releaseFixture("v1.0.0-alpha.9"),
		releaseFixture("v1.0.0-alpha.10"), releaseFixture("v1.0.0-alpha.01"),
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(releases)
	}))
	defer server.Close()
	releasesURL = server.URL
	if got, err := fetchReleases(t.Context(), "", "prerelease"); err != nil || got != "v1.0.0-alpha.10" {
		t.Fatalf("alpha discovery = %q, %v", got, err)
	}
	if got, err := fetchReleases(t.Context(), "", "stable"); err == nil || got != "" {
		t.Fatalf("stable before v1 = %q, %v", got, err)
	}
	releases = append(releases, releaseFixture("v1.0.0"), releaseFixture("v1.1.0-alpha.1"))
	for _, test := range []struct{ channel, want string }{
		{"stable", "v1.0.0"}, {"prerelease", "v1.1.0-alpha.1"},
	} {
		if got, err := fetchReleases(t.Context(), "", test.channel); err != nil || got != test.want {
			t.Fatalf("%s discovery = %q, %v", test.channel, got, err)
		}
	}
	for _, historical := range []string{"v0.6.5", "whipcode-v0.0.999", "desktop-v1.0.0", "v1", "v1.0", "1.0.0"} {
		if Newer(historical, "v1.1.0") || Newer("v1.0.0-alpha.1", historical) {
			t.Fatalf("compared obsolete/invalid release %q", historical)
		}
	}
	if Newer("v1.0.0+build.1", "v1.0.0+build.2") {
		t.Fatal("metadata changed precedence")
	}
}
