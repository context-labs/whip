package tui

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/context-labs/whip/internal/daemon"
	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/session"
)

type clientExportMsg struct {
	path string
	err  error
}
type transcriptExporter interface {
	HistoryPage(context.Context, daemon.HistoryPageParams) (session.BoundedTranscriptPage, error)
	ReadContent(context.Context, protocol.ContentReadParams) (protocol.ContentReadResult, error)
}

// Explicit exports page the stable host transcript into a temporary file. The
// visible transcript and agent model context are never enlarged by an export.
func exportHostTranscript(ctx context.Context, host transcriptExporter, path, rootID, agentID string) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".whip-export-*")
	if err != nil {
		return err
	}
	defer func() { _ = file.Close(); _ = os.Remove(file.Name()) }()
	if _, err = io.WriteString(file, "# Session transcript\n\n"); err != nil {
		return err
	}
	params := daemon.HistoryPageParams{RootID: rootID, AgentID: agentID, ThroughSeq: -1, Limit: 64, MaxBytes: 256 << 10}
	for {
		page, err := host.HistoryPage(ctx, params)
		if err != nil {
			return err
		}
		if params.Revision != nil && *params.Revision != page.HistoryRevision {
			return session.ErrHistoryRevision
		}
		revision := page.HistoryRevision
		params.Revision = &revision
		params.ThroughSeq = page.ThroughSeq
		for _, entry := range page.Messages {
			message := entry.Message
			if message == nil {
				if entry.Body == nil {
					return errors.New("transcript entry has no body")
				}
				message, err = readTranscriptBody(ctx, host, rootID, agentID, *entry.Body)
				if err != nil {
					return err
				}
			}
			if err = writeTranscriptMessage(file, *message); err != nil {
				return err
			}
		}
		if !page.HasMore {
			break
		}
		if page.NextSeq <= params.AfterSeq {
			return errors.New("transcript pagination did not advance")
		}
		params.AfterSeq = page.NextSeq
	}
	// A destructive edit during the last content download must also invalidate
	// the export; no partial or mixed-revision file is published.
	params.AfterSeq = params.ThroughSeq
	if _, err = host.HistoryPage(ctx, params); err != nil {
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}

func readTranscriptBody(ctx context.Context, host transcriptExporter, rootID, agentID string, body session.RuntimeValue) (*llm.Message, error) {
	if body.Size < 0 || body.Size > daemon.MaxUploadSize {
		return nil, errors.New("transcript body exceeds transfer limit")
	}
	data := make([]byte, 0, int(body.Size))
	for int64(len(data)) < body.Size {
		chunk, err := host.ReadContent(ctx, protocol.ContentReadParams{RootID: rootID, AgentID: agentID, ReferenceID: body.ReferenceID, Offset: int64(len(data)), Limit: 64 << 10})
		if err != nil {
			return nil, err
		}
		if chunk.Content.ReferenceID != body.ReferenceID || chunk.Content.Digest != body.Digest || chunk.Content.Size != body.Size || len(chunk.Data) == 0 || len(chunk.Data) > 64<<10 || int64(len(chunk.Data)) > body.Size-int64(len(data)) {
			return nil, errors.New("invalid transcript content chunk")
		}
		data = append(data, chunk.Data...)
	}
	digest := sha256.Sum256(data)
	if hex.EncodeToString(digest[:]) != body.Digest {
		return nil, errors.New("transcript content digest mismatch")
	}
	var message llm.Message
	if err := json.Unmarshal(data, &message); err != nil {
		return nil, err
	}
	return &message, nil
}

func writeTranscriptMessage(w io.Writer, msg llm.Message) error {
	if msg.Role == "tool" {
		_, err := fmt.Fprintf(w, "#### Tool result\n\n%s\n\n", msg.TextContent())
		return err
	}
	if _, err := fmt.Fprintf(w, "## %s\n\n", displayRole(msg.Role)); err != nil {
		return err
	}
	if text := msg.TextContent(); text != "" {
		if _, err := fmt.Fprintf(w, "%s\n\n", text); err != nil {
			return err
		}
	}
	for _, call := range msg.ToolCalls {
		if _, err := fmt.Fprintf(w, "`%s`\n\n", call.Function.Name); err != nil {
			return err
		}
	}
	return nil
}
