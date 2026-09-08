package update

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func releaseFixture(tag string) githubRelease {
	r := githubRelease{TagName: tag, Prerelease: true}
	for _, name := range []string{"whipcode-linux-x64", "whipcode-linux-arm64", "whipcode-darwin-x64", "whipcode-darwin-arm64", "SHA256SUMS", "install-whipcode.sh"} {
		r.Assets = append(r.Assets, struct {
			Name string `json:"name"`
			Size int64  `json:"size"`
		}{Name: name, Size: 1})
	}
	return r
}

func TestWhipcodeVersionComparison(t *testing.T) {
	for _, test := range []struct {
		current, latest string
		want            bool
	}{
		{"whipcode-v0.0.9", "whipcode-v0.0.10", true},
		{"whipcode-v0.0.10", "whipcode-v0.0.9", false},
		{"whipcode-v0.0.10", "whipcode-v0.0.10", false},
		{"v0.5.14", "whipcode-v0.0.99", false},
		{"whipcode-v0.0.1", "v99.0.0", false},
		{"whipcode-v0.0.1", "whipcode-v0.0.02", false},
		{"dev", "whipcode-v0.0.2", false},
		{"whipcode-v0.0.0", "whipcode-v0.0.1", false},
		{"whipcode-v0.0.1", "whipcode-v0.0.18446744073709551616", false},
	} {
		if got := Newer(test.current, test.latest); got != test.want {
			t.Errorf("Newer(%q,%q) = %v", test.current, test.latest, got)
		}
	}
}

func TestFetchWhipcodePages(t *testing.T) {
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
					page[0] = releaseFixture("whipcode-v0.0.9")
					page[1] = releaseFixture("whipcode-v0.0.999")
					page[1].Draft = true
					page[2] = releaseFixture("whipcode-v0.0.998")
					page[2].Assets = nil
					page[3] = releaseFixture("v999.0.0")
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
					_ = json.NewEncoder(w).Encode([]githubRelease{releaseFixture("whipcode-v0.0.10")})
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
			tag, err := fetchWhipcode(ctx, "test-token")
			switch mode {
			case "ok":
				if err != nil || tag != "whipcode-v0.0.10" || calls != 2 {
					t.Fatalf("tag=%q calls=%d err=%v", tag, calls, err)
				}
			case "missing":
				if err != nil || tag != "whipcode-v0.0.9" {
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
	release := releaseFixture("whipcode-v0.0.1")
	if !release.complete() {
		t.Fatal("fixture is incomplete")
	}
	for i := range release.Assets {
		release.Assets[i].Size = 0
		if release.complete() {
			t.Fatalf("zero-size %s accepted", release.Assets[i].Name)
		}
		release.Assets[i].Size = 1
	}
	release.Prerelease = false
	if release.complete() {
		t.Fatal("stable release accepted")
	}
}
