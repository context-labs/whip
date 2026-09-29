package tool

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"testing/synctest"
	"time"

	"github.com/context-labs/whip/internal/session"
)

type preparedFixture func(context.Context, session.Session, Invocation) (Prepared, error)

func (f preparedFixture) PrepareCoordination(ctx context.Context, s session.Session, call Invocation) (Prepared, error) {
	return f(ctx, s, call)
}

type settlementFault struct {
	Ledger
	calls int
}

func (f *settlementFault) SettleOperation(context.Context, session.OperationID, session.OperationResult) (session.Operation, error) {
	f.calls++
	return session.Operation{}, session.ErrInvalid
}

func TestDispatcherFatalSettlementPreservesEffectAndDispatchedEvidence(t *testing.T) {
	db, dispatcher, owner, _, _ := dispatchFixture(t)
	if _, err := db.CreateGrant(t.Context(), session.Grant{ID: "write", SessionID: owner.ID, Capability: "files.write", Resource: owner.WorkingDirectory}); err != nil {
		t.Fatal(err)
	}
	fault := &settlementFault{Ledger: db}
	dispatcher.ledger = fault
	_, id, err := dispatcher.Call(t.Context(), invokeWrite(owner, "failed-settlement"))
	if !isFatal(err) || !errors.Is(err, session.ErrInvalid) || fault.calls != 1 {
		t.Fatalf("settlement error=%v writes=%d", err, fault.calls)
	}
	value, err := db.Operation(t.Context(), id)
	if err != nil || value.State != session.OperationDispatched || value.Result != nil {
		t.Fatalf("evidence=%+v err=%v", value, err)
	}
	path := filepath.Join(owner.WorkingDirectory, "note.txt")
	assertBytes(t, path, "once")
	if err := os.WriteFile(path, []byte("external update"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, err = dispatcher.Call(t.Context(), invokeWrite(owner, "failed-settlement"))
	if !isFatal(err) {
		t.Fatalf("unresolved replay was catchable: %v", err)
	}
	assertBytes(t, path, "external update")
}

func TestDispatcherHelperIdentityFatalAndSettledFailures(t *testing.T) {
	for _, fatal := range []bool{false, true} {
		t.Run(map[bool]string{false: "settled provider failure", true: "unresolved accounting"}[fatal], func(t *testing.T) {
			db, dispatcher, owner, _, _ := dispatchFixture(t)
			if _, err := db.CreateGrant(t.Context(), session.Grant{ID: "model", SessionID: owner.ID, Capability: "models.call", Resource: string(owner.TreeID)}); err != nil {
				t.Fatal(err)
			}
			injected := errors.New("provider evidence failed")
			var received session.OperationID
			dispatcher.coordination = preparedFixture(func(context.Context, session.Session, Invocation) (Prepared, error) {
				return Prepared{Capability: "models.call", Resource: string(owner.TreeID), Arguments: json.RawMessage(`{"prompt":"bounded"}`), ModelTimeouts: true, Acquire: func(ctx context.Context) (func(), error) { return func() {}, ctx.Err() }, Run: func(ctx context.Context, id session.OperationID) (any, error) {
					received = id
					value, err := db.Operation(ctx, id)
					if err != nil || value.State != session.OperationDispatched {
						t.Fatalf("Run before committed dispatch: %+v %v", value, err)
					}
					if _, ok := ctx.Deadline(); ok {
						t.Error("helper inherited a blanket deadline")
					}
					if fatal {
						return nil, Fatal(injected)
					}
					return nil, injected
				}}, nil
			})
			_, id, err := dispatcher.Call(t.Context(), Invocation{SessionID: owner.ID, CellID: "cell", RequestID: "model", Module: "models", Name: "call"})
			if received != id || !errors.Is(err, injected) || isFatal(err) != fatal {
				t.Fatalf("id=%s received=%s err=%v", id, received, err)
			}
			value, err := db.Operation(t.Context(), id)
			if err != nil {
				t.Fatal(err)
			}
			want := session.OperationFailed
			if fatal {
				want = session.OperationDispatched
			}
			if value.State != want {
				t.Fatalf("state=%s want=%s", value.State, want)
			}
		})
	}
}

func TestDispatcherFatalCleanupAndRestrictedModelLifetime(t *testing.T) {
	db, dispatcher, owner, _, _ := dispatchFixture(t)
	injected := errors.New("permission read failed")
	dispatcher.ledger = &operationReadFault{Ledger: &settlementFault{Ledger: db}, failure: injected}
	_, id, err := dispatcher.Call(t.Context(), invokeWrite(owner, "cleanup"))
	if !isFatal(err) || !errors.Is(err, injected) {
		t.Fatalf("unresolved waiter was catchable: %v", err)
	}
	value, err := db.Operation(t.Context(), id)
	if err != nil || value.State != session.OperationWaiting {
		t.Fatalf("waiter lost: %+v %v", value, err)
	}
	dispatcher.coordination = preparedFixture(func(context.Context, session.Session, Invocation) (Prepared, error) {
		return Prepared{Capability: "context.read", ModelTimeouts: true}, nil
	})
	if _, id, err := dispatcher.Call(t.Context(), Invocation{SessionID: owner.ID, CellID: "cell", RequestID: "invalid-timeout", Module: "context", Name: "read"}); !errors.Is(err, session.ErrInvalid) || id != "" {
		t.Fatalf("unbounded non-model operation: %s %v", id, err)
	}
}

func TestModelOperationLifetimeRetainsCancellationWithoutBlanketDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		parent, stop := context.WithCancel(t.Context())
		defer stop()
		normal, cancelNormal := operationContext(parent, false, 0)
		defer cancelNormal()
		helper, cancelHelper := operationContext(parent, true, 0)
		defer cancelHelper()
		time.Sleep(11 * time.Minute)
		if !errors.Is(normal.Err(), context.DeadlineExceeded) || helper.Err() != nil {
			t.Fatalf("normal=%v helper=%v", normal.Err(), helper.Err())
		}
		stop()
		if !errors.Is(helper.Err(), context.Canceled) {
			t.Fatalf("helper detached from turn: %v", helper.Err())
		}
	})
}
