package tui

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/context-labs/whip/internal/client"
	"github.com/context-labs/whip/internal/protocol"
)

const (
	nativeHistoryMessages = 512
	nativeHistoryBytes    = 8 << 20
)

// nativeTranscript is one bounded, replaceable display window. The canonical
// owner, group, input and imported-source identities stay in Message unchanged.
// It has no authority to synthesize execution links or rewrite host history.
type nativeTranscript struct {
	owner      protocol.ID
	snapshot   protocol.HistorySnapshot
	messages   []protocol.Message
	bytes      int
	earlier    bool
	epoch      protocol.ID
	preview    *protocol.MessagePreview
	cellOutput *protocol.CellOutputPreview
}

func (v *nativeTranscript) replace(page protocol.HistoryPageResult) error {
	messages, size, err := v.checked(page.Snapshot, page.Messages)
	if err != nil {
		return err
	}
	v.snapshot, v.messages, v.bytes = page.Snapshot, messages, size
	v.earlier = page.NextCursor != nil
	v.epoch = ""
	v.preview, v.cellOutput = nil, nil
	return nil
}

func (v *nativeTranscript) checked(snapshot protocol.HistorySnapshot, messages []protocol.Message) ([]protocol.Message, int, error) {
	if snapshot.SessionID != v.owner || v.owner == "" || snapshot.Revision <= 0 || snapshot.ThroughSequence < 0 || len(messages) > nativeHistoryMessages {
		return nil, 0, errors.New("invalid native transcript owner, revision or page size")
	}
	seen := make(map[protocol.ID]bool, len(messages))
	var previous protocol.Counter
	for _, message := range messages {
		if message.SessionID != v.owner || message.ID == "" || message.GroupID == "" || seen[message.ID] || message.Sequence <= previous || message.Sequence > snapshot.ThroughSequence || message.RetiredBy != nil || message.RetiredRevision != nil {
			return nil, 0, errors.New("invalid native transcript message identity or sequence")
		}
		seen[message.ID], previous = true, message.Sequence
	}
	raw, err := json.Marshal(messages)
	if err != nil {
		return nil, 0, err
	}
	if len(raw) > nativeHistoryBytes {
		return nil, 0, errors.New("native transcript page exceeds 8 MiB; request a smaller page")
	}
	var copyValue []protocol.Message
	if err := json.Unmarshal(raw, &copyValue); err != nil {
		return nil, 0, err
	}
	return copyValue, len(raw), nil
}

func (v *nativeTranscript) observe(page client.Observation) error {
	if page.Epoch == "" || page.Preview != nil && len(page.Preview.Text)+len(page.Preview.Reasoning) > 128<<10 {
		return errors.New("invalid native preview epoch or byte bound")
	}
	if page.Reset {
		if err := v.replace(protocol.HistoryPageResult{Snapshot: page.Snapshot, Messages: page.Messages}); err != nil {
			return err
		}
	} else {
		if v.snapshot.Revision != 0 && (v.snapshot.Revision != page.Snapshot.Revision || v.snapshot.ThroughSequence > page.Snapshot.ThroughSequence) {
			return errors.New("history revision changed; reload the native transcript")
		}
		incoming, _, err := v.checked(page.Snapshot, page.Messages)
		if err != nil {
			return err
		}
		if len(incoming) == 0 {
			v.snapshot = page.Snapshot
		} else {
			messages := append([]protocol.Message{}, v.messages...)
			seen := make(map[protocol.ID]bool, len(messages))
			for _, message := range messages {
				seen[message.ID] = true
			}
			for _, message := range incoming {
				if seen[message.ID] || len(messages) != 0 && message.Sequence <= messages[len(messages)-1].Sequence {
					return errors.New("native transcript observation did not advance")
				}
				messages = append(messages, message)
			}
			// Evict only complete oldest messages and keep an explicit history gap.
			// Each already validated incoming page must itself fit; no body is cut.
			sizes := make([]int, len(messages))
			size := 2 // JSON array brackets.
			for i, message := range messages {
				raw, err := json.Marshal(message)
				if err != nil {
					return err
				}
				sizes[i] = len(raw) + 1
				size += sizes[i]
			}
			if len(messages) > 0 {
				size--
			} // The last message has no trailing comma.
			drop := 0
			for len(messages)-drop > nativeHistoryMessages || size > nativeHistoryBytes {
				size -= sizes[drop]
				drop++
			}
			v.messages, v.snapshot, v.bytes, v.earlier = slices.Clone(messages[drop:]), page.Snapshot, size, v.earlier || drop > 0
		}
	}
	if v.epoch != page.Epoch {
		v.preview, v.cellOutput = nil, nil
	}
	v.epoch = page.Epoch
	v.preview = nil
	if page.Cursor.After >= page.Snapshot.ThroughSequence && page.Preview != nil && !slices.ContainsFunc(v.messages, func(m protocol.Message) bool { return m.ID == page.Preview.MessageID }) {
		value := *page.Preview
		value.Calls = nil // Incomplete argument bytes are never canonical calls.
		v.preview = &value
	}
	if v.cellOutput != nil && v.settledCall(v.cellOutput.CallID) {
		v.cellOutput = nil
	}
	return nil
}

func (v *nativeTranscript) output(value protocol.CellOutput) {
	v.cellOutput = nil
	p := value.Preview
	if p == nil || value.Epoch != v.epoch || p.SessionID != v.owner || p.HistoryRevision != v.snapshot.Revision || len(p.Text) > 64<<10 || v.settledCall(p.CallID) {
		return
	}
	copyValue := *p
	v.cellOutput = &copyValue
}

func (v *nativeTranscript) settledCall(id protocol.ID) bool {
	for _, message := range v.messages {
		for _, part := range message.Parts {
			if part.Result != nil && part.Result.CallID == id {
				return true
			}
		}
	}
	return false
}

// nativeMessageText is a display projection, never a provider transcript. In
// particular, attachment handles and imported provenance remain references;
// displaying them does not read another owner or manufacture a local turn.
func nativeMessageText(message protocol.Message) string {
	var result strings.Builder
	for _, part := range message.Parts {
		if result.Len() > 0 {
			result.WriteString("\n")
		}
		switch part.Type {
		case "text":
			result.WriteString(part.Text)
		case "content":
			fmt.Fprintf(&result, "[attachment %s]", part.ReferenceID)
		case "tool_call":
			if part.Call != nil {
				fmt.Fprintf(&result, "%s · %s\n%s", part.Call.Name, part.Call.ID, part.Call.Arguments)
			}
		case "tool_result":
			if part.Result != nil {
				label := "result"
				if part.Result.IsError {
					label = "failed result"
				}
				fmt.Fprintf(&result, "%s · %s\n%s", label, part.Result.CallID, part.Result.Output)
			}
		}
	}
	return result.String()
}
