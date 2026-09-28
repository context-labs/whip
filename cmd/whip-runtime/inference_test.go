package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/inferenceauth"
	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/session"
)

func TestConfiguredInferenceUsesOnlyExplicitManagedSource(t *testing.T) {
	directory := t.TempDir()
	host := config.Default()
	host.Providers["managed"] = config.Provider{Kind: "openai-chat", BaseURL: "https://api.inference.net/v1", CredentialSource: "inference-net"}
	host.Providers["plain"] = config.Provider{Kind: "openai-chat", BaseURL: "https://api.inference.net/v1"}
	host.Providers["environment"] = config.Provider{Kind: "openai-chat", BaseURL: "https://api.inference.net/v1", CredentialEnv: "WHIP_INFERENCE_FIXTURE_KEY"}
	if err := config.Save(directory, host); err != nil {
		t.Fatal(err)
	}
	manager, err := inferenceauth.New(t.Context(), directory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = manager.Close() })
	credential := inferenceauth.Credentials{Scope: inferenceauth.Scope{TeamID: "team", ProjectID: "project"}, MachineKey: inferenceauth.MachineKey{ID: "machine", Value: "private-machine"}}
	if err := manager.Install(t.Context(), manager.Generation(), credential); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WHIP_INFERENCE_FIXTURE_KEY", "private-env")
	// An undeclared credential variable must never act as an implicit fallback.
	t.Setenv("INFERENCE_API_KEY", "private-implicit")
	provider := configuredProvider(directory, nil, manager)
	request := model.Request{Selection: session.ModelSelection{Name: "model"}, Messages: []model.Message{{Role: session.User, Parts: []session.Part{{Type: "text", Text: "hello"}}}}}
	for _, test := range []struct{ route, header string }{{"managed", "Bearer private-machine"}, {"plain", ""}, {"environment", "Bearer private-env"}} {
		t.Run(test.route, func(t *testing.T) {
			calls := 0
			provider.Client = &http.Client{Transport: subscriptionTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.URL.String() != "https://api.inference.net/v1/chat/completions" || r.Header.Get("Authorization") != test.header {
					t.Error("route inferred the wrong credential source")
				}
				return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"choices":[{"message":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}`))}, nil
			})}
			request.Selection.Provider = test.route
			prepared, err := provider.Prepare(t.Context(), request)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := prepared.Execute(t.Context(), nil); err != nil || calls != 1 {
				t.Fatalf("explicit source execution: %v", err)
			}
			raw, err := json.Marshal(prepared.Snapshot)
			if err != nil || strings.Contains(string(raw), "private-") {
				t.Fatal("private credential entered durable snapshot")
			}
		})
	}
	if _, err := manager.Logout(); err != nil {
		t.Fatal(err)
	}
	request.Selection.Provider = "managed"
	if _, err := provider.Prepare(t.Context(), request); !errors.Is(err, inferenceauth.ErrKeyRequired) {
		t.Fatalf("managed logout fell back to implicit credential: %v", err)
	}
	if err := manager.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "inference-net.json"), []byte("invalid-private-file"), 0o600); err != nil {
		t.Fatal(err)
	}
	unread, err := inferenceauth.New(t.Context(), directory)
	if err != nil {
		t.Fatal(err)
	}
	defer unread.Close()
	provider = configuredProvider(directory, nil, unread)
	for _, route := range []string{"plain", "environment"} {
		request.Selection.Provider = route
		if _, err := provider.Prepare(t.Context(), request); err != nil {
			t.Fatalf("unrelated route read Inference.net record: %v", err)
		}
	}
	request.Selection.Provider = "managed"
	if _, err := provider.Prepare(t.Context(), request); !errors.Is(err, inferenceauth.ErrStorage) {
		t.Fatalf("managed route accepted malformed private file: %v", err)
	}
}

type readyWriter struct{ cancel context.CancelFunc }

func (w readyWriter) Write(raw []byte) (int, error) {
	w.cancel()
	return len(raw), nil
}

func TestCommandStartsWithoutReadingUnusedInferenceCredentials(t *testing.T) {
	t.Setenv("TMPDIR", "/tmp")
	directory := t.TempDir()
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "inference-net.json"), []byte("invalid-private-file"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var diagnostics bytes.Buffer
	err := run(ctx, []string{"-directory", directory, "-scripted"}, readyWriter{cancel: cancel}, &diagnostics)
	if ctx.Err() == nil || err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("unrelated private file blocked command readiness: %v", err)
	}
	if strings.Contains(diagnostics.String(), "invalid-private-file") {
		t.Fatal("command exposed private file")
	}
}
