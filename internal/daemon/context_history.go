package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/context-labs/whip/internal/llm"
	sessionstore "github.com/context-labs/whip/internal/session"
)

// historyView freezes the upper sequence, including the immutable journal
// prefix observed during this turn. No message body is copied or serialized
// merely to create a view. Committed rows are read one at a time from SQLite.
type historyView struct {
	node         *AgentSession
	through      int
	committed    int
	count        int
	turnID       string
	cursorTurnID string
	pending      []llm.Message
}

func (host *recursiveHost) historyView(ctx context.Context, arguments map[string]any) (historyView, error) {
	node := host.session
	if node == nil || node.root == nil {
		return historyView{}, errors.New("history requires a bound session")
	}
	for key, own := range map[string]string{"root_id": node.root.ID(), "agent_id": node.id} {
		if target, _ := stringArgument(arguments, key); target != "" && target != own {
			return historyView{}, sessionstore.ErrAgentAccess
		}
	}
	bounds, err := node.root.store.TranscriptBounds(ctx, node.root.ID(), node.id)
	if err != nil {
		return historyView{}, err
	}
	node.mu.Lock()
	// Bodies and nested slices are immutable; clone headers because final
	// tool metadata can replace a journal entry before the batch settles.
	pending, turnID := slices.Clone(node.turn.Messages), node.turn.TurnID
	node.mu.Unlock()
	cursorTurnID, _ := stringArgument(arguments, "turn_id")
	if cursorTurnID != "" && cursorTurnID != turnID {
		return historyView{}, errors.New("provisional history view expired; inspect history again")
	}
	through, count := bounds.LastSeq, bounds.Count
	for _, message := range pending {
		if message.RawSequence > bounds.LastSeq {
			through = max(through, message.RawSequence)
			count++
		}
	}
	requested := intArgument(arguments, "through_seq", -1)
	if requested < -1 || requested > through {
		return historyView{}, errors.New("history upper sequence is unavailable; inspect history again")
	}
	if requested >= 0 {
		through = requested
	}
	if through > bounds.LastSeq {
		cursorTurnID = turnID
	}
	return historyView{node: node, through: through, committed: bounds.LastSeq, count: count, turnID: turnID, cursorTurnID: cursorTurnID, pending: pending}, nil
}

// Provisional sequence numbers can be reused after an uncommitted turn is
// abandoned. Carry the originating turn through every continuation, including
// after commit, so the next turn cannot silently substitute a different row.
func (view historyView) cursor(value map[string]any) map[string]any {
	value["through_seq"] = view.through
	if view.cursorTurnID != "" {
		value["turn_id"] = view.cursorTurnID
	}
	return value
}

func (view historyView) next(ctx context.Context, after int) (llm.Message, bool, error) {
	if err := ctx.Err(); err != nil {
		return llm.Message{}, false, err
	}
	if after < min(view.committed, view.through) {
		page, err := view.node.root.store.ReadTranscript(ctx, view.node.root.ID(), view.node.id, after, min(view.committed, view.through), 1)
		if err != nil {
			return llm.Message{}, false, err
		}
		if len(page.Messages) > 0 {
			return page.Messages[0].Message, true, nil
		}
	}
	for _, message := range view.pending {
		if message.RawSequence > max(after, view.committed) && message.RawSequence <= view.through {
			return message, true, nil
		}
	}
	return llm.Message{}, false, nil
}

func (view historyView) source(message llm.Message) map[string]any {
	result := map[string]any{"agent_id": view.node.id, "seq": message.RawSequence, "provisional": message.RawSequence > view.committed}
	if message.RawSequence > view.committed {
		result["turn_id"] = view.turnID
	}
	return result
}

func (host *recursiveHost) history(ctx context.Context, operation string, arguments map[string]any) (any, error) {
	for _, key := range []string{"seq", "after_seq", "through_seq", "offset", "length", "limit"} {
		if raw, present := arguments[key]; present {
			value, ok := raw.(float64)
			if !ok || math.IsNaN(value) || math.IsInf(value, 0) || value != math.Trunc(value) || math.Abs(value) > (1<<53)-1 {
				return nil, fmt.Errorf("history %s must be an exactly representable integer", key)
			}
		}
	}
	view, err := host.historyView(ctx, arguments)
	if err != nil {
		return nil, err
	}
	if operation == "inspect" {
		return view.cursor(map[string]any{"source": "agent history", "agent_id": view.node.id, "committed_through_seq": view.committed, "retained_messages": view.count, "read": "context.history(after_seq=0) or context.history(seq=N, offset=0, length=8192)"}), nil
	}
	if operation == "search" {
		return view.search(ctx, arguments)
	}
	if operation != "history" {
		return nil, errors.New("history is paged; use context.history(), or provide a content handle to context.read")
	}
	sequence := intArgument(arguments, "seq", 0)
	after := intArgument(arguments, "after_seq", 0)
	if sequence < 0 || after < 0 {
		return nil, errors.New("history sequences must be nonnegative")
	}
	if sequence > 0 {
		message, exists, err := view.next(ctx, sequence-1)
		if err != nil {
			return nil, err
		}
		if !exists || message.RawSequence != sequence {
			return nil, errors.New("history message is unavailable in this view")
		}
		field, _ := stringArgument(arguments, "field")
		var body, messageRevision string
		if field == "" || field == "message" {
			message.Continuation = llm.ResponseContinuation{}
			data, err := json.Marshal(message)
			if err != nil {
				return nil, err
			}
			body, field = string(data), "message"
			// Tool completion can replace metadata in a provisional message.
			// A byte cursor into serialized JSON must fail explicitly if that
			// replacement shifted its offsets between reads.
			messageRevision = fmt.Sprintf("%x", sha256.Sum256(data))
			if expected, _ := stringArgument(arguments, "message_revision"); expected != "" && expected != messageRevision {
				return nil, errors.New("history message changed; retry reading this message from offset 0")
			}
		} else {
			found := false
			for _, candidate := range historyFields(message) {
				if candidate.name == field {
					body, found = candidate.text, true
					break
				}
			}
			if !found {
				return nil, errors.New("unknown history text field")
			}
		}
		offset, length := intArgument(arguments, "offset", 0), intArgument(arguments, "length", sessionstore.InlineValueLimit)
		if offset < 0 || offset > len(body) || length < 1 || length > sessionstore.InlineValueLimit {
			return nil, errors.New("history read requires an in-range offset and length 1..8192")
		}
		if offset < len(body) && !utf8.RuneStart(body[offset]) {
			return nil, errors.New("history offset must be a UTF-8 boundary")
		}
		text := utf8PrefixRuntime(body[offset:], length)
		if text == "" && offset < len(body) {
			return nil, errors.New("history length is too small for the next UTF-8 character")
		}
		result := view.cursor(view.source(message))
		result["field"], result["text"], result["size"] = field, text, len(body)
		result["span"] = map[string]any{"start": offset, "end": offset + len(text)}
		result["through_seq"], result["truncated"] = view.through, offset+len(text) < len(body)
		if offset+len(text) < len(body) {
			next := view.cursor(map[string]any{"seq": sequence, "field": field, "offset": offset + len(text)})
			if messageRevision != "" {
				next["message_revision"] = messageRevision
			}
			result["next"] = next
		}
		return result, nil
	}
	limit := intArgument(arguments, "limit", 20)
	if limit < 1 || limit > 20 {
		return nil, errors.New("history page limit must be 1..20")
	}
	rows, used := make([]map[string]any, 0, limit), 0
	for len(rows) < limit {
		message, exists, err := view.next(ctx, after)
		if err != nil {
			return nil, err
		}
		if !exists {
			break
		}
		row := view.source(message)
		row["role"], row["preview"] = message.Role, utf8PrefixRuntime(message.TextContent(), 256)
		row["parts"], row["tool_calls"] = len(message.Parts), len(message.ToolCalls)
		encoded, err := json.Marshal(row)
		if err != nil {
			return nil, err
		}
		if used+len(encoded) > 6<<10 {
			break
		}
		rows, used, after = append(rows, row), used+len(encoded), message.RawSequence
	}
	_, more, err := view.next(ctx, after)
	if err != nil {
		return nil, err
	}
	return view.cursor(map[string]any{"agent_id": view.node.id, "messages": rows, "next_seq": after, "truncated": more}), nil
}

type historyField struct{ name, text string }

func historyFields(message llm.Message) []historyField {
	fields := []historyField{{"content", message.Content}}
	for i, part := range message.Parts {
		if part.Type == "text" {
			fields = append(fields, historyField{fmt.Sprintf("parts.%d.text", i), part.Text})
		}
	}
	for i, call := range message.ToolCalls {
		fields = append(fields, historyField{fmt.Sprintf("tool_calls.%d.arguments", i), call.Function.Arguments})
	}
	return fields
}

func (view historyView) search(ctx context.Context, arguments map[string]any) (any, error) {
	query, _ := stringArgument(arguments, "query")
	if query == "" || len(query) > maxSearchQueryBytes {
		return nil, fmt.Errorf("query must contain 1 to %d bytes", maxSearchQueryBytes)
	}
	sequence, after := intArgument(arguments, "seq", 0), intArgument(arguments, "after_seq", 0)
	field, _ := stringArgument(arguments, "field")
	offset := int64Argument(arguments, "offset", 0)
	if sequence < 0 || after < 0 || offset < 0 || sequence == 0 && (field != "" || offset != 0) {
		return nil, errors.New("invalid history search cursor")
	}
	if sequence > 0 {
		after = sequence - 1
	}
	matches := []map[string]any{}
	var scanned int64
	result := view.cursor(map[string]any{"agent_id": view.node.id})
	finish := func(reason string, next map[string]any) (any, error) {
		result["matches"], result["scanned"] = matches, scanned
		result["truncated"], result["stop_reason"] = next != nil, reason
		if next != nil {
			result["next"] = view.cursor(next)
		}
		return result, nil
	}
	for messages := 0; ; messages++ {
		message, exists, err := view.next(ctx, after)
		if err != nil {
			return nil, err
		}
		if !exists {
			if sequence > 0 {
				return nil, errors.New("history search message is unavailable")
			}
			return finish("end", nil)
		}
		if sequence > 0 && message.RawSequence != sequence {
			return nil, errors.New("history search message is unavailable")
		}
		if messages == 128 {
			return finish("message_limit", map[string]any{"after_seq": after})
		}
		fields := historyFields(message)
		start := 0
		if field != "" {
			start = -1
			for i := range fields {
				if fields[i].name == field {
					start = i
					break
				}
			}
			if start < 0 {
				return nil, errors.New("history search field is unavailable")
			}
		}
		for _, candidate := range fields[start:] {
			next := map[string]any{"seq": message.RawSequence, "field": candidate.name, "offset": offset}
			if len(matches) == maxSearchMatches {
				return finish("match_limit", next)
			}
			if maxSearchScan-scanned < int64(len(query)) {
				return finish("scan_limit", next)
			}
			reader := strings.NewReader(candidate.text)
			found, err := searchLiteral(ctx, func(ctx context.Context, at int64, length int) ([]byte, error) {
				if err := ctx.Err(); err != nil {
					return nil, err
				}
				data := make([]byte, length)
				n, err := reader.ReadAt(data, at)
				return data[:n], err
			}, int64(len(candidate.text)), query, offset, maxSearchScan-scanned, maxSearchMatches-len(matches))
			if err != nil {
				return nil, err
			}
			scanned += found.Scanned
			for _, match := range found.Matches {
				value := view.source(message)
				value["field"], value["text"] = candidate.name, match.Text
				value["span"] = map[string]any{"start": match.Start, "end": match.End}
				value["text_span"] = map[string]any{"start": match.TextStart, "end": match.TextEnd}
				matches = append(matches, value)
			}
			if found.Truncated {
				next["offset"] = found.NextOffset
				return finish(found.StopReason, next)
			}
			offset = 0
		}
		after, sequence, field = message.RawSequence, 0, ""
	}
}
