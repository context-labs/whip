//go:build integration

package tui

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/daemon"
	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/session"
)

// Exercise the TUI's actual startup requests against the daemon's validation,
// handlers, persistence and subscriptions, without provider credentials.
func TestInteractiveSessionOverTrustedProtocol(t *testing.T) {
	for _, transport := range []string{"unix", "websocket"} {
		t.Run(transport, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("WHIP_HOME", home)
			cfg := config.Default()
			cfg.DefaultModel, cfg.DefaultProvider = "test-model", "test-provider"
			cfg.Models["test-model"] = config.Model{Providers: []string{"test-provider"}}
			cfg.Providers["test-provider"] = config.Provider{BaseURL: "http://localhost:1"}
			if err := cfg.Save(); err != nil {
				t.Fatal(err)
			}
			paths, err := daemon.Paths(home)
			if err != nil {
				t.Fatal(err)
			}
			if paths.Runtime != paths.Home {
				t.Cleanup(func() { _ = os.RemoveAll(paths.Runtime) })
			}
			store, err := session.Open(filepath.Join(paths.Home, "sessions.db"))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = store.Close() })
			owner, err := daemon.New(store, func(context.Context, session.Meta, []llm.Message) (daemon.Components, error) {
				return daemon.Components{Runner: &startupRunner{}}, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = owner.Close() })
			server, err := daemon.NewServer(owner, daemon.ServerOptions{RuntimeDir: paths.Runtime, Network: daemon.NetworkOptions{Enabled: true}})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = server.Close() })
			listener, err := net.Listen("unix", paths.Socket)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = listener.Close() })
			if err := os.Chmod(paths.Socket, 0o600); err != nil {
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
			initialize := daemon.InitializeParams{ProtocolMajor: daemon.ProtocolMajor, ClientID: "tui-startup", ClientKind: "tui"}
			probe, err := daemon.DialClient(ctx, paths, initialize)
			if err != nil {
				t.Fatal(err)
			}
			endpoint := "ws" + strings.TrimPrefix(probe.InitializeResult().NetworkEndpoint, "http") + "/api/v3/ws"
			_ = probe.Close()
			client, err := NewClient(ClientOptions{
				ClientID: initialize.ClientID,
				Create:   &daemon.CreateSession{Kind: session.SessionKindAgent, CWD: home},
				Connector: func(ctx context.Context, cursors map[string]int64) (daemonConnection, error) {
					params := initialize
					params.Cursors = cursors
					if transport == "websocket" {
						return daemon.DialWebSocketClient(ctx, endpoint, params)
					}
					return daemon.DialClient(ctx, paths, params)
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			client.Start()
			t.Cleanup(func() { _ = client.Close() })
			if err := configureInteractiveSession(ctx, client, false, false); err != nil {
				t.Fatal(err)
			}
			m := &model{client: client, clientState: ClientLive, sessionID: client.RootID()}
			for _, operation := range []string{"provider.catalogs", "history.user.list"} {
				_, command := m.submitClientAction(operation, protocol.EmptyParams{}, "")
				message := clientCommandFrom(t, command)
				if message.err != nil || message.result.Status != "succeeded" {
					t.Fatalf("startup %s: %+v", operation, message)
				}
			}
			if _, ok := m.requestHostSkills()().(clientSkillsMsg); !ok {
				t.Fatal("startup skill discovery failed")
			}
			_, command := m.submitClientAction("submit", protocol.SubmitPayload{Text: "Investigate workers"}, "")
			message := clientCommandFrom(t, command)
			if message.err != nil || message.result.Status != "succeeded" {
				t.Fatalf("first prompt: %+v", message)
			}
			for {
				select {
				case update := <-client.Updates():
					if update.Err != nil {
						t.Fatal(update.Err)
					}
					if update.Event == nil || update.Event.Kind != "session.title.updated" {
						continue
					}
					snapshot, err := client.Snapshot(ctx)
					if err != nil {
						t.Fatal(err)
					}
					if snapshot.Meta.Model != cfg.DefaultModel || snapshot.Meta.Provider != cfg.DefaultProvider || snapshot.Meta.Title != "Worker Investigation" || len(snapshot.Messages) != 2 {
						t.Fatalf("first turn snapshot: %+v", snapshot)
					}
					return
				case <-ctx.Done():
					t.Fatal("automatic title update not delivered:", ctx.Err())
				}
			}
		})
	}
}

type startupRunner struct {
	mu      sync.Mutex
	history []llm.Message
}

func (r *startupRunner) Turn(_ context.Context, input string, authored bool, started func(), _ func(string)) (string, error) {
	if started != nil {
		started()
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.history = append(r.history, llm.Message{Role: "user", Content: input, Authored: authored}, llm.Message{Role: "assistant", Content: "Done"})
	return "Done", nil
}

func (r *startupRunner) History() []llm.Message {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]llm.Message(nil), r.history...)
}

func (*startupRunner) Close() {}

func (*startupRunner) GenerateTitle(context.Context) (string, llm.Usage, error) {
	return "Worker Investigation", llm.Usage{}, nil
}
