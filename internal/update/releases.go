package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/mod/semver"
)

var releasesURL = "https://api.github.com/repos/context-labs/whip/releases"

// Match the publisher's bounded, flat ASCII filenames, including Desktop assets.
var releaseAssetName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,199}$`)

// validRelease excludes historical products and shorthand semver tags.
func validRelease(tag string) bool {
	base, _, _ := strings.Cut(tag, "+")
	base, _, _ = strings.Cut(base, "-")
	return semver.IsValid(tag) && semver.Major(tag) != "v0" && strings.Count(base, ".") == 2
}

func inChannel(tag, channel string) bool {
	return validRelease(tag) && (channel == "prerelease" || (channel == "stable" && semver.Prerelease(tag) == ""))
}

// Channel follows an explicit selection or the installed version's channel.
// A prerelease installation can graduate to stable without opting in again.
func Channel(current string) (string, error) {
	channel := os.Getenv("WHIPCODE_CHANNEL")
	if channel == "" {
		channel = "stable"
		if validRelease(current) && semver.Prerelease(current) != "" {
			channel = "prerelease"
		}
	}
	if channel != "stable" && channel != "prerelease" {
		return "", fmt.Errorf("invalid WHIPCODE_CHANNEL %q: use stable or prerelease", channel)
	}
	return channel, nil
}

type githubRelease struct {
	TagName    string `json:"tag_name"`
	Draft      *bool  `json:"draft"`
	Prerelease *bool  `json:"prerelease"`
	Assets     []struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
		Size int64  `json:"size"`
	} `json:"assets"`
}

func (r githubRelease) complete(channel string) bool {
	if r.Draft == nil || *r.Draft || r.Prerelease == nil || !inChannel(r.TagName, channel) || *r.Prerelease != (semver.Prerelease(r.TagName) != "") {
		return false
	}
	required := map[string]bool{
		"whipcode-linux-x64": false, "whipcode-linux-arm64": false,
		"whipcode-darwin-x64": false, "whipcode-darwin-arm64": false,
		"SHA256SUMS": false, "install.sh": false,
	}
	// Discovery requires the CLI subset; the publisher owns the full inventory.
	seenNames := make(map[string]bool, len(r.Assets))
	seenIDs := make(map[int64]bool, len(r.Assets))
	for _, asset := range r.Assets {
		if !releaseAssetName.MatchString(asset.Name) || seenNames[asset.Name] || seenIDs[asset.ID] || asset.Size <= 0 || asset.ID <= 0 {
			return false
		}
		seenNames[asset.Name] = true
		seenIDs[asset.ID] = true
		if _, known := required[asset.Name]; known {
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

// fetchReleases checks every bounded page before returning a candidate. Returning
// a partial result after a failed page could send someone to an older build.
func fetchReleases(ctx context.Context, token, channel string) (string, error) {
	if channel != "stable" && channel != "prerelease" {
		return "", errors.New("invalid release channel")
	}
	var latest string
	for page := 1; page <= 100; page++ {
		releases, err := fetchReleasePage(ctx, token, page)
		if err != nil {
			return "", err
		}
		for _, release := range releases {
			if release.complete(channel) && (latest == "" || semver.Compare(release.TagName, latest) > 0) {
				latest = release.TagName
			}
		}
		if len(releases) < 100 {
			if latest == "" {
				return "", fmt.Errorf("no complete whipcode %s release found", channel)
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
