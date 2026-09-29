package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/context-labs/whip/internal/runner"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/store"
	"github.com/context-labs/whip/internal/tool"
)

type modelHelperItem struct {
	AttemptID  *session.ModelAttemptID `json:"attempt_id"`
	Text       string                  `json:"text"`
	Failure    *string                 `json:"failure"`
	ContentRef *string                 `json:"content_ref"`
	Truncated  bool                    `json:"truncated"`
	Bytes      int64                   `json:"bytes,string"`
}

func (r *Runtime) prepareModel(ctx context.Context, current session.Session, call tool.Invocation) (tool.Prepared, error) {
	if call.Module != "models" || call.SessionID != current.ID {
		return tool.Prepared{}, session.ErrInvalid
	}
	prompts, limit, arguments, err := modelHelperArguments(call)
	if err != nil {
		return tool.Prepared{}, err
	}
	cell, err := r.store.Cell(ctx, call.CellID)
	if err != nil {
		return tool.Prepared{}, err
	}
	if cell.SessionID != current.ID {
		return tool.Prepared{}, fmt.Errorf("%w: model helper cell owner mismatch", session.ErrInvalid)
	}
	turn, err := r.store.Turn(ctx, cell.TurnID)
	if err != nil {
		return tool.Prepared{}, err
	}
	if turn.SessionID != current.ID {
		return tool.Prepared{}, session.ErrInvalid
	}
	configuration, err := r.store.Configuration(ctx, current.ID, turn.ConfigRevision)
	if err != nil {
		return tool.Prepared{}, err
	}
	cacheKey := ""
	if configuration.Run != nil {
		cacheKey = configuration.Run.CacheKey
	}
	return tool.Prepared{
		Capability: "models." + call.Name, Resource: string(current.TreeID), Arguments: arguments, ModelTimeouts: true,
		Acquire: func(ctx context.Context) (func(), error) { return func() {}, ctx.Err() },
		Run: func(ctx context.Context, id session.OperationID) (any, error) {
			results, err := r.runner.CallModels(ctx, runner.ModelHelperRequest{Turn: turn, Model: configuration.Model, CacheKey: cacheKey, OperationID: id, Prompts: prompts, MaxTokens: limit}, modelAdmissionRefused)
			if _, fatal := errors.AsType[*runner.AccountingError](err); fatal {
				return nil, tool.Fatal(err)
			}
			if err != nil {
				return nil, err
			}
			items := make([]modelHelperItem, len(results))
			for i, result := range results {
				items[i], err = r.modelHelperOutput(ctx, result)
				if err != nil {
					return nil, err
				}
			}
			if call.Name == "call" {
				return items[0], nil
			}
			return items, nil
		},
	}, nil
}

func modelHelperArguments(call tool.Invocation) ([]string, *int64, json.RawMessage, error) {
	// encoding/json replaces invalid UTF-8, so inspect supplied strings before
	// the common strict decoder can normalize them.
	for _, value := range call.Arguments {
		if !modelArgumentUTF8(value) {
			return nil, nil, nil, fmt.Errorf("%w: model prompts require valid UTF-8", session.ErrInvalid)
		}
	}
	if value, present := call.Arguments["max_tokens"]; present && value == nil {
		return nil, nil, nil, fmt.Errorf("%w: max_tokens must be a positive integer", session.ErrInvalid)
	}
	var prompts []string
	var limit *int64
	var canonical any
	switch call.Name {
	case "call":
		var args struct {
			Prompt    string `json:"prompt"`
			MaxTokens *int64 `json:"max_tokens,omitempty"`
		}
		if err := decodeArguments(call.Arguments, &args); err != nil {
			return nil, nil, nil, err
		}
		prompts, limit, canonical = []string{args.Prompt}, args.MaxTokens, args
	case "batch":
		var args struct {
			Prompts   []string `json:"prompts"`
			MaxTokens *int64   `json:"max_tokens,omitempty"`
		}
		if err := decodeArguments(call.Arguments, &args); err != nil {
			return nil, nil, nil, err
		}
		prompts, limit, canonical = args.Prompts, args.MaxTokens, args
	default:
		return nil, nil, nil, session.ErrInvalid
	}
	if len(prompts) == 0 || len(prompts) > session.MaxModelBatchItems || (limit != nil && (*limit < 1 || *limit > 1000000)) {
		return nil, nil, nil, fmt.Errorf("%w: invalid model helper count or output limit", session.ErrInvalid)
	}
	for _, prompt := range prompts {
		if err := session.ValidateText(prompt, session.MaxDocumentBytes); err != nil {
			return nil, nil, nil, err
		}
	}
	arguments, err := json.Marshal(canonical)
	return prompts, limit, arguments, err
}

func modelArgumentUTF8(value any) bool {
	switch value := value.(type) {
	case string:
		return utf8.ValidString(value)
	case []string:
		for _, item := range value {
			if !utf8.ValidString(item) {
				return false
			}
		}
	case []any:
		for _, item := range value {
			if !modelArgumentUTF8(item) {
				return false
			}
		}
	}
	return true
}

// Only wholly semantic refusals are safe to expose as positional failures.
// errors.Is would hide a SQL rollback error joined to a known refusal.
//
//nolint:errorlint // Inspect each immediate unwrap edge and compare exact leaves; errors.As/Is can conceal unknown joined branches.
func modelAdmissionRefused(err error) bool {
	switch wrapped := err.(type) {
	case interface{ Unwrap() []error }:
		children := wrapped.Unwrap()
		if len(children) == 0 {
			return false
		}
		for _, child := range children {
			if !modelAdmissionRefused(child) {
				return false
			}
		}
		return true
	case interface{ Unwrap() error }:
		return modelAdmissionRefused(wrapped.Unwrap())
	default:
		return err == store.ErrLimit || err == store.ErrStopped || err == session.ErrInvalid
	}
}

func (r *Runtime) modelHelperOutput(ctx context.Context, result runner.ModelResult) (modelHelperItem, error) {
	item := modelHelperItem{Text: result.Text, Bytes: int64(len(result.Text))}
	if result.AttemptID != "" {
		item.AttemptID = &result.AttemptID
	}
	if result.Failure != nil {
		item.Failure = new(modelPreview(strings.ToValidUTF8(*result.Failure, "�"), 1024))
	}
	encoded, err := json.Marshal(item)
	if err != nil {
		return item, err
	}
	if len(result.Text) <= 8192 && len(encoded) <= 15*1024 {
		return item, ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return item, err
	}
	item.Truncated = true
	if result.Failure == nil {
		body, putErr := r.content.Put([]byte(result.Text))
		if putErr != nil {
			item.Failure = new("model output unavailable: content publication failed")
		} else {
			reference, registerErr := r.store.RegisterModelHelperContent(ctx, result.AttemptID, body.Digest, body.Size)
			if registerErr != nil {
				item.Failure = new("model output unavailable: content registration failed")
			} else {
				item.ContentRef = &reference.ID
			}
		}
	}
	for limit := 4096; ; limit /= 2 {
		item.Text = modelPreview(result.Text, limit)
		encoded, err = json.Marshal(item)
		if err != nil {
			return item, err
		}
		if len(encoded) <= 15*1024 {
			return item, ctx.Err()
		}
	}
}

func modelPreview(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	const gap = "\n…\n"
	if limit < len(gap) {
		return ""
	}
	head := (limit - len(gap)) / 2
	tail := len(text) - (limit - len(gap) - head)
	for head > 0 && !utf8.RuneStart(text[head]) {
		head--
	}
	for tail < len(text) && !utf8.RuneStart(text[tail]) {
		tail++
	}
	return text[:head] + gap + text[tail:]
}
