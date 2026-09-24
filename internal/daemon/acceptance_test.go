//go:build unix

package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/rlm"
	"github.com/context-labs/whip/internal/session"
)

const (
	acceptanceNeedle = "RLM_ACCEPTANCE_NEEDLE_94721"
	acceptanceCanary = "RLM_ACCEPTANCE_SECRET_61803"
)

type acceptanceProcess struct {
	command *exec.Cmd
	output  bytes.Buffer
	cancel  context.CancelFunc
}

func startAcceptanceProcess(t *testing.T, home, corpus string) *acceptanceProcess {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	process := &acceptanceProcess{cancel: cancel}
	process.command = exec.CommandContext(ctx, executable, "-test.run=^TestRuntimeAcceptanceDaemonHelper$")
	process.command.Env = append(os.Environ(),
		"WHIP_ACCEPTANCE_DAEMON=1",
		"WHIP_ACCEPTANCE_HOME="+home,
		"WHIP_ACCEPTANCE_CORPUS="+corpus,
	)
	process.command.Stdout = &process.output
	process.command.Stderr = &process.output
	if err := process.command.Start(); err != nil {
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		process.cancel()
		if process.command.ProcessState == nil {
			_ = process.command.Wait()
		}
	})
	return process
}

func stopAcceptanceProcess(t *testing.T, process *acceptanceProcess) {
	t.Helper()
	defer process.cancel()
	if err := process.command.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err := process.command.Wait(); err == nil {
		t.Fatal("acceptance daemon exited successfully after a forced crash")
	} else if process.command.ProcessState == nil {
		t.Fatalf("acceptance daemon did not exit: %v\n%s", err, process.output.String())
	}
}

func acceptanceClient(t *testing.T, paths RuntimePaths, clientID string, cursors map[string]int64) *Client {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	for {
		client, err := DialClient(ctx, paths, InitializeParams{
			ProtocolMajor: ProtocolMajor, BuildID: "acceptance", ClientID: clientID, ClientKind: "acceptance", Cursors: cursors,
		})
		if err == nil {
			return client
		}
		select {
		case <-ctx.Done():
			t.Fatalf("acceptance daemon was not ready: %v", err)
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func waitAcceptanceFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		} else if !errors.Is(err, os.ErrNotExist) {
			t.Fatal(err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("acceptance marker %s was not created", path)
}

func acceptanceCommand(rootID, commandID, text string) CommandParams {
	payload, _ := json.Marshal(SubmitPayload{Text: text})
	return CommandParams{CommandID: commandID, Scope: "root", RootID: rootID, Operation: "submit", Payload: payload}
}

// TestRuntimeAcceptanceDetachedRecoveryAndContextIsolation is the hermetic
// release proof for the cross-process spine. It launches the real Unix daemon
// server and the real Starlark worker in subprocesses, then exercises detach,
// cursor replay, snapshot reconstruction, crash recovery, idempotent retry,
// and oversized content isolation.
func TestRuntimeAcceptanceDetachedRecoveryAndContextIsolation(t *testing.T) {
	home := t.TempDir()
	workspace := t.TempDir()
	database := filepath.Join(home, "sessions.db")
	corpusPath := filepath.Join(home, "oversized-corpus.txt")
	var corpus strings.Builder
	for index := range 20_000 {
		value := strings.Repeat("bounded-context-filler-", 2)
		if index == 12_345 {
			value = acceptanceNeedle
		}
		if index == 17_777 {
			value = acceptanceCanary
		}
		fmt.Fprintf(&corpus, "record=%05d value=%s\n", index, value)
	}
	if err := os.WriteFile(corpusPath, []byte(corpus.String()), 0o600); err != nil {
		t.Fatal(err)
	}

	store, err := session.Open(database)
	if err != nil {
		t.Fatal(err)
	}
	rlmRoot, err := store.Create(session.SessionKindAgent, workspace, "scripted", "acceptance")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	paths, err := Paths(home)
	if err != nil {
		t.Fatal(err)
	}

	firstDaemon := startAcceptanceProcess(t, home, corpusPath)
	client := acceptanceClient(t, paths, "acceptance-client", map[string]int64{rlmRoot: 0})
	if info, err := os.Stat(paths.Socket); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("daemon socket mode = %v, %v", info, err)
	}

	detached := acceptanceCommand(rlmRoot, "detached-command", "detached")
	firstCall := make(chan error, 1)
	go func() {
		_, err := client.Command(context.Background(), detached)
		firstCall <- err
	}()
	waitAcceptanceFile(t, filepath.Join(home, "detached.started"))
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-firstCall:
		if err == nil {
			t.Fatal("detached client unexpectedly received the command result")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("detached client call did not unblock")
	}
	if err := os.WriteFile(filepath.Join(home, "detached.release"), nil, 0o600); err != nil {
		t.Fatal(err)
	}

	client = acceptanceClient(t, paths, "acceptance-client", map[string]int64{rlmRoot: 0})
	result, err := client.Command(t.Context(), detached)
	if err != nil || result.Status != "succeeded" || result.IngressSeq != 1 || !strings.Contains(result.Output, acceptanceNeedle) {
		t.Fatalf("detached retry = %+v, %v", result, err)
	}
	replay, err := client.Replay(t.Context(), ReplayParams{RootID: rlmRoot, Cursor: 0})
	if err != nil || replay.Expired || len(replay.Events) == 0 {
		t.Fatalf("replay = %+v, %v", replay, err)
	}
	for index := 1; index < len(replay.Events); index++ {
		if replay.Events[index].Seq <= replay.Events[index-1].Seq {
			t.Fatalf("replay is not strictly ordered: %+v", replay.Events)
		}
	}
	snapshot, err := client.Snapshot(t.Context(), rlmRoot)
	if err != nil || snapshot.Cursor != replay.Latest {
		t.Fatalf("snapshot = %+v, replay latest=%d, %v", snapshot, replay.Latest, err)
	}

	interrupted := acceptanceCommand(rlmRoot, "interrupted-command", "interrupted")
	interruptedCall := make(chan error, 1)
	go func() {
		_, err := client.Command(context.Background(), interrupted)
		interruptedCall <- err
	}()
	waitAcceptanceFile(t, filepath.Join(home, "interrupted.started"))
	stopAcceptanceProcess(t, firstDaemon)
	select {
	case err := <-interruptedCall:
		if err == nil {
			t.Fatal("crashed daemon returned an in-flight command result")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("command did not observe the daemon crash")
	}

	secondDaemon := startAcceptanceProcess(t, home, corpusPath)
	client = acceptanceClient(t, paths, "acceptance-client", map[string]int64{rlmRoot: snapshot.Cursor})
	defer func() { _ = client.Close() }()
	result, err = client.Command(t.Context(), interrupted)
	if err != nil || result.Status != "interrupted" || result.IngressSeq != 2 || result.Error == "" {
		t.Fatalf("recovered command = %+v, %v", result, err)
	}

	kernelStarts, err := os.ReadFile(filepath.Join(home, "kernel-starts.log"))
	if err != nil || strings.Count(string(kernelStarts), "started\n") != 1 {
		t.Fatalf("kernel starts = %q, %v", kernelStarts, err)
	}
	calls, err := os.ReadFile(filepath.Join(home, "turn-calls.log"))
	if err != nil || strings.Count(string(calls), "detached\n") != 1 || strings.Count(string(calls), "interrupted\n") != 1 {
		t.Fatalf("turn calls = %q, %v", calls, err)
	}

	stopAcceptanceProcess(t, secondDaemon)
	for _, suffix := range []string{"", "-wal", "-shm"} {
		data, readErr := os.ReadFile(database + suffix)
		if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
			t.Fatal(readErr)
		}
		if bytes.Contains(data, []byte(acceptanceCanary)) {
			t.Fatalf("oversized corpus canary leaked into sessions.db%s", suffix)
		}
	}
}

type acceptanceHost struct{ corpus string }

func (host acceptanceHost) Call(_ context.Context, module, operation string, arguments map[string]any) (any, error) {
	switch module + "." + operation {
	case "context.search":
		query, _ := arguments["query"].(string)
		index := strings.Index(host.corpus, query)
		if index < 0 {
			return map[string]any{"matches": []any{}}, nil
		}
		return map[string]any{
			"text": host.corpus[index : index+len(query)],
			"span": map[string]any{"start": index, "end": index + len(query)},
		}, nil
	default:
		return nil, fmt.Errorf("unsupported acceptance operation %s.%s", module, operation)
	}
}

type acceptanceRunner struct {
	mu      sync.Mutex
	history []llm.Message
	kernel  *rlm.Kernel
	home    string
}

func (runner *acceptanceRunner) Turn(ctx context.Context, input string, authored bool, started func(), _ func(string)) (string, error) {
	started()
	if err := appendAcceptanceLine(filepath.Join(runner.home, "turn-calls.log"), input); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(runner.home, input+".started"), nil, 0o600); err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(runner.home, input+".release")); err == nil {
			break
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
	defer runner.kernel.Close()
	cell := fmt.Sprintf(`context.search(query=%q)`, acceptanceNeedle)
	value, err := runner.kernel.Exec(ctx, cell)
	if err != nil {
		return "", err
	}
	encoded, err := json.Marshal(value.Value)
	if err != nil {
		return "", err
	}
	runner.mu.Lock()
	runner.history = append(runner.history,
		llm.Message{Role: "user", Content: input, Authored: authored},
		llm.Message{Role: "assistant", Content: string(encoded)},
	)
	runner.mu.Unlock()
	return string(encoded), nil
}

func (*acceptanceRunner) Steer(string) bool { return true }

func (runner *acceptanceRunner) History() []llm.Message {
	runner.mu.Lock()
	defer runner.mu.Unlock()
	return append([]llm.Message(nil), runner.history...)
}

func (runner *acceptanceRunner) Close() { runner.kernel.Close() }

func appendAcceptanceLine(path, value string) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, writeErr := fmt.Fprintln(file, value)
	return errors.Join(writeErr, file.Close())
}

func TestRuntimeAcceptanceDaemonHelper(t *testing.T) {
	if os.Getenv("WHIP_ACCEPTANCE_DAEMON") != "1" {
		return
	}
	home := os.Getenv("WHIP_ACCEPTANCE_HOME")
	corpusPath := os.Getenv("WHIP_ACCEPTANCE_CORPUS")
	corpus, err := os.ReadFile(corpusPath)
	if err != nil {
		t.Fatal(err)
	}
	paths, err := Paths(home)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := AcquireOwner(paths.Lock)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = owner.Close() }()
	store, err := session.Open(filepath.Join(home, "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	generation, err := store.BeginDaemonGeneration(t.Context(), "acceptance")
	if err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	manager := rlm.NewManager(1)
	value, err := New(store, func(_ context.Context, meta session.Meta, history []llm.Message) (Components, error) {
		kernel, kernelErr := rlm.NewKernel(rlm.KernelOptions{
			Command: []string{executable, "-test.run=^TestRuntimeAcceptanceKernelWorker$", "--", "-acceptance-start", filepath.Join(home, "kernel-starts.log")},
			Manager: manager, Host: acceptanceHost{corpus: string(corpus)},
		})
		if kernelErr != nil {
			return Components{}, kernelErr
		}
		return Components{Runner: &acceptanceRunner{history: history, kernel: kernel, home: home}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	server, err := NewServer(value, ServerOptions{BuildID: "acceptance", Generation: generation, RuntimeDir: paths.Runtime})
	if err != nil {
		t.Fatal(err)
	}
	if err := server.ListenAndServe(paths); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeAcceptanceKernelWorker(t *testing.T) {
	separator := -1
	for index, argument := range os.Args {
		if argument == "--" {
			separator = index
			break
		}
	}
	if separator < 0 {
		return
	}
	args := os.Args[separator+1:]
	if len(args) >= 2 && args[0] == "-acceptance-start" {
		if err := appendAcceptanceLine(args[1], "started"); err != nil {
			t.Fatal(err)
		}
		args = args[2:]
	}
	if err := rlm.WorkerMain(args, os.Stdin, os.Stdout); err != nil {
		t.Fatal(err)
	}
}
