package workflow

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf16"
)

// homeDir is the on-disk home for workflow runtime artifacts
// (~/.whip/workflows), overridable via WHIP_HOME for tests. Port of
// persistence.ts workflowHome; resolved lazily so tests can flip WHIP_HOME.
func homeDir() (string, error) {
	dir := os.Getenv("WHIP_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, ".whip")
	}
	dir = filepath.Join(dir, "workflows")
	if err := os.MkdirAll(filepath.Join(dir, "scripts"), 0o700); err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Join(dir, "runs"), 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

var runCounter atomic.Int64

// GenerateRunID returns a monotonic-ish run id (host code, so time and
// randomness are fine — the determinism contract only binds inside scripts).
func GenerateRunID() string {
	n := runCounter.Add(1) % 0xffff
	var b [2]byte
	if _, err := rand.Read(b[:]); err == nil {
		return fmt.Sprintf("run-%x-%x-%s", time.Now().UnixMilli(), n, hex.EncodeToString(b[:]))
	}
	return fmt.Sprintf("run-%x-%x", time.Now().UnixMilli(), n)
}

// PersistScript writes the script to disk and returns its path, so the model
// can edit it and re-invoke with {scriptPath} (persistence.ts persistScript).
func PersistScript(name, runID, script string) string {
	dir, err := homeDir()
	if err != nil {
		return ""
	}
	safe := sanitizeName(name)
	file := filepath.Join(dir, "scripts", safe+"-"+runID+".js")
	if err := os.WriteFile(file, []byte(script), 0o600); err != nil {
		return ""
	}
	return file
}

func sanitizeName(name string) string {
	var b strings.Builder
	for _, r := range name {
		ok := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-'
		if ok {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
		if b.Len() >= 50 {
			break
		}
	}
	if b.Len() == 0 {
		return "workflow"
	}
	return b.String()
}

// JournalEntry is one settled agent() call: its lexical call index, the
// resume hash of (prompt, opts), and the result to replay.
type JournalEntry struct {
	Index  int    `json:"index"`
	Hash   string `json:"hash"`
	Result any    `json:"result"`
}

// PersistedRun is the JSON shape of ~/.whip/workflows/runs/<runId>.json.
type PersistedRun struct {
	RunID      string         `json:"runId"`
	Name       string         `json:"name"`
	ScriptPath string         `json:"scriptPath,omitempty"`
	Status     string         `json:"status"` // running | complete | error | stopped
	Args       any            `json:"args,omitempty"`
	Journal    []JournalEntry `json:"journal"`
	Result     any            `json:"result,omitempty"`
	Error      string         `json:"error,omitempty"`
	StartedAt  int64          `json:"startedAt"`
	FinishedAt int64          `json:"finishedAt,omitempty"`
}

// SaveRun writes the run journal. Persistence is best-effort: a failed write
// must never kill a run (persistence.ts saveRun).
func SaveRun(state *PersistedRun) {
	dir, err := homeDir()
	if err != nil {
		return
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(dir, "runs", state.RunID+".json"), data, 0o600)
}

// LoadRun reads a persisted run, or nil if unknown/corrupt.
func LoadRun(runID string) *PersistedRun {
	dir, err := homeDir()
	if err != nil {
		return nil
	}
	data, err := os.ReadFile(filepath.Join(dir, "runs", runID+".json"))
	if err != nil {
		return nil
	}
	var run PersistedRun
	if json.Unmarshal(data, &run) != nil {
		return nil
	}
	return &run
}

// JournalMap builds a resume lookup (callIndex → {hash, result}) from a
// persisted run's journal.
func JournalMap(run *PersistedRun) map[int]JournalEntry {
	m := map[int]JournalEntry{}
	if run == nil {
		return m
	}
	for _, e := range run.Journal {
		m[e.Index] = e
	}
	return m
}

// HashString is the dependency-free djb2 hash used for resume cache keys.
// It reproduces persistence.ts hashString EXACTLY so journals stay
// cross-compatible with the TS extension: JS iterates the string by UTF-16
// code units (charCodeAt), so we hash uint16 units — not bytes — or
// non-ASCII prompts would hash differently.
func HashString(s string) string {
	h := int32(5381)
	for _, u := range utf16.Encode([]rune(s)) {
		h = ((h << 5) + h + int32(u)) | 0
	}
	return strconvUint32Base36(uint32(h))
}

// strconvUint32Base36 renders n in base36 (the TS `(h >>> 0).toString(36)`).
func strconvUint32Base36(n uint32) string {
	const digits = "0123456789abcdefghijklmnopqrstuvwxyz"
	if n == 0 {
		return "0"
	}
	var buf [8]byte // uint32 max is "1z141z3" in base36 — 7 chars + slack
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = digits[n%36]
		n /= 36
	}
	return string(buf[i:])
}
