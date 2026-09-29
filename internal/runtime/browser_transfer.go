package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/context-labs/whip/internal/browserhost"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/store"
	"github.com/context-labs/whip/internal/tool"
)

func transferScopes(attachments []browserhost.Attachment) []session.BrowserScope {
	scopes := make([]session.BrowserScope, len(attachments))
	for i, a := range attachments {
		scopes[i] = a.Scope
	}
	return scopes
}

func (r *Runtime) prepareBrowserTransfer(ctx context.Context, current session.Session, call tool.Invocation, request store.ChildRequest) (tool.Prepared, error) {
	parent, err := r.browserIdentity(ctx, current)
	if err != nil {
		return tool.Prepared{}, err
	}
	childID := store.TransferChildID(call.OperationID())
	child := browserhost.Identity{RootID: parent.RootID, AgentID: string(childID)}
	capture, err := r.browser.PrepareTransfer(ctx, parent, child, request.BrowserAttachments)
	if err != nil {
		return tool.Prepared{}, err
	}
	intent := store.ChildTransferIntent{Request: request, ChildID: childID, Parents: transferScopes(capture.Parents()), Children: transferScopes(capture.Attachments())}
	raw, err := json.Marshal(intent)
	if err != nil {
		return tool.Prepared{}, err
	}
	var lease *browserhost.TransferLease
	return tool.Prepared{
		Capability: "agents.spawn", Resource: string(current.TreeID), Arguments: raw, Mutating: true, Lifetime: capture.Lifetime(), Timeout: 60 * time.Second,
		Acquire: func(ctx context.Context) (func(), error) {
			if err := r.store.CheckChildTransfer(ctx, call.OperationID(), false); err != nil {
				return nil, err
			}
			lease, err = capture.Acquire(ctx)
			if err != nil {
				return nil, err
			}
			return lease.Close, nil
		},
		Run: func(ctx context.Context, id session.OperationID) (any, error) {
			var committed json.RawMessage
			_, err := lease.Execute(ctx, string(id), func(ctx context.Context) error { return r.store.CheckChildTransfer(ctx, id, true) }, func(ctx context.Context, attachments []browserhost.Attachment) error {
				if len(attachments) != len(intent.Children) {
					return store.ErrConflict
				}
				for _, a := range attachments {
					if a.Owner != child {
						return store.ErrConflict
					}
				}
				admitted, err := r.store.CommitChildTransfer(ctx, id, transferScopes(attachments))
				if err != nil {
					return err
				}
				committed, err = store.ChildAdmissionValue(admitted)
				return err
			})
			if committed != nil {
				// The child and receipt are durable even if native revocation won the final
				// activation race. Those handles remain retired; success does not claim a
				// presently available browser or restore either native owner.
				r.Wake()
				return committed, nil
			}
			if err == nil {
				return nil, tool.Fatal(errors.New("browser transfer omitted child admission"))
			}
			return nil, browserFailure(err)
		},
	}, nil
}

// spawnTransferredChild observes runtime-owned accepted work. Caller cancellation
// ends only this bounded wait, never the native handoff or its durable settlement.
func (r *Runtime) spawnTransferredChild(ctx context.Context, identity session.RequestIdentity, request store.ChildRequest) (store.ChildAdmission, error) {
	result, err := r.store.ChildTransferResult(ctx, identity, request)
	if !errors.Is(err, store.ErrBusy) && !errors.Is(err, store.ErrNotFound) {
		return result, err
	}
	if errors.Is(err, store.ErrNotFound) {
		if _, err := r.store.BeginChildTransfer(ctx, identity, request); err != nil {
			return store.ChildAdmission{}, err
		}
		r.Wake()
	}
	wait, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		result, err = r.store.ChildTransferResult(wait, identity, request)
		if !errors.Is(err, store.ErrBusy) {
			return result, err
		}
		select {
		case <-wait.Done():
			return store.ChildAdmission{}, store.ErrBusy
		case <-ticker.C:
		}
	}
}
