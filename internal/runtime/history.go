package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/tool"
)

func (r *Runtime) HistorySnapshot(ctx context.Context, owner session.SessionID) (session.HistorySnapshot, error) {
	return r.store.HistorySnapshot(ctx, owner)
}

func (r *Runtime) HistoryMetadata(ctx context.Context, owner session.SessionID, after, through int64, limit int) (session.HistoryMetadataPage, error) {
	return r.store.HistoryMetadata(ctx, owner, after, through, limit)
}

func (r *Runtime) ReadHistoryMessage(ctx context.Context, owner session.SessionID, id session.MessageID, offset int64, length int) (session.HistoryRead, error) {
	return r.store.ReadHistoryMessage(ctx, owner, id, offset, length)
}

func (r *Runtime) SearchHistory(ctx context.Context, owner session.SessionID, after, through int64, query string, limit int) (session.HistorySearchPage, error) {
	return r.store.SearchHistory(ctx, owner, after, through, query, limit)
}

type historyPageRequest struct {
	After           int64  `json:"after,string"`
	ThroughSequence *int64 `json:"through_sequence,string"`
	Limit           int    `json:"limit"`
}

type historySearchRequest struct {
	historyPageRequest
	Query string `json:"query"`
}

type historyReadRequest struct {
	ID     session.MessageID `json:"id"`
	Offset int64             `json:"offset,string"`
	Length int               `json:"length"`
}

func (r *Runtime) prepareHistory(ctx context.Context, current session.Session, call tool.Invocation) (tool.Prepared, error) {
	var request any
	var page *historyPageRequest
	var run func(context.Context, session.OperationID) (any, error)
	switch call.Name {
	case "inspect":
		args := &historyPageRequest{Limit: 20}
		request, page = args, args
		run = func(ctx context.Context, _ session.OperationID) (any, error) {
			return r.HistoryMetadata(ctx, current.ID, args.After, *args.ThroughSequence, args.Limit)
		}
	case "search":
		args := &historySearchRequest{Limit: 20}
		request, page = args, &args.historyPageRequest
		run = func(ctx context.Context, _ session.OperationID) (any, error) {
			return r.SearchHistory(ctx, current.ID, args.After, *args.ThroughSequence, args.Query, args.Limit)
		}
	case "read":
		args := &historyReadRequest{Length: session.MaxHistoryReadBytes}
		request = args
		run = func(ctx context.Context, _ session.OperationID) (any, error) {
			return r.ReadHistoryMessage(ctx, current.ID, args.ID, args.Offset, args.Length)
		}
	default:
		return tool.Prepared{}, session.ErrInvalid
	}
	if err := decodeArguments(call.Arguments, request); err != nil {
		return tool.Prepared{}, err
	}
	if page != nil {
		snapshot, err := r.HistorySnapshot(ctx, current.ID)
		if err != nil {
			return tool.Prepared{}, err
		}
		if page.ThroughSequence == nil {
			page.ThroughSequence = &snapshot.ThroughSequence
		}
		if page.After < 0 || *page.ThroughSequence < page.After || *page.ThroughSequence > snapshot.ThroughSequence || page.Limit < 1 || page.Limit > 100 {
			return tool.Prepared{}, fmt.Errorf("%w: history requires 0 <= after <= through_sequence and limit from 1 to 100", session.ErrInvalid)
		}
	}
	// Validate user arguments before creating a permission prompt or operation.
	switch args := request.(type) {
	case *historySearchRequest:
		query := args.Query
		if !utf8.ValidString(query) || strings.ContainsRune(query, 0) || strings.TrimSpace(query) == "" || len(query) > session.MaxHistoryQueryBytes {
			return tool.Prepared{}, fmt.Errorf("%w: history query must be UTF-8 text from 1 to 256 bytes", session.ErrInvalid)
		}
	case *historyReadRequest:
		if err := session.ValidateID(string(args.ID)); err != nil {
			return tool.Prepared{}, err
		}
		if err := validEvidenceRange(args.Offset, args.Length); err != nil {
			return tool.Prepared{}, err
		}
	}
	arguments, err := json.Marshal(request)
	if err != nil {
		return tool.Prepared{}, err
	}
	return tool.Prepared{
		Capability: "context." + call.Name, Resource: string(current.TreeID), Arguments: arguments,
		Acquire: func(ctx context.Context) (func(), error) { return func() {}, ctx.Err() }, Run: run,
	}, nil
}

func (r *Runtime) HistoryPage(ctx context.Context, owner session.SessionID, after int64, limit int, expected *session.Revision) (session.HistorySnapshot, []session.Message, error) {
	return r.store.HistoryPage(ctx, owner, after, limit, expected)
}

func (r *Runtime) HistoryMetadataAtRevision(ctx context.Context, owner session.SessionID, after, through int64, limit int, expected *session.Revision) (session.HistoryMetadataPage, error) {
	return r.store.HistoryMetadataAtRevision(ctx, owner, after, through, limit, expected)
}

func (r *Runtime) SearchHistoryAtRevision(ctx context.Context, owner session.SessionID, after, through int64, query string, limit int, expected *session.Revision) (session.HistorySearchPage, error) {
	return r.store.SearchHistoryAtRevision(ctx, owner, after, through, query, limit, expected)
}

// TranscriptPage supplies bounded tail/older pages without scanning the transcript.
func (r *Runtime) TranscriptPage(ctx context.Context, request session.HistoryPageRequest) (session.TranscriptPage, error) {
	return r.store.TranscriptPage(ctx, request)
}
