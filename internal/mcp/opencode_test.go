package mcp

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestParseOpenCode(t *testing.T) {
	doc := `{
	  // opencode.jsonc allows comments and trailing commas
	  "$schema": "https://opencode.ai/config.json",
	  "mcp": {
	    "gsc": {"type": "local", "command": ["npx", "-y", "mcp-server-gsc"],
	            "environment": {"GOOGLE_APPLICATION_CREDENTIALS": "$GSC_CREDS", "KEYFILE": "{file:~/gsc.json}"}, "enabled": true, "timeout": 5000},
	    "ahrefs": {"type": "remote", "url": "https://api.ahrefs.com/mcp/mcp", "headers": {"Authorization": "Bearer {env:AHREFS_TOKEN}"}},
	    "figma": {"type": "remote", "url": "https://mcp.figma.com/mcp", "oauth": {}},
	    "linear": {"type": "remote", "url": "https://mcp.linear.app/mcp", "oauth": {"clientId": "x"}, "enabled": true},
	    "plain": {"type": "remote", "url": "https://example.com/mcp", "oauth": false},
	    "off": {"type": "local", "command": ["node", "srv.js"], "enabled": false},
	    "odd": {"type": "websocket", "url": "https://example.com/ws"},
	  },
	}`
	got, err := ParseOpenCode([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 7 {
		t.Fatalf("parsed %d servers, want 7: %v", len(got), got)
	}
	gsc := got["gsc"]
	if !slices.Equal(gsc.Command, []string{"npx", "-y", "mcp-server-gsc"}) || gsc.Env["GOOGLE_APPLICATION_CREDENTIALS"] != "$GSC_CREDS" || gsc.Disabled() {
		t.Errorf("local entry mis-parsed: %+v", gsc)
	}
	if gsc.StartupTimeout != 0 {
		t.Errorf("opencode timeout must be ignored, got %d", gsc.StartupTimeout)
	}
	if gsc.Env["KEYFILE"] != "{file:~/gsc.json}" {
		t.Errorf("{file:} has no whip equivalent and must stay as written, got %q", gsc.Env["KEYFILE"])
	}
	if a := got["ahrefs"]; !a.Remote() || a.Headers["Authorization"] != "Bearer ${AHREFS_TOKEN}" || a.Disabled() {
		t.Errorf("remote entry mis-parsed or {env:} not rewritten to a whip reference: %+v", a)
	}
	for _, name := range []string{"figma", "linear"} {
		if s := got[name]; !s.Disabled() || s.Note != SignInNote {
			t.Errorf("%s: an oauth entry must import disabled with the sign-in note, got %+v", name, s)
		}
	}
	if p := got["plain"]; p.Disabled() || p.Note != "" {
		t.Errorf("oauth:false must not be treated as a sign-in, got %+v", p)
	}
	if o := got["off"]; !o.Disabled() || o.Note != "" {
		t.Errorf("enabled:false must survive without a note, got %+v", o)
	}
	if o := got["odd"]; !strings.Contains(o.Note, "unknown opencode transport type") {
		t.Errorf("unknown type should carry a note, got %+v", o)
	}
	if _, err := ParseOpenCode([]byte(`{"mcp": [1]}`)); err == nil {
		t.Error("a malformed mcp block must be a parse error")
	}
}

// TestLoadMergedOpenCode covers the fourth source end to end: OpenCode's
// three files merge in its own order, the source sits below every other
// import, a broken file is one unreadable-source row, and the gate blocks
// with an opencode note.
func TestLoadMergedOpenCode(t *testing.T) {
	dir := t.TempDir()
	ocDir := filepath.Join(dir, "opencode")
	if err := os.MkdirAll(ocDir, 0o700); err != nil {
		t.Fatal(err)
	}
	first := writeFile(t, ocDir, "config.json", `{"mcp": {"shared": {"type": "local", "command": ["first"]}, "only-first": {"type": "local", "command": ["one"]}}}`)
	second := writeFile(t, ocDir, "opencode.json", `{"mcp": {"shared": {"type": "local", "command": ["second"]}, "dup": {"type": "local", "command": ["oc-dup"]}}}`)
	third := filepath.Join(ocDir, "opencode.jsonc") // absent
	codexFile := writeFile(t, dir, "codex.toml", "[mcp_servers.dup]\ncommand = \"codex-dup\"\n")
	stubSources(t, codexFile, filepath.Join(dir, "absent-claude.json"), first, second, third)

	f := LoadMergedFiltered(dir, nil, everySource())
	if len(f.Errs) != 0 {
		t.Fatalf("unexpected discovery errors: %v", f.Errs)
	}
	if got := f.Merged["shared"].Command[0]; got != "second" {
		t.Errorf("a later opencode file must win over an earlier one, got %q", got)
	}
	if got := f.Merged["shared"].Source; got != second {
		t.Errorf("Source should name the winning file, got %q", got)
	}
	if got := f.Merged["dup"].Command[0]; got != "codex-dup" {
		t.Errorf("codex must win over opencode on a shared name, got %q", got)
	}
	if f.Sources["dup"] != "codex" || f.Sources["only-first"] != "opencode" {
		t.Errorf("source attribution wrong: %v", f.Sources)
	}
	if f.Merged["only-first"].Origin != "opencode" || f.Merged["only-first"].Trusted {
		t.Errorf("opencode entries are imported and untrusted, got %+v", f.Merged["only-first"])
	}
	if SourceLabel(second) != "opencode config" {
		t.Errorf("SourceLabel(%q) = %q", second, SourceLabel(second))
	}

	// Gate off: every opencode-only entry becomes a blocked row with its source named.
	off := everySource()
	off.Opencode = ImportSourcePolicy{}
	f = LoadMergedFiltered(dir, nil, off)
	if _, ok := f.Merged["only-first"]; ok {
		t.Error("a disabled opencode source must not merge")
	}
	if b, ok := f.Blocked["only-first"]; !ok || !strings.Contains(b.Note, "(opencode)") {
		t.Errorf("blocked opencode entry should carry an opencode note, got %+v", f.Blocked["only-first"])
	}

	// A broken file is one unreadable-source error; the other file still loads.
	writeFile(t, ocDir, "opencode.jsonc", `{broken`)
	f = LoadMergedFiltered(dir, nil, everySource())
	if _, ok := f.Errs[third]; !ok {
		t.Errorf("expected a parse error for %s, got %v", third, f.Errs)
	}
	if _, ok := f.Merged["shared"]; !ok {
		t.Error("readable opencode files should still merge when one is broken")
	}
}
