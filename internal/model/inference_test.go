package model

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/inferenceauth"
	"github.com/context-labs/whip/internal/session"
)

func inferenceCredentials() inferenceauth.Credentials {
	return inferenceauth.Credentials{
		Management: inferenceauth.Management{Token: "private-management", UserID: "user", ExpiresAt: new(time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC))},
		Scope:      inferenceauth.Scope{TeamID: "team", ProjectID: "project"},
		MachineKey: inferenceauth.MachineKey{ID: "key", Value: "private-machine"},
	}
}

func inferenceManager(t *testing.T) *inferenceauth.Manager {
	t.Helper()
	manager, err := inferenceauth.New(t.Context(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := manager.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := manager.Install(t.Context(), manager.Generation(), inferenceCredentials()); err != nil {
		t.Fatal(err)
	}
	return manager
}

func inferenceProvider(auth InferenceAuth) OpenAI {
	return OpenAI{InferenceAuth: auth, Resolve: func(context.Context, session.ModelSelection) (Route, error) {
		return Route{Kind: "openai-chat", URL: "https://api.inference.net/v1", ManagedInference: true, MaxOutputTokens: 100, TimeoutMillis: 1000, MaxAttempts: 3}, nil
	}}
}

func localInferenceClient(t *testing.T, handler http.Handler) *http.Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	target, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	return &http.Client{Transport: contextLimitTransport(func(request *http.Request) (*http.Response, error) {
		if request.URL.String() != "https://api.inference.net/v1/chat/completions" {
			return nil, errors.New("unexpected inference gateway route")
		}
		local := request.Clone(request.Context())
		local.URL.Scheme, local.URL.Host = target.Scheme, target.Host
		return server.Client().Transport.RoundTrip(local)
	})}
}

func TestManagedInferenceFreezesRequestAndCapturesOnlyMachineKey(t *testing.T) {
	manager := inferenceManager(t)
	provider := inferenceProvider(manager)
	var bodies [][]byte
	var headers []http.Header
	provider.Client = localInferenceClient(t, http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Error(err)
		}
		bodies = append(bodies, body)
		headers = append(headers, request.Header.Clone())
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"done"},"finish_reason":"stop"}],"usage":{"prompt_tokens":20,"completion_tokens":1}}`)
	}))
	request := chatRequest()
	prepared, err := provider.Prepare(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	if prepared.BeforeDispatch == nil || prepared.RefreshCredentials != nil {
		t.Fatal("missing generation check or unexpected management refresh")
	}
	request.Messages[0].Parts[0].Text = "changed after preparation"
	provider.Resolve = func(context.Context, session.ModelSelection) (Route, error) {
		return Route{}, errors.New("must not resolve again")
	}
	for range 2 {
		if err := prepared.BeforeDispatch(t.Context()); err != nil {
			t.Fatal(err)
		}
		response, err := prepared.Execute(t.Context(), nil)
		if err != nil || len(response.Parts) != 1 || response.Parts[0].Text != "done" || response.Usage.Input == nil || *response.Usage.Input != 20 {
			t.Fatalf("managed completion: %v", err)
		}
	}
	if len(bodies) != 2 || string(bodies[0]) != string(bodies[1]) || strings.Contains(string(bodies[0]), "changed after preparation") {
		t.Fatal("recorded retry changed frozen request")
	}
	digest := sha256.Sum256(bodies[0])
	if prepared.Snapshot.RequestDigest != hex.EncodeToString(digest[:]) || prepared.Snapshot.Adapter != "openai-chat" {
		t.Fatal("managed key replaced the chat adapter or its request evidence")
	}
	for _, header := range headers {
		if header.Get("Authorization") != "Bearer private-machine" || header.Get("Chatgpt-Account-Id") != "" {
			t.Fatal("management or subscription authorization reached inference")
		}
	}
	raw, err := json.Marshal(prepared.Snapshot)
	if err != nil || strings.Contains(string(raw), "private-") || strings.Contains(string(bodies[0]), "private-") {
		t.Fatal("private credential entered request evidence")
	}
}

func TestManagedInferenceRejectsRevokedCaptureBeforeHTTP(t *testing.T) {
	for _, change := range []string{"same record", "key rotation", "team", "project", "logout", "close"} {
		t.Run(change, func(t *testing.T) {
			manager := inferenceManager(t)
			provider := inferenceProvider(manager)
			var calls atomic.Int32
			provider.Client = localInferenceClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.Header.Get("Authorization") != "Bearer replacement" {
					t.Error("new preparation did not capture replacement key")
				}
				_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}`)
			}))
			prepared, err := provider.Prepare(t.Context(), chatRequest())
			if err != nil {
				t.Fatal(err)
			}
			value := inferenceCredentials()
			switch change {
			case "logout":
				_, err = manager.Logout()
			case "close":
				err = manager.Close()
			default:
				switch change {
				case "key rotation":
					value.MachineKey.Value = "replacement"
				case "team":
					value.Scope.TeamID = "other-team"
				case "project":
					value.Scope.ProjectID = "other-project"
				}
				err = manager.Install(t.Context(), manager.Generation(), value)
			}
			if err != nil {
				t.Fatal(err)
			}
			expected := inferenceauth.ErrChanged
			if change == "close" {
				expected = inferenceauth.ErrClosed
			}
			if err := prepared.BeforeDispatch(t.Context()); !errors.Is(err, expected) {
				t.Fatalf("reservation reused revoked capture: %v", err)
			}
			if _, err := prepared.Execute(t.Context(), nil); !errors.Is(err, expected) {
				t.Fatalf("execution reused revoked capture: %v", err)
			}
			if calls.Load() != 0 {
				t.Fatal("revoked request reached HTTP")
			}
			if change == "key rotation" {
				fresh, err := provider.Prepare(t.Context(), chatRequest())
				if err != nil {
					t.Fatal(err)
				}
				if _, err := fresh.Execute(t.Context(), nil); err != nil || calls.Load() != 1 {
					t.Fatalf("new key unavailable to new preparation: %v", err)
				}
			}
		})
	}
}

func TestManagedInferenceRejectsEndpointChangesBeforeCredentialCapture(t *testing.T) {
	// A nil owner returns ErrKeyRequired if reached. Invalid routes must be
	// rejected independently, before a private credential could be captured.
	for _, test := range []struct{ kind, url, credential string }{
		{"openai-chat", "http://api.inference.net/v1", ""},
		{"openai-chat", "https://api.inference.net/v1/", ""},
		{"openai-chat", "https://api.inference.net:443/v1", ""},
		{"openai-chat", "https://api.inference.net/v1/other", ""},
		{"openai-chat", "https://other.example/v1", ""},
		{"openai-chat", "https://api.inference.net/v1", "other-secret"},
		{"openai-responses", "https://api.inference.net/v1", ""},
		{"openai-codex", "", ""},
	} {
		t.Run(test.kind+test.url+test.credential, func(t *testing.T) {
			provider := inferenceProvider(nil)
			provider.Resolve = func(context.Context, session.ModelSelection) (Route, error) {
				return Route{Kind: test.kind, URL: test.url, Credential: test.credential, ManagedInference: true, MaxOutputTokens: 100, TimeoutMillis: 1000, MaxAttempts: 3}, nil
			}
			if _, err := provider.Prepare(t.Context(), chatRequest()); !errors.Is(err, session.ErrInvalid) {
				t.Fatalf("invalid route reached authorization: %v", err)
			}
		})
	}
}

func TestManagedInferenceManagementSessionCannotAuthorizeRequest(t *testing.T) {
	manager := inferenceManager(t)
	value := inferenceCredentials()
	value.MachineKey = inferenceauth.MachineKey{}
	if err := manager.Install(t.Context(), manager.Generation(), value); err != nil {
		t.Fatal(err)
	}
	if _, err := inferenceProvider(manager).Prepare(t.Context(), chatRequest()); !errors.Is(err, inferenceauth.ErrKeyRequired) {
		t.Fatalf("management token authorized inference: %v", err)
	}
	if _, err := inferenceProvider(nil).Prepare(t.Context(), chatRequest()); !errors.Is(err, inferenceauth.ErrKeyRequired) {
		t.Fatalf("missing manager used implicit authorization: %v", err)
	}
}
