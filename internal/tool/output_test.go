package tool

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/context-labs/whip/internal/session"
)

func TestTypedHostOutputPreservesValueAndAttachmentEvidence(t *testing.T) {
	for _, wrapped := range []bool{false, true} {
		t.Run(map[bool]string{false: "ordinary JSON", true: "typed metadata"}[wrapped], func(t *testing.T) {
			db, dispatcher, owner, _, _ := dispatchFixture(t)
			if _, err := db.CreateGrant(t.Context(), session.Grant{ID: "grant", SessionID: owner.ID, Capability: "computer.run", Resource: "capture"}); err != nil {
				t.Fatal(err)
			}
			digest := sha256.Sum256([]byte("image"))
			if _, err := db.RegisterContent(t.Context(), session.ContentReference{ID: "image", SessionID: owner.ID, Digest: hex.EncodeToString(digest[:]), Size: 5, MediaType: "image/jpeg"}); err != nil {
				t.Fatal(err)
			}
			value := map[string]any{"content_references": []string{"image"}, "count": json.Number("9007199254740993")}
			dispatcher.coordination = preparedFixture(func(context.Context, session.Session, Invocation) (Prepared, error) {
				return Prepared{Capability: "computer.run", Resource: "capture", Arguments: json.RawMessage(`{}`), Acquire: func(context.Context) (func(), error) { return func() {}, nil }, Run: func(context.Context, session.OperationID) (any, error) {
					if wrapped {
						return Output{Value: value, ContentReferences: []string{"image"}}, nil
					}
					return value, nil
				}}, nil
			})
			result, id, err := dispatcher.Call(t.Context(), Invocation{SessionID: owner.ID, CellID: "cell", RequestID: "image", Module: "computer", Name: "run"})
			if err != nil || !reflect.DeepEqual(result, value) {
				t.Fatal("typed wrapper changed guest value", result, err)
			}
			operation, err := db.Operation(t.Context(), id)
			if err != nil {
				t.Fatal(err)
			}
			if (len(operation.Result.ContentReferences) == 1) != wrapped {
				t.Fatal("JSON was confused with authority", operation.Result)
			}
			if string(operation.Result.Value) != `{"content_references":["image"],"count":9007199254740993}` {
				t.Fatal("exact result changed", string(operation.Result.Value))
			}
		})
	}
}
