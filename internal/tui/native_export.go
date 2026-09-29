package tui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/context-labs/whip/internal/client"
	"github.com/context-labs/whip/internal/protocol"
)

func (m *nativeModel) exportNative(path string) tea.Cmd {
	owner, connection, generation := m.owner.ID, m.connection, m.generation
	if path == "" {
		path = "whip-transcript-" + string(owner) + ".md"
	}
	path, err := filepath.Abs(path)
	if err != nil {
		m.status = err.Error()
		return nil
	}
	m.controlling = true
	m.input.Reset()
	return func() tea.Msg {
		value := nativeControlResult{generation: generation, label: "Transcript exported to " + path}
		ctx, done, err := m.work.beginFor(10 * time.Minute)
		if err != nil {
			value.err = err
			return value
		}
		defer done()
		value.err = exportNativeTranscript(ctx, connection, owner, path)
		return value
	}
}

// Export pins the first canonical high water and streams bounded pages. Appends
// may continue; a destructive edit invalidates the export before publication.
func exportNativeTranscript(ctx context.Context, connection *client.Client, owner protocol.ID, path string) error {
	handle, err := connection.Session(owner)
	if err != nil {
		return err
	}
	var snapshot protocol.HistorySnapshot
	if err := connection.Call(ctx, "context.snapshot", protocol.SessionParams{SessionID: owner}, &snapshot); err != nil {
		return err
	}
	if snapshot.SessionID != owner {
		return errors.New("export snapshot ownership mismatch")
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".whip-export-*")
	if err != nil {
		return err
	}
	defer func() { _ = file.Close(); _ = os.Remove(file.Name()) }()
	if _, err := fmt.Fprintf(file, "# Session transcript\n\nOwner: %s · revision: %d · through: %d\n\nContent references below remain scoped to this owner; attachment bytes are not embedded.\n\n", owner, snapshot.Revision, snapshot.ThroughSequence); err != nil {
		return err
	}
	params := protocol.HistoryPageParams{Direction: "forward", ExpectedRevision: &snapshot.Revision, Limit: 64}
	var count protocol.Counter
	for count < snapshot.MessageCount {
		page, err := handle.History(ctx, params)
		if err != nil {
			return err
		}
		for _, message := range page.Messages {
			if message.Sequence > snapshot.ThroughSequence {
				break
			}
			if err := writeNativeExportMessage(file, message); err != nil {
				return err
			}
			count++
		}
		if count >= snapshot.MessageCount {
			break
		}
		if page.NextCursor == nil || *page.NextCursor >= snapshot.ThroughSequence {
			return errors.New("export ended before its captured message count")
		}
		params.Cursor = page.NextCursor
	}
	var current protocol.HistorySnapshot
	if err := connection.Call(ctx, "context.snapshot", protocol.SessionParams{SessionID: owner}, &current); err != nil {
		return err
	}
	if count != snapshot.MessageCount || current.SessionID != owner || current.Revision != snapshot.Revision {
		return errors.New("history changed during export; no file was published")
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}

func writeNativeExportMessage(output io.Writer, message protocol.Message) error {
	if _, err := fmt.Fprintf(output, "## %s · #%d · %s\n\n", message.Role, message.Sequence, message.ID); err != nil {
		return err
	}
	for _, part := range message.Parts {
		var text string
		switch {
		case part.Type == "text":
			text = part.Text
		case part.Type == "tool_call" && part.Call != nil:
			text = fmt.Sprintf("Tool call: %s · %s\n\n%s", part.Call.Name, part.Call.ID, part.Call.Arguments)
		case part.Type == "tool_result" && part.Result != nil:
			text = fmt.Sprintf("Tool result: %s · error: %t\n\n%s", part.Result.CallID, part.Result.IsError, part.Result.Output)
		case part.Type == "content":
			text = "Content reference: " + string(message.SessionID) + "/" + string(part.ReferenceID)
		default:
			return fmt.Errorf("unsupported transcript part %q; no incomplete export was published", part.Type)
		}
		if _, err := fmt.Fprintln(output, text+"\n"); err != nil {
			return err
		}
	}
	return nil
}
