package main

import (
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

func TestCLIChooser(t *testing.T) {
	for _, tc := range []struct {
		name    string
		input   string
		options []string
		want    string
		wantErr bool
	}{
		{name: "defaults to first option", input: "\n", options: []string{"personal", "work"}, want: "personal"},
		{name: "selects numbered option", input: "2\n", options: []string{"personal", "work"}, want: "work"},
		{name: "rejects invalid choice", input: "3\n", options: []string{"personal", "work"}, wantErr: true},
		{name: "reads a project name", input: "new project\n", want: "new project"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, w, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			if _, err := w.WriteString(tc.input); err != nil {
				t.Fatal(err)
			}
			if err := w.Close(); err != nil {
				t.Fatal(err)
			}
			original := os.Stdin
			os.Stdin = r
			t.Cleanup(func() {
				os.Stdin = original
				_ = r.Close()
			})

			got, err := cliChooser("project", "Choose a project", tc.options)
			if (err != nil) != tc.wantErr {
				t.Fatalf("cliChooser() error = %v, want error=%t", err, tc.wantErr)
			}
			if got != tc.want {
				t.Fatalf("cliChooser() = %q, want %q", got, tc.want)
			}
		})
	}
}
