package runclient

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/context-labs/whip/internal/client"
	"github.com/context-labs/whip/internal/protocol"
)

// Output retains only one bounded preview/final message and one tool batch.
// Preview text is provisional; committed messages replace it by exact identity.
// Tool start/end events come from canonical calls/results, never incomplete JSON.
type Output struct {
	stdout, diagnostics io.Writer
	encoder             *json.Encoder
	quiet               bool
	failure             error
	epoch               protocol.ID
	preview             *protocol.MessagePreview
	final               string
	calls               map[protocol.ID]string
}

func NewOutput(format string, quiet bool, stdout, diagnostics io.Writer) (*Output, error) {
	if format != "text" && format != "json" {
		return nil, errors.New("output format must be text or json")
	}
	if stdout == nil || diagnostics == nil {
		return nil, errors.New("output writers are required")
	}
	result := &Output{stdout: stdout, diagnostics: diagnostics, quiet: quiet, calls: map[protocol.ID]string{}}
	if format == "json" {
		result.encoder = json.NewEncoder(stdout)
	}
	return result, nil
}

func (o *Output) Observe(page client.Observation, turn protocol.ID) error {
	if o.epoch != "" && o.epoch != page.Epoch {
		o.DiscardPreview()
	}
	o.epoch = page.Epoch
	for _, message := range page.Messages {
		if message.TurnID == nil || *message.TurnID != turn {
			continue
		}
		if message.Role == "assistant" {
			var text strings.Builder
			for _, part := range message.Parts {
				if part.Type == "text" {
					text.WriteString(part.Text)
				}
			}
			body := text.String()
			if o.preview != nil && o.preview.MessageID == message.ID {
				if strings.HasPrefix(body, o.preview.Text) {
					o.text(body[len(o.preview.Text):])
				} else {
					o.DiscardPreview()
					o.text(body)
				}
				o.preview = nil
			} else {
				o.DiscardPreview()
				o.text(body)
			}
			o.final = body
		}
		for _, part := range message.Parts {
			if part.Call != nil {
				if len(o.calls) >= 16 {
					return errors.New("tool batch exceeds 16 calls")
				}
				o.calls[part.Call.ID] = part.Call.Name
				o.Event("tool_start", map[string]string{"name": part.Call.Name, "args": string(part.Call.Arguments)})
			}
			if part.Result != nil {
				name, ok := o.calls[part.Result.CallID]
				if !ok {
					return errors.New("tool result has no observed call")
				}
				delete(o.calls, part.Result.CallID)
				o.Event("tool_end", map[string]string{"name": name, "result": part.Result.Output})
			}
		}
	}
	// When the reader is behind, a current preview may come after messages not
	// yet read. Do not print it ahead of the missing committed tool/result batch.
	if page.Cursor.After < page.Snapshot.ThroughSequence {
		return o.failure
	}
	preview := page.Preview
	if preview != nil && preview.TurnID == turn {
		if o.preview != nil && (o.preview.AttemptID != preview.AttemptID || o.preview.MessageID != preview.MessageID || !strings.HasPrefix(preview.Text, o.preview.Text) || !strings.HasPrefix(preview.Reasoning, o.preview.Reasoning)) {
			o.DiscardPreview()
		}
		oldText, oldReasoning := "", ""
		if o.preview != nil {
			oldText, oldReasoning = o.preview.Text, o.preview.Reasoning
		}
		o.text(preview.Text[len(oldText):])
		if reasoning := preview.Reasoning[len(oldReasoning):]; reasoning != "" {
			o.Event("reasoning", map[string]string{"delta": reasoning})
		}
		snapshot := *preview
		snapshot.Calls = nil // incomplete tool arguments have no CLI event authority
		o.preview = &snapshot
	} else if o.preview != nil {
		o.DiscardPreview()
	}
	return o.failure
}

func (o *Output) DiscardPreview() {
	if o.preview != nil && o.preview.Text != "" {
		o.Event("discard", map[string]string{"chars": strconv.Itoa(utf8.RuneCountInString(o.preview.Text))})
	}
	o.preview = nil
}
func (o *Output) FinalText() string { return o.final }
func (o *Output) Err() error        { return o.failure }
func (o *Output) text(text string) {
	if text != "" {
		o.Event("text", map[string]string{"delta": text})
	}
}

// Event preserves the documented CLI NDJSON fields. Decision observations are
// supplied explicitly by a caller; they do not grant permission or answer one.
func (o *Output) Event(kind string, fields map[string]string) {
	if o.failure != nil {
		return
	}
	if o.encoder != nil {
		event := make(map[string]string, len(fields)+1)
		maps.Copy(event, fields)
		event["type"] = kind
		o.failure = o.encoder.Encode(event)
		return
	}
	switch kind {
	case "text":
		_, o.failure = io.WriteString(o.stdout, fields["delta"])
	case "discard":
		_, o.failure = fmt.Fprintln(o.stdout)
		o.Note("response interrupted; regenerating")
	case "tool_start":
		o.Note("⚒ %s", fields["name"])
	case "permission_pending":
		o.Note("permission pending: %s", fields["detail"])
	case "question_pending":
		o.Note("question pending: %s", fields["detail"])
	}
}

func (o *Output) Finish(err error) error {
	if o.encoder != nil {
		if err != nil {
			o.Event("error", map[string]string{"error": err.Error()})
		} else {
			o.Event("done", map[string]string{"text": o.final})
		}
	} else if o.failure == nil {
		_, o.failure = fmt.Fprintln(o.stdout)
	}
	return o.failure
}

func (o *Output) Note(format string, args ...any) {
	if !o.quiet && o.failure == nil {
		_, o.failure = fmt.Fprintf(o.diagnostics, format+"\n", args...)
	}
}
