package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	acpsdk "github.com/coder/acp-go-sdk"
	nativeacp "github.com/context-labs/whip/internal/acp"
	"github.com/context-labs/whip/internal/client"
	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/session"
)

// Other retained CLI fixtures still use this helper until their client cutover.
func writeConfig(t *testing.T, home, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(home, "config.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestAcpCLIConfigErrors(t *testing.T) {
	previous := connectNativeRuntime
	defer func() { connectNativeRuntime = previous }()
	connectNativeRuntime = func(context.Context) (*client.Client, error) {
		return nil, errors.New("native host configuration is invalid")
	}
	if err := acpCLI(nil); err == nil || !strings.Contains(err.Error(), "native host configuration") {
		t.Fatalf("native config error=%v", err)
	}
	for _, args := range [][]string{{"extra"}, {"-unknown"}} {
		if err := acpCLI(args); err == nil {
			t.Fatalf("invalid args %v succeeded", args)
		}
	}
}
func TestAcpCLIServeExitsOnEOF(t *testing.T)            { testAcpCLIServe(t, "env") }
func TestAcpCLIServeWithoutAuthentication(t *testing.T) { testAcpCLIServe(t, "none") }
func testAcpCLIServe(t *testing.T, credential string) {
	t.Helper()
	useNativeAuth(t, func(directory string) {
		host := config.Default()
		host.Defaults.Model = session.ModelSelection{Provider: "scripted", Name: "scripted"}
		host.Providers["scripted"] = config.Provider{Kind: "openai-chat", BaseURL: "http://127.0.0.1:1", CredentialSource: credential}
		if credential == "env" {
			value := host.Providers["scripted"]
			value.CredentialEnv = "WHIP_ACP_ABSENT_KEY"
			host.Providers["scripted"] = value
			t.Setenv("WHIP_ACP_ABSENT_KEY", "")
		}
		if err := config.Save(directory, host); err != nil {
			t.Fatal(err)
		}
	})
	inR, inW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	oldIn, oldOut := os.Stdin, os.Stdout
	os.Stdin, os.Stdout = inR, outW
	done := make(chan error, 1)
	finished := make(chan struct{})
	t.Cleanup(func() {
		_ = inW.Close()
		_ = inR.Close()
		_ = outW.Close()
		_ = outR.Close()
		select {
		case <-finished:
		case <-time.After(10 * time.Second):
			t.Error("ACP did not join before stdio restoration")
		}
		os.Stdin, os.Stdout = oldIn, oldOut
	})
	go func() { defer close(finished); done <- acpCLI(nil) }()
	reader := bufio.NewReader(outR)
	call := func(id int, method string, params any) map[string]json.RawMessage {
		t.Helper()
		raw, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := inW.Write(append(raw, '\n')); err != nil {
			t.Fatal(err)
		}
		if err := outR.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
			t.Fatal(err)
		}
		for range 100 {
			raw, err := reader.ReadBytes('\n')
			if err != nil {
				t.Fatal(err)
			}
			var value map[string]json.RawMessage
			if err := json.Unmarshal(raw, &value); err != nil {
				t.Fatal(err)
			}
			if _, ok := value["id"]; !ok {
				continue
			}
			if _, ok := value["error"]; ok {
				t.Fatalf("ACP %s failed: %s", method, raw)
			}
			return value
		}
		t.Fatal("ACP response did not arrive")
		return nil
	}
	initialized := call(1, "initialize", map[string]any{"protocolVersion": 1, "clientCapabilities": map[string]any{}})
	if !strings.Contains(string(initialized["result"]), "protocolVersion") {
		t.Fatalf("initialize=%s", initialized["result"])
	}
	working := t.TempDir()
	created := call(2, "session/new", map[string]any{"cwd": working, "mcpServers": []any{}})
	var owner struct {
		SessionID string `json:"sessionId"`
	}
	if err := json.Unmarshal(created["result"], &owner); err != nil || owner.SessionID == "" {
		t.Fatalf("create=%s %v", created["result"], err)
	}
	answer := call(3, "session/prompt", map[string]any{"sessionId": owner.SessionID, "prompt": []map[string]string{{"type": "text", "text": "fixture prompt"}}})
	if !strings.Contains(string(answer["result"]), "end_turn") {
		t.Fatalf("prompt=%s", answer["result"])
	}
	if err := inW.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("ACP did not detach on EOF")
	}
}

func TestAcpSupportsVision(t *testing.T) {
	useNativeAuth(t, func(directory string) {
		host := config.Default()
		host.Providers["openrouter"] = config.Provider{Kind: "openai-chat", BaseURL: "https://openrouter.ai/api/v1", CredentialSource: "env", CredentialEnv: "WHIP_ACP_ABSENT_KEY"}
		if err := config.Save(directory, host); err != nil {
			t.Fatal(err)
		}
	})
	c, err := connectNativeRuntime(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	var bundled protocol.ProviderModelsResult
	if err := c.Call(t.Context(), "providers.bundled", protocol.ProviderParams{Provider: "openrouter"}, &bundled); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, value := range bundled.Items {
		if slices.Contains(value.InputModalities, "image") {
			found = true
			if !acpSupportsVision(t.Context(), c, protocol.ModelSelection{Provider: "openrouter", Name: value.ID}) {
				t.Fatal("known bundled image model rejected")
			}
			break
		}
	}
	if !found {
		t.Fatal("fixture bundled catalog has no image model")
	}
	if acpSupportsVision(t.Context(), c, protocol.ModelSelection{Provider: "openrouter", Name: "unknown"}) || acpSupportsVision(t.Context(), c, protocol.ModelSelection{}) {
		t.Fatal("unknown model fabricated image support")
	}
}

func testNativeACPPromptContext(t *testing.T) {
	t.Helper()
	requests := make(chan model.Request, 4)
	working := t.TempDir()
	standing := filepath.Join(t.TempDir(), "standing.md")
	writePromptRequestFile(t, standing, "# COMMENT_MUST_NOT_REACH_MODEL\nSTANDING_BEFORE_EDIT")
	writePromptRequestFile(t, filepath.Join(working, "CLAUDE.md"), "CLAUDE_REQUEST_MARKER")
	writePromptRequestFile(t, filepath.Join(working, "AGENTS.md"), "AGENTS_REQUEST_MARKER")
	writePromptRequestFile(t, filepath.Join(working, ".agents", "skills", "fixture", "SKILL.md"), "---\nname: fixture\ndescription: CATALOG_REQUEST_MARKER\n---\n")
	host := config.Default()
	host.StandingInstructionsFile = standing
	r := nativeRunFixtureConfigured(t, nativeCLIProvider(func(_ context.Context, request model.Request, _ func(model.Chunk)) (model.Response, error) {
		if request.Purpose != session.AutomaticTitlePurpose {
			requests <- request
		}
		return model.Response{Parts: []session.Part{{Type: "text", Text: "prompt verified"}}}, nil
	}), host)
	c, err := connectNativeRuntime(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	bridge := nativeacp.NewBridge("fixture", c, nativeacp.Options{})
	defer bridge.CloseAll()
	created, err := bridge.NewSession(t.Context(), acpsdk.NewSessionRequest{Cwd: working, McpServers: []acpsdk.McpServer{}})
	if err != nil {
		t.Fatal(err)
	}
	for _, grant := range []session.Grant{
		{ID: "read-workspace", SessionID: session.SessionID(created.SessionId), Capability: "files.read", Resource: working},
		{ID: "read-standing", SessionID: session.SessionID(created.SessionId), Capability: "instructions.read", Resource: "standing"},
	} {
		if _, err := r.CreateGrant(t.Context(), grant); err != nil {
			t.Fatal(err)
		}
	}
	for turn := range 2 {
		marker := "STANDING_BEFORE_EDIT"
		if turn == 1 {
			marker = "STANDING_AFTER_EDIT"
			writePromptRequestFile(t, standing, marker)
		}
		if _, err := bridge.Prompt(t.Context(), acpsdk.PromptRequest{SessionId: created.SessionId, Prompt: []acpsdk.ContentBlock{acpsdk.TextBlock("verify prompt environment")}}); err != nil {
			t.Fatal(err)
		}
		var request model.Request
		select {
		case request = <-requests:
		case <-time.After(5 * time.Second):
			t.Fatal("native ACP provider was not called")
		}
		for _, want := range []string{"never force-push", "<env>", "Current date/time:", "User:", working, "Identity: root agent", "CLAUDE_REQUEST_MARKER", "AGENTS_REQUEST_MARKER", "CATALOG_REQUEST_MARKER", marker} {
			if !strings.Contains(request.Instructions, want) {
				t.Errorf("native ACP request missing %q: %s", want, request.Instructions)
			}
		}
		if strings.Contains(request.Instructions, "COMMENT_MUST_NOT_REACH_MODEL") || turn == 1 && strings.Contains(request.Instructions, "STANDING_BEFORE_EDIT") {
			t.Fatal("native ACP used stale or unfiltered host instructions")
		}
		if strings.Index(request.Instructions, "CLAUDE_REQUEST_MARKER") > strings.Index(request.Instructions, "AGENTS_REQUEST_MARKER") {
			t.Fatal("native source order reversed")
		}
	}
}
