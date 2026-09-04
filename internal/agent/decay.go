package agent

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/tools/bashrun"
)

// Context decay keeps old tool output from polluting the prompt while
// preserving the prefix cache. The invariant: a "hot window" of the newest
// ~decayHotWindow tokens (measured from the back of the message list) is
// never mutated, so the pruned prefix stays byte-identical across turns and
// the provider cache keeps hitting; only the window itself is cold each turn.
//
// The pass runs once per Turn (before round 1), not per round: within-turn
// tool output is load-bearing and per-round mutation would churn the cache
// inside the tool loop. Three mechanisms, all deterministic — no LLM call:
//
//  1. Superseded reads: a read whose file has since been re-read or written
//     collapses to a one-line pointer ("superseded by newer read at line N").
//     The model never needs two vintages of the same file; it follows the
//     newest. (Write-invalidates-read counts here too.)
//  2. Age decay: tool results that were big at ingestion (>decayMinBytes,
//     ~2k tokens) and now sit older than the hot window collapse to a
//     placeholder naming the command/path, the original byte size, and the
//     spill path when one exists. Small results (errors, short greps — the
//     semantic glue of the conversation) stay inline forever.
//  3. Exclusions: assistant messages are never rewritten (reasoning chains
//     matter), and anything inside the hot window is untouched.
//
// Placeholders are HTML-comment-style so the model reads them as metadata,
// not content to quote.

const (
	// decayHotWindow is the newest slice of context (approx tokens, len/4)
	// left byte-stable for cache reuse. Anything older may be pruned.
	decayHotWindow = 24_000
	// decayMinBytes is the size at ingestion (approx 2k tokens) above which a
	// tool result is eligible for age decay; smaller results never decay.
	decayMinBytes = 8_000
)

// decayedMarker prefixes every placeholder so later passes (and tests) can
// recognize an already-decayed message cheaply.
const decayedMarker = "⟨"

// decay applies superseded-read replacement and age decay to a.Messages
// outside the hot window, and returns the indices of messages it rewrote so
// the caller can re-persist them (a.Save with from=0 rewrites the whole
// prefix — INSERT OR REPLACE makes it cheap).
//
// Returns the number of messages rewritten.
func (a *Agent) decay() int {
	a.msgsMu.Lock()
	defer a.msgsMu.Unlock()

	boundary := hotBoundary(a.Messages)
	rewritten := 0

	// Pass 1b: duplicate reads of the same unchanged file region. A re-read
	// with identical args returning identical bytes carries no new
	// information; collapse the LATER copy to a pointer at the first, so the
	// history keeps one inline vintage per (path, offset, limit). Runs BEFORE
	// Pass 1: it must compare pristine contents, and its "duplicate"
	// placeholder is more informative than Pass 1's "superseded" for the
	// exact-copy case (Pass 1 then skips the collapsed copy via its
	// decayedMarker check). Window-gated like everything else.
	type readKey struct {
		path, args string
	}
	seen := map[readKey]int{} // key → message index of the surviving copy
	for i := range boundary {
		m := &a.Messages[i]
		if m.Role != "tool" || m.Name != "read" || strings.HasPrefix(m.Content, decayedMarker) {
			continue
		}
		k := readKey{readPathFromCall(a.Messages, i), callArgs(a.Messages, i)}
		if k.path == "" {
			continue
		}
		first, dup := seen[k]
		if !dup {
			seen[k] = i
			continue
		}
		if a.Messages[first].Content == m.Content {
			m.Content = fmt.Sprintf("%sduplicate read of %s — same content as the first read above⟩",
				decayedMarker, filepath.Base(k.path))
			rewritten++
		} else {
			seen[k] = i // content changed between identical reads: newest wins
		}
	}

	// Pass 1: superseded reads. Walk backward (newest→oldest) tracking, per
	// path, the newest read/write position; an older read of the same path
	// collapses to a pointer at the newer evidence. The newest sighting may
	// sit inside the hot window (recent evidence is exactly what supersedes),
	// but the rewrite itself respects the window: reads inside it stay
	// byte-stable.
	latest := map[string]sighting{}
	type readRef struct {
		idx  int
		path string
	}
	var reads []readRef

	// Count tool calls per path as we go so the placeholder can say "at line N"
	// — the line count lives in the read result itself ("<n>\t..." numbered).
	for i := len(a.Messages) - 1; i > 0; i-- {
		m := a.Messages[i]
		if m.Role != "tool" {
			continue
		}
		switch m.Name {
		case "read":
			p := readPathFromCall(a.Messages, i)
			if p == "" || strings.HasPrefix(m.Content, decayedMarker) {
				continue
			}
			reads = append(reads, readRef{i, p})
			if _, ok := latest[p]; !ok {
				latest[p] = sighting{idx: i, lines: readLineCount(m.Content)}
			}
		case "write", "edit":
			p := writePathFromCall(a.Messages, i)
			if p == "" {
				continue
			}
			if _, ok := latest[p]; !ok {
				latest[p] = sighting{idx: i, write: true}
			}
		}
	}
	// Walking newest→oldest means latest[p] is always the newest evidence for
	// p; a read whose own index is not the newest sighting is superseded.
	for _, r := range reads {
		s := latest[r.path]
		if s.idx == r.idx || r.idx >= boundary {
			continue // this read IS the newest evidence, or is inside the hot window
		}
		a.Messages[r.idx].Content = supersededNotice(r.path, s)
		rewritten++
	}

	// Pass 2: age decay of big tool outputs past the hot window. Pass-1
	// placeholders are already short, but skip them explicitly so a read that
	// was just superseded isn't counted twice. turns counts authored user
	// messages between the result and the tail.
	turns := 0
	for i := len(a.Messages) - 1; i >= 0; i-- {
		m := &a.Messages[i]
		if m.Role == "user" && m.Authored {
			turns++
		}
		if i >= boundary {
			continue
		}
		if m.Role != "tool" || len(m.Content) <= decayMinBytes || strings.HasPrefix(m.Content, decayedMarker) {
			continue
		}
		m.Content = decayNotice(a.Messages, i, turns)
		rewritten++
	}

	// Pass 3: image decay. An image part past the hot window costs thousands
	// of tokens per request and is almost never looked at again — screenshots
	// of a UI the user has since iterated on, error dialogs already fixed.
	// Swap the part for a text placeholder pointing at the spilled file on
	// disk; the model re-attaches it via @mention if it genuinely needs the
	// pixels again. Text parts of the message stay inline (the prompt that
	// accompanied the image is semantic glue). Runs after Pass 2 so turns
	// already reflects authored-user distance.
	for i := boundary - 1; i > 0; i-- {
		rewritten += stripImageParts(&a.Messages[i])
	}
	return rewritten
}

// stripImageParts replaces every image part of m with a text placeholder
// naming the image's pixel size and the disk path the bytes were spilled to.
// Returns the number of parts stripped. The message's text parts (and
// Content) are untouched.
func stripImageParts(m *llm.Message) int {
	stripped := 0
	var kept []llm.ContentPart
	for _, p := range m.Parts {
		if p.Type != "image_url" || p.ImageURL == nil {
			kept = append(kept, p)
			continue
		}
		path := spillImage(p.ImageURL.URL)
		size := "image"
		if p.W > 0 {
			size = fmt.Sprintf("%d×%d image", p.W, p.H)
		}
		note := decayedMarker + size + " omitted"
		if path != "" {
			note += " — bytes at " + path + " (re-attach with @" + path + " if needed)"
		}
		note += "⟩"
		kept = append(kept, llm.ContentPart{Type: "text", Text: note})
		stripped++
	}
	if stripped == 0 {
		return 0
	}
	// Content stays as it was: the wire form prepends a non-empty Content as
	// its own text part, so mirroring the placeholder there would send it
	// twice. TextContent() already reads text parts for a pure-image message.
	m.Parts = kept
	return stripped
}

// spillImage writes a base64 data URL's bytes to disk and returns the path,
// or "" on any failure (the placeholder then just says the image is gone —
// a failed spill must never break decay). Files live beside bash spills.
func spillImage(dataURL string) string {
	const prefix = ";base64,"
	i := strings.Index(dataURL, prefix)
	if i < 0 {
		return ""
	}
	mime := dataURL[len("data:"):i]
	ext := "png"
	switch mime {
	case "image/jpeg":
		ext = "jpg"
	case "image/gif":
		ext = "gif"
	case "image/webp":
		ext = "webp"
	case "image/bmp":
		ext = "bmp"
	}
	raw, err := base64.StdEncoding.DecodeString(dataURL[i+len(prefix):])
	if err != nil {
		return ""
	}
	dir := filepath.Join(os.TempDir(), fmt.Sprintf("whip-img-%d", os.Getpid()))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return ""
	}
	f, err := os.CreateTemp(dir, "*."+ext)
	if err != nil {
		return ""
	}
	if _, err := f.Write(raw); err != nil {
		_ = f.Close()
		_ = os.Remove(f.Name())
		return ""
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(f.Name())
		return ""
	}
	return f.Name()
}

// hotBoundary returns the index in msgs where the hot window begins. Walking
// from the back, the window covers the newest messages whose approx tokens
// (len/4) fit in decayHotWindow; the first message that overflows the budget
// is OUTSIDE the window, so the boundary is that index — the window is
// msgs[boundary+1:] when the overflow happens at msgs[boundary]. Index 0 (the
// system prompt) is never decayed regardless (it is not a tool message).
func hotBoundary(msgs []llm.Message) int {
	budget := decayHotWindow
	for i := len(msgs) - 1; i > 0; i-- {
		t := msgTokens(msgs[i])
		if t > budget {
			return i // msgs[i] itself no longer fits — it and everything older may decay
		}
		budget -= t
	}
	return 1 // the whole conversation fits in the window
}

// msgTokens approximates a message's token footprint (len/4, matching the
// compaction threshold's heuristic).
func msgTokens(m llm.Message) int {
	n := len(m.Content) + len(m.ToolCallID) + len(m.Name)
	for _, tc := range m.ToolCalls {
		n += len(tc.Function.Name) + len(tc.Function.Arguments)
	}
	t := n / 4
	for _, p := range m.Parts {
		// image parts cost thousands of tokens and carry no Content; without
		// this an image-only screenshot turn never spends the hot budget and
		// Pass 3 never gets to strip it
		t += llm.PartTokens(p)
	}
	return t
}

// readPathFromCall finds the assistant tool_call that produced the tool
// message at i and extracts its path argument. The assistant message carrying
// the call sits somewhere before i (possibly several tool results back).
func readPathFromCall(msgs []llm.Message, i int) string {
	return toolArgFromCall(msgs, i, "path")
}

func writePathFromCall(msgs []llm.Message, i int) string {
	return toolArgFromCall(msgs, i, "path")
}

// toolArgFromCall extracts arg key from the tool_call whose id matches
// msgs[i].ToolCallID. Tool messages immediately follow their assistant
// message, so scanning back to it is short.
func toolArgFromCall(msgs []llm.Message, i int, key string) string {
	id := msgs[i].ToolCallID
	if id == "" {
		return ""
	}
	for j := i - 1; j >= 0; j-- {
		if msgs[j].Role != "assistant" {
			continue
		}
		for _, tc := range msgs[j].ToolCalls {
			if tc.ID != id {
				continue
			}
			var args map[string]any
			if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
				return ""
			}
			if v, ok := args[key].(string); ok {
				return v
			}
			return ""
		}
		return "" // the first assistant message back owns the call; id not in it
	}
	return ""
}

// callArgs returns the raw arguments JSON of the tool call that produced the
// tool message at i ("" when the call can't be found). Used to key duplicate
// reads by their full region (path+offset+limit), not just the path.
func callArgs(msgs []llm.Message, i int) string {
	id := msgs[i].ToolCallID
	if id == "" {
		return ""
	}
	for j := i - 1; j >= 0; j-- {
		if msgs[j].Role != "assistant" {
			continue
		}
		for _, tc := range msgs[j].ToolCalls {
			if tc.ID == id {
				return tc.Function.Arguments
			}
		}
		return ""
	}
	return ""
}

// readLineCount counts the numbered lines a read result rendered ("<n>\t...").
func readLineCount(content string) int {
	n := 0
	for l := range strings.Lines(content) {
		if l != "" {
			n++
		}
	}
	return n
}

// sighting records the newest evidence for a path: the message index of the
// newest read or write of it, whether that evidence is a write, and (for a
// read) how many lines it reported — so a superseded placeholder can point at
// the current vintage.
type sighting struct {
	idx   int  // message index of the newest read/write of this path
	write bool // the newest evidence is a write, not a read
	lines int  // line count the newer read reported (0 for writes)
}

// supersededNotice is the Layer-1 placeholder. A write supersedes without a
// line count ("content changed by write"); a newer read carries its line
// count so the model can re-read precisely if it truly needs the old region.
func supersededNotice(path string, s sighting) string {
	if s.write {
		return fmt.Sprintf("%sread of %s superseded — file changed by a later write/edit⟩", decayedMarker, filepath.Base(path))
	}
	return fmt.Sprintf("%sread of %s superseded by newer read (%d lines)⟩", decayedMarker, filepath.Base(path), s.lines)
}

// decayNotice is the Layer-2 placeholder: what ran (the command for bash, the
// path for file tools), how big the output was, how many authored turns ago
// it landed, and where the full text lives. When the result was never
// truncated at ingestion (no spill marker to inherit), the full content is
// spilled now so the placeholder still points at a recoverable copy.
func decayNotice(msgs []llm.Message, i, turnsAgo int) string {
	m := msgs[i]
	what := m.Name
	switch m.Name {
	case "bash":
		if cmd := toolArgFromCall(msgs, i, "command"); cmd != "" {
			what = fmt.Sprintf("bash %q", firstWords(cmd, 60))
		}
	case "read", "write", "edit":
		if p := toolArgFromCall(msgs, i, "path"); p != "" {
			what = fmt.Sprintf("%s %s", m.Name, filepath.Base(p))
		}
	}
	spill := spillPathOf(m.Content)
	if spill == "" {
		spill = bashrun.Spill(m.Content) // untruncated at ingestion: spill now
	}
	age := ""
	if turnsAgo > 0 {
		age = fmt.Sprintf(" — ran here %d turn(s) ago", turnsAgo)
	}
	size := fmt.Sprintf("%dk bytes", len(m.Content)/1024)
	if spill != "" {
		return fmt.Sprintf("%s%s output, %s%s; full output: %s⟩", decayedMarker, what, size, age, spill)
	}
	return fmt.Sprintf("%s%s output, %s%s⟩", decayedMarker, what, size, age)
}

// firstWords collapses s to one line and truncates at n runes with an
// ellipsis — a compact command preview for placeholders.
func firstWords(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

// spillPathOf extracts the "full output (N bytes): /path" pointer the
// truncation markers carry ("[full output (N bytes): /path]" from bash's
// legacy marker, "— full output (N bytes): /path] ..." from middleElide), so
// the decayed placeholder keeps the recovery path.
func spillPathOf(content string) string {
	i := strings.LastIndex(content, "full output (")
	if i < 0 {
		return ""
	}
	j := strings.Index(content[i:], "): ")
	if j < 0 {
		return ""
	}
	start := i + j + 3
	// the path runs to the next "]" or newline, whichever comes first
	end := strings.IndexAny(content[start:], "]\n")
	if end < 0 {
		return ""
	}
	return strings.TrimSpace(content[start : start+end])
}
