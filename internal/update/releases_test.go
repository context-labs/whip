package update

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/mod/semver"
)

func releaseFixture(tag string, extras ...string) githubRelease {
	draft, prerelease := false, semver.Prerelease(tag) != ""
	r := githubRelease{TagName: tag, Draft: &draft, Prerelease: &prerelease}
	names := []string{"whipcode-linux-x64", "whipcode-linux-arm64", "whipcode-darwin-x64", "whipcode-darwin-arm64", "SHA256SUMS", "install.sh"}
	for _, name := range append(names, extras...) {
		r.Assets = append(r.Assets, struct {
			ID   int64  `json:"id"`
			Name string `json:"name"`
			Size int64  `json:"size"`
		}{ID: int64(len(r.Assets) + 1), Name: name, Size: 1})
	}
	return r
}

func TestReleaseVersionComparison(t *testing.T) {
	for _, test := range []struct {
		current, latest string
		want            bool
	}{
		{"v1.0.0-alpha.9", "v1.0.0-alpha.10", true},
		{"v1.0.0-beta.1", "v1.0.0", true},
		{"v1.0.0-alpha.10", "v1.0.0-alpha.9", false},
		{"v1.0.0-alpha.10", "v1.0.0-alpha.10", false},
		{"v0.5.14", "v1.0.0-alpha.99", false},
		{"v1.0.0-alpha.1", "v99.0.0", true},
		{"v1.0.0-alpha.1", "v1.0.0-alpha.02", false},
		{"dev", "v1.0.0-alpha.2", false},
		{"v1.0.0-alpha.0", "v1.0.0-alpha.1", true},
		{"v1.0.0-alpha.1", "v1.0.0-alpha.18446744073709551616", true},
	} {
		if got := Newer(test.current, test.latest); got != test.want {
			t.Errorf("Newer(%q,%q) = %v", test.current, test.latest, got)
		}
	}
}

func TestFetchReleasePages(t *testing.T) {
	previous := releasesURL
	t.Cleanup(func() { releasesURL = previous })
	for _, mode := range []string{"ok", "unauthorized", "malformed", "malformed asset", "missing", "timeout", "page limit"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Header.Get("Authorization") != "Bearer test-token" {
					t.Error("missing token")
				}
				if r.URL.Query().Get("per_page") != "100" {
					t.Error("missing page size")
				}
				if r.URL.Query().Get("page") == "1" || mode == "page limit" {
					page := make([]githubRelease, 100)
					page[0] = releaseFixture("v1.0.0-alpha.9")
					page[1] = releaseFixture("v1.0.0-alpha.999")
					*page[1].Draft = true
					page[2] = releaseFixture("v1.0.0-alpha.998")
					page[2].Assets = nil
					page[3] = releaseFixture("whipcode-v0.0.999")
					_ = json.NewEncoder(w).Encode(page)
					return
				}
				switch mode {
				case "unauthorized":
					w.WriteHeader(http.StatusUnauthorized)
				case "malformed":
					_, _ = w.Write([]byte("invalid json"))
				case "malformed asset":
					_, _ = w.Write([]byte(`[{"tag_name":"v1.0.0","assets":[{"name":"WHIP.dmg","id":true,"size":1}]}]`))
				case "timeout":
					<-r.Context().Done()
				case "missing":
					_ = json.NewEncoder(w).Encode([]githubRelease{})
				default:
					_ = json.NewEncoder(w).Encode([]githubRelease{releaseFixture("v1.0.0-alpha.10", "WHIP-arm64.dmg", "RELEASES.json")})
				}
			}))
			defer server.Close()
			releasesURL = server.URL
			ctx := t.Context()
			if mode == "timeout" {
				// A canceled request must not return the candidate from a preceding page.
				var cancel func()
				ctx, cancel = context.WithTimeout(ctx, 100*time.Millisecond)
				defer cancel()
			}
			tag, err := fetchReleases(ctx, "test-token", "prerelease")
			switch mode {
			case "ok":
				if err != nil || tag != "v1.0.0-alpha.10" || calls != 2 {
					t.Fatalf("tag=%q calls=%d err=%v", tag, calls, err)
				}
			case "missing":
				if err != nil || tag != "v1.0.0-alpha.9" {
					t.Fatalf("tag=%q err=%v", tag, err)
				}
			default:
				if err == nil || tag != "" {
					t.Fatalf("partial lookup returned %q, %v", tag, err)
				}
			}
		})
	}
}

func TestWhipcodeIncompleteRelease(t *testing.T) {
	release := releaseFixture("v1.0.0-alpha.1")
	if !release.complete("prerelease") {
		t.Fatal("fixture is incomplete")
	}
	for i := range release.Assets {
		release.Assets[i].Size = 0
		if release.complete("prerelease") {
			t.Fatalf("zero-size %s accepted", release.Assets[i].Name)
		}
		release.Assets[i].Size = 1
	}
	*release.Prerelease = false
	if release.complete("prerelease") {
		t.Fatal("stable release accepted")
	}
}

func TestReleaseRejectsDuplicateAssets(t *testing.T) {
	r := releaseFixture("v1.0.0")
	r.Assets = append(r.Assets, r.Assets[0])
	if r.complete("stable") {
		t.Fatal("duplicate extra asset accepted")
	}
	r = releaseFixture("v1.0.0")
	r.Assets[1] = r.Assets[0]
	if r.complete("stable") {
		t.Fatal("duplicate asset accepted")
	}
	r = releaseFixture("v1.0.0")
	r.Assets[0].ID = 0
	if r.complete("stable") {
		t.Fatal("invalid asset ID accepted")
	}
	r = releaseFixture("v1.0.0")
	r.Assets[1].ID = r.Assets[0].ID
	if r.complete("stable") {
		t.Fatal("duplicate required asset ID accepted")
	}
}

func TestReleaseAcceptsUnifiedAssets(t *testing.T) {
	extras := []string{"WHIP-1.0.0-arm64.dmg", "WHIP-1.0.0-arm64.zip", "RELEASES.json", "artifact-manifest.json", "artifact-manifest.json.intoto.jsonl"}
	for _, test := range []struct{ tag, channel string }{
		{"v1.0.0", "stable"}, {"v1.0.0", "prerelease"}, {"v1.0.0-alpha.10", "prerelease"},
	} {
		t.Run(test.tag+"/"+test.channel, func(t *testing.T) {
			r := releaseFixture(test.tag, extras...)
			if !r.complete(test.channel) {
				t.Fatal("well-formed unified release rejected")
			}
			for missing := range 6 {
				r = releaseFixture(test.tag, extras...)
				r.Assets = append(r.Assets[:missing], r.Assets[missing+1:]...)
				if r.complete(test.channel) {
					t.Fatalf("missing CLI asset %d accepted", missing)
				}
			}
		})
	}
}

func TestUnifiedReleaseRejectsInvalidExtras(t *testing.T) {
	for _, name := range []string{"", "../escape", "/absolute", "sub/file", "sub\\file", ".hidden", "bad name", "bad\tname", "bad\nname", "bad\x00name", "évidence", strings.Repeat("a", 201)} {
		t.Run(name, func(t *testing.T) {
			if releaseFixture("v1.0.0", name).complete("stable") {
				t.Fatalf("unsafe name %q accepted", name)
			}
		})
	}
	for _, mutate := range []struct {
		name  string
		apply func(*githubRelease)
	}{
		{"duplicate name", func(r *githubRelease) { r.Assets[6].Name = r.Assets[0].Name }},
		{"duplicate ID", func(r *githubRelease) { r.Assets[6].ID = r.Assets[0].ID }},
		{"zero ID", func(r *githubRelease) { r.Assets[6].ID = 0 }},
		{"negative ID", func(r *githubRelease) { r.Assets[6].ID = -1 }},
		{"zero size", func(r *githubRelease) { r.Assets[6].Size = 0 }},
		{"negative size", func(r *githubRelease) { r.Assets[6].Size = -1 }},
		{"draft", func(r *githubRelease) { *r.Draft = true }},
		{"wrong channel flag", func(r *githubRelease) { *r.Prerelease = true }},
		{"historical tag", func(r *githubRelease) { r.TagName = "desktop-v1.0.0" }},
	} {
		t.Run(mutate.name, func(t *testing.T) {
			r := releaseFixture("v1.0.0", "WHIP.dmg")
			mutate.apply(&r)
			if r.complete("stable") {
				t.Fatal("invalid unified release accepted")
			}
		})
	}
	if releaseFixture("v1.0.0", "WHIP.dmg").complete("nightly") {
		t.Fatal("invalid channel accepted")
	}
	if releaseFixture("v1.0.0-alpha.10", "WHIP.dmg").complete("stable") {
		t.Fatal("prerelease accepted on stable channel")
	}
}

func TestReleaseRequiresExplicitFlags(t *testing.T) {
	for _, field := range []string{"draft", "prerelease"} {
		release := releaseFixture("v1.0.0")
		if field == "draft" {
			release.Draft = nil
		} else {
			release.Prerelease = nil
		}
		if release.complete("stable") {
			t.Fatalf("missing %s accepted", field)
		}
	}
}
