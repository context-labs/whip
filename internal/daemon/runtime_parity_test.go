package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/session"
)

type runtimeFixture struct {
	payload     string
	unavailable bool
}

// Every runtime operation crosses the real codec, registry validation and RPC
// dispatch. Unsupported fake services must report failure, never fake success.
// Existing authority/runtime tests supply the deeper successful agent/tool
// scenarios; this inventory prevents adding an untested registered operation.
func TestRuntimeRegistryEveryOperationOverUnixRPC(t *testing.T) {
	fixtures := map[string]runtimeFixture{
		"session.create":    {`{"kind":"agent","cwd":"$CWD","model":"model","provider":"provider"}`, false},
		"session.delete":    {`{"root_id":"$ROOT"}`, false},
		"daemon.checkpoint": {`{"reason":"test"}`, false},
		"submit":            {`{"text":"hello"}`, false}, "steer": {`{"text":"steer"}`, false},
		"cancel":   {`{"turn_id":"stale"}`, true},
		"goal.set": {`{"text":"ship"}`, false}, "goal.run": {`{"text":"ship"}`, false}, "goal.from-context": {`{"window":2}`, true},
		"schedule.list": {`{}`, false}, "schedule.create": {`{"schedule":"@every 10m","prompt":"inspect"}`, false}, "schedule.delete": {`{"schedule_id":1}`, false},
		"session.fork":      {`{"title":"fork","expected_revision":"0"}`, false},
		"workspace.inspect": {`{}`, false}, "workspace.set": {`{"path":"nested"}`, false},
		"session.effort": {`{"effort":"off","persist_default":false}`, false}, "session.effort.get": {`{}`, false},
		"session.model": {`{"model":"replacement","provider":"provider","persist_default":false}`, false}, "session.model.get": {`{}`, false},
		"session.list": {`{"limit":10}`, false}, "session.open": {`{"id":"$ROOT"}`, false}, "session.rename": {`{"title":"renamed"}`, false},
		"session.reload": {`{}`, false}, "session.autotitle": {`{}`, false}, "run.configure": {`{"max_turns":2}`, true},
		"history.clear": {`{}`, false}, "history.rewind": {`{"cut":2,"expected_revision":"0"}`, false}, "history.compact": {`{}`, true},
		"history.compact.log": {`{}`, false}, "history.compact.retry": {`{}`, false}, "compaction.configure": {`{}`, false}, "history.user.list": {`{}`, false},
		"session.preview": {`{"id":"$ROOT"}`, false}, "agents.list": {`{}`, false}, "agent.transcript": {`{"id":"$ROOT"}`, false},
		"agent.submit": {`{"id":"missing","text":"input"}`, true}, "agent.turn.cancel": {`{"id":"missing","turn_id":"stale"}`, true},
		"question.answer":   {`{"id":"missing","answer":["yes"],"dismissed":false}`, true},
		"provider.catalogs": {`{}`, false}, "agent.control": {`{"id":"missing"}`, true}, "agent.delete": {`{"id":"missing"}`, true},
		"budget.cap": {`{"id":"$ROOT","kind":"tokens","limit":"100"}`, false}, "capability.revoke": {`{"id":"missing"}`, true},
		"shell.run": {`{"command":"echo"}`, true}, "context.audit": {`{}`, true},
		"mcp.status": {`{}`, false}, "mcp.reconnect": {`{"name":"alpha"}`, false}, "mcp.enable": {`{"name":"alpha"}`, false}, "mcp.disable": {`{"name":"alpha"}`, false},
		"mcp.import.status": {`{}`, false}, "mcp.import.configure": {`{"source":"claude","enabled":false}`, false},
		"lsp.status": {`{}`, false}, "browser.status": {`{}`, false}, "browser.set_driver": {`{"driver":"rod"}`, true},
		"computer.status": {`{}`, false}, "computer.allow": {`{"app":"Terminal"}`, true}, "computer.deny": {`{"app":"Terminal"}`, true},
		"terminal.input": {`{"id":"terminal","bytes":"eWVz"}`, false}, "tool.configure": {`{"deny_permissions":true}`, true},
		"tool.schema": {`{}`, true}, "tool.call": {`{"tool":"read","arguments":{}}`, true},
		"permission.mode": {`{"external_permissions":true}`, true}, "permission.rules": {`{}`, false}, "permission.forget": {`{"id":"missing"}`, true},
		"mcp.attach": {`{"servers":{}}`, false},
	}
	tested := 0
	for _, operation := range protocol.Operations() {
		if operation.Surface != "runtime" {
			continue
		}
		fixture, ok := fixtures[operation.Name]
		if !ok {
			t.Errorf("registered runtime operation %s has no behavioral fixture", operation.Name)
			continue
		}
		tested++
		t.Run(operation.Name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("WHIP_HOME", home)
			if err := (&config.Config{Providers: map[string]config.Provider{}}).Save(); err != nil {
				t.Fatal(err)
			}
			store := openStore(t, filepath.Join(home, "runtime.db"))
			rootID := createRoot(t, store)
			if err := store.Save(rootID, 1, []llm.Message{{Role: "system"}, {Role: "user", Content: "hello"}, {Role: "assistant", Content: "answer"}}, "model", "provider"); err != nil {
				t.Fatal(err)
			}
			if _, err := store.AddSchedule(rootID, "@every 10m", "existing", time.Now().UTC().Add(time.Hour)); err != nil {
				t.Fatal(err)
			}
			value, err := New(store, func(context.Context, session.Meta, []llm.Message) (Components, error) {
				return Components{Runner: &controlSurfaceRunner{fakeRunner: &fakeRunner{}, workingDirectory: home}, MCP: &controlSurfaceMCP{}}, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			client := runtimeUnixClient(t, value)
			payload := json.RawMessage(strings.NewReplacer("$ROOT", rootID, "$CWD", home).Replace(fixture.payload))
			if err := protocol.ValidateRuntime(operation.Name, payload); err != nil {
				t.Fatalf("fixture params violate registry: %v", err)
			}
			params := CommandParams{CommandID: "fixture", Scope: "root", RootID: rootID, Operation: operation.Name, Payload: payload}
			if operation.Name == "session.create" || operation.Name == "session.delete" || operation.Name == "daemon.checkpoint" {
				params.Scope = "daemon"
				params.RootID = ""
			}
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			result, err := client.Command(ctx, params)
			failed := err != nil || result.Status == "failed" || result.Failure != nil
			if failed != fixture.unavailable {
				t.Fatalf("expected unavailable=%t result=%+v error=%v", fixture.unavailable, result, err)
			}
			if failed {
				var rpcErr *RPCError
				if errors.As(err, &rpcErr) && rpcErr.Code == -32601 {
					t.Fatalf("registered operation has no handler: %v", err)
				}
				if result.Failure != nil && result.Failure.Code == -32601 {
					t.Fatalf("registered operation has no handler: %+v", result.Failure)
				}
				return
			}
			schema, err := protocol.SchemaFor(operation.Result)
			if err != nil {
				t.Fatal(err)
			}
			resolved, err := schema.Resolve(nil)
			if err != nil {
				t.Fatal(err)
			}
			var body any
			if err := json.Unmarshal(result.Result, &body); err != nil {
				t.Fatalf("unstructured result %s: %v", result.Result, err)
			}
			if err := resolved.Validate(body); err != nil {
				t.Fatalf("runtime result violates %s: %s: %v", operation.Result, result.Result, err)
			}
			if err := json.Unmarshal(result.Result, reflect.New(operation.Result).Interface()); err != nil {
				t.Fatalf("runtime result cannot decode: %v", err)
			}
		})
	}
	if tested != len(fixtures) {
		t.Fatalf("fixture registry drift: tested %d of %d", tested, len(fixtures))
	}
}

func runtimeUnixClient(t *testing.T, value *Daemon) *Client {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "whip-v2-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "unix", filepath.Join(dir, "runtime.sock"))
	if err != nil {
		t.Fatal(err)
	}
	server, err := NewServer(value, ServerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	served := make(chan error, 1)
	go func() { served <- server.Serve(listener) }()
	conn, err := (&net.Dialer{}).DialContext(t.Context(), "unix", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	client, err := NewClient(t.Context(), conn, InitializeParams{ProtocolMajor: 2, ClientID: "fixture-client", ClientKind: "test"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = client.Close()
		_ = server.Close()
		if err := <-served; err != nil {
			t.Error(err)
		}
	})
	return client
}
