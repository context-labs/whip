package extrelay

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
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
	nc, br, _, err := ws.Dial(context.Background(), url)
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
