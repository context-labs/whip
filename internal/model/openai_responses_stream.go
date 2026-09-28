package model

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/context-labs/whip/internal/session"
)

type responseStreamCall struct {
	index               int
	id, name, arguments string
}

type responsesStream struct {
	response Response
	scope    string
	allowed  map[string]bool
	emit     func(Chunk)
	chunks   int
	text     strings.Builder
	calls    map[int]*responseStreamCall
	items    []json.RawMessage
}

func (s *responsesStream) chunk(value Chunk) error {
	s.chunks++
	if s.chunks > maxStreamChunks {
		return streamError("provider stream exceeded the chunk limit")
	}
	if s.emit != nil {
		s.emit(value)
	}
	return nil
}

func (s *responsesStream) consume(raw []byte) (bool, error) {
	var event struct {
		Type        string          `json:"type"`
		Delta       string          `json:"delta"`
		OutputIndex int             `json:"output_index"`
		Item        json.RawMessage `json:"item"`
		Response    json.RawMessage `json:"response"`
	}
	if !utf8.Valid(raw) || json.Unmarshal(raw, &event) != nil {
		return false, streamError("provider returned malformed stream data")
	}
	if event.OutputIndex < 0 || event.OutputIndex >= maxResponseItems {
		return false, streamError("provider output index exceeds the limit")
	}
	switch event.Type {
	case "response.output_text.delta", "response.refusal.delta":
		s.text.WriteString(event.Delta)
		return false, s.chunk(Chunk{Text: event.Delta})
	case "response.output_item.added", "response.output_item.done":
		var item responseItem
		if json.Unmarshal(event.Item, &item) != nil {
			return false, streamError("provider returned a malformed output item")
		}
		for len(s.items) <= event.OutputIndex {
			s.items = append(s.items, nil)
		}
		if event.Type == "response.output_item.done" {
			if s.items[event.OutputIndex] != nil {
				return false, streamError("provider repeated a completed output item")
			}
			s.items[event.OutputIndex] = event.Item
		} else if item.Type == "function_call" {
			if s.calls[event.OutputIndex] != nil || len(s.calls) >= session.MaxToolCalls || session.ValidateID(item.CallID) != nil || session.ValidateToolName(item.Name) != nil || !s.allowed[item.Name] {
				return false, streamError("provider returned an invalid or undeclared function call")
			}
			call := &responseStreamCall{index: len(s.calls), id: item.CallID, name: item.Name, arguments: item.Arguments}
			s.calls[event.OutputIndex] = call
			return false, s.chunk(Chunk{Call: &CallChunk{Index: call.index, ID: call.id, Name: call.name, Arguments: call.arguments}})
		}
	case "response.function_call_arguments.delta":
		call := s.calls[event.OutputIndex]
		if call == nil || len(call.arguments)+len(event.Delta) > session.MaxDocumentBytes {
			return false, streamError("provider sent function arguments without a bounded call")
		}
		call.arguments += event.Delta
		return false, s.chunk(Chunk{Call: &CallChunk{Index: call.index, Arguments: event.Delta}})
	case "response.completed", "response.failed", "response.incomplete":
		if event.Type != "response.completed" {
			var response struct {
				Usage json.RawMessage `json:"usage"`
			}
			if json.Unmarshal(event.Response, &response) == nil {
				s.response.Usage, s.response.ReportedCostNanoUSD, s.response.UsageNote = decodeResponsesUsage(response.Usage)
			}
			return false, streamError("provider response did not complete; tool calls were discarded")
		}
		return true, s.complete(event.Response)
	case "error":
		return false, streamError("provider returned a stream error; outcome is unknown")
	}
	return false, nil
}

func (s *responsesStream) complete(raw json.RawMessage) error {
	var envelope struct {
		Status json.RawMessage `json:"status"`
		Output json.RawMessage `json:"output"`
		Usage  json.RawMessage `json:"usage"`
	}
	if !utf8.Valid(raw) || json.Unmarshal(raw, &envelope) != nil {
		return streamError("provider returned a malformed terminal response")
	}
	s.response.Usage, s.response.ReportedCostNanoUSD, s.response.UsageNote = decodeResponsesUsage(envelope.Usage)
	var status string
	if json.Unmarshal(envelope.Status, &status) != nil || status != "completed" {
		return streamError("provider response did not complete; tool calls were discarded")
	}
	output := envelope.Output
	var items []json.RawMessage
	if len(output) == 0 || json.Unmarshal(output, &items) == nil && len(items) == 0 {
		output, _ = json.Marshal(s.items)
	}
	parts, err := responseParts(output, s.allowed)
	if err != nil {
		return err
	}
	continuation := &session.ModelContinuation{Scope: s.scope, Data: string(output)}
	if continuation.Validate() != nil {
		return streamError("provider continuation is invalid or exceeds the size limit")
	}
	var text strings.Builder
	calls := map[string]*session.ToolCall{}
	for _, part := range parts {
		text.WriteString(part.Text)
		if part.Call != nil {
			calls[part.Call.ID] = part.Call
		}
	}
	if !strings.HasPrefix(text.String(), s.text.String()) {
		return streamError("provider completed text disagrees with its stream")
	}
	seen := map[string]bool{}
	for _, streamed := range s.calls {
		call := calls[streamed.id]
		if call == nil || call.Name != streamed.name || !strings.HasPrefix(string(call.Arguments), streamed.arguments) || seen[streamed.id] {
			return streamError("provider completed calls disagree with its stream")
		}
		seen[streamed.id] = true
	}
	if tail := strings.TrimPrefix(text.String(), s.text.String()); tail != "" {
		if err := s.chunk(Chunk{Text: tail}); err != nil {
			return err
		}
	}
	nextIndex := len(s.calls)
	for _, part := range parts {
		if part.Call == nil {
			continue
		}
		call := part.Call
		var streamed *responseStreamCall
		for _, candidate := range s.calls {
			if candidate.id == call.ID {
				streamed = candidate
				break
			}
		}
		chunk := CallChunk{Index: nextIndex, ID: call.ID, Name: call.Name, Arguments: string(call.Arguments)}
		if streamed != nil {
			chunk = CallChunk{Index: streamed.index, Arguments: strings.TrimPrefix(string(call.Arguments), streamed.arguments)}
			if chunk.Arguments == "" {
				continue
			}
		} else {
			nextIndex++
		}
		if err := s.chunk(Chunk{Call: &chunk}); err != nil {
			return err
		}
	}
	s.response.Parts, s.response.Continuation = parts, continuation
	return nil
}

// Responses uses response.completed as its boundary. [DONE] alone never proves
// completion. Provisional deltas cannot become executable transcript parts.
func decodeResponsesStream(ctx context.Context, reader io.Reader, scope string, allowed map[string]bool, emit func(Chunk)) (Response, error) {
	state := responsesStream{scope: scope, allowed: allowed, emit: emit, calls: map[int]*responseStreamCall{}}
	scanner := bufio.NewScanner(io.LimitReader(reader, maxResponseBytes+1))
	scanner.Buffer(make([]byte, 4096), maxResponseBytes+2)
	scanner.Split(splitStreamLine)
	var frame strings.Builder
	lines, events, total := 0, 0, 0
	consume := func() (bool, error) {
		if frame.Len() == 0 {
			return false, nil
		}
		events++
		if events > maxStreamEvents {
			return false, streamError("provider stream exceeded the event limit")
		}
		data := frame.String()
		frame.Reset()
		if strings.TrimSpace(data) == "[DONE]" {
			return false, streamError("provider stream ended without a completed response")
		}
		return state.consume([]byte(data))
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
			if done, err := consume(); done || err != nil {
				return state.response, err
			}
		} else if data, ok := strings.CutPrefix(line, "data:"); ok {
			frame.WriteString(strings.TrimPrefix(data, " "))
			frame.WriteByte('\n')
		}
	}
	if err := ctx.Err(); err != nil {
		return state.response, errors.Join(err, streamError("provider stream was interrupted; outcome is unknown"))
	}
	if scanner.Err() != nil {
		return state.response, streamError("provider stream transport failed; outcome is unknown")
	}
	if done, err := consume(); done || err != nil {
		return state.response, err
	}
	return state.response, streamError("provider stream ended without a completed response")
}
