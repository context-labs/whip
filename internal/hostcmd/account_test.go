package hostcmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/client"
	"github.com/context-labs/whip/internal/protocol"
)

type commandReady chan []byte

func (ready commandReady) Write(raw []byte) (int, error) {
	ready <- bytes.Clone(raw)
	return len(raw), nil
}

func TestCommandAccountShutdown(t *testing.T) {
	for _, path := range []string{"/api/accounts/deviceauth/usercode", "/api/accounts/deviceauth/token"} {
		t.Run(strings.TrimPrefix(path, "/api/accounts/deviceauth/"), func(t *testing.T) {
			t.Setenv("TMPDIR", "/tmp")
			directory := t.TempDir()
			if err := os.Chmod(directory, 0o700); err != nil {
				t.Fatal(err)
			}
			started, cancelled, release, exited := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
			unblock := sync.OnceFunc(func() { close(release) })
			defer unblock()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				if r.URL.Path == path {
					close(started)
					select {
					case <-r.Context().Done():
					case <-release:
					}
					return
				}
				if r.URL.Path != "/api/accounts/deviceauth/usercode" {
					t.Error("unexpected auth request")
				}
				_, _ = w.Write([]byte(`{"device_auth_id":"private-device-id","user_code":"SAFE-CODE","interval":1}`))
			}))
			t.Cleanup(server.Close)
			endpoint, err := url.Parse(server.URL)
			if err != nil {
				t.Fatal(err)
			}
			original := http.DefaultTransport
			local := server.Client().Transport
			http.DefaultTransport = subscriptionTransport(func(r *http.Request) (*http.Response, error) {
				if r.URL.Scheme != "https" || r.URL.Host != "auth.openai.com" {
					return nil, errors.New("unexpected auth destination")
				}
				request := r.Clone(r.Context())
				request.URL.Scheme, request.URL.Host = endpoint.Scheme, endpoint.Host
				response, err := local.RoundTrip(request)
				if r.URL.Path == path {
					close(cancelled)
					<-release
					close(exited)
				}
				return response, err
			})
			t.Cleanup(func() { http.DefaultTransport = original })
			ctx, cancel := context.WithCancel(t.Context())
			ready := make(commandReady, 1)
			var diagnostics bytes.Buffer
			result := make(chan error, 1)
			done := make(chan struct{})
			go func() {
				defer close(done)
				result <- Run(ctx, []string{"-directory", directory, "-scripted"}, ready, &diagnostics)
			}()
			t.Cleanup(func() {
				cancel()
				unblock()
				select {
				case <-done:
				case <-time.After(5 * time.Second):
					t.Error("command did not join account shutdown")
				}
			})
			var initial struct {
				Socket string `json:"socket"`
			}
			select {
			case raw := <-ready:
				if err := json.Unmarshal(raw, &initial); err != nil {
					t.Fatal(err)
				}
			case err := <-result:
				t.Fatal("command exited before ready", err)
			case <-time.After(5 * time.Second):
				t.Fatal("command not ready")
			}
			c, err := client.Connect(t.Context(), initial.Socket, nil)
			if err != nil {
				t.Fatal(err)
			}
			var status protocol.OpenAIAccountStatus
			if err := c.Call(t.Context(), "accounts.openai.status", protocol.EmptyParams{}, &status); err != nil || status.AuthState != "signed_out" {
				t.Fatal("lazy status failed", err)
			}
			select {
			case <-started:
				t.Fatal("startup/status began authentication")
			default:
			}
			var flow protocol.OpenAILoginFlow
			if err := c.Call(t.Context(), "accounts.openai.begin", protocol.EmptyParams{}, &flow); err != nil {
				t.Fatal(err)
			}
			select {
			case <-started:
			case <-time.After(5 * time.Second):
				t.Fatal("device request not started")
			}
			cancel()
			select {
			case <-cancelled:
			case <-time.After(5 * time.Second):
				t.Fatal("device request not cancelled")
			}
			select {
			case err := <-result:
				t.Fatal("command returned before account request joined", err)
			default:
			}
			unblock()
			select {
			case err := <-result:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("command did not stop")
			}
			select {
			case <-exited:
			default:
				t.Fatal("account request escaped command lifetime")
			}
			if strings.Contains(diagnostics.String(), "private-") {
				t.Fatal("auth secret reached command diagnostics")
			}
		})
	}
}
