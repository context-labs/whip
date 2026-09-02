package daemon

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/session"
)

type reconnectServer struct {
	mu       sync.Mutex
	events   []ProtocolEvent
	attempts int
	ids      []string
}

type reconnectConnection struct {
	server *reconnectServer
	events chan ProtocolEvent
	done   chan struct{}
	once   sync.Once
}

func (s *reconnectServer) connect(context.Context, map[string]int64) (RootConnection, error) {
	return &reconnectConnection{server: s, events: make(chan ProtocolEvent, 4), done: make(chan struct{})}, nil
}

func (c *reconnectConnection) Command(_ context.Context, params CommandParams) (CommandResult, error) {
	c.server.mu.Lock()
	c.server.attempts++
	c.server.ids = append(c.server.ids, params.CommandID)
	attempt := c.server.attempts
	if len(c.server.events) == 0 {
		c.server.events = append(c.server.events, ProtocolEvent{RootID: "root", Seq: 1, Kind: "stream.text", Payload: []byte(`{"text":"one"}`)})
	}
	event := c.server.events[0]
	c.server.mu.Unlock()
	if attempt == 1 {
		c.events <- event
		_ = c.Close()
		return CommandResult{}, net.ErrClosed
	}
	return CommandResult{CommandID: params.CommandID, Status: "succeeded", Output: "one"}, nil
}

func (c *reconnectConnection) Replay(_ context.Context, params ReplayParams) (ReplayResult, error) {
	c.server.mu.Lock()
	defer c.server.mu.Unlock()
	result := ReplayResult{Latest: int64(len(c.server.events))}
	for _, event := range c.server.events {
		if event.Seq > params.Cursor {
			result.Events = append(result.Events, event)
		}
	}
	return result, nil
}

func (*reconnectConnection) Snapshot(context.Context, string) (session.RootSnapshot, error) {
	return session.RootSnapshot{RootID: "root"}, nil
}
func (c *reconnectConnection) Events() <-chan ProtocolEvent { return c.events }
func (c *reconnectConnection) Done() <-chan struct{}        { return c.done }
func (*reconnectConnection) Err() error                     { return net.ErrClosed }
func (c *reconnectConnection) Close() error {
	c.once.Do(func() { close(c.done) })
	return nil
}

func TestRootClientReconnectsStableCommandWithoutDuplicateEvent(t *testing.T) {
	server := &reconnectServer{}
	client, err := NewRootClient(RootClientOptions{
		ClientID: "test", RootID: "root", Connector: server.connect,
		RetryMin: time.Millisecond, RetryMax: time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	client.Start()
	t.Cleanup(func() { _ = client.Close() })
	if err := client.WaitLive(t.Context()); err != nil {
		t.Fatal(err)
	}
	action, err := client.NewAction("submit", SubmitPayload{Text: "go"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.Command(t.Context(), action)
	if err != nil || result.Output != "one" {
		t.Fatalf("command = %+v, %v", result, err)
	}
	deadline := time.After(time.Second)
	events := 0
	for events < 1 {
		select {
		case update := <-client.Updates():
			if update.Event != nil {
				events++
			}
		case <-deadline:
			t.Fatal("event was not replayed")
		}
	}
	time.Sleep(10 * time.Millisecond)
	for {
		select {
		case update := <-client.Updates():
			if update.Event != nil {
				events++
			}
		default:
			if events != 1 {
				t.Fatalf("events = %d, want one", events)
			}
			server.mu.Lock()
			defer server.mu.Unlock()
			if server.attempts != 2 || len(server.ids) != 2 || server.ids[0] != server.ids[1] {
				t.Fatalf("attempts=%d ids=%v", server.attempts, server.ids)
			}
			return
		}
	}
}

type failingRootConnection struct{ reconnectConnection }

func (*failingRootConnection) Snapshot(context.Context, string) (session.RootSnapshot, error) {
	return session.RootSnapshot{}, errors.New("no session")
}

func TestRootClientStopsOnPermanentSynchronizationError(t *testing.T) {
	client, err := NewRootClient(RootClientOptions{
		ClientID: "test", RootID: "missing",
		Connector: func(context.Context, map[string]int64) (RootConnection, error) {
			return &failingRootConnection{reconnectConnection{events: make(chan ProtocolEvent), done: make(chan struct{})}}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	client.Start()
	if err := client.WaitLive(t.Context()); err == nil || err.Error() != "no session" {
		t.Fatalf("WaitLive error = %v", err)
	}
	_ = client.Close()
}

func TestRootClientClosesBeforeStart(t *testing.T) {
	client, err := NewRootClient(RootClientOptions{
		ClientID: "client", RootID: "root",
		Connector: func(context.Context, map[string]int64) (RootConnection, error) {
			t.Fatal("closed client attempted to connect")
			return nil, errors.New("unexpected connection")
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	client.Start()
	select {
	case <-client.Updates():
	default:
		t.Fatal("updates remained open after close")
	}
}
