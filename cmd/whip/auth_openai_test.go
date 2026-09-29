package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/protocol"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/openaiauth"
)

func TestOpenAIAuthCLIStatusAndLogout(t *testing.T) {
	directory := useNativeAuth(t, func(directory string) {
		auth := openaiauth.New(t.Context(), directory)
		defer auth.Close()
		if err := auth.Install(t.Context(), auth.Generation(), openaiauth.Credentials{AccessToken: "fixture-access", RefreshToken: "fixture-refresh", AccountID: "fixture-account", Email: "subscriber@example.com", Plan: "pro", ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
			t.Fatal(err)
		}
		host := config.Default()
		if err := host.EnsureSubscription(); err != nil {
			t.Fatal(err)
		}
		if err := config.Save(directory, host); err != nil {
			t.Fatal(err)
		}
	})

	output := invokeMain(t, "auth", "openai-codex", "status")
	for _, want := range []string{"OpenAI (ChatGPT subscription)", "Status  stored", "Account subscriber@example.com", "Plan    pro"} {
		if !strings.Contains(output, want) {
			t.Errorf("status missing %q: %s", want, output)
		}
	}
	for _, operation := range []string{"logout", "logout", "status"} {
		output = invokeMain(t, "auth", "openai-codex", operation)
		if !strings.Contains(output, "Status  signed_out") || strings.Contains(output, "subscriber@example.com") {
			t.Fatalf("%s retained account identity: %s", operation, output)
		}
	}
	stored := openaiauth.New(t.Context(), directory)
	defer stored.Close()
	credentials, err := stored.Snapshot()
	if err != nil || credentials.AccessToken != "" || credentials.RefreshToken != "" {
		t.Fatalf("logout did not remove persisted credentials: %v", err)
	}
}

func TestOpenAIAuthCLIRejectsInvalidOperations(t *testing.T) {
	for _, args := range [][]string{{"unknown"}, {"status", "extra"}, {"logout", "extra"}} {
		if err := authOpenAICLI(args); err == nil || !strings.Contains(err.Error(), "usage:") {
			t.Errorf("arguments %q: expected usage error, got %v", args, err)
		}
	}
}

func TestOpenAIAuthCLIMalformedCredentialsRequireRepairWithoutDisclosure(t *testing.T) {
	useNativeAuth(t, func(directory string) {
		if err := os.WriteFile(filepath.Join(directory, "openai-codex.json"), []byte(`{"accessToken":"private-fixture-secret"`), 0o600); err != nil {
			t.Fatal(err)
		}
	})

	var output string
	warning := captureStderr(t, func() { output = invokeMain(t, "auth", "openai-codex", "status") })
	if !strings.Contains(output, "Status  unavailable") || warning == "" {
		t.Fatalf("corrupt credentials did not explain recovery: %s %s", output, warning)
	}
	if strings.Contains(output+warning, "private-fixture-secret") {
		t.Fatal("credential file contents appeared in status output")
	}
	// Default login must stop before launching a browser when storage needs repair.
	if err := authOpenAICLI(nil); err == nil || !strings.Contains(err.Error(), "credentials") || strings.Contains(err.Error(), "private-fixture-secret") {
		t.Fatalf("login did not report safe storage recovery instructions: %v", err)
	}
}

func TestAuthOpenAIObserverTimeoutPreservesFlowUntilExplicitCancellation(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	started, cancelled := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		close(started)
		<-r.Context().Done()
		close(cancelled)
	}))
	defer server.Close()
	redirectAuthRequests(t, server.URL, "auth.openai.com", "")
	useNativeAuth(t, nil)
	c, err := connectNativeRuntime(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	var flow protocol.OpenAILoginFlow
	if err := c.Call(t.Context(), "accounts.openai.begin", protocol.EmptyParams{}, &flow); err != nil {
		t.Fatal(err)
	}
	<-started
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer cancel()
	if err := waitOpenAIFlow(ctx, c, flow); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	var retained protocol.OpenAILoginFlow
	if err := c.Call(t.Context(), "accounts.openai.get", protocol.OpenAIFlowParams{FlowID: flow.ID}, &retained); err != nil || retained.State != "authorizing" {
		t.Fatal("observer cancelled host-owned work", retained, err)
	}
	if output := invokeMain(t, "auth", "openai-codex", "flows"); !strings.Contains(output, flow.ID) {
		t.Fatal(output)
	}
	if err := authOpenAICLI([]string{"flow", "cancel", flow.ID}); err != nil {
		t.Fatal(err)
	}
	<-cancelled
	if err := c.Call(t.Context(), "accounts.openai.get", protocol.OpenAIFlowParams{FlowID: flow.ID}, &retained); err != nil || retained.State != "cancelled" {
		t.Fatal(retained, err)
	}
}
