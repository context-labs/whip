package tui

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/mcp"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestStartupReportSkillsAndWarnings: the report names loaded skills, flags a
// description that exceeds maxDesc (truncated in the system prompt), and
// flags a SKILL.md that fails to parse — pi's [Skill conflicts] block.
func TestStartupReportSkillsAndWarnings(t *testing.T) {
	dir := t.TempDir()
	mkSkill := func(name, desc string) {
		d := filepath.Join(dir, ".agents", "skills", name)
		os.MkdirAll(d, 0o755)
		os.WriteFile(filepath.Join(d, "SKILL.md"), []byte("---\nname: "+name+"\ndescription: "+desc+"\n---\n"), 0o644)
	}
	mkSkill("good", "fine")
	mkSkill("wordy", strings.Repeat("x", 1100)) // over the spec's 1024
	// A SKILL.md with no frontmatter = parse problem.
	bad := filepath.Join(dir, ".agents", "skills", "broken")
	os.MkdirAll(bad, 0o755)
	os.WriteFile(filepath.Join(bad, "SKILL.md"), []byte("no frontmatter here"), 0o644)

	t.Chdir(dir)

	m := tasksModel("http://unused")
	m.startupReport()
	if len(m.blocks) == 0 {
		t.Fatal("no report rendered")
	}
	out := m.blocks[0].text
	if !strings.Contains(out, "skills: 2 loaded") {
		t.Errorf("missing loaded count:\n%s", out)
	}
	if !strings.Contains(out, "wordy") || !strings.Contains(out, "exceeds 1024") {
		t.Errorf("missing truncation warning:\n%s", out)
	}
	if !strings.Contains(out, "broken") {
		t.Errorf("missing parse problem:\n%s", out)
	}
}

// TestStartupReportMCP: ready/failed/disabled servers render with the right
// glyphs in one line.
func TestStartupReportMCP(t *testing.T) {
	m := tasksModel("http://unused")
	disabled := false
	m.mcpMgr = mcp.NewManager(map[string]mcp.ServerConfig{
		"off":     {Command: []string{"true"}, Enabled: &disabled},
		"invalid": {},
	})
	m.startupReport()
	out := m.blocks[0].text
	if !strings.Contains(out, "mcp:") || !strings.Contains(out, "off ○") || !strings.Contains(out, "invalid ✗") {
		t.Errorf("bad mcp line:\n%s", out)
	}
}

// TestStartupReportMCPReadyAndQuiet: a REAL streamable-HTTP MCP server (the
// sdk handler behind httptest) connects to ready; the normal report lists it
// with its tool count (a no-warning report — the dim path), while the quiet
// opencode-mode report drops the healthy roster and surfaces only failures.
func TestStartupReportMCPReadyAndQuiet(t *testing.T) {
	srv := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "ok"}, nil)
	sdkmcp.AddTool(srv, &sdkmcp.Tool{
		Name:        "ping",
		Description: "pong",
		InputSchema: map[string]any{"type": "object"},
	}, func(ctx context.Context, req *sdkmcp.CallToolRequest, in struct{}) (*sdkmcp.CallToolResult, any, error) {
		return &sdkmcp.CallToolResult{Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: "pong"}}}, nil, nil
	})
	hs := httptest.NewServer(sdkmcp.NewStreamableHTTPHandler(func(*http.Request) *sdkmcp.Server { return srv }, nil))
	defer hs.Close()

	mgr := mcp.NewManager(map[string]mcp.ServerConfig{
		"ok":      {URL: hs.URL},
		"invalid": {}, // no command, no URL: fails validation at birth
	})
	mgr.Start(context.Background())
	defer mgr.Close()
	deadline := time.Now().Add(10 * time.Second)
	for {
		sts := mgr.Statuses()
		if len(sts) == 2 && sts[1].Status == mcp.StatusReady {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("server never became ready: %+v", sts)
		}
		time.Sleep(10 * time.Millisecond)
	}

	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("HOME", t.TempDir()) // no skills: the mcp line stands alone

	// quiet (opencode mode): healthy roster suppressed, failure surfaced
	mdMu.Lock()
	sl, sk := mdLight, mdKnown
	mdLight, mdKnown = true, true // skip the unknown-background notice
	mdMu.Unlock()
	t.Cleanup(func() { mdMu.Lock(); mdLight, mdKnown = sl, sk; mdMu.Unlock() })
	m := tasksModel("http://unused")
	m.uiMode = opencodeMode
	m.mcpMgr = mgr
	m.startupReport()
	out := m.blocks[0].text
	if !strings.Contains(out, "invalid ✗") || strings.Contains(out, "ok ✓") {
		t.Errorf("quiet report should list only failures:\n%s", out)
	}

	// normal mode: the full roster, ready server with its tool count
	m2 := tasksModel("http://unused")
	m2.mcpMgr = mgr
	m2.startupReport()
	out2 := m2.blocks[0].text
	if !strings.Contains(out2, "ok ✓ (1 tools)") || !strings.Contains(out2, "invalid ✗") {
		t.Errorf("full report should list every server:\n%s", out2)
	}

	// a manager with nothing failed renders a no-warning (dim) report, and a
	// server that has not settled yet shows the connecting glyph
	mgr2 := mcp.NewManager(map[string]mcp.ServerConfig{
		"ok":      {URL: hs.URL},
		"pending": {Command: []string{"true"}}, // never Started: stays connecting
	})
	defer mgr2.Close()
	m3 := tasksModel("http://unused")
	m3.mcpMgr = mgr2
	m3.startupReport()
	out3 := m3.blocks[0].text
	if !strings.Contains(out3, "pending ◌") {
		t.Errorf("unsettled server should show ◌:\n%s", out3)
	}
}

// TestStartupReportSilent: nothing loaded, nothing said.
func TestStartupReportSilent(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("HOME", t.TempDir()) // no ~/.whip/skills either

	m := tasksModel("http://unused")
	m.startupReport()
	if len(m.blocks) != 0 {
		t.Errorf("expected silence, got %q", m.blocks[0].text)
	}
}

// TestStartupReportUpdateNotice: a pending newer release (spotted by main's
// background check) renders as a notice line naming `whip update`.
func TestStartupReportUpdateNotice(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("HOME", t.TempDir())

	m := tasksModel("http://unused")
	m.updateLatest = "v0.4.0"
	m.startupReport()
	if len(m.blocks) == 0 {
		t.Fatal("no report rendered")
	}
	out := m.blocks[0].text
	if !strings.Contains(out, "update available: v0.4.0") || !strings.Contains(out, "whip update") {
		t.Errorf("missing update notice:\n%s", out)
	}
}
