package rpc_test

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/client"
	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/inferenceaccount"
	"github.com/context-labs/whip/internal/inferenceauth"
	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/rpc"
	"github.com/context-labs/whip/internal/runtime"
)

func inferenceSocket(t *testing.T, r *runtime.Runtime, host rpc.HostServices) (*client.Client, func()) {
	t.Helper()
	server, err := rpc.Listen(r, host)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := server.Serve(ctx); err != nil {
			t.Error(err)
		}
	}()
	stop := sync.OnceFunc(func() { cancel(); <-done })
	t.Cleanup(stop)
	c, err := client.Connect(t.Context(), r.SocketPath(), nil)
	if err != nil {
		t.Fatal(err)
	}
	return c, stop
}

func inferenceService(t *testing.T, r *runtime.Runtime, authority *config.Authority, handler http.HandlerFunc) (*inferenceaccount.Service, *inferenceauth.Manager) {
	t.Helper()
	manager, err := inferenceauth.New(t.Context(), filepath.Dir(r.SocketPath()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := manager.Close(); err != nil {
			t.Error(err)
		}
	})
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	target, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	transport := accountTransport(func(request *http.Request) (*http.Response, error) {
		if request.URL.Scheme+"://"+request.URL.Host != "https://observability-api.inference.net" {
			return nil, errors.New("unexpected account route")
		}
		local := request.Clone(request.Context())
		local.URL.Scheme, local.URL.Host = target.Scheme, target.Host
		return server.Client().Transport.RoundTrip(local)
	})
	service, err := inferenceaccount.New(t.Context(), manager, &http.Client{Transport: transport}, func(ctx context.Context) error {
		current, err := authority.Snapshot(ctx)
		if err != nil {
			return err
		}
		_, err = authority.Update(ctx, current.Revision, func(host *config.Host) error { return host.EnsureInference() })
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(service.Close)
	return service, manager
}

func inferenceState(t *testing.T, c *client.Client, id, wanted string) protocol.InferenceFlow {
	t.Helper()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		value := call[protocol.InferenceFlow](t, c, "accounts.inference.get", protocol.InferenceFlowParams{FlowID: id})
		if value.State == wanted {
			return value
		}
		select {
		case <-deadline.C:
			t.Fatalf("wanted %s, got %+v", wanted, value)
		case <-ticker.C:
		}
	}
}

func TestInferenceSocketLostDeliveryChoicesAndCleanup(t *testing.T) {
	r, _, authority, _ := accountFixture(t)
	started, release := make(chan struct{}), make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	defer unblock()
	var minted atomic.Int32
	var failCleanup atomic.Bool
	failCleanup.Store(true)
	service, manager := inferenceService(t, r, authority, func(w http.ResponseWriter, request *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/api/auth/device/code":
			close(started)
			select {
			case <-release:
			case <-request.Context().Done():
				return
			}
			_, _ = io.WriteString(w, `{"device_code":"private-device","user_code":"PUBLIC-CODE","expires_in":900,"interval":1}`)
		case "/api/auth/device/token":
			_, _ = io.WriteString(w, `{"access_token":"private-management"}`)
		case "/api/auth/get-session":
			_, _ = io.WriteString(w, `{"user":{"id":"user","email":"person@example.test"}}`)
		case "/api/auth/organization/list":
			_, _ = io.WriteString(w, `[{"id":"one","name":"One","slug":"one"},{"id":"two","name":"Two","slug":"two"}]`)
		case "/api/auth/organization/set-active":
			_, _ = io.WriteString(w, `{}`)
		case "/api/rest/projects":
			_, _ = io.WriteString(w, `[{"id":"first","name":"First"},{"id":"second","name":"Second"}]`)
		case "/api/rest/api-keys":
			minted.Add(1)
			_, _ = io.WriteString(w, `{"id":"key-id","key":"private-machine"}`)
		case "/api/rest/api-keys/key-id":
			if failCleanup.Load() {
				w.WriteHeader(http.StatusInternalServerError)
			}
			_, _ = io.WriteString(w, `{}`)
		case "/api/auth/sign-out":
			_, _ = io.WriteString(w, `{}`)
		default:
			t.Errorf("unexpected account request %s", request.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})
	host := rpc.HostServices{Inference: service, Config: authority}
	c, stop := inferenceSocket(t, r, host)
	before, err := authority.Snapshot(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	conn, err := (&net.Dialer{}).DialContext(t.Context(), "unix", r.SocketPath())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	encoder := json.NewEncoder(conn)
	if err := encoder.Encode(protocol.Request{JSONRPC: "2.0", ID: "init", Method: "initialize", Params: json.RawMessage(`{"major":4}`)}); err != nil {
		t.Fatal(err)
	}
	if scanner := bufio.NewScanner(conn); !scanner.Scan() {
		t.Fatal("initialize missing", scanner.Err())
	}
	if err := encoder.Encode(protocol.Request{JSONRPC: "2.0", ID: "lost", Method: "accounts.inference.begin", Params: json.RawMessage(`{}`)}); err != nil {
		t.Fatal(err)
	}
	accountSignal(t, started)
	_ = conn.Close()
	found := call[protocol.InferenceFlowsResult](t, c, "accounts.inference.list", protocol.EmptyParams{})
	if len(found.Items) != 1 {
		t.Fatalf("accepted flow not recoverable: %+v", found)
	}
	id := found.Items[0].ID
	unblock()
	flow := inferenceState(t, c, id, "choose_team")
	if len(flow.Teams) != 2 || flow.VerificationURL != nil || flow.UserCode != nil {
		t.Fatalf("unsafe selection projection: %+v", flow)
	}
	_ = call[protocol.InferenceFlow](t, c, "accounts.inference.team", protocol.InferenceTeamParams{FlowID: id, TeamID: "two"})
	flow = inferenceState(t, c, id, "choose_project")
	if len(flow.Projects) != 2 || *flow.TeamID != "two" {
		t.Fatal("selection did not retain stable team ID")
	}
	_ = call[protocol.InferenceFlow](t, c, "accounts.inference.project", protocol.InferenceProjectParams{FlowID: id, ProjectID: "second"})
	flow = inferenceState(t, c, id, "succeeded")
	status := call[protocol.InferenceAccountStatus](t, c, "accounts.inference.status", protocol.EmptyParams{})
	if status.ManagementState != "stored" || status.InferenceState != "stored" || status.RouteState != "configured" || *status.ProjectID != "second" || minted.Load() != 1 {
		t.Fatalf("wrong account outcome: %+v", status)
	}
	after, err := authority.Snapshot(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before.Host.Defaults, after.Host.Defaults) {
		t.Fatal("onboarding changed model defaults")
	}
	_ = call[protocol.InferenceAccountStatus](t, c, "accounts.inference.setup", protocol.EmptyParams{})
	again, err := authority.Snapshot(t.Context())
	if err != nil || again.Revision != after.Revision {
		t.Fatal("setup was not idempotent", err)
	}
	loggedOut := call[protocol.InferenceLogoutResult](t, c, "accounts.inference.logout", protocol.EmptyParams{})
	if loggedOut.LocalFailure != nil || loggedOut.CleanupFailure == nil || !loggedOut.Status.CleanupPending || loggedOut.Status.InferenceState != "absent" {
		t.Fatalf("cleanup lost local revoke: %+v", loggedOut)
	}
	if _, err := manager.Capture(t.Context()); !errors.Is(err, inferenceauth.ErrKeyRequired) {
		t.Fatal("local key survived logout", err)
	}
	pending := call[protocol.InferenceCleanupResult](t, c, "accounts.inference.retry_cleanup", protocol.EmptyParams{})
	if pending.Failure == nil || len(pending.Items) != 1 || pending.Items[0].KeyState != "pending" {
		t.Fatalf("cleanup failure projection lost: %+v", pending)
	}
	failCleanup.Store(false)
	cleaned := call[protocol.InferenceCleanupResult](t, c, "accounts.inference.retry_cleanup", protocol.EmptyParams{})
	if cleaned.Failure != nil || len(cleaned.Items) != 1 || cleaned.Items[0].KeyState != "archived" || cleaned.Items[0].SessionState != "signed_out" {
		t.Fatalf("cleanup retry wrong: %+v", cleaned)
	}
	raw, _ := json.Marshal([]any{flow, status, loggedOut, pending, cleaned})
	if strings.Contains(string(raw), "private-") {
		t.Fatal("account secrets reached socket")
	}
	stop()
	service.Close()
	replacement, err := inferenceaccount.New(t.Context(), manager, nil, func(context.Context) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(replacement.Close)
	c, _ = inferenceSocket(t, r, rpc.HostServices{Inference: replacement, Config: authority})
	old := call[protocol.InferenceFlow](t, c, "accounts.inference.get", protocol.InferenceFlowParams{FlowID: id})
	if old.State != "interrupted" || old.Kind != nil || old.ExpiresAt != nil || len(old.Teams) != 0 {
		t.Fatalf("process restart fabricated flow: %+v", old)
	}
}

func TestInferenceSocketCancellationAndUnavailableService(t *testing.T) {
	r, _, authority, _ := accountFixture(t)
	started, release := make(chan struct{}), make(chan struct{})
	defer close(release)
	service, _ := inferenceService(t, r, authority, func(_ http.ResponseWriter, request *http.Request) {
		close(started)
		select {
		case <-request.Context().Done():
		case <-release:
		}
	})
	c, stop := inferenceSocket(t, r, rpc.HostServices{})
	var flow protocol.InferenceFlow
	var remote *client.Error
	err := c.Call(t.Context(), "accounts.inference.begin", protocol.EmptyParams{}, &flow)
	if !errors.As(err, &remote) || remote.Kind != "METHOD" {
		t.Fatal("missing host service silently accepted", err)
	}
	stop()
	c, _ = inferenceSocket(t, r, rpc.HostServices{Inference: service, Config: authority})
	flow = call[protocol.InferenceFlow](t, c, "accounts.inference.begin", protocol.EmptyParams{})
	accountSignal(t, started)
	cancelled := call[protocol.InferenceFlow](t, c, "accounts.inference.cancel", protocol.InferenceFlowParams{FlowID: flow.ID})
	if cancelled.State != "cancelled" || cancelled.UserCode != nil {
		t.Fatalf("cancel projection wrong: %+v", cancelled)
	}
}
