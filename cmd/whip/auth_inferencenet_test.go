package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/context-labs/whip/internal/protocol"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/inferenceauth"
)

func TestAuthInferenceNetDispatch(t *testing.T) {
	for _, args := range [][]string{{"bogus"}, {"key"}, {"key", "rotate", "extra"}, {"status", "extra"}, {"logout", "extra"}, {"login", "--key", "x", "--env"}, {"flow", "bad", "id"}} {
		if err := authInferenceNetCLI(args); err == nil {
			t.Fatalf("invalid args accepted: %v", args)
		}
	}
	if err := authCLI([]string{"inference", "bogus"}); err == nil {
		t.Fatal("invalid alias operation accepted")
	}
}

func TestAuthInferenceNetBYOKNoKey(t *testing.T) {
	t.Setenv(inferenceEnvironment, "")
	if err := authCLI([]string{"inference-net", "login", "--key", ""}); err == nil {
		t.Fatal("empty key accepted")
	}
}

func TestAuthInferenceNetStatusAndLogoutUnsigned(t *testing.T) {
	useNativeAuth(t, nil)
	if err := authCLI([]string{"inference-net", "status"}); err != nil {
		t.Fatal(err)
	}
	if err := authCLI([]string{"inference-net", "logout"}); err != nil {
		t.Fatal(err)
	}
	if err := authCLI([]string{"inference-net", "key", "rotate"}); err == nil {
		t.Fatal("unsigned rotation accepted")
	}
}

func TestAuthInferenceNetLogoutClearsStoredAuthAndReportsPendingCleanup(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "private-remote-failure", http.StatusInternalServerError)
	}))
	defer server.Close()
	redirectAuthRequests(t, server.URL, "observability-api.inference.net", "")
	directory := useNativeAuth(t, func(directory string) {
		manager, err := inferenceauth.New(t.Context(), directory)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = manager.Close() }()
		if err := manager.Install(t.Context(), manager.Generation(), inferenceauth.Credentials{Management: inferenceauth.Management{Token: "private-session", UserID: "user", Email: "dev@example.com"}, Scope: inferenceauth.Scope{TeamID: "team", ProjectID: "project"}, MachineKey: inferenceauth.MachineKey{ID: "key", Value: "private-key"}}); err != nil {
			t.Fatal(err)
		}
	})
	output := invokeMain(t, "auth", "inference-net", "logout")
	if !strings.Contains(output, "Remote cleanup pending") || strings.Contains(output, "private-") {
		t.Fatal(output)
	}
	manager, err := inferenceauth.New(t.Context(), directory)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = manager.Close() }()
	auth, err := manager.Snapshot()
	if err != nil || auth.Management.Token != "" || auth.MachineKey.Value != "" {
		t.Fatal("local logout failed", err)
	}
	cleanup := invokeMain(t, "auth", "inference-net", "cleanup")
	if !strings.Contains(cleanup, "key=pending") || !strings.Contains(cleanup, "session=pending") {
		t.Fatal(cleanup)
	}
}

func TestAuthInferenceNetBYOKValidatesAndPersists(t *testing.T) {
	t.Setenv(inferenceEnvironment, "")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" || r.Header.Get("Authorization") != "Bearer good" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		fmt.Fprint(w, `{"data":[{"id":"kimi-k3-fast"}]}`)
	}))
	defer server.Close()
	redirectAuthRequests(t, server.URL, "api.inference.net", "/v1")
	directory := useNativeAuth(t, nil)
	if err := authCLI([]string{"inference-net", "login", "--key", "bad"}); err == nil {
		t.Fatal("bad key accepted")
	}
	if err := authCLI([]string{"inference-net", "login", "--key", " good\n"}); err != nil {
		t.Fatal(err)
	}
	host, err := config.Load(directory)
	if err != nil {
		t.Fatal(err)
	}
	p := host.Providers[inferenceProvider]
	key, err := os.ReadFile(p.CredentialFile)
	if err != nil || string(key) != "good" || p.CredentialSource != "file" {
		t.Fatal("private key missing", err)
	}
	t.Setenv(inferenceEnvironment, "good")
	if err := authCLI([]string{"inference-net", "login", "--env"}); err != nil {
		t.Fatal(err)
	}
	host, err = config.Load(directory)
	if err != nil {
		t.Fatal(err)
	}
	p = host.Providers[inferenceProvider]
	if p.CredentialEnv != inferenceEnvironment || p.CredentialFile != "" || p.CredentialSource != "env" {
		t.Fatal("environment reference lost")
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

func TestProviderChoiceUsesStableIDsWithDuplicateNames(t *testing.T) {
	for _, test := range []struct {
		name, input, want string
		choices           []providerChoice
		wantErr           bool
	}{
		{name: "no workspaces", wantErr: true},
		{name: "one workspace", choices: []providerChoice{{ID: "team-1", Name: "Work"}}, want: "team-1"},
		{name: "duplicate names", input: "2\n", choices: []providerChoice{{ID: "team-1", Name: "Work"}, {ID: "team-2", Name: "Work"}}, want: "team-2"},
		{name: "new project", input: "2\n", choices: []providerChoice{{ID: "project-1", Name: "Existing"}, {Name: "+ Create new project"}}},
		{name: "invalid choice", input: "3\n", choices: []providerChoice{{ID: "team-1"}, {ID: "team-2"}}, wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			reader, writer, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			if _, err := writer.WriteString(test.input); err != nil {
				t.Fatal(err)
			}
			_ = writer.Close()
			previous := os.Stdin
			os.Stdin = reader
			t.Cleanup(func() { os.Stdin = previous; _ = reader.Close() })
			got, err := chooseProviderID("workspace", test.choices)
			if (err != nil) != test.wantErr || got != test.want {
				t.Fatalf("selected %q, error %v; want %q, error %t", got, err, test.want, test.wantErr)
			}
		})
	}
}

func TestAuthInferenceNetDeviceLoginAndKeyRotation(t *testing.T) {
	t.Setenv(inferenceEnvironment, "")
	mux := http.NewServeMux()
	mux.HandleFunc("/api/auth/device/code", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"device_code":"device","user_code":"CODE","expires_in":30,"interval":1}`)
	})
	mux.HandleFunc("/api/auth/device/token", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"access_token":"session-token"}`)
	})
	mux.HandleFunc("/api/auth/get-session", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"user":{"email":"dev@example.com","id":"user-1"}}`)
	})
	mux.HandleFunc("/api/auth/organization/list", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `[{"id":"user-1","name":"Personal","slug":"personal"}]`)
	})
	mux.HandleFunc("/api/auth/organization/set-active", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{}`)
	})
	mux.HandleFunc("/api/rest/projects", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `[{"id":"project-1","name":"Primary"}]`)
	})
	mux.HandleFunc("/api/rest/api-keys", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"id":"key-1","key":"machine-secret"}`)
	})
	mux.HandleFunc("/api/rest/api-keys/key-1", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{}`)
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	redirectAuthRequests(t, server.URL, "observability-api.inference.net", "")
	directory := useNativeAuth(t, nil)
	t.Setenv("PATH", t.TempDir()) // openBrowser reports false without launching an app

	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.WriteString("\n"); err != nil {
		t.Fatal(err)
	}
	_ = writer.Close()
	previousInput := os.Stdin
	os.Stdin = reader
	defer func() {
		os.Stdin = previousInput
		_ = reader.Close()
	}()

	if err := authCLI([]string{"inference-net", "login"}); err != nil {
		t.Fatal(err)
	}
	manager, err := inferenceauth.New(t.Context(), directory)
	if err != nil {
		t.Fatal(err)
	}
	auth, err := manager.Snapshot()
	_ = manager.Close()
	if err != nil || auth.Scope.ProjectID != "project-1" || auth.MachineKey.Value != "machine-secret" {
		t.Fatal("device credential not published", err)
	}
	if err := authCLI([]string{"inference-net", "key", "rotate"}); err != nil {
		t.Fatal(err)
	}
	manager, err = inferenceauth.New(t.Context(), directory)
	if err != nil {
		t.Fatal(err)
	}
	rotated, err := manager.Snapshot()
	_ = manager.Close()
	if err != nil || rotated.MachineKey.ID != "key-1" || rotated.MachineKey.Value == "" {
		t.Fatal("rotated credential missing", err)
	}
	host, err := config.Load(directory)
	if err != nil || host.Providers[inferenceProvider].CredentialSource != "inference-net" {
		t.Fatal("managed route missing", err)
	}
}

func TestAuthInferenceUncertainCreationIsInspectableWithoutAutomaticReplay(t *testing.T) {
	t.Setenv(inferenceEnvironment, "")
	t.Setenv("PATH", t.TempDir())
	var creations atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		responses := map[string]string{
			"/api/auth/device/code":             `{"device_code":"device","user_code":"CODE","expires_in":30,"interval":1}`,
			"/api/auth/device/token":            `{"access_token":"session-token"}`,
			"/api/auth/get-session":             `{"user":{"id":"user","email":"dev@example.com"}}`,
			"/api/auth/organization/list":       `[{"id":"team","name":"Team"}]`,
			"/api/auth/organization/set-active": `{}`,
			"/api/rest/projects":                `[{"id":"project","name":"Project"}]`,
		}
		if r.URL.Path == "/api/rest/api-keys" {
			creations.Add(1)
			fmt.Fprint(w, `{"id":"created-but-secret-response-lost"}`)
			return
		}
		if body, ok := responses[r.URL.Path]; ok {
			fmt.Fprint(w, body)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	redirectAuthRequests(t, server.URL, "observability-api.inference.net", "")
	useNativeAuth(t, nil)
	withStdin(t, "\n")
	var loginErr error
	captureStdout(t, func() { loginErr = authCLI([]string{"inference-net", "login"}) })
	if loginErr == nil || !strings.Contains(loginErr.Error(), "uncertain") || creations.Load() != 1 {
		t.Fatal(loginErr, creations.Load())
	}
	c, err := connectNativeRuntime(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	var flows protocol.InferenceFlowsResult
	if err := c.Call(t.Context(), "accounts.inference.list", protocol.EmptyParams{}, &flows); err != nil || len(flows.Items) != 1 || flows.Items[0].State != "uncertain" {
		t.Fatal(flows, err)
	}
	id := flows.Items[0].ID
	if err := authInferenceNetCLI([]string{"flow", "get", id}); err != nil {
		t.Fatal(err)
	}
	if err := authInferenceNetCLI([]string{"flow", "wait", id}); err == nil || !strings.Contains(err.Error(), "uncertain") {
		t.Fatal(err)
	}
	if err := authInferenceNetCLI([]string{"flow", "retry", id}); err == nil {
		t.Fatal("uncertain remote creation retried")
	}
	if creations.Load() != 1 {
		t.Fatal("inspection or explicit unavailable retry repeated key creation")
	}
}
