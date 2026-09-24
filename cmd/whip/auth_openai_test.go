package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/openaiauth"
)

func TestOpenAIAuthCLIStatusAndLogout(t *testing.T) {
	directory := t.TempDir()
	t.Setenv("WHIP_HOME", directory)
	auth := openaiauth.New(t.Context(), directory)
	if err := auth.Install(t.Context(), auth.Generation(), openaiauth.Credentials{
		AccessToken: "fixture-access", RefreshToken: "fixture-refresh", AccountID: "fixture-account",
		Email: "subscriber@example.com", Plan: "pro", ExpiresAt: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	auth.Close()
	cfg := config.Default()
	if err := cfg.UpsertOpenAICodex(); err != nil {
		t.Fatal(err)
	}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	useTestDaemon(t)

	output := invokeMain(t, "auth", "openai-codex", "status")
	for _, want := range []string{"OpenAI (ChatGPT subscription)", "Status  connected", "Account subscriber@example.com", "Plan    pro"} {
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
	directory := t.TempDir()
	t.Setenv("WHIP_HOME", directory)
	if err := os.WriteFile(filepath.Join(directory, "openai-codex.json"), []byte(`{"accessToken":"private-fixture-secret"`), 0o600); err != nil {
		t.Fatal(err)
	}
	useTestDaemon(t)
	var output string
	warning := captureStderr(t, func() { output = invokeMain(t, "auth", "openai-codex", "status") })
	if !strings.Contains(output, "sign_in_required") || !strings.Contains(warning, "repair or remove") {
		t.Fatalf("corrupt credentials did not explain recovery: %s %s", output, warning)
	}
	if strings.Contains(output+warning, "private-fixture-secret") {
		t.Fatal("credential file contents appeared in status output")
	}
	// Default login must stop before launching a browser when storage needs repair.
	if err := authOpenAICLI(nil); err == nil || !strings.Contains(err.Error(), "repair or remove") || strings.Contains(err.Error(), "private-fixture-secret") {
		t.Fatalf("login did not report safe storage recovery instructions: %v", err)
	}
}
