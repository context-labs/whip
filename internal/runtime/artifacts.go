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

type artifactMetadata struct {
	ID        string `json:"id"`
	Digest    string `json:"digest"`
	MediaType string `json:"media_type"`
	Bytes     int64  `json:"bytes,string"`
}

func artifactView(reference session.ContentReference) artifactMetadata {
	return artifactMetadata{reference.ID, reference.Digest, reference.MediaType, reference.Size}
}

func (r *Runtime) prepareArtifact(current session.Session, call tool.Invocation) (tool.Prepared, error) {
	if call.Name == "read" {
		return r.prepareArtifactRead(current, call)
	}
	var arguments []byte
	var run func(context.Context, session.OperationID) (any, error)
	switch call.Name {
	case "put":
		var args struct {
			Text   *string `json:"text"`
			Source string  `json:"source"`
		}
		if err := decodeArguments(call.Arguments, &args); err != nil {
			return tool.Prepared{}, err
		}
		if args.Text == nil || !utf8.ValidString(*args.Text) || len(*args.Text) > 128<<10 || len(args.Source) > 256 || !utf8.ValidString(args.Source) || strings.ContainsRune(args.Source, 0) {
			return tool.Prepared{}, fmt.Errorf("%w: artifact text exceeds128KiB or source exceeds256 bytes", session.ErrInvalid)
		}
		if args.Source == "" {
			args.Source = "agent artifact"
		}
		arguments, _ = json.Marshal(args)
		run = func(ctx context.Context, operation session.OperationID) (any, error) {
			// The operation identity supplies stable owner-scoped reference identity.
			// A source label stays in this canonical operation, not a second index.
			reference, err := r.PutContent(ctx, current.ID, "artifact_"+string(operation), "text/plain", []byte(*args.Text))
			if err != nil {
				return nil, err
			}
			return struct {
				artifactMetadata
				Source string `json:"source"`
			}{artifactView(reference), args.Source}, nil
		}
	case "inspect":
		var args struct {
			ID string `json:"id"`
		}
		if err := decodeArguments(call.Arguments, &args); err != nil {
			return tool.Prepared{}, err
		}
		if err := session.ValidateID(args.ID); err != nil {
			return tool.Prepared{}, err
		}
		arguments, _ = json.Marshal(args)
		run = func(ctx context.Context, _ session.OperationID) (any, error) {
			reference, err := r.store.ContentReference(ctx, current.ID, args.ID)
			return artifactView(reference), err
		}
	default:
		return tool.Prepared{}, session.ErrInvalid
	}
	return tool.Prepared{
		Capability: "artifacts." + call.Name, Resource: string(current.TreeID), Arguments: arguments,
		Mutating: call.Name == "put",
		Acquire:  func(ctx context.Context) (func(), error) { return func() {}, ctx.Err() },
		Run:      run,
	}, nil
}
