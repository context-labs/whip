package daemon

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/tools/bashrun"
)

func TestDaemonInteractiveRunnerForwardsBytesAndOrdersEvents(t *testing.T) {
	type emitted struct {
		kind  string
		event StreamEvent
	}
	events := make(chan emitted, 16)
	runner := newDaemonInteractiveRunner(func(kind string, event StreamEvent) {
		events <- emitted{kind: kind, event: event}
	})
	result := make(chan string, 1)
	go func() {
		result <- runner.Run(t.Context(), bashrun.Options{
			Command: `stty -echo; printf 'ready\n'; read line; stty echo; printf '\naccepted\n'`,
			Cwd:     t.TempDir(),
			Timeout: 5 * time.Second,
		})
	}()

	started := <-events
	if started.kind != "stream.terminal.started" || started.event.ID == "" {
		t.Fatalf("first terminal event = %+v", started)
	}
	if err := runner.Send("stale-terminal", []byte("ignored\n")); err == nil {
		t.Fatal("stale terminal accepted input")
	}
	var kinds []string
	var output strings.Builder
	for !strings.Contains(output.String(), "ready") {
		select {
		case item := <-events:
			kinds = append(kinds, item.kind)
			output.WriteString(item.event.Text)
		case <-time.After(5 * time.Second):
			t.Fatal("terminal did not become ready")
		}
	}
	secret := "terminal-secret-90210"
	if err := runner.Send(started.event.ID, []byte(secret+"\n")); err != nil {
		t.Fatal(err)
	}

	for {
		select {
		case item := <-events:
			kinds = append(kinds, item.kind)
			output.WriteString(item.event.Text)
			if item.kind == "stream.terminal.completed" {
				if strings.Contains(output.String(), secret) {
					t.Fatalf("no-echo terminal leaked input in output %q", output.String())
				}
				if !strings.Contains(output.String(), "accepted") {
					t.Fatalf("terminal output = %q", output.String())
				}
				if got := <-result; !strings.Contains(got, "accepted") {
					t.Fatalf("terminal result = %q", got)
				}
				if kinds[len(kinds)-1] != "stream.terminal.completed" {
					t.Fatalf("terminal event order = %v", kinds)
				}
				return
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("terminal events timed out: %v", kinds)
		}
	}
}

type terminalInput struct {
	id    string
	bytes []byte
}

type terminalFakeRunner struct {
	fakeRunner
	inputs chan terminalInput
}

func (r *terminalFakeRunner) SendTerminalInput(id string, input []byte) error {
	r.inputs <- terminalInput{id: id, bytes: append([]byte(nil), input...)}
	return nil
}

func TestProtocolRedactsTerminalInputFromDurableState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.db")
	store := openStore(t, path)
	rootID := createRoot(t, store)
	runner := &terminalFakeRunner{inputs: make(chan terminalInput, 1)}
	value, err := New(store, func(context.Context, session.Meta, []llm.Message) (Components, error) {
		return Components{Runner: runner}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	server, client, served := startTCPClient(t, value, "terminal-client")
	t.Cleanup(func() {
		_ = client.Close()
		_ = server.Close()
		<-served
	})

	secret := "terminal-secret-314159"
	payload, err := json.Marshal(clientActionPayload{ID: "terminal-1", Bytes: []byte(secret + "\n")})
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.Command(t.Context(), CommandParams{
		CommandID: "terminal-command", Scope: string(session.CommandScopeRoot), RootID: rootID,
		Operation: "terminal.input", Payload: payload,
	})
	if err != nil || result.Status != "succeeded" {
		t.Fatalf("terminal input result = %+v, %v", result, err)
	}
	if delivered := <-runner.inputs; delivered.id != "terminal-1" || string(delivered.bytes) != secret+"\n" {
		t.Fatalf("delivered terminal input = %+v", delivered)
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var stored []byte
	if err := db.QueryRowContext(t.Context(), `SELECT payload_inline FROM commands WHERE client_id=? AND command_id=?`, "terminal-client", "terminal-command").Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(stored), secret) || !strings.Contains(string(stored), `"redacted":true`) {
		t.Fatalf("durable terminal payload = %q", stored)
	}
	replay, err := client.Replay(t.Context(), ReplayParams{RootID: rootID, Limit: session.MaxEventReplay})
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range replay.Events {
		if strings.Contains(string(event.Payload), secret) {
			t.Fatalf("terminal secret leaked in replay event %q: %q", event.Kind, event.Payload)
		}
	}
	snapshot, err := client.Snapshot(t.Context(), rootID)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), secret) {
		t.Fatal("terminal secret leaked in snapshot")
	}
}
