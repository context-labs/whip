package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/context-labs/whip/internal/content"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/store"
	"github.com/context-labs/whip/internal/tool"
)

const maxEvidenceReadBytes = 64 << 10

type pendingReportsRequest struct {
	After session.SessionID `json:"after"`
	Limit int               `json:"limit"`
}

type readReportRequest struct {
	ChildID session.SessionID `json:"child_id"`
	TurnID  session.TurnID    `json:"turn_id"`
	Offset  int64             `json:"offset,string"`
	Length  int               `json:"length"`
}

// CompletionSlice pages the JSON of one exact pending completion. Inspection
// neither clears the pending slot nor acknowledges any published mail.
type CompletionSlice struct {
	Completion session.CompletionMetadata `json:"completion"`
	Offset     int64                      `json:"offset,string"`
	TotalBytes int64                      `json:"total_bytes,string"`
	Data       []byte                     `json:"data"`
	NextOffset *int64                     `json:"next_offset,string"`
}

func (r *Runtime) ListPendingCompletions(ctx context.Context, parent, after session.SessionID, limit int) ([]session.CompletionMetadata, error) {
	return r.store.PendingCompletions(ctx, parent, after, limit)
}

func (r *Runtime) ReadPendingCompletion(ctx context.Context, parent, child session.SessionID, turn session.TurnID, offset int64, maxBytes int) (CompletionSlice, error) {
	if err := validEvidenceRange(offset, maxBytes); err != nil {
		return CompletionSlice{}, err
	}
	completion, err := r.store.PendingCompletion(ctx, parent, child, turn)
	if err != nil {
		return CompletionSlice{}, err
	}
	raw, err := json.Marshal(completion)
	if err != nil {
		return CompletionSlice{}, err
	}
	if offset > int64(len(raw)) {
		return CompletionSlice{}, fmt.Errorf("%w: offset exceeds completion size", session.ErrInvalid)
	}
	end := min(offset+int64(maxBytes), int64(len(raw)))
	result := CompletionSlice{Completion: completion.CompletionMetadata, Offset: offset, TotalBytes: int64(len(raw)), Data: bytes.Clone(raw[offset:end])}
	if end < result.TotalBytes {
		result.NextOffset = &end
	}
	return result, ctx.Err()
}

// publishCompletions examines one bounded cursor page per scheduler pass. A
// blocked or replaced slot cannot hide later children; a completed scan wraps.
func (r *Runtime) publishCompletions(ctx context.Context, after session.SessionID) (session.SessionID, error) {
	const pageSize = 4
	candidates, err := r.store.CompletionCandidates(ctx, after, pageSize)
	if err != nil {
		return after, err
	}
	next := after
	for _, candidate := range candidates {
		next = candidate.ChildID
		if err := r.publishCompletion(ctx, candidate); err != nil {
			if errors.Is(err, store.ErrConflict) || errors.Is(err, store.ErrNotFound) || errors.Is(err, store.ErrLimit) || errors.Is(err, store.ErrStopped) {
				continue
			}
			return next, err
		}
	}
	if len(candidates) < pageSize {
		next = ""
	}
	return next, nil
}

func (r *Runtime) publishCompletion(ctx context.Context, metadata session.CompletionMetadata) error {
	completion, err := r.store.PendingCompletion(ctx, metadata.ParentID, metadata.ChildID, metadata.TurnID)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(completion)
	if err != nil {
		return err
	}
	if len(raw) > session.MaxContentBytes {
		return fmt.Errorf("%w: completion evidence exceeds content limit", store.ErrLimit)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	body, err := r.content.Put(raw)
	if err != nil {
		return err
	}
	_, err = r.store.PublishCompletion(ctx, metadata.ParentID, metadata.ChildID, metadata.TurnID, session.ContentReference{
		ID: "completion_" + string(metadata.TurnID), SessionID: metadata.ParentID,
		Digest: body.Digest, Size: body.Size, MediaType: "application/json",
	})
	return err
}

func validEvidenceRange(offset int64, maxBytes int) error {
	if offset < 0 || maxBytes < 1 || maxBytes > maxEvidenceReadBytes {
		return fmt.Errorf("%w: evidence requires a nonnegative offset and length from 1 to 65536", session.ErrInvalid)
	}
	return nil
}

// ReadContentRange resolves owner-scoped authority before reading a verified
// immutable body. The returned bytes may split a UTF-8 character or JSON token.
func (r *Runtime) ReadContentRange(ctx context.Context, owner session.SessionID, id string, offset int64, maxBytes int) (session.ContentReference, []byte, error) {
	if err := validEvidenceRange(offset, maxBytes); err != nil {
		return session.ContentReference{}, nil, err
	}
	reference, err := r.store.ContentReference(ctx, owner, id)
	if err != nil {
		return reference, nil, err
	}
	if offset > reference.Size {
		return reference, nil, fmt.Errorf("%w: offset exceeds content size", session.ErrInvalid)
	}
	if err := ctx.Err(); err != nil {
		return reference, nil, err
	}
	data, err := r.content.ReadVerifiedRange(content.Body{Digest: reference.Digest, Size: reference.Size}, offset, maxBytes)
	if err != nil {
		return reference, nil, err
	}
	return reference, data, ctx.Err()
}

func (r *Runtime) prepareCompletionRead(current session.Session, call tool.Invocation) (tool.Prepared, error) {
	var request any
	var run func(context.Context, session.OperationID) (any, error)
	switch call.Name {
	case "pending_reports":
		args := &pendingReportsRequest{Limit: 20}
		request = args
		run = func(ctx context.Context, _ session.OperationID) (any, error) {
			items, err := r.ListPendingCompletions(ctx, current.ID, args.After, args.Limit)
			if err != nil {
				return nil, err
			}
			// Failed-turn metadata can contain a long escaped failure. Keep a
			// guest page inside the operation result bound; its last child is
			// the ordinary cursor for the next page.
			size := 0
			for i, item := range items {
				raw, err := json.Marshal(item)
				if err != nil {
					return nil, err
				}
				if size+len(raw) > session.MaxDocumentBytes/4 {
					items = items[:i]
					break
				}
				size += len(raw)
			}
			return struct {
				Items []session.CompletionMetadata `json:"items"`
			}{Items: items}, nil
		}
	case "read_report":
		args := &readReportRequest{Length: maxEvidenceReadBytes}
		request = args
		run = func(ctx context.Context, _ session.OperationID) (any, error) {
			return r.ReadPendingCompletion(ctx, current.ID, args.ChildID, args.TurnID, args.Offset, args.Length)
		}
	default:
		return tool.Prepared{}, session.ErrInvalid
	}
	if err := decodeArguments(call.Arguments, request); err != nil {
		return tool.Prepared{}, err
	}
	switch args := request.(type) {
	case *pendingReportsRequest:
		if args.Limit < 1 || args.Limit > 100 {
			return tool.Prepared{}, fmt.Errorf("%w: report limit must be 1 to 100", session.ErrInvalid)
		}
		if args.After != "" {
			if err := session.ValidateID(string(args.After)); err != nil {
				return tool.Prepared{}, err
			}
		}
	case *readReportRequest:
		for _, id := range []string{string(args.ChildID), string(args.TurnID)} {
			if err := session.ValidateID(id); err != nil {
				return tool.Prepared{}, err
			}
		}
		if err := validEvidenceRange(args.Offset, args.Length); err != nil {
			return tool.Prepared{}, err
		}
	}
	arguments, err := json.Marshal(request)
	if err != nil {
		return tool.Prepared{}, err
	}
	return tool.Prepared{Capability: "agents." + call.Name, Resource: string(current.TreeID), Arguments: arguments, Acquire: func(ctx context.Context) (func(), error) { return func() {}, ctx.Err() }, Run: run}, nil
}

func (r *Runtime) prepareArtifactRead(current session.Session, call tool.Invocation) (tool.Prepared, error) {
	if call.Name != "read" {
		return tool.Prepared{}, session.ErrInvalid
	}
	var args struct {
		ID     string `json:"id"`
		Offset int64  `json:"offset,string"`
		Length int    `json:"length"`
	}
	args.Length = maxEvidenceReadBytes
	if err := decodeArguments(call.Arguments, &args); err != nil {
		return tool.Prepared{}, err
	}
	if err := session.ValidateID(args.ID); err != nil {
		return tool.Prepared{}, err
	}
	if err := validEvidenceRange(args.Offset, args.Length); err != nil {
		return tool.Prepared{}, err
	}
	arguments, err := json.Marshal(args)
	if err != nil {
		return tool.Prepared{}, err
	}
	return tool.Prepared{
		Capability: "artifacts.read",
		Resource:   string(current.TreeID),
		Arguments:  arguments,
		Acquire:    func(ctx context.Context) (func(), error) { return func() {}, ctx.Err() },
		Run: func(ctx context.Context, _ session.OperationID) (any, error) {
			reference, data, err := r.ReadContentRange(ctx, current.ID, args.ID, args.Offset, args.Length)
			if err != nil {
				return nil, err
			}
			var next *int64
			if end := args.Offset + int64(len(data)); end < reference.Size {
				next = &end
			}
			return struct {
				ID         string `json:"id"`
				Digest     string `json:"digest"`
				MediaType  string `json:"media_type"`
				Offset     int64  `json:"offset,string"`
				TotalBytes int64  `json:"total_bytes,string"`
				Data       []byte `json:"data"`
				NextOffset *int64 `json:"next_offset,string"`
			}{reference.ID, reference.Digest, reference.MediaType, args.Offset, reference.Size, data, next}, nil
		},
	}, nil
}
