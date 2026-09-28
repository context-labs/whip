package daemon

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/legacy/protocol"
	"github.com/context-labs/whip/internal/legacy/session"
	"github.com/context-labs/whip/internal/llm"
)

func titleNotificationServer(t *testing.T, options ServerOptions) (*Daemon, *Server) {
	t.Helper()
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	owner, err := New(store, func(context.Context, session.Meta, []llm.Message) (Components, error) {
		return Components{Runner: &fakeRunner{}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	server, err := NewServer(owner, options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	return owner, server
}

func readTitleMessage(t *testing.T, transport messageTransport) rpcMessage {
	t.Helper()
	_ = transport.SetReadDeadline(time.Now().Add(5 * time.Second))
	frame, err := transport.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	message, err := decodeFrame(frame)
	if err != nil {
		t.Fatal(err)
	}
	return message
}

func titlePing(t *testing.T, transport messageTransport) {
	t.Helper()
	if err := writeTransportMessage(transport, rpcMessage{ID: json.RawMessage("2"), Method: "daemon.ping", Params: json.RawMessage("{}")}); err != nil {
		t.Fatal(err)
	}
	message := readTitleMessage(t, transport)
	if string(message.ID) != "2" || message.Error != nil {
		t.Fatalf("expected ping, not a notification: %+v", message)
	}
}

func connectTitleClient(t *testing.T, server *Server, id string, capabilities []string) messageTransport {
	t.Helper()
	// A short temporary directory keeps Unix socket paths below macOS's limit.
	directory, err := os.MkdirTemp("", "whip-title-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(directory) })
	listener, err := net.Listen("unix", filepath.Join(directory, "socket"))
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	clientSide, err := net.Dial("unix", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	serverSide, err := listener.Accept()
	if err != nil {
		_ = clientSide.Close()
		t.Fatal(err)
	}
	transport := newUnixMessageTransport(clientSide)
	t.Cleanup(func() { _ = transport.Close() })
	if !server.goWorker(func() { server.serveConn(serverSide) }) {
		t.Fatal("server closed")
	}
	params, _ := json.Marshal(InitializeParams{
		ProtocolMajor: ProtocolMajor, ClientID: id, ClientKind: "sdk", Capabilities: capabilities,
	})
	if err := writeTransportMessage(transport, rpcMessage{ID: json.RawMessage("1"), Method: "initialize", Params: params}); err != nil {
		t.Fatal(err)
	}
	message := readTitleMessage(t, transport)
	var result InitializeResult
	raw, _ := json.Marshal(message.Result)
	if err := json.Unmarshal(raw, &result); err != nil || message.Error != nil {
		t.Fatalf("initialize: %+v %v", message, err)
	}
	want := slices.Contains(capabilities, protocol.SessionTitleNotificationsCapability)
	if !slices.Contains(result.Capabilities, protocol.SessionTitleNotificationsCapability) ||
		slices.Contains(result.NegotiatedCapabilities, protocol.SessionTitleNotificationsCapability) != want {
		t.Fatalf("capability negotiation: %+v", result)
	}
	// This barrier also proves eligibility is set before any normal request runs.
	titlePing(t, transport)
	return transport
}

func assertTitleMessage(t *testing.T, transport messageTransport, rootID string) {
	t.Helper()
	message := readTitleMessage(t, transport)
	var params map[string]string
	if err := json.Unmarshal(message.Params, &params); err != nil || message.Method != "sessions.title.changed" ||
		len(message.ID) != 0 || len(params) != 1 || params["root_id"] != rootID {
		t.Fatalf("title invalidation: %+v %v", message, err)
	}
}

func TestTitleNotificationsReachCatalogClientsWithoutSubscriptions(t *testing.T) {
	owner, server := titleNotificationServer(t, ServerOptions{})
	capabilities := []string{protocol.SessionTitleNotificationsCapability}
	first := connectTitleClient(t, server, "first", capabilities)
	second := connectTitleClient(t, server, "second", append(capabilities, protocol.NetworkClientCapability))
	legacy := connectTitleClient(t, server, "legacy", nil)
	rootID := createRoot(t, owner.store)
	root, err := owner.Open(rootID)
	if err != nil {
		t.Fatal(err)
	}
	if result := clientCommand(t, root, "first", "rename", "session.rename", protocol.TitleParams{Title: "Private title"}); result.Status != "succeeded" {
		t.Fatalf("rename: %+v", result)
	}
	assertTitleMessage(t, first, rootID)
	assertTitleMessage(t, second, rootID)
	for _, transport := range []messageTransport{first, second} {
		params, _ := json.Marshal(protocol.RootParams{RootID: rootID})
		if err := writeTransportMessage(transport, rpcMessage{ID: json.RawMessage("3"), Method: "sessions.get", Params: params}); err != nil {
			t.Fatal(err)
		}
		message := readTitleMessage(t, transport)
		var metadata session.SessionMetadata
		raw, _ := json.Marshal(message.Result)
		if err := json.Unmarshal(raw, &metadata); err != nil || message.Error != nil || metadata.Title != "Private title" {
			t.Fatalf("notification read before commit: %+v %v", message, err)
		}
	}
	titlePing(t, legacy)
	server.mu.Lock()
	defer server.mu.Unlock()
	for connection := range server.clients {
		if len(connection.subscriptions) != 0 {
			t.Fatal("title delivery opened a root subscription")
		}
	}
}

type titleInitializeTransport struct {
	messageTransport
	started chan struct{}
	release chan struct{}
	fail    bool
	once    sync.Once
}

func (t *titleInitializeTransport) WriteMessage(frame []byte) error {
	failed := false
	t.once.Do(func() {
		close(t.started)
		<-t.release
		failed = t.fail
	})
	if failed {
		return errors.New("initialize write failed")
	}
	return t.messageTransport.WriteMessage(frame)
}

func TestTitleNotificationsWaitForSuccessfulInitializeWrite(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "blocked", true: "failed"}[fail], func(t *testing.T) {
			owner, server := titleNotificationServer(t, ServerOptions{})
			serverSide, clientSide := net.Pipe()
			transport := newUnixMessageTransport(clientSide)
			t.Cleanup(func() { _ = transport.Close() })
			blocked := &titleInitializeTransport{
				messageTransport: newUnixMessageTransport(serverSide),
				started:          make(chan struct{}), release: make(chan struct{}), fail: fail,
			}
			served := make(chan struct{})
			server.goWorker(func() { defer close(served); server.serveTransport(blocked, false) })
			params, _ := json.Marshal(InitializeParams{
				ProtocolMajor: ProtocolMajor, ClientKind: "sdk", ClientID: "initializing",
				Capabilities: []string{protocol.SessionTitleNotificationsCapability},
			})
			if err := writeTransportMessage(transport, rpcMessage{ID: json.RawMessage("1"), Method: "initialize", Params: params}); err != nil {
				t.Fatal(err)
			}
			waitTitleLifecycle(t, blocked.started)
			owner.notifyTitleChanged("before-initialize")
			server.mu.Lock()
			for connection := range server.clients {
				if connection.titleNotifications || len(connection.out) != 0 {
					t.Error("notification queued before initialize completed")
				}
			}
			server.mu.Unlock()
			close(blocked.release)
			if fail {
				waitTitleLifecycle(t, served)
				server.mu.Lock()
				defer server.mu.Unlock()
				if len(server.clients) != 0 {
					t.Fatal("failed initialize left a registered client")
				}
				return
			}
			if message := readTitleMessage(t, transport); string(message.ID) != "1" {
				t.Fatalf("initialize response: %+v", message)
			}
			titlePing(t, transport)
			owner.notifyTitleChanged("after-initialize")
			assertTitleMessage(t, transport, "after-initialize")
		})
	}
}

func TestTitleNotificationsBoundSlowClientsAndUnregisterOnClose(t *testing.T) {
	owner, server := titleNotificationServer(t, ServerOptions{MaxOutbound: 1})
	_ = connectTitleClient(t, server, "slow", []string{protocol.SessionTitleNotificationsCapability})
	server.mu.Lock()
	var slow *serverConn
	for connection := range server.clients {
		slow = connection
	}
	server.mu.Unlock()
	rootID := createRoot(t, owner.store)
	root, err := owner.Open(rootID)
	if err != nil {
		t.Fatal(err)
	}
	// Force overflow from the root actor, not just an unrelated broadcaster.
	// Saturate byte accounting so the socket buffer cannot hide overflow.
	slow.mu.Lock()
	slow.outBytes = server.options.MaxOutboundBytes
	slow.mu.Unlock()
	published := make(chan struct{})
	go func() {
		defer close(published)
		if err := root.routeControl(t.Context(), func(ctx context.Context) error {
			if err := root.store.SetTitle(rootID, "Slow title"); err != nil {
				return err
			}
			return root.publishTitle(ctx, "Slow title")
		}); err != nil {
			t.Error(err)
		}
	}()
	waitTitleLifecycle(t, published)
	waitTitleLifecycle(t, slow.done)
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		for range 1000 {
			owner.notifyTitleChanged("shutdown-race")
		}
	}()
	closed := make(chan struct{})
	go func() { defer close(closed); _ = server.Close() }()
	waitTitleLifecycle(t, closed)
	waitTitleLifecycle(t, finished)
	owner.titleMu.Lock()
	defer owner.titleMu.Unlock()
	if len(owner.titleListeners) != 0 {
		t.Fatal("server retained a title listener after close")
	}
}

// Verify committed data and the registry are available before every callback.
func observeTitleChanges(t *testing.T, owner *Daemon) <-chan string {
	t.Helper()
	changes := make(chan string, 32)
	unsubscribe := owner.listenTitleChanges(func(rootID string) {
		if !owner.mu.TryLock() {
			t.Error("title notification holds root registry mutex")
		} else {
			owner.mu.Unlock()
		}
		if meta, err := owner.store.SessionMetadata(t.Context(), rootID); err != nil || meta.Title == "" {
			t.Errorf("notification preceded title commit: %+v %v", meta, err)
		}
		changes <- rootID
	})
	t.Cleanup(unsubscribe)
	return changes
}

func assertTitleChanges(t *testing.T, changes <-chan string, rootIDs ...string) {
	t.Helper()
	for _, rootID := range rootIDs {
		select {
		case got := <-changes:
			if got != rootID {
				t.Fatalf("title change = %q, want %q", got, rootID)
			}
		default:
			t.Fatalf("missing title change for %q", rootID)
		}
	}
	select {
	case extra := <-changes:
		t.Fatalf("unexpected title change: %q", extra)
	default:
	}
}

func TestTitleNotificationsCommittedWritePaths(t *testing.T) {
	for _, test := range []struct {
		name, prompt string
		generated    bool
		optOut       bool
	}{
		{name: "short", prompt: "Short"},
		{name: "definition opt-out", prompt: "A long prompt with automatic generation disabled", optOut: true},
		{name: "generated", prompt: "A long first prompt requiring a generated title", generated: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
			rootID := createRoot(t, store)
			runner := &lifecycleTitleRunner{fakeRunner: &fakeRunner{}, generate: func(context.Context, string) (string, llm.Usage, error) {
				return "Generated title", llm.Usage{}, nil
			}}
			owner, root := openTitleLifecycle(t, store, rootID, runner)
			if test.optOut {
				if err := root.routeControl(t.Context(), func(context.Context) error {
					root.definition.Surface.AutoTitle = false
					return nil
				}); err != nil {
					t.Fatal(err)
				}
			}
			changes := observeTitleChanges(t, owner)
			assertTitleChanges(t, changes) // Ordinary empty creation/open never emits.
			receipt, err := root.Submit(t.Context(), test.prompt)
			if err != nil {
				t.Fatal(err)
			}
			if result := waitReceipt(t, receipt); result.Err != nil {
				t.Fatal(result.Err)
			}
			settleTitleLifecycle(t, root)
			if test.generated {
				assertTitleChanges(t, changes, rootID, rootID)
			} else {
				assertTitleChanges(t, changes, rootID)
			}
			for _, title := range []string{"Manual title", "Manual title"} {
				result := clientCommand(t, root, "client", "rename-"+title, "session.rename", protocol.TitleParams{Title: title})
				if result.Status != "succeeded" {
					t.Fatalf("rename: %+v", result)
				}
			}
			assertTitleChanges(t, changes, rootID) // Command replay does not repeat the hint.
			for _, title := range []string{"", "Custom fork"} {
				result := clientCommand(t, root, "client", "fork-"+title, "session.fork", protocol.ForkParams{Title: title})
				var fork protocol.RootIDResult
				if err := json.Unmarshal(result.Result, &fork); err != nil || result.Status != "succeeded" {
					t.Fatalf("fork: %+v %v", result, err)
				}
				assertTitleChanges(t, changes, fork.RootID)
				_ = clientCommand(t, root, "client", "fork-"+title, "session.fork", protocol.ForkParams{Title: title})
				assertTitleChanges(t, changes)
			}
		})
	}
}

func TestTitleNotificationsGeneratedResultRequiresCommittedWrite(t *testing.T) {
	for _, scenario := range []string{"generated", "stale", "deleted", "generation failure", "write failure", "event failure"} {
		t.Run(scenario, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "sessions.db")
			store := openStore(t, path)
			rootID := createRoot(t, store)
			if err := store.SetTitle(rootID, "Placeholder"); err != nil {
				t.Fatal(err)
			}
			owner, root := openTitleLifecycle(t, store, rootID, &fakeRunner{})
			changes := observeTitleChanges(t, owner)
			if scenario == "write failure" {
				titleFailureDB(t, path, `CREATE TRIGGER fail_title BEFORE UPDATE OF title ON sessions
					BEGIN SELECT RAISE(ABORT,'title write failed'); END`)
			} else if scenario == "event failure" {
				titleFailureDB(t, path, `CREATE TRIGGER fail_title BEFORE INSERT ON events
					WHEN NEW.kind='session.title.updated' BEGIN SELECT RAISE(ABORT,'title event failed'); END`)
			}
			if err := root.routeControl(t.Context(), func(context.Context) error {
				work := &titleWork{cancel: func() {}, placeholder: "Placeholder", title: "Generated title"}
				root.titleWork = work
				switch scenario {
				case "stale":
					if err := store.SetTitle(rootID, "Manual title"); err != nil {
						return err
					}
				case "deleted":
					if err := store.DeleteSession(t.Context(), rootID); err != nil {
						return err
					}
				case "generation failure":
					work.err = errors.New("generation failed")
				}
				root.completeTitle(work)
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if scenario == "generated" || scenario == "event failure" {
				assertTitleChanges(t, changes, rootID)
				assertTitleLifecycle(t, store, rootID, "Generated title")
			} else {
				assertTitleChanges(t, changes)
			}
		})
	}
}

func TestTitleNotificationsLiveWriteAndEventFailures(t *testing.T) {
	for _, operation := range []string{"initial", "session.rename", "session.fork"} {
		for _, failure := range []string{"write", "event"} {
			if operation == "session.fork" && failure == "event" {
				continue // Fork creates a root without publishing session.title.updated.
			}
			t.Run(operation+"/"+failure, func(t *testing.T) {
				path := filepath.Join(t.TempDir(), "sessions.db")
				store := openStore(t, path)
				rootID := createRoot(t, store)
				owner, root := openTitleLifecycle(t, store, rootID, &fakeRunner{})
				changes := observeTitleChanges(t, owner)
				if failure == "event" {
					titleFailureDB(t, path, `CREATE TRIGGER fail_title BEFORE INSERT ON events
						WHEN NEW.kind='session.title.updated' BEGIN SELECT RAISE(ABORT,'title event failed'); END`)
				} else if operation == "session.fork" {
					titleFailureDB(t, path, `CREATE TRIGGER fail_title BEFORE INSERT ON sessions
						BEGIN SELECT RAISE(ABORT,'fork failed'); END`)
				} else {
					titleFailureDB(t, path, `CREATE TRIGGER fail_title BEFORE UPDATE OF title ON sessions
						BEGIN SELECT RAISE(ABORT,'title write failed'); END`)
				}
				if operation == "initial" {
					receipt, err := root.Submit(t.Context(), "Short title")
					if (err != nil) != (failure == "write") {
						t.Fatalf("initial title error: %v", err)
					}
					if err == nil {
						if completion := waitReceipt(t, receipt); completion.Err != nil {
							t.Fatal(completion.Err)
						}
					}
				} else {
					result := clientCommand(t, root, "client", "failed-command", operation, protocol.TitleParams{Title: "Manual title"})
					if result.Status != "failed" {
						t.Fatalf("expected failed command: %+v", result)
					}
				}
				if failure == "event" {
					assertTitleChanges(t, changes, rootID)
				} else {
					assertTitleChanges(t, changes)
				}
			})
		}
	}
}

func titleFailureDB(t *testing.T, path, statement string) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.ExecContext(t.Context(), statement); err != nil {
		t.Fatal(err)
	}
}

func TestTitleNotificationsColdRenameCommitAndFailure(t *testing.T) {
	for _, failure := range []string{"none", "event", "write"} {
		t.Run(failure, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "sessions.db")
			store := openStore(t, path)
			rootID := createRoot(t, store)
			owner, err := New(store, func(context.Context, session.Meta, []llm.Message) (Components, error) {
				t.Error("cold rename reconstructed a root")
				return Components{}, errors.New("unavailable workspace")
			})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = owner.Close() })
			changes := observeTitleChanges(t, owner)
			if failure == "event" {
				titleFailureDB(t, path, `CREATE TRIGGER fail_title BEFORE INSERT ON events
					WHEN NEW.kind='session.title.updated' BEGIN SELECT RAISE(ABORT,'title event failed'); END`)
			} else if failure == "write" {
				titleFailureDB(t, path, `CREATE TRIGGER fail_title BEFORE UPDATE OF title ON sessions
					BEGIN SELECT RAISE(ABORT,'title write failed'); END`)
			}
			payload := json.RawMessage(`{"title":"Cold title"}`)
			admission := session.CommandAdmission{
				ClientID: "cold", CommandID: "rename", RequestDigest: "rename", Payload: session.RuntimePayload{Data: payload},
			}
			record, err := owner.control.SessionMetadataCommand(t.Context(), admission, "session.rename", payload, rootID)
			if (err == nil) != (failure == "none") {
				t.Fatalf("rename error: %v", err)
			}
			if (record.Status == "succeeded") != (failure == "none") {
				t.Fatalf("command status: %+v", record)
			}
			if failure == "write" {
				assertTitleChanges(t, changes)
			} else {
				assertTitleChanges(t, changes, rootID)
				assertTitleLifecycle(t, store, rootID, "Cold title")
			}
			_, _ = owner.control.SessionMetadataCommand(t.Context(), admission, "session.rename", payload, rootID)
			assertTitleChanges(t, changes)
		})
	}
}
