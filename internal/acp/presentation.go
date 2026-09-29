package acp

import (
	"errors"
	"fmt"
	"strings"

	acp "github.com/coder/acp-go-sdk"

	"github.com/context-labs/whip/internal/client"
	"github.com/context-labs/whip/internal/protocol"
)

// presentation retains one provisional message and one canonical tool batch.
// ACP has no retract update: an interrupted preview is explicitly marked, and a
// destructive history revision requires the editor to reload the session.
type presentation struct {
	epoch   protocol.ID
	preview *protocol.MessagePreview
	calls   map[protocol.ID]protocol.ToolCall
	emit    func(acp.SessionUpdate) error
	content func(protocol.ID) (acp.ContentBlock, error)
}

func (p *presentation) observe(page client.Observation, replay bool) error {
	if page.Reset {
		_ = p.emit(updateThoughtText("History changed. Reload this session to replace the displayed conversation.\n"))
		return errors.New("history changed; reload the ACP session")
	}
	if p.epoch != "" && p.epoch != page.Epoch {
		if err := p.discard(); err != nil {
			return err
		}
	}
	p.epoch = page.Epoch
	for _, message := range page.Messages {
		if err := p.message(message, replay); err != nil {
			return err
		}
	}
	if page.Cursor.After < page.Snapshot.ThroughSequence {
		return nil
	}
	value := page.Preview
	if value == nil {
		return p.discard()
	}
	if p.preview != nil && (p.preview.AttemptID != value.AttemptID || p.preview.MessageID != value.MessageID || !strings.HasPrefix(value.Text, p.preview.Text) || !strings.HasPrefix(value.Reasoning, p.preview.Reasoning)) {
		if err := p.discard(); err != nil {
			return err
		}
	}
	text, reasoning := "", ""
	if p.preview != nil {
		text, reasoning = p.preview.Text, p.preview.Reasoning
	}
	if err := p.text(value.Text[len(text):], false); err != nil {
		return err
	}
	if rest := value.Reasoning[len(reasoning):]; rest != "" {
		if err := p.emit(updateThoughtText(rest)); err != nil {
			return err
		}
	}
	snapshot := *value
	snapshot.Calls = nil // Incomplete arguments cannot start a canonical tool card.
	p.preview = &snapshot
	return nil
}

func (p *presentation) discard() error {
	prior := p.preview
	p.preview = nil
	if prior != nil && (prior.Text != "" || prior.Reasoning != "") {
		return p.emit(updateThoughtText("\nResponse interrupted; the preceding preview was not committed.\n"))
	}
	return nil
}

func (p *presentation) message(message protocol.Message, replay bool) error {
	if message.Role == "system" {
		return nil
	}
	if message.Role == "assistant" {
		var text strings.Builder
		for _, part := range message.Parts {
			if part.Type == "text" {
				text.WriteString(part.Text)
			}
		}
		value := text.String()
		if p.preview != nil && p.preview.MessageID == message.ID && strings.HasPrefix(value, p.preview.Text) {
			value = value[len(p.preview.Text):]
			p.preview = nil
		} else if err := p.discard(); err != nil {
			return err
		}
		if err := p.text(value, false); err != nil {
			return err
		}
	}
	var resultID protocol.ID
	var resultContent []acp.ToolCallContent
	contentCount, contentBytes := 0, 0
	for _, part := range message.Parts {
		switch part.Type {
		case "text":
			if message.Role == "user" && replay {
				var err error
				if message.Mail != nil {
					err = p.emit(updateThoughtText(part.Text))
				} else {
					err = p.text(part.Text, true)
				}
				if err != nil {
					return err
				}
			}
		case "tool_call":
			if part.Call == nil {
				return errors.New("canonical tool call is absent")
			}
			if p.calls == nil {
				p.calls = make(map[protocol.ID]protocol.ToolCall)
			}
			if len(p.calls) >= 16 {
				return errors.New("ACP tool batch exceeds 16 calls")
			}
			if _, found := p.calls[part.Call.ID]; found {
				return errors.New("duplicate canonical tool call")
			}
			p.calls[part.Call.ID] = *part.Call
			if err := p.emit(startToolCall(string(part.Call.ID), part.Call.Name, string(part.Call.Arguments))); err != nil {
				return err
			}
		case "tool_result":
			if part.Result == nil {
				return errors.New("canonical tool result is absent")
			}
			resultID = part.Result.CallID
			call, found := p.calls[resultID]
			if !found {
				return fmt.Errorf("tool result %s has no observed canonical call", resultID)
			}
			delete(p.calls, resultID)
			update := endToolCall(string(resultID), call.Name, string(call.Arguments), part.Result.Output, part.Result.IsError)
			resultContent = update.ToolCallUpdate.Content
			if err := p.emit(update); err != nil {
				return err
			}
		case "content":
			if message.Role == "user" && !replay {
				continue
			}
			if p.content == nil {
				return errors.New("content reader is unavailable")
			}
			contentCount++
			if contentCount > 8 {
				return errors.New("ACP message exceeds 8 content references")
			}
			block, err := p.content(part.ReferenceID)
			if err != nil {
				return err
			}
			if block.Image != nil {
				contentBytes += len(block.Image.Data)
			}
			if block.Text != nil {
				contentBytes += len(block.Text.Text)
			}
			if contentBytes > 24<<20 {
				return errors.New("ACP encoded message content exceeds 24 MiB")
			}
			switch message.Role {
			case "user":
				err = p.emit(acp.UpdateUserMessage(block))
			case "assistant":
				err = p.emit(acp.UpdateAgentMessage(block))
			case "tool":
				if resultID == "" {
					return errors.New("tool attachment has no canonical result")
				}
				resultContent = append(resultContent, acp.ToolContent(block))
				err = p.emit(acp.UpdateToolCall(acp.ToolCallId(resultID), acp.WithUpdateContent(append([]acp.ToolCallContent{}, resultContent...))))
			}
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (p *presentation) text(text string, user bool) error {
	if text == "" {
		return nil
	}
	if user {
		return p.emit(acp.UpdateUserMessageText(text))
	}
	return p.emit(acp.UpdateAgentMessageText(text))
}
