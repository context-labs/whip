package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/daemon"
	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/session"
)

func TestMain(m *testing.M) {
	code := func() int {
		home, err := os.MkdirTemp("", "whip-cli-test-home-")
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		defer os.RemoveAll(home)
		if err := os.Setenv("HOME", home); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		if err := os.Setenv("WHIP_HOME", filepath.Join(home, "whip")); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return m.Run()
	}()
	os.Exit(code)
}

func invokeMain(t *testing.T, args ...string) string {
	t.Helper()
	previousArgs, previousFlags := os.Args, flag.CommandLine
	previousInput, previousOutput := os.Stdin, os.Stdout
	defer func() {
		os.Args, flag.CommandLine = previousArgs, previousFlags
		os.Stdin, os.Stdout = previousInput, previousOutput
	}()
	os.Args = append([]string{"whip"}, args...)
	flag.CommandLine = flag.NewFlagSet("whip", flag.ContinueOnError)
	flag.CommandLine.SetOutput(io.Discard)

	inR, inW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	_ = inW.Close()
	os.Stdin = inR
	defer func() { _ = inR.Close() }()
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = outW
	var output bytes.Buffer
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(&output, outR)
		close(done)
	}()
	main()
	_ = outW.Close()
	<-done
	_ = outR.Close()
	return output.String()
}

func TestMainDispatchesHeadlessCommands(t *testing.T) {
	t.Run("version", func(t *testing.T) {
		if output := invokeMain(t, "-version"); !strings.Contains(output, "whip "+version) {
			t.Fatalf("version output = %q", output)
		}
	})
	t.Run("kernel EOF", func(t *testing.T) {
		invokeMain(t, "_kernel")
	})

	t.Run("bench", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("WHIP_HOME", home)
		writeConfig(t, home, `{
			"defaultModel":"test",
			"providers":{"testprov":{"baseUrl":"http://127.0.0.1:1","api":"openai-completions","apiKey":"k"}},
			"models":{"test":{"providers":["testprov"],"maxOut":100}}
		}`)
		invokeMain(t, "-bench")
	})

	t.Run("browser install", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		t.Setenv("PATH", t.TempDir())
		if output := invokeMain(t, "browser", "install"); !strings.Contains(output, "Load unpacked") {
			t.Fatalf("browser output = %q", output)
		}
	})

	t.Run("update", func(t *testing.T) {
		home, bin := t.TempDir(), t.TempDir()
		t.Setenv("WHIP_HOME", home)
		installer := filepath.Join(bin, "sh")
		if err := os.WriteFile(installer, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
			t.Fatal(err)
		}
		t.Setenv("PATH", bin)
		if output := invokeMain(t, "update"); !strings.Contains(output, "whip updated") {
			t.Fatalf("update output = %q", output)
		}
	})

	t.Run("run sessions mcp and auth", func(t *testing.T) {
		runFixture(t, "main reply", nil)
		if output := invokeMain(t, "run", "hello"); !strings.Contains(output, "main reply") {
			t.Fatalf("run output = %q", output)
		}
		if output := invokeMain(t, "sessions"); !strings.Contains(output, "test") {
			t.Fatalf("sessions output = %q", output)
		}
		if output := invokeMain(t, "mcp", "list"); output == "" {
			t.Fatal("mcp list produced no output")
		}
		if output := invokeMain(t, "auth", "inference-net", "status"); !strings.Contains(output, "Inference.net") {
			t.Fatalf("auth output = %q", output)
		}
	})
}

// These tests capture actual daemon-factory model requests. A client-side
// system-prompt string is insufficient evidence for a presentation-only CLI.
func TestClientEntryPathsSendAssembledPromptToProvider(t *testing.T) {
	for _, kind := range []string{"headless", "acp", "tui"} {
		t.Run(kind, func(t *testing.T) {
			requests, workingDirectory := promptRequestFixture(t)
			standing := filepath.Join(os.Getenv("WHIP_HOME"), "me.md")
			writePromptRequestFile(t, standing, "# COMMENT_MUST_NOT_REACH_MODEL\nSTANDING_BEFORE_EDIT")
			writePromptRequestFile(t, filepath.Join(workingDirectory, "CLAUDE.md"), "CLAUDE_REQUEST_MARKER")
			writePromptRequestFile(t, filepath.Join(workingDirectory, "AGENTS.md"), "AGENTS_REQUEST_MARKER")
			writePromptRequestFile(t, filepath.Join(workingDirectory, ".agents", "skills", "fixture", "SKILL.md"), "---\nname: fixture\ndescription: CATALOG_REQUEST_MARKER\n---\n")
			submit := promptSubmitter(t, kind, workingDirectory)
			for turn := range 2 {
				standingMarker := "STANDING_BEFORE_EDIT"
				if turn == 1 {
					standingMarker = "STANDING_AFTER_EDIT"
					writePromptRequestFile(t, standing, standingMarker)
				}
				submit("verify prompt environment")
				request := readPromptRequest(t, requests)
				if len(request.Messages) < 2 || request.Messages[0].Role != "system" {
					t.Fatalf("request has no system prompt: %#v", request.Messages)
				}
				prompt := request.Messages[0].Content
				for _, marker := range []string{"rlm_exec", "never force-push", "<env>", "Current date/time:", "User:", workingDirectory, "Identity: root agent", "CLAUDE_REQUEST_MARKER", "AGENTS_REQUEST_MARKER", "CATALOG_REQUEST_MARKER", standingMarker} {
					if !strings.Contains(prompt, marker) {
						t.Errorf("%s turn %d actual request missing %q", kind, turn, marker)
					}
				}
				if strings.Contains(prompt, "COMMENT_MUST_NOT_REACH_MODEL") || turn == 1 && strings.Contains(prompt, "STANDING_BEFORE_EDIT") {
					t.Fatalf("%s request contains stale or commented standing instructions", kind)
				}
				if strings.Index(prompt, "CLAUDE_REQUEST_MARKER") > strings.Index(prompt, "AGENTS_REQUEST_MARKER") {
					t.Fatal("project source precedence was reversed in actual request")
				}
			}
		})
	}
}

func TestHeadlessSystemOverrideReachesProviderExactly(t *testing.T) {
	requests, workingDirectory := promptRequestFixture(t)
	writePromptRequestFile(t, filepath.Join(os.Getenv("WHIP_HOME"), "me.md"), "NORMAL_STANDING_RULE")
	writePromptRequestFile(t, filepath.Join(workingDirectory, "AGENTS.md"), "NORMAL_PROJECT_RULE")
	const override = "Exact user system override.\nKeep this byte-for-byte."
	if _, err := runCapture(t, "", "-quiet", "-system", override, "hello"); err != nil {
		t.Fatal(err)
	}
	request := readPromptRequest(t, requests)
	if len(request.Messages) == 0 || request.Messages[0].Role != "system" || request.Messages[0].Content != override {
		t.Fatalf("explicit -system was replaced or supplemented: %#v", request.Messages)
	}
}

func TestHeadlessRejectsIncompleteInstructionsBeforeProvider(t *testing.T) {
	requests, workingDirectory := promptRequestFixture(t)
	if err := os.Mkdir(filepath.Join(workingDirectory, "AGENTS.md"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := runCapture(t, "", "-quiet", "hello"); err == nil || !strings.Contains(err.Error(), "expected a regular file") {
		t.Fatalf("invalid applicable rules must fail the run explicitly: %v", err)
	}
	select {
	case request := <-requests:
		t.Fatalf("provider ran with incomplete instructions: %#v", request.Messages)
	default:
	}
}

func promptRequestFixture(t *testing.T) (<-chan llm.Request, string) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	home := t.TempDir()
	t.Setenv("WHIP_HOME", home)
	t.Chdir(t.TempDir())
	workingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	requests := make(chan llm.Request, 8)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request llm.Request
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		select {
		case requests <- request:
		case <-r.Context().Done():
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"prompt verified\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	t.Cleanup(server.Close)
	writeConfig(t, home, fmt.Sprintf(`{
		"defaultModel":"test", "maxRetries":0,
		"mcpImport":{"claude":{"enabled":false},"codex":{"enabled":false}},
		"providers":{"testprov":{"baseUrl":%q,"api":"openai-completions","apiKey":"fixture-key"}},
		"models":{"test":{"providers":["testprov"],"context":65536,"maxOut":128}}
	}`, server.URL))
	useTestDaemon(t)
	return requests, workingDirectory
}

func writePromptRequestFile(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

func readPromptRequest(t *testing.T, requests <-chan llm.Request) llm.Request {
	t.Helper()
	select {
	case request := <-requests:
		return request
	case <-time.After(5 * time.Second):
		t.Fatal("provider did not receive a request")
		return llm.Request{}
	}
}

func promptSubmitter(t *testing.T, kind, workingDirectory string) func(string) {
	t.Helper()
	if kind == "headless" {
		return func(text string) {
			t.Helper()
			if _, err := runCapture(t, "", "-quiet", text); err != nil {
				t.Fatal(err)
			}
		}
	}
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	t.Cleanup(cancel)
	var root *daemon.RootClient
	var err error
	if kind == "acp" {
		backend := &acpDaemonBackend{clientID: "prompt-acp", model: "test", provider: "testprov"}
		root, err = backend.NewRoot(ctx, workingDirectory, nil)
	} else {
		// Mirror the presentation-only TUI's root creation and submit path.
		root, err = daemon.NewRootClient(daemon.RootClientOptions{
			ClientID: "prompt-tui", Connector: daemonConnector("tui", "prompt-tui"),
			Create: &daemon.CreateSession{Kind: session.SessionKindAgent, CWD: workingDirectory, Model: "test", Provider: "testprov"},
		})
		if err == nil {
			root.Start()
			err = root.WaitLive(ctx)
		}
	}
	if err != nil {
		if root != nil {
			_ = root.Close()
		}
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	return func(text string) {
		t.Helper()
		action, err := root.NewAction("submit", daemon.SubmitPayload{Text: text})
		if err != nil {
			t.Fatal(err)
		}
		result, err := root.Command(ctx, action)
		if err != nil || result.Status != "succeeded" || result.Output != "prompt verified" {
			t.Fatalf("%s submit = %#v, %v", kind, result, err)
		}
	}
}
