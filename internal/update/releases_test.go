package update

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"golang.org/x/mod/semver"
)

func releaseFixture(tag string) githubRelease {
	draft, prerelease := false, semver.Prerelease(tag) != ""
	r := githubRelease{TagName: tag, Draft: &draft, Prerelease: &prerelease}
	for _, name := range []string{"whipcode-linux-x64", "whipcode-linux-arm64", "whipcode-darwin-x64", "whipcode-darwin-arm64", "SHA256SUMS", "install.sh"} {
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
	for _, mode := range []string{"ok", "unauthorized", "malformed", "missing", "timeout", "page limit"} {
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
				case "timeout":
					<-r.Context().Done()
				case "missing":
					_ = json.NewEncoder(w).Encode([]githubRelease{})
				default:
					_ = json.NewEncoder(w).Encode([]githubRelease{releaseFixture("v1.0.0-alpha.10")})
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

func TestReleaseRequiresExactAssets(t *testing.T) {
	r := releaseFixture("v1.0.0")
	r.Assets = append(r.Assets, r.Assets[0])
	if r.complete("stable") {
		t.Fatal("extra asset accepted")
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
