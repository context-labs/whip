package tool

import (
	"context"
	"errors"
	"github.com/context-labs/whip/internal/session"
	"testing"
	"testing/synctest"
	"time"
)

func TestHostEffectTimeoutRemainsBoundedAndCancellable(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		parent, stop := context.WithCancel(t.Context())
		defer stop()
		ordinary, cancel := operationContext(parent, false, 0)
		defer cancel()
		shell, cancelShell := operationContext(parent, false, 120*time.Second)
		defer cancelShell()
		remote, cancelRemote := operationContext(parent, false, 300*time.Second)
		defer cancelRemote()
		time.Sleep(31 * time.Second)
		if ordinary.Err() != context.DeadlineExceeded || shell.Err() != nil || remote.Err() != nil {
			t.Fatalf("premature deadline: %v/%v/%v", ordinary.Err(), shell.Err(), remote.Err())
		}
		time.Sleep(90 * time.Second)
		if shell.Err() != context.DeadlineExceeded || remote.Err() != nil {
			t.Fatalf("shell deadline: %v/%v", shell.Err(), remote.Err())
		}
		stop()
		if remote.Err() != context.Canceled {
			t.Fatalf("remote lost cancellation: %v", remote.Err())
		}
	})
}

func TestDispatcherRejectsInvalidTimeoutBeforeAdmission(t *testing.T) {
	for _, prepared := range []Prepared{
		{Timeout: -time.Second},
		{Timeout: 301 * time.Second},
		{Capability: "models.call", ModelTimeouts: true, Timeout: time.Second},
		{Timeout: time.Second, Apply: func(context.Context, session.OperationID) (any, error) { return nil, nil }},
	} {
		_, dispatcher, owner, _, _ := dispatchFixture(t)
		dispatcher.coordination = preparedFixture(func(context.Context, session.Session, Invocation) (Prepared, error) { return prepared, nil })
		_, id, err := dispatcher.Call(t.Context(), Invocation{SessionID: owner.ID, CellID: "cell", RequestID: "invalid", Module: "shell", Name: "run"})
		if !errors.Is(err, session.ErrInvalid) || id != "" {
			t.Fatalf("admitted invalid timeout: %s %v", id, err)
		}
	}
}
