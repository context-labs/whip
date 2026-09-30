// This test-only entrypoint keeps the production host and replaces external HTTP
// with deterministic fake credentials/account responses. It cannot contact a
// real provider. Loopback remains available to the existing native fixture.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"

	"github.com/context-labs/whip/internal/engine/process"
	"github.com/context-labs/whip/internal/hostcmd"
)

type transport struct {
	loopback  http.RoundTripper
	directory string
	mu        sync.Mutex
	count     int
}

func (t *transport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Hostname() == "127.0.0.1" || r.URL.Hostname() == "localhost" {
		return t.loopback.RoundTrip(r)
	}
	var control struct {
		Login          string `json:"login"`
		CleanupFailure bool   `json:"cleanupFailure"`
	}
	raw, err := os.ReadFile(filepath.Join(t.directory, "provider-control.json"))
	if err != nil || len(raw) > 4096 || json.Unmarshal(raw, &control) != nil {
		return nil, errors.New("invalid fixture control")
	}
	t.mu.Lock()
	t.count++
	if t.count > 2048 {
		t.mu.Unlock()
		return nil, errors.New("fixture request limit")
	}
	log, err := os.OpenFile(filepath.Join(t.directory, "provider-requests.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err == nil {
		_, err = fmt.Fprintln(log, r.Method+" "+r.URL.Host+r.URL.Path)
		_ = log.Close()
	}
	t.mu.Unlock()
	if err != nil {
		return nil, err
	}
	status, body := 200, `{}`
	switch r.URL.Host {
	case "api.inference.net", "openrouter.ai":
		if r.URL.Path != "/v1/models" && r.URL.Path != "/api/v1/models" && r.URL.Path != "/api/v1/key" {
			return nil, errors.New("fixture forbids inference")
		}
		if r.Header.Get("Authorization") == "Bearer fixture-invalid-key" {
			status, body = 401, `{"error":"Invalid fixture key"}`
		} else {
			body = `{"data":[{"id":"fixture-model","context_length":8192,"reasoning_efforts":["off","high"]}]}`
		}
	case "observability-api.inference.net":
		switch r.URL.Path {
		case "/api/auth/device/code":
			body = `{"device_code":"fixture-device","user_code":"FIXTURE-CODE","expires_in":60,"interval":1}`
		case "/api/auth/device/token":
			switch control.Login {
			case "success":
				body = `{"access_token":"fixture-management"}`
			case "expired":
				status, body = 400, `{"error":"expired_token"}`
			default:
				status, body = 400, `{"error":"authorization_pending"}`
			}
		case "/api/auth/get-session":
			body = `{"user":{"id":"fixture-user","email":"person@example.test"}}`
		case "/api/auth/organization/list":
			body = `[{"id":"fixture-team","name":"Fixture team","slug":"fixture-team"}]`
		case "/api/auth/organization/set-active":
		case "/api/rest/projects":
			body = `[{"id":"fixture-project","name":"Fixture project"}]`
		case "/api/rest/api-keys":
			body = `{"id":"fixture-key-id","key":"fixture-machine-key"}`
		case "/api/rest/api-keys/fixture-key-id", "/api/auth/sign-out":
			if control.CleanupFailure {
				status = 503
			}
		default:
			return nil, errors.New("unexpected fixture account path")
		}
	default:
		return nil, errors.New("fixture forbids external network")
	}
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
}
func main() {
	if len(os.Args) > 1 && os.Args[1] == "_kernel" {
		if err := process.WorkerMain(os.Args[2:], os.Stdin, os.Stdout, nil); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	directory := os.Getenv("WHIP_PROVIDER_FIXTURE_DIRECTORY")
	if directory == "" {
		fmt.Fprintln(os.Stderr, "explicit fixture directory required")
		os.Exit(1)
	}
	http.DefaultTransport = &transport{loopback: http.DefaultTransport, directory: directory}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := hostcmd.Run(ctx, os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
