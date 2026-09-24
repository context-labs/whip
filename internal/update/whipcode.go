package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
)

var releasesURL = "https://api.github.com/repos/context-labs/whip/releases"

const whipcodePrefix = "whipcode-v0.0."

func whipcodeNumber(tag string) (uint64, bool) {
	suffix, ok := strings.CutPrefix(tag, whipcodePrefix)
	if !ok || suffix == "" {
		return 0, false
	}
	n, err := strconv.ParseUint(suffix, 10, 64)
	return n, err == nil && n > 0 && strconv.FormatUint(n, 10) == suffix
}

type githubRelease struct {
	TagName    string `json:"tag_name"`
	Draft      bool   `json:"draft"`
	Prerelease bool   `json:"prerelease"`
	Assets     []struct {
		Name string `json:"name"`
		Size int64  `json:"size"`
	} `json:"assets"`
}

func (r githubRelease) complete() bool {
	if r.Draft || !r.Prerelease {
		return false
	}
	required := map[string]bool{
		"whipcode-linux-x64": false, "whipcode-linux-arm64": false,
		"whipcode-darwin-x64": false, "whipcode-darwin-arm64": false,
		"SHA256SUMS": false, "install-whipcode.sh": false,
	}
	for _, asset := range r.Assets {
		if _, ok := required[asset.Name]; ok && asset.Size > 0 {
			required[asset.Name] = true
		}
	}
	for _, present := range required {
		if !present {
			return false
		}
	}
	return true
}

// fetchWhipcode checks every bounded page before returning a candidate. Returning
// a partial result after a failed page could send someone to an older build.
func fetchWhipcode(ctx context.Context, token string) (string, error) {
	var latest string
	var newest uint64
	for page := 1; page <= 100; page++ {
		releases, err := fetchReleasePage(ctx, token, page)
		if err != nil {
			return "", err
		}
		for _, release := range releases {
			if n, ok := whipcodeNumber(release.TagName); ok && n > newest && release.complete() {
				latest, newest = release.TagName, n
			}
		}
		if len(releases) < 100 {
			if latest == "" {
				return "", errors.New("no complete whipcode release found")
			}
			return latest, nil
		}
	}
	return "", errors.New("whipcode release lookup exceeded 100 pages")
}

func fetchReleasePage(ctx context.Context, token string, page int) ([]githubRelease, error) {
	url := releasesURL + "?per_page=100&page=" + strconv.Itoa(page)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github: %s", resp.Status)
	}
	releases := []githubRelease{}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&releases); err != nil {
		return nil, err
	}
	return releases, nil
}
