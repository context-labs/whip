package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/session"
)

const crashFixtureEnv = "WHIP_V2_CRASH_FIXTURE"
const crashFixtureExit = 73

type crashReceipt struct {
	RootID    string        `json:"root_id"`
	RuntimeID string        `json:"runtime_id"`
	Receipt   CommandResult `json:"receipt"`
}

// This subprocess intentionally bypasses every defer and Close after durable
// acceptance and one externally observable fake effect. It is not a user daemon.
func TestV2CrashProcess(t *testing.T) {
	home := os.Getenv(crashFixtureEnv)
	if home == "" {
		t.Skip("subprocess fixture")
	}
	store := openStore(t, filepath.Join(home, "sessions.db"))
	rootID := createRoot(t, store)
	started := make(chan error, 1)
	runner := &fakeRunner{turn: func(context.Context, string, bool) (string, error) {
		started <- appendCrashEffect(home)
		select {}
	}}
	fixture := serveCrashFixture(t, home, store, rootID, runner)
	client := fixture.dial(os.Getenv("WHIP_V2_CRASH_TRANSPORT"), "crash-client")
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	receipt, err := client.Submit(ctx, crashCommand(rootID))
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Status != "queued" && receipt.Status != "running" {
		t.Fatalf("unexpected acceptance: %+v", receipt)
	}
	select {
	case err := <-started:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("fake external effect did not execute")
	}
	record, err := store.LoadCommand(ctx, "crash-client", "crash-command")
	if err != nil || record.Status != "running" {
		t.Fatalf("command was not durably running: %+v %v", record, err)
	}
	data, err := json.Marshal(crashReceipt{RootID: rootID, RuntimeID: client.InitializeResult().RuntimeID, Receipt: receipt})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "receipt.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	os.Exit(crashFixtureExit)
}

func TestV2CrashAfterAcceptanceRecoversAcrossTransports(t *testing.T) {
	for _, transport := range []string{"unix", "websocket"} {
		t.Run(transport, func(t *testing.T) {
			home := t.TempDir()
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()
			process := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestV2CrashProcess$")
			process.Env = append(os.Environ(), crashFixtureEnv+"="+home, "WHIP_V2_CRASH_TRANSPORT="+transport)
			output, err := process.CombinedOutput()
			var exited *exec.ExitError
			if !errors.As(err, &exited) || exited.ExitCode() != crashFixtureExit {
				t.Fatalf("crash fixture failed: %v\n%s", err, output)
			}
			wal, err := os.Stat(filepath.Join(home, "sessions.db-wal"))
			if err != nil || wal.Size() == 0 {
				t.Fatalf("crash did not leave committed WAL: %v", err)
			}
			data, err := os.ReadFile(filepath.Join(home, "receipt.json"))
			if err != nil {
				t.Fatal(err)
			}
			var accepted crashReceipt
			if err := json.Unmarshal(data, &accepted); err != nil {
				t.Fatal(err)
			}
			store, err := session.Open(filepath.Join(home, "sessions.db"))
			if err != nil {
				t.Fatal(err)
			}
			runner := &fakeRunner{turn: func(context.Context, string, bool) (string, error) { return "repeated", appendCrashEffect(home) }}
			fixture := serveCrashFixture(t, home, store, accepted.RootID, runner)
			for _, reconnectTransport := range []string{"unix", "websocket"} {
				client := fixture.dial(reconnectTransport, "crash-client")
				if client.InitializeResult().RuntimeID != accepted.RuntimeID {
					t.Fatal("runtime identity changed after crash")
				}
				status, err := client.CommandStatus(ctx, "crash-command")
				if err != nil || status.Status != "interrupted" || status.IngressSeq != accepted.Receipt.IngressSeq || status.Failure == nil {
					t.Fatalf("recovered status=%+v error=%v", status, err)
				}
				retry, err := client.Submit(ctx, crashCommand(accepted.RootID))
				if err != nil || retry.Status != "interrupted" || retry.IngressSeq != status.IngressSeq {
					t.Fatalf("retry=%+v error=%v", retry, err)
				}
				if _, err := client.Snapshot(ctx, accepted.RootID); err != nil {
					t.Fatal(err)
				}
			}
			if runner.calls.Load() != 0 {
				t.Fatalf("uncertain effect reexecuted %d times", runner.calls.Load())
			}
			effect, err := os.ReadFile(filepath.Join(home, "effect.log"))
			if err != nil || string(effect) != "effect\n" {
				t.Fatalf("external effect repeated or missing: %q %v", effect, err)
			}
		})
	}
}

func crashCommand(rootID string) CommandParams {
	return CommandParams{CommandID: "crash-command", Scope: "root", RootID: rootID, Operation: "submit", Payload: json.RawMessage(`{"text":"perform one effect"}`)}
}

func appendCrashEffect(home string) error {
	file, err := os.OpenFile(filepath.Join(home, "effect.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	_, writeErr := file.WriteString("effect\n")
	syncErr := file.Sync()
	return errors.Join(writeErr, syncErr, file.Close())
}

func serveCrashFixture(t *testing.T, home string, store *session.Store, rootID string, runner Runner) v2Fixture {
	t.Helper()
	paths, err := Paths(home)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := New(store, func(context.Context, session.Meta, []llm.Message) (Components, error) {
		return Components{Runner: runner}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	server, err := NewServer(owner, ServerOptions{RuntimeDir: paths.Runtime, Network: NetworkOptions{Enabled: true}})
	if err != nil {
		t.Fatal(err)
	}
	listener, err := listenLocal(paths)
	if err != nil {
		t.Fatal(err)
	}
	served := make(chan error, 1)
	go func() { served <- server.Serve(listener) }()
	t.Cleanup(func() {
		if err := server.Close(); err != nil {
			t.Error(err)
		}
		if err := <-served; err != nil {
			t.Error(err)
		}
	})
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	initial, err := DialClient(ctx, paths, InitializeParams{ProtocolMajor: ProtocolMajor, ClientID: "probe", ClientKind: "test"})
	if err != nil {
		t.Fatal(err)
	}
	endpoint := "ws" + strings.TrimPrefix(initial.InitializeResult().NetworkEndpoint, "http") + "/api/v3/ws"
	_ = initial.Close()
	return v2Fixture{store: store, rootID: rootID, endpoint: endpoint, dial: func(transport, clientID string) *Client {
		t.Helper()
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		defer cancel()
		params := InitializeParams{ProtocolMajor: ProtocolMajor, ClientID: clientID, ClientKind: "test"}
		var client *Client
		var err error
		if transport == "unix" {
			client, err = DialClient(ctx, paths, params)
		} else {
			client, err = DialWebSocketClient(ctx, endpoint, params)
		}
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = client.Close() })
		return client
	}}
}
