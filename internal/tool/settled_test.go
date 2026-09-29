package tool

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/session"
)

func TestConfirmedFailureRetainsEvidenceWhileLostOutcomesStayUncertain(t *testing.T) {
	for _, kind := range []string{"confirmed", "transport", "cancel", "oversized"} {
		t.Run(kind, func(t *testing.T) {
			db, dispatcher, owner, _, _ := dispatchFixture(t)
			if _, err := db.CreateGrant(t.Context(), session.Grant{ID: "shell", SessionID: owner.ID, Capability: "shell.run", Resource: owner.WorkingDirectory}); err != nil {
				t.Fatal(err)
			}
			cause := errors.New("remote completed with an error")
			failure := error(cause)
			if kind == "cancel" {
				failure = SettledFailure(context.Canceled)
			} else if kind == "confirmed" || kind == "oversized" {
				failure = SettledFailure(cause)
			}
			output := map[string]any{"reference": "content_owned_error_evidence"}
			if kind == "oversized" {
				output["text"] = strings.Repeat("x", session.MaxDocumentBytes)
			}
			dispatcher.coordination = preparedFixture(func(context.Context, session.Session, Invocation) (Prepared, error) {
				return Prepared{
					Capability: "shell.run", Resource: owner.WorkingDirectory, Arguments: json.RawMessage(`{}`), Mutating: true,
					Acquire: func(context.Context) (func(), error) { return func() {}, nil },
					Run:     func(context.Context, session.OperationID) (any, error) { return output, failure },
				}, nil
			})
			_, id, err := dispatcher.Call(t.Context(), Invocation{SessionID: owner.ID, CellID: "cell", RequestID: kind, Module: "shell", Name: "run"})
			if err == nil {
				t.Fatal("failure silently succeeded")
			}
			operation, readErr := db.Operation(t.Context(), id)
			if readErr != nil {
				t.Fatal(readErr)
			}
			want := session.OperationUncertain
			if kind == "confirmed" || kind == "oversized" {
				want = session.OperationFailed
			}
			if operation.State != want || operation.Result == nil || len(operation.Result.Value) == 0 {
				t.Fatalf("lost effect evidence: %+v", operation)
			}
			if kind == "oversized" {
				if strings.Contains(string(operation.Result.Value), "xxxx") || !errors.Is(err, cause) {
					t.Fatalf("unbounded output or lost failure: %s %v", operation.Result.Value, err)
				}
			} else if !strings.Contains(string(operation.Result.Value), "content_owned_error_evidence") {
				t.Fatalf("lost reference: %s", operation.Result.Value)
			}
		})
	}
}
