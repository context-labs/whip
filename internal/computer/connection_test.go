package computer

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/capability"
)

const ownedFakeHelper = `package main
import("bufio";"encoding/json";"fmt";"os";"strings";"time")
func main(){
 if os.Getenv("WHIP_TEST_MODE")=="bad-version"{fmt.Println("incompatible");time.Sleep(time.Hour);return}
 fmt.Println("whip-computer/1")
 scanner:=bufio.NewScanner(os.Stdin);scanner.Buffer(make([]byte,4096),1<<20)
 encoder:=json.NewEncoder(os.Stdout)
 for scanner.Scan(){
  var request struct{ID int64 ` + "`json:\"id\"`" + `;Method string ` + "`json:\"method\"`" + `;Params map[string]json.RawMessage ` + "`json:\"params\"`" + `}
  if json.Unmarshal(scanner.Bytes(),&request)!=nil{os.Exit(2)}
  var token string;_=json.Unmarshal(request.Params["token"],&token)
  if token==""||token!=os.Getenv("WHIP_COMPUTER_TOKEN"){os.Exit(3)}
  if request.Method=="handshake"{_ = encoder.Encode(map[string]any{"jsonrpc":"2.0","id":request.ID,"result":map[string]any{"version":"whip-computer/1"}});if os.Getenv("WHIP_TEST_MODE")=="block-write"{time.Sleep(time.Hour)};continue}
  if path:=os.Getenv("WHIP_TEST_RECORD");path!=""{f,err:=os.OpenFile(path,os.O_WRONLY|os.O_CREATE|os.O_APPEND,0600);if err!=nil{os.Exit(4)};_,_=fmt.Fprintln(f,request.Method);_=f.Close()}
  switch request.Method{
  case "crash":os.Exit(1)
  case "block":time.Sleep(time.Hour)
  case "bad-id":_ = encoder.Encode(map[string]any{"jsonrpc":"2.0","id":request.ID+1,"result":true});continue
  case "bad-json":fmt.Println("private-token-"+token);continue
  case "trailing":fmt.Printf("{\"jsonrpc\":\"2.0\",\"id\":%d,\"result\":null}{}\n",request.ID);continue
  case "oversized":fmt.Println(strings.Repeat("x",(16<<20)+1));continue
  case "stale":_ = encoder.Encode(map[string]any{"jsonrpc":"2.0","id":request.ID,"error":map[string]any{"code":4,"message":token}});continue
  case "secret-env":_ = encoder.Encode(map[string]any{"jsonrpc":"2.0","id":request.ID,"result":os.Getenv("OPENAI_API_KEY")});continue
  }
  delete(request.Params,"token")
  _ = encoder.Encode(map[string]any{"jsonrpc":"2.0","id":request.ID,"result":request.Params})
 }
}
`

func ownedHelperBinary(t *testing.T) string {
	t.Helper()
	directory := t.TempDir()
	source := filepath.Join(directory, "main.go")
	binary := filepath.Join(directory, "helper")
	if err := os.WriteFile(source, []byte(ownedFakeHelper), 0o600); err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(t.Context(), "go", "build", "-o", binary, source)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("fake helper: %v\n%s", err, output)
	}
	return binary
}

func ownedConnection(t *testing.T, binary string, environment map[string]string) *Connection {
	t.Helper()
	manager := capability.NewProcessManager()
	t.Cleanup(func() {
		if err := manager.Close(); err != nil {
			t.Error(err)
		}
	})
	connection, err := OpenConnection(t.Context(), ConnectionOptions{Processes: manager, Owner: "root", Executable: binary, Directory: t.TempDir(), Environment: environment})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(connection.Close)
	return connection
}

func waitOwned(t *testing.T, condition func() bool) {
	t.Helper()
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for !condition() {
		select {
		case <-ticker.C:
		case <-timer.C:
			t.Fatal("helper condition timed out")
		}
	}
}

func TestOwnedHelperExactHandshakeEnvironmentAndKnownFailure(t *testing.T) {
	binary := ownedHelperBinary(t)
	t.Setenv("OPENAI_API_KEY", "must-not-inherit")
	t.Setenv("WHIP_COMPUTER_BIN", filepath.Join(t.TempDir(), "not-the-helper"))
	connection := ownedConnection(t, binary, nil)
	arguments := json.RawMessage(`{"number":9007199254740993,"zero":0,"absent":null}`)
	result, err := connection.Call(t.Context(), "echo", arguments)
	if err != nil || string(result) != `{"absent":null,"number":9007199254740993,"zero":0}` {
		t.Fatal("exact value lost", string(result), err)
	}
	if strings.Contains(string(arguments), "token") {
		t.Fatal("mutated caller bytes")
	}
	result, err = connection.Call(t.Context(), "secret-env", json.RawMessage(`{}`))
	if err != nil || string(result) != `""` {
		t.Fatal("inherited secret environment", err)
	}
	_, err = connection.Call(t.Context(), "stale", json.RawMessage(`{}`))
	var helperError *HelperError
	if !errors.As(err, &helperError) || helperError.Code != 4 || strings.Contains(err.Error(), connection.token) {
		t.Fatal("unsafe helper error", err)
	}
	if connection.Lifetime().Err() != nil {
		t.Fatal("completed helper rejection killed connection")
	}
	if _, err := connection.Call(t.Context(), "echo", json.RawMessage(`{}`)); err != nil {
		t.Fatal(err)
	}
	for _, arguments := range []json.RawMessage{json.RawMessage(`{"token":"supplied"}`), json.RawMessage(`null`), json.RawMessage(`[]`), json.RawMessage(`{"data":"` + strings.Repeat("x", MaxConnectionRequestBytes) + `"}`)} {
		if _, err := connection.Call(t.Context(), "echo", arguments); err == nil {
			t.Fatal("invalid request accepted")
		}
	}
	if connection.Lifetime().Err() != nil {
		t.Fatal("preflight validation retired connection")
	}
}

func TestOwnedHelperTransportLossNeverReplaysOrRestarts(t *testing.T) {
	binary := ownedHelperBinary(t)
	for _, method := range []string{"crash", "bad-id", "bad-json", "trailing", "oversized"} {
		t.Run(method, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "calls")
			connection := ownedConnection(t, binary, map[string]string{"WHIP_TEST_RECORD": path})
			_, err := connection.Call(t.Context(), method, json.RawMessage(`{}`))
			if err == nil {
				t.Fatal("invalid response accepted")
			}
			if strings.Contains(err.Error(), connection.token) {
				t.Fatal("helper token leaked")
			}
			if connection.Lifetime().Err() == nil {
				t.Fatal("failed generation remains valid")
			}
			if _, err := connection.Call(t.Context(), "echo", json.RawMessage(`{}`)); !errors.Is(err, ErrConnectionClosed) {
				t.Fatal("closed generation accepted new work", err)
			}
			calls, err := os.ReadFile(path)
			if err != nil || string(calls) != method+"\n" {
				t.Fatal("uncertain effect replayed", string(calls), err)
			}
			replacement := ownedConnection(t, binary, nil)
			if replacement.Epoch() == connection.Epoch() {
				t.Fatal("explicit replacement reused epoch")
			}
		})
	}
}

type observedHelperWrite struct {
	io.WriteCloser
	started chan struct{}
	once    sync.Once
}

func (w *observedHelperWrite) Write(value []byte) (int, error) {
	w.once.Do(func() { close(w.started) })
	return w.WriteCloser.Write(value)
}

func TestOwnedHelperCancelledIOAndBoundedWaitersJoin(t *testing.T) {
	binary := ownedHelperBinary(t)
	for _, mode := range []string{"read", "block-write"} {
		t.Run(mode, func(t *testing.T) {
			connection := ownedConnection(t, binary, map[string]string{"WHIP_TEST_MODE": mode})
			writer := &observedHelperWrite{WriteCloser: connection.stdin, started: make(chan struct{})}
			connection.stdin = writer
			active, cancel := context.WithCancel(t.Context())
			defer cancel()
			result := make(chan error, 1)
			method, arguments := "block", json.RawMessage(`{}`)
			if mode == "block-write" {
				method = "echo"
				arguments = json.RawMessage(`{"data":"` + strings.Repeat("x", 240<<10) + `"}`)
			}
			go func() { _, err := connection.Call(active, method, arguments); result <- err }()
			select {
			case <-writer.started:
			case <-time.After(5 * time.Second):
				t.Fatal("helper I/O did not start")
			}
			queued, cancelQueued := context.WithCancel(t.Context())
			queuedResults := make(chan error, MaxConnectionWaiters)
			for range MaxConnectionWaiters {
				go func() { _, err := connection.Call(queued, "echo", json.RawMessage(`{}`)); queuedResults <- err }()
			}
			waitOwned(t, func() bool { return len(connection.waiters) == MaxConnectionWaiters })
			if _, err := connection.Call(t.Context(), "echo", json.RawMessage(`{}`)); !errors.Is(err, ErrConnectionCapacity) {
				t.Fatal("unbounded wait queue", err)
			}
			cancelQueued()
			for range MaxConnectionWaiters {
				if err := <-queuedResults; !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
			}
			if connection.Lifetime().Err() != nil {
				t.Fatal("cancelled queued call retired active generation")
			}
			cancel()
			select {
			case err := <-result:
				if !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("cancel did not join helper IO")
			}
			if connection.Lifetime().Err() == nil {
				t.Fatal("active cancellation did not revoke generation")
			}
			connection.Close()
		})
	}
}

func TestOwnedHelperOpenRequiresExplicitPathAndMatchingVersion(t *testing.T) {
	binary := ownedHelperBinary(t)
	manager := capability.NewProcessManager()
	defer func() { _ = manager.Close() }()
	options := ConnectionOptions{Processes: manager, Owner: "root", Executable: binary, Directory: t.TempDir()}
	for _, mutate := range []func(*ConnectionOptions){func(o *ConnectionOptions) { o.Processes = nil }, func(o *ConnectionOptions) { o.Owner = "" }, func(o *ConnectionOptions) { o.Executable = "helper" }, func(o *ConnectionOptions) { o.Directory = "." }, func(o *ConnectionOptions) { o.Environment = map[string]string{tokenEnvVar: "injected"} }} {
		invalid := options
		mutate(&invalid)
		if connection, err := OpenConnection(t.Context(), invalid); err == nil {
			connection.Close()
			t.Fatal("implicit helper authority accepted")
		}
	}
	options.Environment = map[string]string{"WHIP_TEST_MODE": "bad-version"}
	if connection, err := OpenConnection(t.Context(), options); err == nil {
		connection.Close()
		t.Fatal("version mismatch accepted")
	}
}

func TestOwnedHelperRejectsIncompleteHandshake(t *testing.T) {
	for _, tc := range []struct{ name, script string }{
		{"missing announcement", "exit 0\n"},
		{"handshake rejection", "printf 'whip-computer/1\\n'\nread request\nprintf '{\"jsonrpc\":\"2.0\",\"id\":1,\"error\":{\"code\":1,\"message\":\"no\"}}\\n'\nsleep 60\n"},
		{"handshake version", "printf 'whip-computer/1\\n'\nread request\nprintf '{\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"version\":\"wrong\"}}\\n'\nsleep 60\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			directory := t.TempDir()
			binary := filepath.Join(directory, "helper")
			if err := os.WriteFile(binary, []byte("#!/bin/sh\n"+tc.script), 0o700); err != nil {
				t.Fatal(err)
			}
			manager := capability.NewProcessManager()
			defer func() { _ = manager.Close() }()
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			connection, err := OpenConnection(ctx, ConnectionOptions{Processes: manager, Owner: "root", Executable: binary, Directory: directory})
			if connection != nil {
				connection.Close()
				t.Fatal("incomplete handshake returned a live connection")
			}
			if err == nil || ctx.Err() != nil {
				t.Fatalf("handshake must reject and join promptly: %v", err)
			}
		})
	}
}

func TestOwnedHelperIdleExitRevokesGenerationAndCloseJoins(t *testing.T) {
	connection := ownedConnection(t, ownedHelperBinary(t), nil)
	if err := connection.process.Kill(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-connection.Lifetime().Done():
	case <-time.After(5 * time.Second):
		t.Fatal("idle helper exit left generation active")
	}
	connection.Close()
	select {
	case <-connection.done:
	default:
		t.Fatal("close did not join helper monitor")
	}
	if _, err := connection.Call(t.Context(), "echo", json.RawMessage(`{}`)); !errors.Is(err, ErrConnectionClosed) {
		t.Fatal("idle crash reconnected", err)
	}
}
