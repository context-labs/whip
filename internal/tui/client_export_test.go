package tui

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/daemon"
	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/session"
)

type exportTestHost struct {
	data  []byte
	body  session.RuntimeValue
	pages int
	fail  bool
}

func (h *exportTestHost) HistoryPage(_ context.Context, p daemon.HistoryPageParams) (session.BoundedTranscriptPage, error) {
	h.pages++
	if p.RootID != "root" || p.AgentID != "child" || p.Limit > 64 || p.MaxBytes > 256<<10 {
		return session.BoundedTranscriptPage{}, errors.New("unbounded or wrong scope")
	}
	page := session.BoundedTranscriptPage{HistoryRevision: 3, ThroughSeq: 4, NextSeq: 4}
	if h.pages > 1 && (p.Revision == nil || *p.Revision != 3 || p.ThroughSeq != 4) {
		return page, errors.New("missing stable view")
	}
	switch h.pages {
	case 1:
		page.Messages = []session.TranscriptPageEntry{{Seq: 1, Message: &llm.Message{Role: "user", Content: "oldest"}}}
		page.HasMore = true
		page.NextSeq = 1
	case 2:
		if h.fail {
			return page, session.ErrHistoryRevision
		}
		page.Messages = []session.TranscriptPageEntry{{Seq: 4, Body: &h.body}}
	}
	return page, nil
}
func (h *exportTestHost) ReadContent(_ context.Context, p protocol.ContentReadParams) (protocol.ContentReadResult, error) {
	end := min(len(h.data), int(p.Offset)+p.Limit)
	return protocol.ContentReadResult{Data: h.data[p.Offset:end], Content: protocol.ContentHandle{ReferenceID: h.body.ReferenceID, Digest: h.body.Digest, Size: h.body.Size}}, nil
}
func TestFullExportPagesAndResolvesLargeChildBody(t *testing.T) {
	data, _ := json.Marshal(llm.Message{Role: "assistant", Content: strings.Repeat("complete", 20000)})
	digest := sha256.Sum256(data)
	host := &exportTestHost{data: data, body: session.RuntimeValue{ReferenceID: "ref", Digest: hex.EncodeToString(digest[:]), Size: int64(len(data))}}
	path := filepath.Join(t.TempDir(), "transcript.md")
	if err := exportHostTranscript(t.Context(), host, path, "root", "child"); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "oldest") || strings.Count(string(body), "complete") != 20000 || host.pages != 3 {
		t.Fatalf("incomplete export: pages=%d bytes=%d", host.pages, len(body))
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o600 {
		t.Fatal("export permissions")
	}
}
func TestFullExportRevisionFailurePreservesExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transcript.md")
	if err := os.WriteFile(path, []byte("previous"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := exportHostTranscript(t.Context(), &exportTestHost{fail: true}, path, "root", "child"); !errors.Is(err, session.ErrHistoryRevision) {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(path)
	if string(body) != "previous" {
		t.Fatal("partial export replaced destination")
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Fatal("temporary export leaked")
	}
}
func TestOversizedStreamIsExplicitlyMarked(t *testing.T) {
	m := &model{width: 80, height: 24}
	handled, _ := m.applyClientStream("stream.text", []byte(`{"truncated":true,"content":{"reference_id":"ref"}}`))
	if !handled || len(m.blocks) != 1 || !strings.Contains(m.blocks[0].text, "Live output omitted") {
		t.Fatal("silent stream omission")
	}
}
