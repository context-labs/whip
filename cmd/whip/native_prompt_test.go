package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/store"
)

type nativePromptRequest struct {
	Messages []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"messages"`
}

func nativePromptFixture(t *testing.T) (<-chan nativePromptRequest, session.Session, string) {
	t.Helper()
	requests := make(chan nativePromptRequest, 8)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fixture-file-key" {
			t.Error("native provider did not resolve its configured file credential")
			http.Error(w, "missing credential", http.StatusUnauthorized)
			return
		}
		var request nativePromptRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		requests <- request
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"prompt verified\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	t.Cleanup(server.Close)
	standing, key := filepath.Join(t.TempDir(), "published-standing.md"), filepath.Join(t.TempDir(), "provider.key")
	writePromptRequestFile(t, standing, "STANDING_BEFORE_EDIT")
	writePromptRequestFile(t, key, "fixture-file-key\n")
	key, err := filepath.EvalSymlinks(key)
	if err != nil {
		t.Fatal(err)
	}
	host := config.Default()
	host.StandingInstructionsFile = standing
	host.Providers["testprov"] = config.Provider{Kind: "openai-chat", BaseURL: server.URL, CredentialSource: "file", CredentialFile: key}
	provider := model.OpenAI{Resolve: func(ctx context.Context, _ session.ModelSelection) (model.Route, error) {
		declaration := host.Providers["testprov"]
		credential, err := declaration.Credential(ctx, os.LookupEnv)
		return model.Route{Kind: declaration.Kind, URL: declaration.BaseURL, Credential: credential, MaxOutputTokens: 128, MaxAttempts: 1, TimeoutMillis: 5000}, err
	}}
	r := nativeRunFixtureConfigured(t, provider, host)
	refs, err := r.Builtins()
	if err != nil {
		t.Fatal(err)
	}
	index := slices.IndexFunc(refs, func(ref session.DefinitionRef) bool { return ref.ID == "coding" })
	if index < 0 {
		t.Fatal("coding definition missing")
	}
	_, owner, err := r.CreateTree(t.Context(), store.CreateTree{Engine: session.Starlark, Definition: refs[index], WorkingDirectory: t.TempDir(), Overrides: session.ConfigPatch{AutomaticTitle: new(false)}})
	if err != nil {
		t.Fatal(err)
	}
	for _, grant := range []session.Grant{
		{ID: "workspace-instructions", SessionID: owner.ID, Capability: "files.read", Resource: owner.WorkingDirectory},
		{ID: "standing-instructions", SessionID: owner.ID, Capability: "instructions.read", Resource: "standing"},
	} {
		if _, err := r.CreateGrant(t.Context(), grant); err != nil {
			t.Fatal(err)
		}
	}
	return requests, owner, standing
}

func nextNativePrompt(t *testing.T, requests <-chan nativePromptRequest) nativePromptRequest {
	t.Helper()
	select {
	case request := <-requests:
		return request
	case <-time.After(5 * time.Second):
		t.Fatal("native provider did not receive a request")
		return nativePromptRequest{}
	}
}

func TestNativeHeadlessEntrySendsAuthorizedEnvironmentToProvider(t *testing.T) {
	requests, owner, standing := nativePromptFixture(t)
	writePromptRequestFile(t, filepath.Join(owner.WorkingDirectory, "CLAUDE.md"), "CLAUDE_REQUEST_MARKER")
	writePromptRequestFile(t, filepath.Join(owner.WorkingDirectory, "AGENTS.md"), "AGENTS_REQUEST_MARKER")
	writePromptRequestFile(t, filepath.Join(owner.WorkingDirectory, ".agents", "skills", "fixture", "SKILL.md"), "---\nname: fixture\ndescription: CATALOG_REQUEST_MARKER\n---\n")
	for turn := range 2 {
		marker := "STANDING_BEFORE_EDIT"
		if turn == 1 {
			marker = "STANDING_AFTER_EDIT"
		}
		writePromptRequestFile(t, standing, "# COMMENT_MUST_NOT_REACH_MODEL\n"+marker)
		if _, err := runCapture(t, "", "-quiet", "-resume", string(owner.ID), "verify prompt environment"); err != nil {
			t.Fatal(err)
		}
		request := nextNativePrompt(t, requests)
		if len(request.Messages) < 2 || request.Messages[0].Role != "system" {
			t.Fatal(request)
		}
		prompt := request.Messages[0].Content
		for _, value := range []string{"execute", "never force-push", "<env>", "Current date/time:", "User:", owner.WorkingDirectory, "Identity: root agent", "CLAUDE_REQUEST_MARKER", "AGENTS_REQUEST_MARKER", "CATALOG_REQUEST_MARKER", marker} {
			if !strings.Contains(prompt, value) {
				t.Fatal("missing", value, prompt)
			}
		}
		if strings.Contains(prompt, "COMMENT_MUST_NOT_REACH_MODEL") || turn == 1 && strings.Contains(prompt, "STANDING_BEFORE_EDIT") || strings.Index(prompt, "CLAUDE_REQUEST_MARKER") > strings.Index(prompt, "AGENTS_REQUEST_MARKER") {
			t.Fatal("stale source or reversed precedence", prompt)
		}
	}
}

func TestHeadlessSystemOverrideReachesProviderExactly(t *testing.T) {
	requests, owner, _ := nativePromptFixture(t)
	const override = "Exact user system override.\nKeep this byte-for-byte."
	if _, err := runCapture(t, "", "-quiet", "-resume", string(owner.ID), "-system", override, "hello"); err != nil {
		t.Fatal(err)
	}
	request := nextNativePrompt(t, requests)
	if len(request.Messages) == 0 || request.Messages[0].Role != "system" || request.Messages[0].Content != override {
		t.Fatal(request)
	}
}

func TestHeadlessRejectsIncompleteInstructionsBeforeProvider(t *testing.T) {
	requests, owner, _ := nativePromptFixture(t)
	if err := os.Mkdir(filepath.Join(owner.WorkingDirectory, "AGENTS.md"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := runCapture(t, "", "-quiet", "-resume", string(owner.ID), "hello"); err == nil || !strings.Contains(err.Error(), "regular file") {
		t.Fatal(err)
	}
	select {
	case request := <-requests:
		t.Fatal("provider ran with incomplete instructions", request)
	default:
	}
}
