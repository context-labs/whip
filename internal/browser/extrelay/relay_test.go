package extrelay

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
)

// client pairs the conn (writes) with the dial's buffered reader (reads) so
// no bytes are stranded in the handshake buffer. Satisfies io.ReadWriter.
type client struct {
	nc net.Conn
	br *bufio.Reader
}

func (c *client) Read(p []byte) (int, error)  { return c.br.Read(p) }
func (c *client) Write(p []byte) (int, error) { return c.nc.Write(p) }
func (c *client) Close() error                { return c.nc.Close() }

// dialWS connects a client to a relay path.
func dialWS(t *testing.T, url string) *client {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	nc, br, _, err := ws.Dial(ctx, url)
	if err != nil {
		t.Fatalf("dial %s: %v", url, err)
	}
	if br == nil {
		br = bufio.NewReader(nc)
	}
	return &client{nc: nc, br: br}
}

func readSrv(t *testing.T, c *client) string {
	t.Helper()
	if err := c.nc.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.nc.SetReadDeadline(time.Time{}) }()
	b, err := wsutil.ReadServerText(c)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return string(b)
}

func writeCli(t *testing.T, c *client, s string) {
	t.Helper()
	if err := wsutil.WriteClientText(c.nc, []byte(s)); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func TestTokenAuth(t *testing.T) {
	r, err := NewRelay()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	for _, bad := range []string{"", "wrong"} {
		resp, err := http.Get(fmt.Sprintf("http://%s/ext?token=%s", r.Addr(), bad))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("token %q: want 401, got %d", bad, resp.StatusCode)
		}
	}
	ext := dialWS(t, fmt.Sprintf("ws://%s/ext?token=%s", r.Addr(), r.Token()))
	defer ext.Close()
	if !r.Attached() {
		t.Fatal("relay should report attached after extension connects")
	}
}

func TestCDPTunnelRoundTrip(t *testing.T) {
	r, err := NewRelay()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	ext := dialWS(t, fmt.Sprintf("ws://%s/ext?token=%s", r.Addr(), r.Token()))
	defer ext.Close()
	cdp := dialWS(t, "ws://"+r.Addr()+"/cdp")
	defer cdp.Close()

	// rod sends a page-level command → extension receives it → replies → rod gets it.
	writeCli(t, cdp, `{"id":7,"method":"Runtime.evaluate","params":{"expression":"1+1"}}`)
	if got := readSrv(t, ext); !strings.Contains(got, `"Runtime.evaluate"`) {
		t.Fatalf("extension should receive rod's command, got %s", got)
	}
	writeCli(t, ext, `{"id":7,"result":{"result":{"type":"number","value":2}}}`)
	if resp := readSrv(t, cdp); !strings.Contains(resp, `"value":2`) {
		t.Fatalf("rod should receive the result, got %s", resp)
	}
}

func TestSynthTargetCommands(t *testing.T) {
	r, err := NewRelay()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	ext := dialWS(t, fmt.Sprintf("ws://%s/ext?token=%s", r.Addr(), r.Token()))
	defer ext.Close()
	// Extension reports the pinned tab's identity.
	writeCli(t, ext, `{"method":"whip.attached","params":{"tabId":42,"title":"X / chamath","url":"https://x.com/chamath"}}`)
	time.Sleep(100 * time.Millisecond)

	cdp := dialWS(t, "ws://"+r.Addr()+"/cdp")
	defer cdp.Close()

	for m, want := range map[string]string{
		`{"id":1,"method":"Target.setDiscoverTargets","params":{"discover":true}}`:                `"id":1,"result":{}`,
		`{"id":3,"method":"Target.attachToTarget","params":{"targetId":"tab-42","flatten":true}}`: `"sessionId":"whip-ext"`,
	} {
		writeCli(t, cdp, m)
		if resp := readSrv(t, cdp); !strings.Contains(resp, want) {
			t.Fatalf("%s → want %q in %q", m, want, resp)
		}
	}
	// getTargets describes the one pinned tab as a page target.
	writeCli(t, cdp, `{"id":9,"method":"Target.getTargets"}`)
	resp := readSrv(t, cdp)
	if !strings.Contains(resp, `"type":"page"`) || !strings.Contains(resp, "x.com/chamath") {
		t.Fatalf("targets must include the pinned tab: %s", resp)
	}
}

func TestNoTabAttachedErrors(t *testing.T) {
	r, err := NewRelay()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	cdp := dialWS(t, "ws://"+r.Addr()+"/cdp")
	defer cdp.Close()
	writeCli(t, cdp, `{"id":5,"method":"Runtime.evaluate","params":{"expression":"1"}}`)
	if resp := readSrv(t, cdp); !strings.Contains(resp, "click the whipcode extension icon") {
		t.Fatalf("want actionable no-tab error, got %s", resp)
	}
}

func TestExtensionDisconnectDetaches(t *testing.T) {
	r, err := NewRelay()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	ext := dialWS(t, fmt.Sprintf("ws://%s/ext?token=%s", r.Addr(), r.Token()))
	if !r.Attached() {
		t.Fatal("should be attached")
	}
	ext.Close()
	deadline := time.Now().Add(2 * time.Second)
	for r.Attached() && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if r.Attached() {
		t.Fatal("relay must report detached after the extension socket closes")
	}
}

// handshakeGate pauses the server after the client receives HTTP 101 but before
// Flush returns. This is the scheduling window that exposed an absent relay
// extension to an immediately connected CDP client.
type handshakeGate struct {
	http.ResponseWriter
	release <-chan struct{}
}

func (g handshakeGate) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	nc, rw, err := g.ResponseWriter.(http.Hijacker).Hijack()
	if err != nil {
		return nil, nil, err
	}
	gated := &handshakeConn{Conn: nc, release: g.release}
	return gated, bufio.NewReadWriter(rw.Reader, bufio.NewWriter(gated)), nil
}

type handshakeConn struct {
	net.Conn
	release <-chan struct{}
	once    sync.Once
}

func (c *handshakeConn) Write(p []byte) (int, error) {
	n, err := c.Conn.Write(p)
	c.once.Do(func() { <-c.release })
	return n, err
}

func TestExtensionHandshakePublicationPrecedesAttachedObservation(t *testing.T) {
	r := &Relay{token: "test-token"}
	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer unblock()
	mux := http.NewServeMux()
	mux.HandleFunc("/ext", func(w http.ResponseWriter, request *http.Request) {
		r.handleExt(handshakeGate{ResponseWriter: w, release: release}, request)
	})
	mux.HandleFunc("/cdp", r.handleCDP)
	server := httptest.NewServer(mux)
	defer server.Close()
	url := "ws" + strings.TrimPrefix(server.URL, "http")
	ext := dialWS(t, url+"/ext?token="+r.token)
	defer ext.Close()
	observed := make(chan bool, 1)
	go func() { observed <- r.Attached() }()
	select {
	case attached := <-observed:
		t.Fatalf("observed extension=%t while its acknowledged handshake was still being committed", attached)
	case <-time.After(25 * time.Millisecond):
	}
	unblock()
	select {
	case attached := <-observed:
		if !attached {
			t.Fatal("acknowledged extension was not published")
		}
	case <-time.After(time.Second):
		t.Fatal("attachment observation stayed blocked")
	}
	cdp := dialWS(t, url+"/cdp")
	defer cdp.Close()
	writeCli(t, cdp, `{"id":7,"method":"Runtime.evaluate","params":{"expression":"1+1"}}`)
	if got := readSrv(t, ext); !strings.Contains(got, "Runtime.evaluate") {
		t.Fatal(got)
	}
	writeCli(t, ext, `{"id":7,"result":{"value":2}}`)
	if got := readSrv(t, cdp); !strings.Contains(got, `"value":2`) {
		t.Fatal(got)
	}
}

type relayReadBarrier struct {
	io.Reader
	started chan struct{}
	release <-chan struct{}
	once    sync.Once
}

func (r *relayReadBarrier) Read(p []byte) (int, error) {
	r.once.Do(func() { close(r.started) })
	n, err := r.Reader.Read(p)
	if err != nil && r.release != nil {
		<-r.release
	}
	return n, err
}

func TestObsoleteCDPDisconnectCannotClearReplacement(t *testing.T) {
	r := &Relay{}
	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer unblock()
	makeConnection := func(block <-chan struct{}) (*conn, *client, <-chan struct{}) {
		t.Helper()
		server, peer := net.Pipe()
		t.Cleanup(func() { _ = server.Close(); _ = peer.Close() })
		reader := &relayReadBarrier{Reader: server, started: make(chan struct{}), release: block}
		connection := &conn{nc: server, r: reader}
		connection.w = &lockedWriter{nc: server, mu: &connection.wm}
		return connection, &client{nc: peer, br: bufio.NewReader(peer)}, reader.started
	}
	wait := func(done <-chan struct{}) {
		t.Helper()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("relay owner did not reach its test barrier")
		}
	}
	old, _, oldRead := makeConnection(release)
	oldDone := make(chan struct{})
	go func() { r.serveCDP(old); close(oldDone) }()
	t.Cleanup(func() { unblock(); old.close(); wait(oldDone) })
	wait(oldRead)
	current, cdp, currentRead := makeConnection(nil)
	currentDone := make(chan struct{})
	go func() { r.serveCDP(current); close(currentDone) }()
	t.Cleanup(func() { current.close(); wait(currentDone) })
	wait(currentRead) // Replacement is published; obsolete cleanup remains held.
	unblock()
	wait(oldDone)
	r.mu.Lock()
	live := r.cdpConn == current
	r.mu.Unlock()
	if !live {
		t.Fatal("obsolete connection cleanup cleared its live replacement")
	}
	extConnection, ext, _ := makeConnection(nil)
	r.mu.Lock()
	r.ext = extConnection
	r.mu.Unlock()
	extDone := make(chan struct{})
	go func() { r.serveExt(extConnection); close(extDone) }()
	t.Cleanup(func() { extConnection.close(); wait(extDone) })
	writeCli(t, cdp, `{"id":11,"method":"Runtime.evaluate","params":{"expression":"1+1"}}`)
	if got := readSrv(t, ext); !strings.Contains(got, "Runtime.evaluate") {
		t.Fatal(got)
	}
	writeCli(t, ext, `{"id":11,"result":{"value":2}}`)
	if got := readSrv(t, cdp); !strings.Contains(got, `"value":2`) {
		t.Fatal(got)
	}
	_ = cdp.Close()
	_ = ext.Close()
	wait(currentDone)
	wait(extDone)
}
