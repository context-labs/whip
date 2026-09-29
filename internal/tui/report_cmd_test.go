package tui

import (
	"net/url"
	"strings"
	"testing"
)

// TestIssueURL: the link targets the whipcode repo's new-issue page, round-trips
// through url.Parse, and its body carries the skeleton plus the env bundle.
func TestIssueURL(t *testing.T) {
	snippet := "```\nwhip 1.2.3\nTERM xterm\n```"
	link := issueURL(snippet)
	u, err := url.Parse(link)
	if err != nil {
		t.Fatalf("link does not parse: %v", err)
	}
	if u.Scheme+"://"+u.Host+u.Path != issueBase {
		t.Errorf("link target = %s://%s%s, want %s", u.Scheme, u.Host, u.Path, issueBase)
	}
	body := u.Query().Get("body")
	for _, want := range []string{"### What happened", "### Expected", "### Environment", snippet} {
		if !strings.Contains(body, want) {
			t.Errorf("issue body missing %q:\n%s", want, body)
		}
	}
	if len(link) > 8000 { // GitHub's practical URL ceiling
		t.Errorf("link too long: %d bytes", len(link))
	}
}
