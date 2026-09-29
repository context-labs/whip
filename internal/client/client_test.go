package client_test

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/client"
	"github.com/context-labs/whip/internal/protocol"
)

func peer(t *testing.T, handle func(context.Context, protocol.Request) *protocol.Response) (*client.Client, string) {
	t.Helper()
	directory, err := os.MkdirTemp("/tmp", "whip-go-client-") //nolint:usetesting // Unix sockets must fit the macOS path limit.
	if err != nil {
		t.Fatal(err)
	}
	socket := filepath.Join(directory, "runtime.sock")
	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	var workers sync.WaitGroup
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			workers.Go(func() {
				defer func() { _ = conn.Close() }()
				stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
				defer stop()
				scanner := bufio.NewScanner(conn)
				scanner.Buffer(make([]byte, 4096), protocol.MaxFrameBytes+1)
				for scanner.Scan() {
					var request protocol.Request
					if err := json.Unmarshal(scanner.Bytes(), &request); err != nil {
						t.Error(err)
						return
					}
					var response *protocol.Response
					if request.Method == "initialize" {
						response = success(t, request, protocol.InitializeResult{Major: 4, Minor: 0, RuntimeID: "runtime", ProcessEpoch: "boot", Builtins: []protocol.DefinitionRef{}})
					} else {
						response = handle(ctx, request)
					}
					if response == nil {
						return
					}
					if err := json.NewEncoder(conn).Encode(response); err != nil {
						return
					}
				}
			})
		}
	}()
	t.Cleanup(func() {
		cancel()
		_ = listener.Close()
		<-done
		workers.Wait()
		if err := os.RemoveAll(directory); err != nil {
			t.Error(err)
		}
	})
	c, err := client.Connect(t.Context(), socket, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := c.Close(); err != nil {
			t.Error(err)
		}
	})
	return c, socket
}

func success(t *testing.T, request protocol.Request, value any) *protocol.Response {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return &protocol.Response{JSONRPC: "2.0", ID: request.ID, Result: raw}
}

func missing(request protocol.Request) *protocol.Response {
	return &protocol.Response{JSONRPC: "2.0", ID: request.ID, Error: &protocol.RPCError{Code: -32001, Kind: "NOT_FOUND", Message: "missing"}}
}

func params() protocol.SubmitParams {
	return protocol.SubmitParams{SessionID: "owner", Identity: protocol.RequestIdentity{ClientID: "go", RequestID: "prompt"}, Source: "user", Parts: []protocol.Part{{Type: "text", Text: "original"}}}
}

func admission() protocol.Admission {
	p := params()
	return protocol.Admission{Receipt: protocol.Receipt{Identity: p.Identity, Digest: strings.Repeat("a", 64), InputID: new(protocol.ID("input")), CreatedAt: "2026-09-28T00:00:00Z"}, Input: &protocol.Input{ID: "input", SessionID: "owner", Source: "user", Kind: "prompt", State: "queued", Parts: p.Parts, CreatedAt: "2026-09-28T00:00:00Z"}}
}

func TestInputCommandRecoversLostAcknowledgementWithoutReplay(t *testing.T) {
	var sends, reads atomic.Int32
	c, _ := peer(t, func(_ context.Context, r protocol.Request) *protocol.Response {
		switch r.Method {
		case "sessions.submit":
			sends.Add(1)
			return nil
		case "receipts.match":
			reads.Add(1)
			var match protocol.MatchReceiptParams
			if err := json.Unmarshal(r.Params, &match); err != nil {
				t.Error(err)
			}
			raw, err := base64.StdEncoding.DecodeString(match.ParamsBase64)
			if err != nil {
				t.Error(err)
			}
			var got protocol.SubmitParams
			if err := json.Unmarshal(raw, &got); err != nil {
				t.Error(err)
			}
			if got.Parts[0].Text != "original" {
				t.Error("recovery changed original payload")
			}
			return success(t, r, admission())
		default:
			t.Error("unexpected call", r.Method)
			return nil
		}
	})
	p := params()
	command, err := c.PrepareInput("sessions.submit", p)
	if err != nil {
		t.Fatal(err)
	}
	p.Parts[0].Text = "changed"
	raw, err := json.Marshal(command.Record())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := command.Send(t.Context()); err == nil {
		t.Fatal("lost acknowledgement claimed success")
	}
	recovered, err := c.RestoreInput(raw)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := recovered.Send(t.Context()); err == nil {
		t.Fatal("recovery silently sent")
	}
	result, found, err := recovered.Check(t.Context())
	if err != nil || !found || result.Input.ID != "input" || !recovered.Record().Accepted {
		t.Fatal(result, found, err)
	}
	result, err = recovered.Retry(t.Context())
	if err != nil || result.Input.ID != "input" || sends.Load() != 1 || reads.Load() != 2 {
		t.Fatal(result, err, sends.Load(), reads.Load())
	}
}

func TestInputCommandWaiterAbortSharesSendAndCloseJoins(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var sends atomic.Int32
	c, _ := peer(t, func(ctx context.Context, r protocol.Request) *protocol.Response {
		if r.Method != "sessions.submit" {
			t.Error("unexpected remote cancellation", r.Method)
			return nil
		}
		sends.Add(1)
		close(entered)
		select {
		case <-release:
		case <-ctx.Done():
			return nil
		}
		return success(t, r, admission())
	})
	command, err := c.PrepareInput("sessions.submit", params())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	waiter := make(chan error, 1)
	go func() { _, err := command.Send(ctx); waiter <- err }()
	<-entered
	cancel()
	if err := <-waiter; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	close(release)
	result, err := command.Send(t.Context())
	if err != nil || result.Input.ID != "input" || sends.Load() != 1 {
		t.Fatal(result, err, sends.Load())
	}
	result.Input.Parts[0].Text = "caller mutation"
	second, err := command.Send(t.Context())
	if err != nil || second.Input.Parts[0].Text != "original" {
		t.Fatal("shared mutable admission", second, err)
	}
}

func TestInputCommandCloseInterruptsOnlyTransportAndJoins(t *testing.T) {
	entered := make(chan struct{})
	c, _ := peer(t, func(ctx context.Context, r protocol.Request) *protocol.Response {
		close(entered)
		<-ctx.Done()
		return nil
	})
	command, err := c.PrepareInput("sessions.submit", params())
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := command.Send(t.Context()); done <- err }()
	<-entered
	started := time.Now()
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	if time.Since(started) > time.Second {
		t.Fatal("close did not join bounded transport")
	}
	if err := <-done; err == nil {
		t.Fatal("closed transport claimed success")
	}
}

func TestInputCommandExplicitRetryMissingAndPreviouslyAcceptedMissing(t *testing.T) {
	var exists atomic.Bool
	var sends atomic.Int32
	c, _ := peer(t, func(_ context.Context, r protocol.Request) *protocol.Response {
		if r.Method == "receipts.match" {
			if !exists.Load() {
				return missing(r)
			}
			return success(t, r, admission())
		}
		sends.Add(1)
		exists.Store(true)
		return success(t, r, admission())
	})
	command, err := c.PrepareInput("sessions.submit", params())
	if err != nil {
		t.Fatal(err)
	}
	result, err := command.Retry(t.Context())
	if err != nil || result.Input.ID != "input" || sends.Load() != 1 {
		t.Fatal(result, err)
	}
	exists.Store(false)
	if _, err := command.Retry(t.Context()); err == nil || sends.Load() != 1 {
		t.Fatal("accepted input replayed after missing receipt", err)
	}
	for _, change := range []func(*client.InputRecord){func(r *client.InputRecord) { r.RuntimeID = "foreign" }, func(r *client.InputRecord) { r.Namespace = "legacy" }, func(r *client.InputRecord) { r.Method = "shell.input" }} {
		record := command.Record()
		change(&record)
		raw, _ := json.Marshal(record)
		if _, err := c.RestoreInput(raw); err == nil {
			t.Fatal("invalid recovery record accepted")
		}
	}
}

func observed(sequence protocol.Counter, epoch protocol.ID) protocol.SessionObservation {
	value := protocol.SessionObservation{Snapshot: protocol.HistorySnapshot{SessionID: "owner", Revision: 1, ThroughSequence: sequence, MessageCount: sequence}, Epoch: epoch, Messages: []protocol.Message{}}
	if sequence > 0 {
		value.Messages = append(value.Messages, protocol.Message{ID: "message", SessionID: "owner", TurnID: new(protocol.ID("turn")), GroupID: "turn", Sequence: sequence, Role: "assistant", Parts: []protocol.Part{{Type: "text", Text: "done"}}, CreatedAt: "2026-09-28T00:00:00Z"})
	}
	return value
}

func TestObserverRewindRestartAndCommittedPreviewAreExplicit(t *testing.T) {
	var calls atomic.Int32
	c, _ := peer(t, func(_ context.Context, r protocol.Request) *protocol.Response {
		var p protocol.HistoryParams
		if err := json.Unmarshal(r.Params, &p); err != nil {
			t.Error(err)
		}
		switch calls.Add(1) {
		case 1:
			value := observed(1, "boot")
			value.Preview = &protocol.MessagePreview{AttemptID: "attempt", TurnID: "turn", MessageID: "message", Revision: 1, Text: "partial", Calls: []protocol.CallPreview{}}
			return success(t, r, value)
		case 2:
			if p.After != 1 || p.ExpectedRevision == nil || *p.ExpectedRevision != 1 {
				t.Error("lost exact cursor", p)
			}
			return &protocol.Response{JSONRPC: "2.0", ID: r.ID, Error: &protocol.RPCError{Code: -32009, Kind: "CONFLICT", Message: "rewound"}}
		default:
			if p.After != 0 || p.ExpectedRevision != nil {
				t.Error("rewind did not reset cursor", p)
			}
			value := observed(0, "restarted")
			value.Snapshot.Revision = 2
			return success(t, r, value)
		}
	})
	s, err := c.Session("owner")
	if err != nil {
		t.Fatal(err)
	}
	observer, err := s.Observer(client.ObservationCursor{})
	if err != nil {
		t.Fatal(err)
	}
	first, err := observer.Next(t.Context())
	if err != nil || first.Preview != nil || first.Cursor.After != 1 {
		t.Fatal(first, err)
	}
	*first.Cursor.Revision = 99
	second, err := observer.Next(t.Context())
	if err != nil || !second.Reset || second.Epoch != "restarted" || second.Cursor.After != 0 || *second.Cursor.Revision != 2 {
		t.Fatal(second, err)
	}
}

func TestSessionAndObserverRejectForeignProjectionAndBadContent(t *testing.T) {
	body := []byte("body")
	digest := sha256.Sum256(body)
	for _, test := range []string{"input", "turn", "history", "observation", "content"} {
		t.Run(test, func(t *testing.T) {
			c, _ := peer(t, func(_ context.Context, r protocol.Request) *protocol.Response {
				switch test {
				case "input":
					v := admission().Input
					v.SessionID = "foreign"
					return success(t, r, v)
				case "turn":
					return success(t, r, protocol.Turn{ID: "turn", SessionID: "foreign", ConfigRevision: 1, HistoryRevision: 1, Kind: "prompt", State: "running", StartedAt: "2026-09-28T00:00:00Z"})
				case "history":
					v := observed(1, "boot")
					v.Messages[0].SessionID = "foreign"
					return success(t, r, protocol.HistoryPageResult{Snapshot: v.Snapshot, Messages: v.Messages})
				case "observation":
					v := observed(1, "boot")
					v.Snapshot.SessionID = "foreign"
					return success(t, r, v)
				default:
					return success(t, r, protocol.ReadContentResult{Reference: protocol.ContentReference{ID: "content", SessionID: "owner", Size: 4, MediaType: "text/plain", Digest: hex.EncodeToString(digest[:]), CreatedAt: "2026-09-28T00:00:00Z"}, DataBase64: base64.StdEncoding.EncodeToString([]byte("fake"))})
				}
			})
			s, err := c.Session("owner")
			if err != nil {
				t.Fatal(err)
			}
			switch test {
			case "input":
				_, err = s.Input(t.Context(), "input")
			case "turn":
				_, err = s.CancelTurn(t.Context(), "turn")
			case "history":
				_, err = s.History(t.Context(), protocol.HistoryPageParams{Direction: "backward", Limit: 10})
			case "observation":
				observer, _ := s.Observer(client.ObservationCursor{})
				_, err = observer.Next(t.Context())
			default:
				_, _, err = s.ReadContent(t.Context(), "content")
			}
			if err == nil {
				t.Fatal("invalid projection accepted")
			}
		})
	}
}

func TestInputCommandBoundsClientOwnedSendCountAndBytes(t *testing.T) {
	for _, test := range []struct {
		name        string
		count, size int
	}{{"count", 64, 1}, {"bytes", 11, 700000}} {
		t.Run(test.name, func(t *testing.T) {
			entered := make(chan struct{}, 64)
			c, _ := peer(t, func(ctx context.Context, _ protocol.Request) *protocol.Response {
				entered <- struct{}{}
				<-ctx.Done()
				return nil
			})
			var callers sync.WaitGroup
			for i := range test.count {
				p := params()
				p.Identity.RequestID = protocol.ID(fmt.Sprintf("request_%d", i))
				p.Parts[0].Text = strings.Repeat("x", test.size)
				command, err := c.PrepareInput("sessions.submit", p)
				if err != nil {
					t.Fatal(err)
				}
				callers.Go(func() { _, _ = command.Send(t.Context()) })
				select {
				case <-entered:
				case <-time.After(5 * time.Second):
					t.Fatal("send did not reach bounded peer")
				}
			}
			p := params()
			p.Parts[0].Text = strings.Repeat("x", test.size)
			overflow, err := c.PrepareInput("sessions.submit", p)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := overflow.Send(t.Context()); err == nil || !strings.Contains(err.Error(), "pending client send limit") {
				t.Fatal("send capacity was not enforced", err)
			}
			if err := c.Close(); err != nil {
				t.Fatal(err)
			}
			callers.Wait()
		})
	}
}

func TestObserverRejectsConcurrentReadAndStopsOnTransportFailure(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	c, socket := peer(t, func(ctx context.Context, _ protocol.Request) *protocol.Response {
		calls.Add(1)
		close(entered)
		select {
		case <-release:
		case <-ctx.Done():
		}
		return nil
	})
	if _, err := client.Connect(t.Context(), socket, new(protocol.ID("foreign"))); err == nil {
		t.Fatal("foreign runtime initialization accepted")
	}
	s, err := c.Session("owner")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Observer(client.ObservationCursor{After: 1}); err == nil {
		t.Fatal("cursor without revision accepted")
	}
	observer, _ := s.Observer(client.ObservationCursor{})
	done := make(chan error, 1)
	go func() { _, err := observer.Next(t.Context()); done <- err }()
	<-entered
	if _, err := observer.Next(t.Context()); err == nil || !strings.Contains(err.Error(), "already pending") {
		t.Fatal("concurrent observation queued", err)
	}
	close(release)
	if err := <-done; err == nil || calls.Load() != 1 {
		t.Fatal("transport failure silently restarted observation", err, calls.Load())
	}
}
