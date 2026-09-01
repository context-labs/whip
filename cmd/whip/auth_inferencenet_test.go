package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/inferencenet"
)

func TestAuthInferenceNetDispatch(t *testing.T) {
	t.Setenv("WHIP_HOME", t.TempDir())
	// Unknown subcommand is rejected.
	if err := authCLI([]string{"inference-net", "bogus"}); err == nil {
		t.Error("unknown subcommand should error")
	}
	// key without "rotate" is rejected.
	if err := authCLI([]string{"inference-net", "key"}); err == nil {
		t.Error("`key` without rotate should error")
	}
	// The legacy "inference" provider name still routes.
	if err := authCLI([]string{"inference", "bogus"}); err == nil {
		t.Error("legacy alias should route to inference-net handler")
	}
}

func TestAuthInferenceNetBYOKNoKey(t *testing.T) {
	t.Setenv("WHIP_HOME", t.TempDir())
	t.Setenv(config.InferenceNetEnvVar, "")
	if err := authCLI([]string{"inference-net", "login", "--key", ""}); err == nil {
		t.Error("BYOK with no key should error")
	}
}

func TestAuthInferenceNetStatusAndLogoutUnsigned(t *testing.T) {
	t.Setenv("WHIP_HOME", t.TempDir())
	if err := authCLI([]string{"inference-net", "status"}); err != nil {
		t.Errorf("status on a fresh home should not error: %v", err)
	}
	// Logout with nothing stored is a clean no-op.
	if err := authCLI([]string{"inference-net", "logout"}); err != nil {
		t.Errorf("logout with no session should not error: %v", err)
	}
	// Rotate without a session tells the user to log in first.
	if err := authCLI([]string{"inference-net", "key", "rotate"}); err == nil ||
		!strings.Contains(err.Error(), "login") {
		t.Errorf("rotate without session should point at login, got %v", err)
	}
}

func TestAuthInferenceNetLogoutClearsStoredAuth(t *testing.T) {
	t.Setenv("WHIP_HOME", t.TempDir())
	// Point the remote calls at a dead local port so they fail fast (and the
	// test never reaches the real relay); the local state is still cleared.
	defer inferencenet.SetURLsForTest("http://127.0.0.1:1", "", "")()
	if err := inferencenet.SaveAuth(inferencenet.Auth{SessionToken: "tok", MachineKey: "mk"}); err != nil {
		t.Fatal(err)
	}
	// Logout's remote calls fail soft (warnings); the local state is cleared.
	if err := authCLI([]string{"inference-net", "logout"}); err != nil {
		t.Errorf("logout should clear local state even when remote calls fail: %v", err)
	}
	a, _ := inferencenet.LoadAuth()
	if a != (inferencenet.Auth{}) {
		t.Errorf("logout should clear stored auth, got %+v", a)
	}
}

func TestAuthInferenceNetBYOKValidatesAndPersists(t *testing.T) {
	t.Setenv("WHIP_HOME", t.TempDir())
	t.Setenv(config.InferenceNetEnvVar, "")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" || r.Header.Get("Authorization") != "Bearer good" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	defer inferencenet.SetURLsForTest("", "", srv.URL)()

	if err := authCLI([]string{"inference-net", "login", "--key", "bad"}); err == nil {
		t.Fatal("rejected key was accepted")
	}
	if err := authCLI([]string{"inference-net", "login", "--key", " good\n"}); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	provider := cfg.Providers[config.InferenceNetProvider]
	if provider.APIKey != "good" || provider.APIKeyEnv != "" {
		t.Fatalf("persisted provider = %+v", provider)
	}

	t.Setenv(config.InferenceNetEnvVar, "good")
	if err := authCLI([]string{"inference-net", "login", "--env"}); err != nil {
		t.Fatal(err)
	}
	cfg, err = config.Load()
	if err != nil {
		t.Fatal(err)
	}
	provider = cfg.Providers[config.InferenceNetProvider]
	if provider.APIKey != "" || provider.APIKeyEnv != config.InferenceNetEnvVar {
		t.Fatalf("persisted env provider = %+v", provider)
	}
	if err := inferencenet.SaveAuth(inferencenet.Auth{UserEmail: "user@example.com", ProjectID: "project", ProjectName: "Project", MachineKey: "key", MachineKeyName: "whip-test"}); err != nil {
		t.Fatal(err)
	}
	if err := inferenceNetStatusCLI(); err != nil {
		t.Fatal(err)
	}
}

func TestCLIChooser(t *testing.T) {
	withInput := func(input string, choose func() (string, error)) (string, error) {
		t.Helper()
		reader, writer, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := writer.WriteString(input); err != nil {
			t.Fatal(err)
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		old := os.Stdin
		os.Stdin = reader
		defer func() {
			os.Stdin = old
			_ = reader.Close()
		}()
		return choose()
	}

	for _, test := range []struct {
		name    string
		input   string
		options []string
		want    string
		wantErr bool
	}{
		{name: "free text", input: "project\n", want: "project"},
		{name: "default", input: "\n", options: []string{"first", "second"}, want: "first"},
		{name: "selection", input: "2\n", options: []string{"first", "second"}, want: "second"},
		{name: "invalid", input: "3\n", options: []string{"first", "second"}, wantErr: true},
		{name: "closed input", wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := withInput(test.input, func() (string, error) {
				return cliChooser("project", "Choose", test.options)
			})
			if (err != nil) != test.wantErr || got != test.want {
				t.Fatalf("choice=%q err=%v", got, err)
			}
		})
	}
}
