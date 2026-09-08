package agent

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"os"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/tools"
)

// conv builds a message list: system prompt + the given turn messages.
func asstWithCall(id, name, args string) llm.Message {
	return llm.Message{Role: "assistant", ToolCalls: []llm.ToolCall{{
		ID: id, Type: "function",
		Function: struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		}{Name: name, Arguments: args},
	}}}
}

func toolMsg(id, name, content string) llm.Message {
	return llm.Message{Role: "tool", ToolCallID: id, Name: name, Content: content}
}

func readResult(lines int) string {
	var b strings.Builder
	for i := 1; i <= lines; i++ {
		fmt.Fprintf(&b, "%d\tline %d\n", i, i)
	}
	return b.String()
}

// padHotWindow appends filler staying inside the hot window so indices before
// it are eligible for decay. decayHotWindow is approx tokens; msgTokens
// estimates len/4, so the filler needs decayHotWindow*4+ characters.
func padHotWindow(msgs []llm.Message) []llm.Message {
	filler := llm.Message{Role: "assistant", Content: strings.Repeat("y", decayHotWindow*4+100)}
	return append(msgs, filler)
}

func TestDecaySupersededByNewerRead(t *testing.T) {
	a := &Agent{}
	a.Messages = padHotWindow([]llm.Message{
		{Role: "system", Content: "sys"},
		asstWithCall("c1", "read", `{"path":"foo.go"}`),
		toolMsg("c1", "read", readResult(100)),
		asstWithCall("c2", "read", `{"path":"foo.go"}`),
		toolMsg("c2", "read", readResult(80)),
	})

	if n := a.decay(); n != 1 {
		t.Fatalf("rewrites = %d, want 1", n)
	}
	got := a.Messages[2].Content
	want := "⟨read of foo.go superseded by newer read (80 lines)⟩"
	if got != want {
		t.Errorf("old read should collapse to a pointer\ngot:  %q\nwant: %q", got, want)
	}
	// the newer read survives untouched
	if a.Messages[4].Content != readResult(80) {
		t.Error("newest read must stay inline")
	}
	// idempotent: a second pass rewrites nothing
	if n := a.decay(); n != 0 {
		t.Errorf("second pass should be a no-op, rewrote %d", n)
	}
}

func TestDecayNeverRewritesInsideHotWindow(t *testing.T) {
	// The cache-stability invariant is strict: even an obviously superseded
	// read inside the hot window stays byte-stable. The rewrite happens on a
	// later turn, once the window has slid past it.
	a := &Agent{}
	a.Messages = []llm.Message{
		{Role: "system", Content: "sys"},
		asstWithCall("c1", "read", `{"path":"foo.go"}`),
		toolMsg("c1", "read", readResult(100)),
		asstWithCall("c2", "read", `{"path":"foo.go"}`),
		toolMsg("c2", "read", readResult(80)),
	}
	if n := a.decay(); n != 0 {
		t.Fatalf("hot-window content must not decay, rewrote %d", n)
	}
	// once the window slides past (content appended), the old read decays
	a.Messages = padHotWindow(a.Messages)
	if n := a.decay(); n != 1 {
		t.Fatalf("rewrites = %d, want 1", n)
	}
	if !strings.Contains(a.Messages[2].Content, "superseded") {
		t.Errorf("old read should be superseded: %q", a.Messages[2].Content)
	}
}

func TestDecayWriteInvalidatesRead(t *testing.T) {
	a := &Agent{}
	a.Messages = padHotWindow([]llm.Message{
		{Role: "system", Content: "sys"},
		asstWithCall("c1", "read", `{"path":"foo.go"}`),
		toolMsg("c1", "read", readResult(100)),
		asstWithCall("c2", "write", `{"path":"foo.go","content":"x"}`),
		toolMsg("c2", "write", "wrote foo.go"),
	})

	if n := a.decay(); n != 1 {
		t.Fatalf("rewrites = %d, want 1", n)
	}
	got := a.Messages[2].Content
	if !strings.Contains(got, "superseded") || !strings.Contains(got, "write") {
		t.Errorf("write should supersede the read, got %q", got)
	}
	// the write result itself is small — never decayed
	if a.Messages[4].Content != "wrote foo.go" {
		t.Error("small write result must stay inline")
	}
}

func TestDecayHotWindowProtectsRecent(t *testing.T) {
	big := strings.Repeat("x", decayMinBytes*2)
	a := &Agent{}
	// the big tool result is INSIDE the hot window (nothing after it) — no decay
	a.Messages = []llm.Message{
		{Role: "system", Content: "sys"},
		asstWithCall("c1", "bash", `{"command":"go test"}`),
		toolMsg("c1", "bash", big),
	}
	if n := a.decay(); n != 0 {
		t.Errorf("hot-window content must not decay, rewrote %d", n)
	}

	// push it past the window: now it collapses, with size in the placeholder
	a.Messages = padHotWindow(a.Messages)
	if n := a.decay(); n != 1 {
		t.Fatalf("rewrites = %d, want 1", n)
	}
	got := a.Messages[2].Content
	if !strings.HasPrefix(got, "⟨bash ") || !strings.Contains(got, "bytes") {
		t.Errorf("decayed placeholder should name tool and size, got %q", got[:80])
	}
}

func TestDecayNeverTouchesSmallResultsOrAssistant(t *testing.T) {
	big := strings.Repeat("x", decayMinBytes*2)
	a := &Agent{}
	a.Messages = padHotWindow([]llm.Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "q1", Authored: true},
		{Role: "assistant", Content: big}, // big ASSISTANT text: never decayed
		asstWithCall("c1", "bash", `{"command":"grep foo"}`),
		toolMsg("c1", "bash", "3 matches"), // small tool result: never decayed
	})
	if n := a.decay(); n != 0 {
		t.Fatalf("nothing eligible, rewrote %d", n)
	}
	if a.Messages[2].Content != big {
		t.Error("assistant message must be untouched")
	}
	if a.Messages[4].Content != "3 matches" {
		t.Error("small tool result must be untouched")
	}
}

func TestDecayKeepsSpillPath(t *testing.T) {
	spill := tools.Truncate(strings.Repeat("z", 60_000)) // middleElide + spill marker
	a := &Agent{}
	a.Messages = padHotWindow([]llm.Message{
		{Role: "system", Content: "sys"},
		asstWithCall("c1", "read", `{"path":"big.go"}`),
		toolMsg("c1", "read", spill),
	})
	if n := a.decay(); n != 1 {
		t.Fatalf("rewrites = %d, want 1", n)
	}
	got := a.Messages[2].Content
	if !strings.Contains(got, "full output: ") {
		t.Fatalf("decayed placeholder should keep the spill path, got %q", got)
	}
	// the referenced file exists and holds the full output
	i := strings.LastIndex(got, "full output: ")
	path := strings.TrimSuffix(strings.TrimSpace(got[i+len("full output: "):]), "⟩")
	data, err := os.ReadFile(path)
	if err != nil || len(data) != 60_000 {
		t.Errorf("spill file should hold the full 60k output (len=%d, err=%v)", len(data), err)
	}
}

func TestDecayDuplicateReadsSameRegion(t *testing.T) {
	// Identical args returning identical bytes: the LATER copy collapses to a
	// duplicate-pointer (Pass 1b, before supersede), the first stays inline —
	// the history keeps one vintage per (path, offset, limit).
	args := `{"path":"foo.go","offset":10,"limit":50}`
	a := &Agent{}
	a.Messages = padHotWindow([]llm.Message{
		{Role: "system", Content: "sys"},
		asstWithCall("c1", "read", args),
		toolMsg("c1", "read", readResult(50)),
		asstWithCall("c2", "read", args),
		toolMsg("c2", "read", readResult(50)),
	})
	if n := a.decay(); n != 1 {
		t.Fatalf("rewrites = %d, want 1", n)
	}
	if !strings.Contains(a.Messages[4].Content, "duplicate read of foo.go") {
		t.Errorf("the later copy should collapse to a duplicate pointer, got %q", a.Messages[4].Content)
	}
	if a.Messages[2].Content != readResult(50) {
		t.Error("the first copy stays inline")
	}
	// idempotent
	if n := a.decay(); n != 0 {
		t.Errorf("second pass should be a no-op, rewrote %d", n)
	}
}

func TestDecaySameRegionDifferentContentKeepsNewest(t *testing.T) {
	// Same args but the file changed between reads: NOT a duplicate — the
	// newer read is the live vintage (and Layer 1 supersedes the older one
	// via the newer sighting).
	args := `{"path":"foo.go","offset":10,"limit":50}`
	a := &Agent{}
	a.Messages = padHotWindow([]llm.Message{
		{Role: "system", Content: "sys"},
		asstWithCall("c1", "read", args),
		toolMsg("c1", "read", readResult(50)),
		asstWithCall("c2", "read", args),
		toolMsg("c2", "read", readResult(45)), // different content: file changed
	})
	if n := a.decay(); n != 1 {
		t.Fatalf("rewrites = %d, want 1 (Layer-1 supersede only)", n)
	}
	if !strings.Contains(a.Messages[2].Content, "superseded") {
		t.Errorf("older read should be superseded by the newer vintage: %q", a.Messages[2].Content)
	}
	if a.Messages[4].Content != readResult(45) {
		t.Error("the newer, different read must stay inline")
	}
}

func TestSpillPathOfParsesBothMarkerShapes(t *testing.T) {
	legacy := "tail output\n[full output (60000 bytes): /tmp/whip-bash-1/x.log]"
	middle := "head\n... [100 bytes elided from the middle — full output (60000 bytes): /tmp/whip-bash-1/y.log] ...\ntail"
	if got := spillPathOf(legacy); got != "/tmp/whip-bash-1/x.log" {
		t.Errorf("legacy marker: %q", got)
	}
	if got := spillPathOf(middle); got != "/tmp/whip-bash-1/y.log" {
		t.Errorf("middle-elide marker: %q", got)
	}
	if got := spillPathOf("no marker here"); got != "" {
		t.Errorf("no marker should give empty, got %q", got)
	}
}

// Image parts past the hot window are swapped for a text placeholder naming
// the pixel size and the spilled file; text parts and Content stay. The image
// is recoverable by re-attaching the spilled path.
func TestDecayStripsColdImageParts(t *testing.T) {
	png := pngFixtureForDecay(t, 640, 480) // ⌈640/28⌉=23, ⌈480/28⌉=18 → 414 tokens
	img := llm.ImagePart("png", png)

	a := &Agent{}
	a.Messages = padHotWindow([]llm.Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "look at this", Parts: []llm.ContentPart{{Type: "text", Text: "look at this"}, img}},
	})
	before := EstimateTokens(a.Messages)
	n := a.decay()
	if n == 0 {
		t.Fatal("image past the hot window should be stripped")
	}
	m := a.Messages[1]
	// no image parts remain
	for _, p := range m.Parts {
		if p.Type == "image_url" {
			t.Fatal("image part should be replaced")
		}
	}
	// the placeholder lands in Content (the one text field that survives a
	// persist/reload round trip) after the user's own text
	placeholder := m.Content
	if !strings.HasPrefix(placeholder, "look at this\n") || !strings.Contains(placeholder, "omitted") {
		t.Fatalf("Content should keep the user's text and append the placeholder, got %q", placeholder)
	}
	if !strings.Contains(placeholder, "640×480") {
		t.Errorf("placeholder should name the pixel size, got %q", placeholder)
	}
	if !strings.Contains(placeholder, "whip-img-") {
		t.Errorf("placeholder should point at the spilled file, got %q", placeholder)
	}
	// collapsed to plain text: no parts left, the user's text leads Content
	if len(m.Parts) != 0 {
		t.Errorf("stripped message should carry no parts, got %+v", m.Parts)
	}
	// the estimate dropped by roughly the image's token cost
	after := EstimateTokens(a.Messages)
	if before-after < 300 {
		t.Errorf("stripping should drop the estimate (before=%d after=%d)", before, after)
	}
	// idempotent
	if n := a.decay(); n != 0 {
		t.Errorf("second decay should be a no-op, rewrote %d", n)
	}
}

// pngFixtureForDecay builds a real PNG so ImagePart can record dims.
func pngFixtureForDecay(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode fixture: %v", err)
	}
	return buf.Bytes()
}

// Image-only messages (screenshot steers, ACP image blocks) carry no Content,
// so they must spend the hot-window budget through their parts: a run of
// large images alone pushes the oldest past the window and Pass 3 strips it.
func TestDecayImageOnlyMessagesSpendHotWindow(t *testing.T) {
	big := llm.ImagePart("png", pngFixtureForDecay(t, 2000, 2000)) // ⌈2000/28⌉² = 5184 tokens
	a := &Agent{}
	a.Messages = []llm.Message{{Role: "system", Content: "sys"}}
	for range 6 { // 6 × 5184 > the 24k hot window with no text at all
		a.Messages = append(a.Messages, llm.Message{Role: "user", Parts: []llm.ContentPart{big}})
	}
	if n := a.decay(); n == 0 {
		t.Fatal("the oldest image-only message should fall past the hot window and be stripped")
	}
	if len(a.Messages[1].Parts) != 0 || !strings.Contains(a.Messages[1].Content, "omitted") {
		t.Fatalf("oldest message should now be a text placeholder in Content, got parts=%+v content=%q", a.Messages[1].Parts, a.Messages[1].Content)
	}
	last := a.Messages[len(a.Messages)-1]
	if last.Parts[0].Type != "image_url" {
		t.Fatal("the newest image must stay hot")
	}
}

// The decayed message must survive a persist/reload round trip with the
// user's text intact: UnmarshalJSON keeps only the last text part as Content.
func TestDecayedImageMessageRoundTripsKeepingText(t *testing.T) {
	img := llm.ImagePart("png", pngFixtureForDecay(t, 640, 480))
	m := llm.Message{Role: "user", Content: "look at this", Parts: []llm.ContentPart{{Type: "text", Text: "look at this"}, img}}
	if stripImageParts(&m) != 1 {
		t.Fatal("one image should strip")
	}
	raw, err := m.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	var back llm.Message
	if err := back.UnmarshalJSON(raw); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(back.TextContent(), "look at this") || !strings.Contains(back.TextContent(), "omitted") {
		t.Fatalf("reload should keep the user's text and the placeholder, got %q", back.TextContent())
	}
	if strings.Count(string(raw), "omitted") != 1 {
		t.Fatalf("placeholder must be sent once on the wire, got %d in %s", strings.Count(string(raw), "omitted"), raw)
	}
}

// spillImage must never panic on a persisted URL that isn't one we built.
func TestSpillImageRejectsNonDataURL(t *testing.T) {
	for _, u := range []string{"x;base64,y", ";base64,abcd", "http://example/img.png", ""} {
		if got := spillImage(u); got != "" {
			t.Errorf("spillImage(%q) = %q, want \"\"", u, got)
		}
	}
}
