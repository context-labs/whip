package tool

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/session"
)

func TestCustomDeadlineDoesNotBorrowOrdinaryOrModelOverride(t *testing.T) {
	for _, change := range []func(*Invocation, *Prepared){
		func(_ *Invocation, p *Prepared) { p.CustomTimeout = -time.Second },
		func(_ *Invocation, p *Prepared) { p.CustomTimeout = 15*time.Minute + time.Nanosecond },
		func(_ *Invocation, p *Prepared) { p.Timeout = time.Second },
		func(_ *Invocation, p *Prepared) { p.ModelTimeouts = true },
		func(_ *Invocation, p *Prepared) {
			p.Apply = func(context.Context, session.OperationID) (any, error) { return map[string]any{}, nil }
		},
		func(c *Invocation, _ *Prepared) { c.Module = "shell" },
		func(_ *Invocation, p *Prepared) { p.Capability = "tools.other" },
	} {
		_, dispatcher, owner, _, _ := dispatchFixture(t)
		call := Invocation{SessionID: owner.ID, CellID: "cell", RequestID: "custom", Module: "tools", Name: "lookup"}
		prepared := Prepared{Capability: "tools.lookup", CustomTimeout: 15 * time.Minute}
		change(&call, &prepared)
		if _, id, err := dispatcher.callPrepared(t.Context(), call, prepared, false); !errors.Is(err, session.ErrInvalid) || id != "" {
			t.Fatal("invalid custom timeout reached admission", id, err)
		}
	}
	db, dispatcher, owner, _, _ := dispatchFixture(t)
	if _, err := db.CreateGrant(t.Context(), session.Grant{ID: "custom", SessionID: owner.ID, Capability: "tools.lookup", Resource: "definition"}); err != nil {
		t.Fatal(err)
	}
	prepared := Prepared{
		Capability: "tools.lookup", Resource: "definition", Arguments: json.RawMessage(`{}`), CustomTimeout: 15 * time.Minute,
		Acquire: func(context.Context) (func(), error) { return func() {}, nil },
		Run: func(ctx context.Context, _ session.OperationID) (any, error) {
			deadline, ok := ctx.Deadline()
			if !ok || time.Until(deadline) < 14*time.Minute || time.Until(deadline) > 15*time.Minute {
				t.Fatal("custom deadline was silently capped", deadline)
			}
			return json.RawMessage(`null`), nil
		},
	}
	if _, _, err := dispatcher.callPrepared(t.Context(), Invocation{SessionID: owner.ID, CellID: "cell", RequestID: "long", Module: "tools", Name: "lookup"}, prepared, false); err != nil {
		t.Fatal(err)
	}
}
