package main

import (
	"context"
	"flag"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/context-labs/whip/internal/client"
	"github.com/creack/pty"

	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/tui"
)

// This runs the native Bubble Tea event loop against the real socket/provider.
// Input and output are disposable pipes; no installed runtime or real TTY runs.
func testNativeTUIPromptContext(t *testing.T) {
	t.Helper()
	requests, owner, standing := nativePromptFixture(t)
	writePromptRequestFile(t, filepath.Join(owner.WorkingDirectory, "CLAUDE.md"), "CLAUDE_REQUEST_MARKER")
	writePromptRequestFile(t, filepath.Join(owner.WorkingDirectory, "AGENTS.md"), "AGENTS_REQUEST_MARKER")
	writePromptRequestFile(t, filepath.Join(owner.WorkingDirectory, ".agents", "skills", "fixture", "SKILL.md"), "---\nname: fixture\ndescription: CATALOG_REQUEST_MARKER\n---\n")
	writePromptRequestFile(t, standing, "# COMMENT_MUST_NOT_REACH_MODEL\nSTANDING_BEFORE_EDIT")
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	connection, err := connectNativeRuntime(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = connection.Close() }()
	input, writer := io.Pipe()
	defer func() { _ = input.Close(); _ = writer.Close() }()
	type outcome struct {
		id  string
		err error
	}
	acknowledged := make(chan string, 1)
	done := make(chan outcome, 1)
	joined := make(chan struct{})
	clientHome := t.TempDir()
	t.Cleanup(func() { cancel(); _ = writer.Close(); <-joined })
	go func() {
		defer close(joined)
		var lastAcknowledged string
		id, err := tui.RunNative(ctx, connection, tui.NativeOptions{Resume: string(owner.ID), ClientHome: clientHome, InitialPrompt: "verify prompt environment"}, tea.WithInput(input), tea.WithOutput(io.Discard), tea.WithoutRenderer(), tea.WithWindowSize(100, 30), tea.WithFilter(func(model tea.Model, message tea.Msg) tea.Msg {
			// Canonical settlement can precede the UI's admission response. Wait
			// for each distinct acknowledgement before sending the next command.
			if _, status, ok := strings.Cut(model.View().Content, "Accepted input "); ok {
				if fields := strings.Fields(status); len(fields) > 0 && fields[0] != lastAcknowledged {
					lastAcknowledged = fields[0]
					acknowledged <- lastAcknowledged
				}
			}
			return message
		}))
		done <- outcome{id, err}
	}()
	for turn := range 2 {
		marker := "STANDING_BEFORE_EDIT"
		if turn == 1 {

			marker = "STANDING_AFTER_EDIT"
			writePromptRequestFile(t, standing, "# COMMENT_MUST_NOT_REACH_MODEL\n"+marker)
			if _, err := io.WriteString(writer, "/queue verify prompt environment\r"); err != nil {
				t.Fatal(err)
			}
		}
		request := nextNativePrompt(t, requests)
		if len(request.Messages) < 2 || request.Messages[0].Role != "system" {
			t.Fatal(request)
		}
		prompt := request.Messages[0].Content
		for _, value := range []string{"execute", "never force-push", "<env>", "Current date/time:", "User:", owner.WorkingDirectory, "Identity: root agent", "CLAUDE_REQUEST_MARKER", "AGENTS_REQUEST_MARKER", "CATALOG_REQUEST_MARKER", marker} {
			if !strings.Contains(prompt, value) {
				t.Fatal("missing host-composed source", value, prompt)
			}
		}
		if strings.Contains(prompt, "COMMENT_MUST_NOT_REACH_MODEL") || turn == 1 && strings.Contains(prompt, "STANDING_BEFORE_EDIT") || strings.Index(prompt, "CLAUDE_REQUEST_MARKER") > strings.Index(prompt, "AGENTS_REQUEST_MARKER") {
			t.Fatal("stale or reordered prompt sources", prompt)
		}
		// Wait for canonical settlement before the next original input.
		for {
			var activity protocol.SessionActivity
			if err := connection.Call(ctx, "sessions.activity", protocol.SessionParams{SessionID: protocol.ID(owner.ID)}, &activity); err != nil {
				t.Fatal(err)
			}
			if activity.ActiveTurn == nil && activity.QueuedInputCount == 0 {
				break
			}
			select {
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			case <-time.After(10 * time.Millisecond):
			}
		}
		select {
		case <-acknowledged:
		case <-ctx.Done():
			t.Fatal("terminal did not acknowledge its input", ctx.Err())
		}
	}
	// The host may settle before the terminal observes its idle state. Explicit
	// quit detaches; double Ctrl+C targets a still-observed turn. A trailing
	// space closes command completion so Enter submits instead of inserting.
	if _, err := io.WriteString(writer, "/quit \r"); err != nil {
		t.Fatal(err)
	}
	select {
	case result := <-done:
		if result.err != nil || result.id != string(owner.ID) {
			t.Fatal(result)
		}
	case <-ctx.Done():
		t.Fatal("native terminal did not detach", ctx.Err())
	}
}

func TestNativeTUIMainHelper(t *testing.T) {
	socket := os.Getenv("WHIP_NATIVE_TUI_TEST_SOCKET")
	if socket == "" {
		return
	}
	separator := slices.Index(os.Args, "--")
	if separator < 0 {
		t.Fatal("missing CLI fixture arguments")
	}
	connectNativeRuntime = func(ctx context.Context) (*client.Client, error) { return client.Connect(ctx, socket, nil) }
	os.Args = append([]string{"whipcode"}, os.Args[separator+1:]...)
	flag.CommandLine = flag.NewFlagSet("whipcode", flag.ExitOnError)
	main()
}

func TestNativeDefaultMainRouteUsesRealTerminalAndHost(t *testing.T) {
	requests, owner, _ := nativePromptFixture(t)
	paths, err := nativeRuntimePaths()
	if err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, executable, "-test.run=^TestNativeTUIMainHelper$", "--", "--resume", string(owner.ID), "--rlm-engine", "starlark", "--agent", "coding", "up", "native main prompt")
	command.Env = append(os.Environ(), "WHIP_NATIVE_TUI_TEST_SOCKET="+paths.Socket, "TERM=xterm-256color")
	terminal, err := pty.StartWithSize(command, &pty.Winsize{Rows: 30, Cols: 100})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = terminal.Close() }()
	outputDone := make(chan struct{})
	go func() { _, _ = io.Copy(io.Discard, terminal); close(outputDone) }()
	joined := make(chan error, 1)
	go func() { joined <- command.Wait() }()
	var waited bool
	defer func() {
		cancel()
		if !waited {
			<-joined
		}
		_ = terminal.Close()
		<-outputDone
	}()
	request := nextNativePrompt(t, requests)
	if len(request.Messages) < 2 || request.Messages[len(request.Messages)-1].Content != "native main prompt" {
		t.Fatal("main flags did not reach native host input", request)
	}
	if _, err := terminal.WriteString("\x03\x03"); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-joined:
		waited = true
		if err != nil {
			t.Fatal("native main did not detach cleanly", err)
		}
	case <-ctx.Done():
		t.Fatal("native main did not detach", ctx.Err())
	}
}
