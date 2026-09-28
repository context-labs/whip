package rpc_test

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/account"
	"github.com/context-labs/whip/internal/client"
	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/openaiauth"
	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/rpc"
	"github.com/context-labs/whip/internal/runtime"
)

type accountTransport func(*http.Request) (*http.Response, error)

func (f accountTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func accountFixture(t *testing.T) (*runtime.Runtime, *openaiauth.Manager, *config.Authority, *account.Service) {
	t.Helper()
	t.Setenv("TMPDIR", "/tmp")
	directory := t.TempDir()
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	r, err := runtime.Open(t.Context(), directory, model.Scripted{}, runtime.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := r.Close(); err != nil {
			t.Error(err)
		}
	})
	auth := openaiauth.New(t.Context(), directory)
	t.Cleanup(auth.Close)
	authority, err := config.NewAuthority(directory)
	if err != nil {
		t.Fatal(err)
	}
	accounts, err := account.New(t.Context(), auth, authority)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(accounts.Close)
	return r, auth, authority, accounts
}

func accountSocket(t *testing.T, r *runtime.Runtime, accounts *account.Service) (*client.Client, func()) {
	t.Helper()
	server, err := rpc.Listen(r, accounts)
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

func fakeAccountIssuer(t *testing.T, handler http.HandlerFunc) *atomic.Int32 {
	t.Helper()
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		_, _ = io.Copy(io.Discard, r.Body)
		handler(w, r)
	}))
	t.Cleanup(server.Close)
	endpoint, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	original := http.DefaultTransport
	local := server.Client().Transport
	http.DefaultTransport = accountTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Scheme != "https" || r.URL.Host != "auth.openai.com" {
			return nil, errors.New("unexpected account destination")
		}
		request := r.Clone(r.Context())
		request.URL.Scheme, request.URL.Host = endpoint.Scheme, endpoint.Host
		return local.RoundTrip(request)
	})
	t.Cleanup(func() { http.DefaultTransport = original })
	return &requests
}

func accountResponse(t *testing.T, w http.ResponseWriter, r *http.Request) {
	t.Helper()
	switch r.URL.Path {
	case "/api/accounts/deviceauth/usercode":
		_, _ = w.Write([]byte(`{"device_auth_id":"private-device-id","user_code":"SAFE-CODE","interval":1}`))
	case "/api/accounts/deviceauth/token":
		_, _ = w.Write([]byte(`{"authorization_code":"private-code","code_verifier":"private-verifier"}`))
	case "/oauth/token":
		claims := base64.RawURLEncoding.EncodeToString([]byte(`{"https://api.openai.com/auth":{"chatgpt_account_id":"account","chatgpt_plan_type":"pro"}}`))
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "private-access." + claims + ".signature", "refresh_token": "private-refresh", "expires_in": 3600})
	default:
		t.Errorf("unexpected auth path %s", r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	}
}

func accountSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(5 * time.Second):
		t.Fatal("account boundary was not reached")
	}
}

func accountSettled(t *testing.T, c *client.Client, id string) protocol.OpenAILoginFlow {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		var result protocol.OpenAILoginFlow
		if err := c.Call(ctx, "accounts.openai.get", protocol.OpenAIFlowParams{FlowID: id}, &result); err != nil {
			t.Fatal(err)
		}
		if result.State != "authorizing" {
			return result
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			t.Fatal("login did not settle")
		}
	}
}

func TestAccountsSocketLostAcknowledgementAndProcessEpoch(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	defer unblock()
	requests := fakeAccountIssuer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/accounts/deviceauth/usercode" {
			close(started)
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
		}
		accountResponse(t, w, r)
	})
	r, auth, authority, accounts := accountFixture(t)
	c, stop := accountSocket(t, r, accounts)
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
	scanner := bufio.NewScanner(conn)
	if !scanner.Scan() {
		t.Fatal("initialize response missing", scanner.Err())
	}
	if err := encoder.Encode(protocol.Request{JSONRPC: "2.0", ID: "lost", Method: "accounts.openai.begin", Params: json.RawMessage(`{}`)}); err != nil {
		t.Fatal(err)
	}
	accountSignal(t, started)
	_ = conn.Close() // Accepted flow survives losing its socket acknowledgement.
	list := call[protocol.OpenAIFlowsResult](t, c, "accounts.openai.list", protocol.EmptyParams{})
	if len(list.Items) != 1 || list.Items[0].State != "authorizing" || list.Items[0].ExpiresAt == nil {
		t.Fatalf("lost flow not discoverable: %+v", list)
	}
	flow := call[protocol.OpenAILoginFlow](t, c, "accounts.openai.begin", protocol.EmptyParams{})
	if flow.ID != list.Items[0].ID || requests.Load() != 1 {
		t.Fatal("recovery started a second device flow")
	}
	unblock()
	finished := accountSettled(t, c, flow.ID)
	status := call[protocol.OpenAIAccountStatus](t, c, "accounts.openai.status", protocol.EmptyParams{})
	if finished.State != "succeeded" || finished.UserCode != nil || status.AuthState != "stored" || status.RouteState != "configured" || status.ExpiresAt == nil || requests.Load() != 3 {
		t.Fatalf("wrong account outcome: %+v %+v", finished, status)
	}
	raw, _ := json.Marshal([]any{finished, status, list})
	if strings.Contains(string(raw), "private-") || strings.Contains(string(raw), "device_auth_id") {
		t.Fatal("private auth state reached the socket")
	}
	before, err := authority.Snapshot(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	_ = call[protocol.OpenAIAccountStatus](t, c, "accounts.openai.setup", protocol.EmptyParams{})
	after, err := authority.Snapshot(t.Context())
	if err != nil || before.Revision != after.Revision || requests.Load() != 3 {
		t.Fatal("idempotent setup rewrote config or contacted auth", err)
	}
	stop()
	accounts.Close()
	restarted, err := account.New(t.Context(), auth, authority)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(restarted.Close)
	c, _ = accountSocket(t, r, restarted)
	old := call[protocol.OpenAILoginFlow](t, c, "accounts.openai.get", protocol.OpenAIFlowParams{FlowID: flow.ID})
	if old.State != "interrupted" || old.ExpiresAt != nil || old.UserCode != nil {
		t.Fatalf("old epoch lost: %+v", old)
	}
	status = call[protocol.OpenAIAccountStatus](t, c, "accounts.openai.logout", protocol.EmptyParams{})
	if status.AuthState != "signed_out" || status.RouteState != "configured" || requests.Load() != 3 {
		t.Fatalf("logout changed configuration or contacted auth: %+v", status)
	}
}

func TestAccountsSocketCancellationAndSafeErrors(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	defer close(release)
	requests := fakeAccountIssuer(t, func(_ http.ResponseWriter, r *http.Request) {
		close(started)
		select {
		case <-r.Context().Done():
		case <-release:
		}
	})
	r, auth, authority, accounts := accountFixture(t)
	c, stop := accountSocket(t, r, nil)
	var flow protocol.OpenAILoginFlow
	checkKind := func(err error, kind string) {
		t.Helper()
		e, ok := errors.AsType[*client.Error](err)
		if !ok || e.Kind != kind || strings.Contains(e.Message, "private") {
			t.Fatalf("error=%v expected=%s", err, kind)
		}
	}
	checkKind(c.Call(t.Context(), "accounts.openai.begin", protocol.EmptyParams{}, &flow), "METHOD")
	stop()
	c, _ = accountSocket(t, r, accounts)
	var status protocol.OpenAIAccountStatus
	checkKind(c.Call(t.Context(), "accounts.openai.setup", protocol.EmptyParams{}, &status), "ACCOUNT_CREDENTIALS")
	flow = call[protocol.OpenAILoginFlow](t, c, "accounts.openai.begin", protocol.EmptyParams{})
	accountSignal(t, started)
	cancelled := call[protocol.OpenAILoginFlow](t, c, "accounts.openai.cancel", protocol.OpenAIFlowParams{FlowID: flow.ID})
	if cancelled.State != "cancelled" || cancelled.UserCode != nil || requests.Load() != 1 {
		t.Fatal("cancel did not settle starting request")
	}
	current, err := authority.Snapshot(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	_, err = authority.Update(t.Context(), current.Revision, func(h *config.Host) error {
		h.Providers[openaiauth.Provider] = config.Provider{Kind: "openai-chat", BaseURL: "https://custom.test/v1"}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	checkKind(c.Call(t.Context(), "accounts.openai.begin", protocol.EmptyParams{}, &flow), "ACCOUNT_CONFIGURATION")
	if err := auth.Install(t.Context(), auth.Generation(), openaiauth.Credentials{AccessToken: "private-access", RefreshToken: "private-refresh", AccountID: "account", ExpiresAt: time.Now().Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}
	checkKind(c.Call(t.Context(), "accounts.openai.setup", protocol.EmptyParams{}, &status), "ACCOUNT_SETUP")
	if requests.Load() != 1 {
		t.Fatal("status/setup refreshed credentials")
	}
	// Logout failures remain explicit and safe, even though in-memory authority
	// has already been removed and must not be represented as durable logout.
	privatePath := filepath.Join(filepath.Dir(r.SocketPath()), "openai-codex.json")
	if err := os.Remove(privatePath); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(privatePath, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(privatePath, "block"), []byte("private-local-data"), 0o600); err != nil {
		t.Fatal(err)
	}
	checkKind(c.Call(t.Context(), "accounts.openai.logout", protocol.EmptyParams{}, &status), "ACCOUNT_LOGOUT")
	accounts.Close()
	checkKind(c.Call(t.Context(), "accounts.openai.status", protocol.EmptyParams{}, &status), "CLOSED")
}
