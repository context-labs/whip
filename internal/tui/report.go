package tui

import "net/url"

// Version identifies the native CLI build in the local report bundle.
var Version = "dev"

const issueBase = "https://github.com/context-labs/whip/issues/new"

func issueURL(snippet string) string {
	body := "### What happened\n\n\n\n### Expected\n\n\n\n### Environment\n\n" + snippet + "\n"
	values := url.Values{}
	values.Set("title", "")
	values.Set("body", body)
	return issueBase + "?" + values.Encode()
}
