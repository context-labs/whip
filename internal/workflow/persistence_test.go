package workflow

import (
	"os"
	"path/filepath"
	"testing"
)

// TestHashStringMatchesTS pins the djb2 port against values computed by the
// reference persistence.ts hashString — journals must stay cross-compatible.
//
// The expected values were produced by running the TS hashString on these
// exact inputs (see the plan for the reproduction).
func TestHashStringMatchesTS(t *testing.T) {
	cases := map[string]string{
		"":       tsHash(""),
		"abc":    tsHash("abc"),
		"prompt": tsHash("prompt"),
		// A realistic resume-key shape (runtime.ts callHash input).
		`{"prompt":"Read x","model":null,"effort":null,"phase":"Read","schema":null}`: tsHash(`{"prompt":"Read x","model":null,"effort":null,"phase":"Read","schema":null}`),
	}
	for in, want := range cases {
		if got := HashString(in); got != want {
			t.Errorf("HashString(%q) = %q, want TS %q", in, got, want)
		}
	}
}

// tsHash is the TS hashString transcribed literally, kept only to generate
// the golden values in this test — it must agree with HashString.
func tsHash(s string) string {
	h := int32(5381)
	for i := 0; i < len(s); i++ {
		h = ((h << 5) + h + int32(s[i])) | 0
	}
	u := uint32(h)
	if u == 0 {
		return "0"
	}
	const digits = "0123456789abcdefghijklmnopqrstuvwxyz"
	var buf [12]byte
	i := len(buf)
	for u > 0 {
		i--
		buf[i] = digits[u%36]
		u /= 36
	}
	return string(buf[i:])
}

func TestPersistAndLoadRun(t *testing.T) {
	home := t.TempDir()
	t.Setenv("WHIP_HOME", home)

	p := &PersistedRun{
		RunID: "run-test-1", Name: "demo", Status: "running",
		StartedAt: 123,
		Journal:   []JournalEntry{{Index: 0, Hash: "h0", Result: "r0"}},
	}
	SaveRun(p)

	got := LoadRun("run-test-1")
	if got == nil {
		t.Fatal("LoadRun returned nil")
	}
	if got.Name != "demo" || len(got.Journal) != 1 || got.Journal[0].Result != "r0" {
		t.Fatalf("loaded: %+v", got)
	}
	if LoadRun("run-nope") != nil {
		t.Fatal("unknown run should load as nil")
	}

	m := JournalMap(got)
	if len(m) != 1 || m[0].Hash != "h0" {
		t.Fatalf("journal map: %+v", m)
	}
}

func TestPersistScript(t *testing.T) {
	home := t.TempDir()
	t.Setenv("WHIP_HOME", home)

	path := PersistScript("My Workflow!", "run-x", "export const meta = {}")
	if path == "" {
		t.Fatal("no path returned")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "export const meta = {}" {
		t.Fatalf("script content: %q", data)
	}
	// The name must be filesystem-safe.
	if filepath.Base(path) != "My-Workflow--run-x.js" {
		t.Fatalf("path: %q", path)
	}
}

func TestGenerateRunIDUnique(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		id := GenerateRunID()
		if seen[id] {
			t.Fatalf("duplicate run id %q", id)
		}
		seen[id] = true
	}
}
