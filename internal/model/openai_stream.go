package model

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/context-labs/whip/internal/session"
)

const (
	maxStreamLines  = 65536
	maxStreamEvents = 32768
	maxStreamChunks = 65536
)

type streamCall struct {
	id, name, arguments strings.Builder
}

type chatStream struct {
	response Response
	text     strings.Builder
	calls    map[int]*streamCall
	order    []int
	finish   string
	bytes    int
	chunks   int
	allowed  map[string]bool
	emit     func(Chunk)
}

func streamError(message string) error {
	return &CallError{Uncertain: true, Message: message}
}

// Keep delimiters in scanner tokens so the total byte bound also counts blank
// lines, comments, and CRLF. The limited reader bounds allocation before parsing.
func splitStreamLine(data []byte, atEOF bool) (int, []byte, error) {
	if i := bytes.IndexByte(data, '\n'); i >= 0 {
		return i + 1, data[:i+1], nil
	}
	if atEOF && len(data) > 0 {
		return len(data), data, nil
	}
	return 0, nil, nil
}

// decodeChatStream emits provisional fragments only. Parts become available
// after both the provider's finish reason and the transport's completion marker.
func decodeChatStream(ctx context.Context, reader io.Reader, allowed map[string]bool, emit func(Chunk)) (Response, error) {
	state := chatStream{calls: make(map[int]*streamCall), allowed: allowed, emit: emit}
	scanner := bufio.NewScanner(io.LimitReader(reader, maxResponseBytes+1))
	scanner.Buffer(make([]byte, 4096), maxResponseBytes+2)
	scanner.Split(splitStreamLine)
	var frame strings.Builder
	hasData := false
	lines, events, total := 0, 0, 0
	consume := func() (bool, error) {
		if !hasData {
			return false, nil
		}
		events++
		if events > maxStreamEvents {
			return false, streamError("provider stream exceeded the event limit")
		}
		data := frame.String()
		frame.Reset()
		hasData = false
		if strings.TrimSpace(data) == "[DONE]" {
			if err := state.complete(); err != nil {
				return false, err
			}
			return true, nil
		}
		return false, state.consume([]byte(data))
	}
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return state.response, errors.Join(err, streamError("provider stream was interrupted; outcome is unknown"))
		}
		lines++
		total += len(scanner.Bytes())
		if total > maxResponseBytes || lines > maxStreamLines {
			return state.response, streamError("provider stream exceeded the framing limit")
		}
		line := strings.TrimSuffix(strings.TrimSuffix(scanner.Text(), "\n"), "\r")
		if line == "" {
			done, err := consume()
			if err != nil || done {
				return state.response, err
			}
		} else if data, ok := strings.CutPrefix(line, "data:"); ok {
			if hasData {
				frame.WriteByte('\n')
			}
			frame.WriteString(strings.TrimPrefix(data, " "))
			hasData = true
		}
	}
	if err := ctx.Err(); err != nil {
		return state.response, errors.Join(err, streamError("provider stream was interrupted; outcome is unknown"))
	}
	if scanner.Err() != nil {
		return state.response, streamError("provider stream was incomplete or exceeded the framing limit")
	}
	// Some compatible servers close immediately after their last data line.
	// That final event must still contain the explicit completion marker.
	if done, err := consume(); err != nil || done {
		return state.response, err
	}
	return state.response, streamError("provider stream ended without a complete response")
}

func (s *chatStream) consume(raw []byte) error {
	var envelope struct {
		Choices json.RawMessage `json:"choices"`
		Usage   json.RawMessage `json:"usage"`
		Error   json.RawMessage `json:"error"`
	}
	if !utf8.Valid(raw) || json.Unmarshal(raw, &envelope) != nil {
		return streamError("provider returned malformed stream data")
	}
	s.account(envelope.Usage)
	if len(envelope.Error) > 0 && string(envelope.Error) != "null" {
		return streamError("provider reported a stream failure; outcome is unknown")
	}
	var choices []struct {
		Index        int             `json:"index"`
		Delta        json.RawMessage `json:"delta"`
		FinishReason string          `json:"finish_reason"`
	}
	if len(envelope.Choices) == 0 || string(envelope.Choices) == "null" || json.Unmarshal(envelope.Choices, &choices) != nil || len(choices) > 1 {
		return streamError("provider returned invalid stream choices")
	}
	if len(choices) == 0 {
		if len(envelope.Usage) == 0 || string(envelope.Usage) == "null" {
			return streamError("provider returned an empty stream event")
		}
		return nil
	}
	choice := choices[0]
	if choice.Index != 0 {
		return streamError("provider returned an invalid stream completion boundary")
	}
	var delta struct {
		Role      string `json:"role"`
		Content   string `json:"content"`
		Reasoning string `json:"reasoning_content"`
		ToolCalls []struct {
			Index    *int             `json:"index"`
			ID       string           `json:"id"`
			Type     string           `json:"type"`
			Function chatFunctionCall `json:"function"`
		} `json:"tool_calls"`
	}
	if len(choice.Delta) == 0 || string(choice.Delta) == "null" || json.Unmarshal(choice.Delta, &delta) != nil || (delta.Role != "" && delta.Role != "assistant") {
		return streamError("provider returned an invalid assistant delta")
	}
	if s.finish != "" {
		// OpenRouter's documented usage footer repeats the finished choice with
		// a content-free delta. It is accounting, never another response segment.
		if len(envelope.Usage) == 0 || string(envelope.Usage) == "null" ||
			(choice.FinishReason != "" && choice.FinishReason != s.finish) || !contentFreeDelta(choice.Delta) {
			return streamError("provider returned an invalid stream completion boundary")
		}
		return nil
	}
	if s.bytes+len(delta.Content) > session.MaxDocumentBytes || len(delta.ToolCalls) > session.MaxToolCalls {
		return streamError("provider stream content exceeded the size limit")
	}
	s.bytes += len(delta.Content)
	s.text.WriteString(delta.Content)
	if delta.Content != "" {
		if err := s.report(Chunk{Text: delta.Content}); err != nil {
			return err
		}
	}
	if delta.Reasoning != "" {
		if err := s.report(Chunk{Reasoning: delta.Reasoning}); err != nil {
			return err
		}
	}
	for _, fragment := range delta.ToolCalls {
		if fragment.Index == nil || *fragment.Index < 0 || *fragment.Index >= session.MaxToolCalls || (fragment.Type != "" && fragment.Type != "function") {
			return streamError("provider returned an invalid tool-call delta")
		}
		index := *fragment.Index
		call := s.calls[index]
		if call == nil {
			call = &streamCall{}
			s.calls[index] = call
			s.order = append(s.order, index)
		}
		added := len(fragment.ID) + len(fragment.Function.Name) + len(fragment.Function.Arguments)
		if s.bytes+added > session.MaxDocumentBytes || call.id.Len()+len(fragment.ID) > 128 || call.name.Len()+len(fragment.Function.Name) > 64 {
			return streamError("provider tool-call content exceeded the size limit")
		}
		s.bytes += added
		call.id.WriteString(fragment.ID)
		call.name.WriteString(fragment.Function.Name)
		call.arguments.WriteString(fragment.Function.Arguments)
		if added != 0 {
			if err := s.report(Chunk{Call: &CallChunk{Index: index, ID: fragment.ID, Name: fragment.Function.Name, Arguments: fragment.Function.Arguments}}); err != nil {
				return err
			}
		}
	}
	s.finish = choice.FinishReason
	return nil
}

func contentFreeDelta(raw json.RawMessage) bool {
	var fields map[string]any
	if json.Unmarshal(raw, &fields) != nil {
		return false
	}
	for name, value := range fields {
		if name == "role" {
			continue // The typed delta already validated the assistant role.
		}
		switch v := value.(type) {
		case nil:
		case string:
			if v != "" {
				return false
			}
		case []any:
			if len(v) != 0 {
				return false
			}
		case map[string]any:
			if len(v) != 0 {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func (s *chatStream) report(chunk Chunk) error {
	s.chunks++
	if s.chunks > maxStreamChunks {
		return streamError("provider stream exceeded the chunk limit")
	}
	if s.emit != nil {
		s.emit(chunk)
	}
	return nil
}

func (s *chatStream) complete() error {
	if s.finish != "stop" && s.finish != "tool_calls" {
		return streamError("provider did not complete the streamed response")
	}
	text := s.text.String()
	calls := make([]chatCall, 0, len(s.order))
	for _, index := range s.order {
		call := s.calls[index]
		calls = append(calls, chatCall{ID: call.id.String(), Type: "function", Function: chatFunctionCall{Name: call.name.String(), Arguments: call.arguments.String()}})
	}
	raw, err := json.Marshal(calls)
	if err != nil {
		return streamError("provider returned invalid streamed tool calls")
	}
	parts, err := decodeChatParts(&text, raw, s.finish, s.allowed)
	if err != nil || session.ValidateMessage(session.Assistant, parts) != nil {
		return streamError("provider returned invalid streamed assistant content")
	}
	s.response.Parts = parts
	return nil
}

// Usage events are snapshots. Missing or invalid later fields cannot erase
// earlier evidence, and repeated snapshots must never double-charge an attempt.
func (s *chatStream) account(raw json.RawMessage) {
	usage, cost, note := decodeChatUsage(raw)
	merged := s.response.Usage
	for _, pair := range []struct {
		destination **int64
		value       *int64
	}{
		{&merged.Input, usage.Input},
		{&merged.Output, usage.Output},
		{&merged.CachedInput, usage.CachedInput},
		{&merged.Reasoning, usage.Reasoning},
		{&merged.CachedOutput, usage.CachedOutput},
	} {
		if pair.value != nil {
			*pair.destination = pair.value
		}
	}
	if merged.Validate() == nil {
		s.response.Usage = merged
	} else {
		note = new("provider usage snapshots contradict earlier details; previous counts retained")
	}
	if cost != nil {
		s.response.ReportedCostNanoUSD = cost
	}
	if note != nil {
		s.response.UsageNote = note
	}
}
