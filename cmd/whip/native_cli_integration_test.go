//go:build integration

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/localruntime"
	"github.com/context-labs/whip/internal/session"
)

func TestNativeCompiledCLIUsesFreshHostAndRealEngines(t *testing.T) {
	directory, err := os.MkdirTemp("/tmp", "whip-native-cli-") //nolint:usetesting // Unix socket paths must fit macOS.
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(directory) })
	binary := filepath.Join(directory, "whipcode")
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
	build := exec.CommandContext(ctx, "go", "build", "-o", binary, "./cmd/whip")
	build.Dir = "../.."
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, output)
	}
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var request struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
			Tools []json.RawMessage `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		if len(request.Tools) > 0 && len(request.Messages) > 0 && request.Messages[len(request.Messages)-1].Role == "user" {
			code := "print(42)"
			if strings.Contains(request.Messages[0].Content, "JavaScript (QuickJS") {
				code = "console.log(42)"
			}
			arguments, _ := json.Marshal(map[string]string{"code": code})
			chunk, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": "real-cli-call", "type": "function", "function": map[string]any{"name": "execute", "arguments": string(arguments)}}}}, "finish_reason": "tool_calls"}}})
			fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", chunk)
			return
		}
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"native production complete\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	t.Cleanup(server.Close)
	home := filepath.Join(directory, "home")
	paths, err := localruntime.Resolve(home)
	if err != nil {
		t.Fatal(err)
	}
	host := config.Default()
	host.Defaults.Model = session.ModelSelection{Provider: "fixture", Name: "fixture"}
	host.Providers["fixture"] = config.Provider{Kind: "openai-chat", BaseURL: server.URL, CredentialSource: "none"}
	if err := config.Save(paths.Directory, host); err != nil {
		t.Fatal(err)
	}
	legacy := filepath.Join(home, "config.json")
	if err := os.WriteFile(legacy, []byte("retired user data"), 0o600); err != nil {
		t.Fatal(err)
	}
	environment := []string{"HOME=" + directory, "WHIPCODE_HOME=" + home, "PATH=/usr/bin:/bin:/usr/sbin:/sbin"}
	var diagnostics bytes.Buffer
	hostProcess := exec.CommandContext(ctx, binary, "_native-runtime", "-directory", paths.Directory)
	hostProcess.Env = environment
	hostProcess.Stdout = &diagnostics
	hostProcess.Stderr = &diagnostics
	if err := hostProcess.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		stopContext, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer stopCancel()
		if _, err := localruntime.Stop(stopContext, paths); err != nil {
			t.Error(err)
			_ = hostProcess.Process.Kill()
		}
		if err := hostProcess.Wait(); err != nil {
			t.Errorf("native host: %v\n%s", err, diagnostics.String())
		}
	})
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for localruntime.Inspect(ctx, paths).State != "running" {
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-ticker.C:
		}
	}
	run := func(args ...string) string {
		t.Helper()
		command := exec.CommandContext(ctx, binary, args...)
		command.Env = environment
		command.Dir = directory
		var output, notes bytes.Buffer
		command.Stdout = &output
		command.Stderr = &notes
		if err := command.Run(); err != nil {
			t.Fatalf("CLI %v: %v\n%s\n%s", args, err, output.String(), notes.String())
		}
		return output.String()
	}
	for _, engine := range []string{"starlark", "quickjs"} {
		record := filepath.Join(directory, engine+".json")
		output := run("run", "-quiet", "-format", "json", "-record", record, "-rlm-engine", engine, "execute a real cell")
		observedCell := false
		for line := range strings.SplitSeq(strings.TrimSpace(output), "\n") {
			var event map[string]string
			if err := json.Unmarshal([]byte(line), &event); err != nil {
				t.Fatal(err)
			}
			observedCell = observedCell || event["type"] == "tool_end" && strings.Contains(event["result"], `"output":"42\n"`)
		}
		if !observedCell || !strings.Contains(output, `"type":"done"`) {
			t.Fatal(output)
		}
		before := calls.Load()
		if recovered := run("run", "-quiet", "-recover", record); !strings.Contains(recovered, "native production complete") {
			t.Fatal(recovered)
		}
		if calls.Load() != before {
			t.Fatal("recovery resubmitted a completed run")
		}
	}
	if listed := run("sessions"); strings.Count(listed, "session_") != 2 || !strings.Contains(listed, "fixture") {
		t.Fatal(listed)
	}
	if raw, err := os.ReadFile(legacy); err != nil || string(raw) != "retired user data" {
		t.Fatal("retired storage changed", err)
	}
}
