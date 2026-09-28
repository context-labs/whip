package main

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/session"
)

func TestHostRoutesRefreshOnlyForNewPreparedCalls(t *testing.T) {
	directory := t.TempDir()
	host := config.Default()
	host.Providers["fixture"] = config.Provider{
		Kind: "openai-chat", BaseURL: "https://first.example/v1", CredentialEnv: "WHIP_V4_FIXTURE_TOKEN",
		Models: map[string]config.Model{"model": {MaxOutputTokens: 77, TimeoutMillis: 1234, MaxAttempts: 2}},
	}
	if err := config.Save(directory, host); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WHIP_V4_FIXTURE_TOKEN", "credential-must-not-persist")
	provider := configuredProvider(directory, nil, nil)
	request := model.Request{
		Selection: session.ModelSelection{Provider: "fixture", Name: "model"},
		Messages:  []model.Message{{Role: session.User, Parts: []session.Part{{Type: "text", Text: "hello"}}}},
	}
	first, err := provider.Prepare(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	route := host.Providers["fixture"]
	route.BaseURL = "https://second.example/v1"
	route.Kind = "openai-responses"
	host.Providers["fixture"] = route
	if err := config.Save(directory, host); err != nil {
		t.Fatal(err)
	}
	second, err := provider.Prepare(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	if first.Snapshot.Route != "https://first.example/v1/chat/completions" || second.Snapshot.Route != "https://second.example/v1/responses" || second.Snapshot.Adapter != "openai-responses" || second.Snapshot.MaxOutputTokens != 77 || second.Snapshot.TimeoutMillis != 1234 || second.MaxAttempts != 2 {
		t.Fatalf("prepared snapshots: %+v %+v", first.Snapshot, second.Snapshot)
	}
	raw, err := json.Marshal(second.Snapshot)
	if err != nil || strings.Contains(string(raw), "credential-must-not-persist") {
		t.Fatal("credential entered snapshot")
	}
	t.Setenv("WHIP_V4_FIXTURE_TOKEN", "")
	if _, err := provider.Prepare(t.Context(), request); err == nil {
		t.Fatal("unset credential accepted")
	}
}

func TestPreparedGenericCredentialsFreezeAcrossBothAdaptersAndRetries(t *testing.T) {
	for _, adapter := range []string{"openai-chat", "openai-responses"} {
		for _, source := range []string{"env", "file", "command", "none"} {
			t.Run(adapter+"/"+source, func(t *testing.T) {
				directory, err := filepath.EvalSymlinks(t.TempDir())
				if err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(directory, 0o700); err != nil {
					t.Fatal(err)
				}
				keyFile := filepath.Join(directory, "key")
				const envName = "WHIP_PREPARED_CREDENTIAL"
				setKey := func(value string) {
					t.Helper()
					t.Setenv(envName, value)
					if err := os.WriteFile(keyFile, []byte(value+"\n"), 0o600); err != nil {
						t.Fatal(err)
					}
				}
				setKey("private-first-key")
				host := config.Default()
				route := config.Provider{Kind: adapter, BaseURL: "https://fixture.example/v1", CredentialSource: source}
				switch source {
				case "env":
					route.CredentialEnv = envName
				case "file":
					route.CredentialFile = keyFile
				case "command":
					route.CredentialCommand = &config.CredentialCommand{Executable: "/bin/cat", Arguments: []string{keyFile}}
				}
				host.Providers["fixture"] = route
				if err := config.Save(directory, host); err != nil {
					t.Fatal(err)
				}
				provider := configuredProvider(directory, nil, nil)
				calls := 0
				expected := "Bearer private-first-key"
				if source == "none" {
					expected = ""
				}
				provider.Client = &http.Client{Transport: subscriptionTransport(func(request *http.Request) (*http.Response, error) {
					calls++
					if request.Header.Get("Authorization") != expected {
						t.Error("dispatch resolved a credential again or used an implicit source")
					}
					status, raw := http.StatusOK, `{"choices":[{"message":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}`
					if adapter == "openai-responses" {
						raw = `{"status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"done"}]}]}`
					}
					if calls == 1 {
						status, raw = http.StatusServiceUnavailable, `{"error":{"message":"private-response"}}`
					}
					return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(raw))}, nil
				})}
				request := model.Request{Selection: session.ModelSelection{Provider: "fixture", Name: "model"}, Messages: []model.Message{{Role: session.User, Parts: []session.Part{{Type: "text", Text: "hello"}}}}}
				first, err := provider.Prepare(t.Context(), request)
				if err != nil || calls != 0 {
					t.Fatal("preparation failed or dispatched", err)
				}
				setKey("private-second-key")
				if _, err := first.Execute(t.Context(), nil); err == nil {
					t.Fatal("fixture did not produce a retryable failure")
				} else {
					var failure *model.CallError
					if !errors.As(err, &failure) || !failure.Retryable || strings.Contains(err.Error(), "private") {
						t.Fatal("retry failure lost safe classification", err)
					}
				}
				if _, err := first.Execute(t.Context(), nil); err != nil || calls != 2 {
					t.Fatal("frozen retry failed", err)
				}
				second, err := provider.Prepare(t.Context(), request)
				if err != nil {
					t.Fatal(err)
				}
				if source != "none" {
					expected = "Bearer private-second-key"
				}
				t.Setenv(envName, "")
				if err := os.Remove(keyFile); err != nil {
					t.Fatal(err)
				}
				if _, err := second.Execute(t.Context(), nil); err != nil || calls != 3 {
					t.Fatal("prepared credentials changed after source removal", err)
				}
				if _, err := provider.Prepare(t.Context(), request); (err == nil) != (source == "none") || calls != 3 {
					t.Fatal("new call reused a removed key or preparation dispatched", err)
				}
				for _, prepared := range []model.Prepared{first, second} {
					raw, err := json.Marshal(prepared.Snapshot)
					if err != nil || strings.Contains(string(raw), "private-") || strings.Contains(string(raw), keyFile) || strings.Contains(string(raw), envName) {
						t.Fatal("credential material entered request evidence")
					}
				}
				raw, err := os.ReadFile(filepath.Join(directory, config.FileName))
				if err != nil || strings.Contains(string(raw), "private-first-key") || strings.Contains(string(raw), "private-second-key") {
					t.Fatal("resolved credential entered host configuration")
				}
			})
		}
	}
}
