package tui

import (
	"net/url"
	"strings"
	"testing"
)

// TestEnvReportCollectsWhitelist: the bundle names the whip/terminal/system
// facts and reads the whitelisted env vars.
func TestEnvReportCollectsWhitelist(t *testing.T) {
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("TERM_PROGRAM", "ghostty")
	t.Setenv("TERM_PROGRAM_VERSION", "1.2.3")
	t.Setenv("COLORTERM", "truecolor")
	t.Setenv("COLORFGBG", "15;0")
	t.Setenv("TMUX", "") // outside tmux: no tmux row
	t.Setenv("SHELL", "/bin/zsh")
	t.Setenv("LC_ALL", "en_US.UTF-8")
	t.Setenv("SSH_TTY", "")
	t.Setenv("SSH_CONNECTION", "")

	Version = "1.4.0-test"
	defer func() { Version = "dev" }()

	m := &model{
		modelName: "gpt-5", provName: "openai", themeHow: "COLORFGBG",
		mouseOn: true, sessionID: "abc123", width: 120, height: 40,
	}
	r := m.envReport()

	got := map[string]string{}
	for _, row := range r.rows {
		got[row.key] = row.val
	}
	want := map[string]string{
		"whip":         "1.4.0-test",
		"model":        "gpt-5",
		"provider":     "openai",
		"TERM":         "xterm-256color",
		"TERM_PROGRAM": "ghostty 1.2.3",
		"COLORTERM":    "truecolor",
		"COLORFGBG":    "15;0",
		"SHELL":        "/bin/zsh",
		"locale":       "en_US.UTF-8",
		"size":         "120x40",
		"mouse":        "on",
		"session":      "abc123",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("row %q = %q, want %q", k, got[k], v)
		}
	}
	if !strings.HasSuffix(got["theme"], " (COLORFGBG)") {
		t.Errorf("theme row %q should carry the detection source", got["theme"])
	}
	for _, k := range []string{"os", "go", "uname"} {
		if got[k] == "" {
			t.Errorf("row %q missing", k)
		}
	}
	// outside tmux / ssh: those rows are absent, not empty
	if _, ok := got["tmux"]; ok {
		t.Errorf("tmux row present outside tmux: %q", got["tmux"])
	}
	if _, ok := got["ssh"]; ok {
		t.Errorf("ssh row present without SSH_TTY/SSH_CONNECTION")
	}
}

// TestEnvReportTmuxAndSSH: inside tmux over ssh the tmux row carries the
// server version and ssh is flagged.
func TestEnvReportTmuxAndSSH(t *testing.T) {
	t.Setenv("TMUX", "/tmp/tmux-1000/default,1234,0")
	t.Setenv("SSH_TTY", "/dev/pts/3")
	m := &model{width: 80, height: 24}
	r := m.envReport()
	got := map[string]string{}
	for _, row := range r.rows {
		got[row.key] = row.val
	}
	if got["ssh"] != "yes" {
		t.Errorf("ssh = %q, want yes", got["ssh"])
	}
	// tmux -V runs only when tmux is on PATH; either way the row exists.
	if _, ok := got["tmux"]; !ok {
		t.Error("tmux row missing inside tmux")
	}
}

// TestEnvReportNoSecrets: secret-shaped env vars never enter the bundle —
// the whitelist reads only its named keys.
func TestEnvReportNoSecrets(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "sk-supersecret-123")
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-supersecret")
	t.Setenv("GITHUB_TOKEN", "ghp_supersecret")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "aws-supersecret")

	m := &model{width: 80, height: 24}
	r := m.envReport()
	for _, s := range []string{r.snippet, r.link} {
		for _, secret := range []string{"supersecret", "sk-", "ghp_"} {
			if strings.Contains(s, secret) {
				t.Errorf("secret material %q leaked into bundle:\n%s", secret, s)
			}
		}
	}
}

// TestReportSnippetFenced: the copy-paste form is a fenced code block with
// aligned rows and no OSC 8 escape sequences (clipboard-safe verbatim).
func TestReportSnippetFenced(t *testing.T) {
	m := &model{modelName: "m1", width: 100, height: 30}
	r := m.envReport()
	if !strings.HasPrefix(r.snippet, "```\n") || !strings.HasSuffix(r.snippet, "```") {
		t.Errorf("snippet not fenced: %q", r.snippet)
	}
	if strings.ContainsRune(r.snippet, 0x1b) {
		t.Error("snippet contains ESC — hyperlinks/styling must not leak into the paste form")
	}
	if !strings.Contains(r.snippet, "whip ") || !strings.Contains(r.snippet, "model") || !strings.Contains(r.snippet, "m1") {
		t.Errorf("snippet missing rows:\n%s", r.snippet)
	}
}

// TestIssueURL: the link targets the whip repo's new-issue page, round-trips
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

// TestReportBlock: the transcript block pairs the clickable link with the
// snippet; the link is OSC 8 (terminal owns the click).
func TestReportBlock(t *testing.T) {
	m := &model{modelName: "m1", provName: "p1", width: 90, height: 30}
	b := m.reportBlock()
	if !strings.Contains(b, "\x1b]8;;"+issueBase) {
		t.Error("block missing OSC 8 hyperlink to the new-issue page")
	}
	if !strings.Contains(b, "open a prefilled GitHub issue") {
		t.Error("block missing the link label")
	}
	if !strings.Contains(b, "```\n") {
		t.Error("block missing the fenced snippet")
	}
}

// TestReportCommandAppendsOneBlock: /report appends exactly one transcript
// block (headless, like /context-doctor).
func TestReportCommandAppendsOneBlock(t *testing.T) {
	m := &model{width: 80, height: 24}
	before := len(m.blocks)
	if _, cmd := m.command("/report"); cmd != nil {
		t.Error("/report should not return a tea.Cmd")
	}
	if len(m.blocks) != before+1 {
		t.Fatalf("blocks grew by %d, want 1", len(m.blocks)-before)
	}
	if !strings.Contains(m.blocks[before].text, issueBase) {
		t.Error("appended block does not carry the issue link")
	}
}
