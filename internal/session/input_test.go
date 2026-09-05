package session

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestResolveInboxPayloadPreservesCompleteInputAtReadBoundaries(t *testing.T) {
	store, rootID, rootAgentID := newMailboxFixture(t)
	for _, recipient := range []string{rootAgentID, "child"} {
		for _, size := range []int{0, InlineValueLimit, InlineValueLimit + 1, MaxContentRead, MaxContentRead + 1, 2*MaxContentRead + 17} {
			t.Run(fmt.Sprintf("%s/%d", recipient, size), func(t *testing.T) {
				body := bytes.Repeat([]byte("x"), size)
				if size > 0 {
					body[size-1] = '!'
				}
				sequence, err := store.EnqueueInbox(t.Context(), InboxEnqueue{
					RootID: rootID, AgentID: recipient, Kind: "submit", Payload: RuntimePayload{Data: body},
				})
				if err != nil {
					t.Fatal(err)
				}
				items, err := store.LoadQueuedInbox(t.Context(), rootID, recipient, sequence.InboxSeq-1, 1)
				if err != nil || len(items) != 1 {
					t.Fatalf("load = %+v, %v", items, err)
				}
				// The durable content reference owns the size, regardless of a
				// stale or forged size hint supplied by an adapter.
				items[0].Payload.Size = MaxInputPayloadBytes + 1
				resolved, err := store.ResolveInboxPayload(t.Context(), items[0])
				if err != nil || !bytes.Equal(resolved, body) {
					t.Fatalf("resolved %d/%d bytes, %v", len(resolved), len(body), err)
				}
				if len(items[0].Payload.Inline) > 0 {
					resolved[0] = '?'
					if items[0].Payload.Inline[0] != body[0] {
						t.Fatal("resolution aliases durable inline input")
					}
				}
			})
		}
	}
}

func TestResolveInboxPayloadUsesRecipientAuthority(t *testing.T) {
	store, rootID, rootAgentID := newMailboxFixture(t)
	value, err := store.StoreContent(t.Context(), ContentGrant{
		RootID: rootID, AgentID: "child", Scope: ContentGrantAgent,
	}, RuntimePayload{Data: bytes.Repeat([]byte("private"), 2_000)})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name      string
		rootID    string
		agentID   string
		reference string
	}{
		{name: "parent cannot read child private input", rootID: rootID, agentID: rootAgentID, reference: value.ReferenceID},
		{name: "different root", rootID: "other-root", agentID: "child", reference: value.ReferenceID},
		{name: "missing agent", rootID: rootID, agentID: "missing", reference: value.ReferenceID},
		{name: "missing reference", rootID: rootID, agentID: "child", reference: "missing"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := store.ResolveInboxPayload(t.Context(), InboxItem{
				RootID: test.rootID, AgentID: test.agentID, Payload: RuntimeValue{ReferenceID: test.reference},
			})
			if !errors.Is(err, ErrInvalidInput) || !errors.Is(err, ErrContentAccess) {
				t.Fatalf("unauthorized input = %v", err)
			}
		})
	}
	if err := store.RevokeContentGrant(t.Context(), value.ReferenceID, rootID, "child"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ResolveInboxPayload(t.Context(), InboxItem{RootID: rootID, AgentID: "child", Payload: value}); !errors.Is(err, ErrContentAccess) {
		t.Fatalf("revoked input = %v", err)
	}
}

func TestResolveInboxPayloadRejectsMissingAndTruncatedBodies(t *testing.T) {
	for _, damage := range []string{"missing", "empty", "truncated after first chunk"} {
		t.Run(damage, func(t *testing.T) {
			store, rootID, rootAgentID := newSwarmFixture(t)
			value, err := store.StoreContent(t.Context(), ContentGrant{
				RootID: rootID, AgentID: rootAgentID, Scope: ContentGrantAgent,
			}, RuntimePayload{Data: bytes.Repeat([]byte("x"), MaxContentRead+100)})
			if err != nil {
				t.Fatal(err)
			}
			var sequence int
			var name, path string
			if err := store.db.QueryRowContext(t.Context(), `PRAGMA database_list`).Scan(&sequence, &name, &path); err != nil {
				t.Fatal(err)
			}
			bodyPath := filepath.Join(filepath.Dir(path), "artifacts", "sha256", value.Digest)
			switch damage {
			case "missing":
				err = os.Remove(bodyPath)
			case "empty":
				err = os.Truncate(bodyPath, 0)
			case "truncated after first chunk":
				err = os.Truncate(bodyPath, MaxContentRead+7)
			}
			if err != nil {
				t.Fatal(err)
			}
			if data, err := store.ResolveInboxPayload(t.Context(), InboxItem{RootID: rootID, AgentID: rootAgentID, Payload: value}); !errors.Is(err, ErrInvalidInput) || data != nil {
				t.Fatalf("damaged input = %d bytes, %v", len(data), err)
			}
		})
	}
}

func TestResolveInboxPayloadBoundsTrustedContentMetadata(t *testing.T) {
	store, rootID, rootAgentID := newSwarmFixture(t)
	value, err := store.StoreContent(t.Context(), ContentGrant{
		RootID: rootID, AgentID: rootAgentID, Scope: ContentGrantAgent,
	}, RuntimePayload{Data: []byte("small body")})
	if err != nil {
		t.Fatal(err)
	}
	value.Size = 1
	for _, size := range []int64{-1, MaxInputPayloadBytes + 1, 1} {
		t.Run(fmt.Sprintf("stored_size_%d", size), func(t *testing.T) {
			exec(t, store, `UPDATE content_references SET size=? WHERE id=?`, size, value.ReferenceID)
			if _, err := store.ResolveInboxPayload(t.Context(), InboxItem{RootID: rootID, AgentID: rootAgentID, Payload: value}); !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("invalid metadata size = %v", err)
			}
		})
	}
}

func TestInputPayloadMaximumAndAdmissionRejection(t *testing.T) {
	store, rootID, rootAgentID := newSwarmFixture(t)
	body := bytes.Repeat([]byte("x"), MaxInputPayloadBytes+1)
	body[MaxInputPayloadBytes-1] = '!'
	sequence, err := store.EnqueueInbox(t.Context(), InboxEnqueue{
		RootID: rootID, AgentID: rootAgentID, Kind: "submit", Payload: RuntimePayload{Data: body[:MaxInputPayloadBytes]},
	})
	if err != nil {
		t.Fatal(err)
	}
	items, err := store.LoadQueuedInbox(t.Context(), rootID, rootAgentID, sequence.InboxSeq-1, 1)
	if err != nil || len(items) != 1 {
		t.Fatalf("maximum input load = %+v, %v", items, err)
	}
	data, err := store.ResolveInboxPayload(t.Context(), items[0])
	if err != nil || !bytes.Equal(data, body[:MaxInputPayloadBytes]) {
		t.Fatalf("maximum input resolve = %d bytes, %v", len(data), err)
	}
	for _, operation := range []string{"inline resolve", "enqueue", "command admission", "child admission"} {
		t.Run(operation, func(t *testing.T) {
			var err error
			switch operation {
			case "inline resolve":
				_, err = store.ResolveInboxPayload(t.Context(), InboxItem{RootID: rootID, AgentID: rootAgentID, Payload: RuntimeValue{Inline: body}})
			case "enqueue":
				_, err = store.EnqueueInbox(t.Context(), InboxEnqueue{RootID: rootID, AgentID: rootAgentID, Kind: "submit", Payload: RuntimePayload{Data: body}})
			case "command admission":
				_, err = store.AdmitCommand(t.Context(), CommandAdmission{
					ClientID: "client", CommandID: "oversized", Scope: CommandScopeRoot, RootID: rootID,
					AgentID: rootAgentID, Kind: "submit", RequestDigest: "digest", Payload: RuntimePayload{Data: body},
				})
			case "child admission":
				_, err = store.AdmitAgent(t.Context(), AgentAdmission{
					RootID: rootID, ParentAgentID: rootAgentID, ChildAgentID: "oversized-child", Prompt: RuntimePayload{Data: body},
				})
			}
			if !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("oversized input = %v", err)
			}
		})
	}
	bodies, err := store.content.Bodies()
	if err != nil || len(bodies) != 1 || bodies[0].Size != MaxInputPayloadBytes {
		t.Fatalf("rejected admissions wrote content bodies: %+v, %v", bodies, err)
	}
	var commands, inbox, children int
	if err := store.db.QueryRowContext(t.Context(), `SELECT
		(SELECT count(*) FROM commands), (SELECT count(*) FROM inbox),
		(SELECT count(*) FROM agents WHERE parent_id IS NOT NULL)`).Scan(&commands, &inbox, &children); err != nil {
		t.Fatal(err)
	}
	if commands != 0 || inbox != 1 || children != 0 {
		t.Fatalf("oversized admissions retained rows: commands=%d inbox=%d children=%d", commands, inbox, children)
	}
}

func TestResolveInboxPayloadPreservesInfrastructureErrors(t *testing.T) {
	for _, fault := range []string{"closed database", "cancelled context", "deadline exceeded", "body is a directory"} {
		t.Run(fault, func(t *testing.T) {
			store, rootID, rootAgentID := newSwarmFixture(t)
			value, err := store.StoreContent(t.Context(), ContentGrant{
				RootID: rootID, AgentID: rootAgentID, Scope: ContentGrantAgent,
			}, RuntimePayload{Data: []byte("input")})
			if err != nil {
				t.Fatal(err)
			}
			ctx := t.Context()
			var want error
			switch fault {
			case "closed database":
				if err := store.Close(); err != nil {
					t.Fatal(err)
				}
			case "cancelled context":
				cancelled, cancel := context.WithCancel(ctx)
				cancel()
				ctx, want = cancelled, context.Canceled
			case "deadline exceeded":
				expired, cancel := context.WithDeadline(ctx, time.Now().Add(-time.Second))
				defer cancel()
				ctx, want = expired, context.DeadlineExceeded
			case "body is a directory":
				var sequence int
				var name, path string
				if err := store.db.QueryRowContext(ctx, `PRAGMA database_list`).Scan(&sequence, &name, &path); err != nil {
					t.Fatal(err)
				}
				bodyPath := filepath.Join(filepath.Dir(path), "artifacts", "sha256", value.Digest)
				if err := os.Remove(bodyPath); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(bodyPath, 0o700); err != nil {
					t.Fatal(err)
				}
				want = syscall.EISDIR
			}
			data, err := store.ResolveInboxPayload(ctx, InboxItem{RootID: rootID, AgentID: rootAgentID, Payload: value})
			if err == nil || data != nil || errors.Is(err, ErrInvalidInput) || errors.Is(err, ErrContentAccess) {
				t.Fatalf("infrastructure error misclassified: %d bytes, %v", len(data), err)
			}
			if want != nil && !errors.Is(err, want) {
				t.Fatalf("lost underlying error: %v; want %v", err, want)
			}
		})
	}
}
