package extrelay

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
)

func ownedRelay(t *testing.T) *Relay {
	t.Helper()
	r, err := NewRelay()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := r.Close(); err != nil {
			t.Error(err)
		}
	})
	return r
}

func pinRelay(t *testing.T, r *Relay, id int) *client {
	t.Helper()
	c := dialWS(t, "ws://"+r.Addr()+"/ext?token="+r.Token())
	t.Cleanup(func() { _ = c.Close() })
	writeCli(t, c, fmt.Sprintf(`{"method":"whip.attached","params":{"tabId":%d,"title":"Pinned","url":"https://example.com"}}`, id))
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if err := r.WaitAttached(ctx); err != nil {
		t.Fatal(err)
	}
	return c
}

func assertRelayPeerClosed(t *testing.T, c *client) {
	t.Helper()
	_ = c.nc.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := wsutil.ReadServerText(c); err == nil {
		t.Fatal("retired relay peer remained readable")
	} else if e, ok := err.(interface{ Timeout() bool }); ok && e.Timeout() {
		t.Fatal("retired relay peer did not close")
	}
}

func TestRelayCloseJoinsPeersAndWaitingPin(t *testing.T) {
	r := ownedRelay(t)
	ext := pinRelay(t, r, 1)
	cdp := dialWS(t, r.CDPURL())
	t.Cleanup(func() { _ = cdp.Close() })
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	assertRelayPeerClosed(t, ext)
	assertRelayPeerClosed(t, cdp)
	if r.Attached() {
		t.Fatal("closed relay remains attached")
	}
	if err := r.WaitAttached(t.Context()); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	active := r.active
	r.mu.Unlock()
	if active != 0 {
		t.Fatalf("Close returned with %d handlers", active)
	}
}

func TestRelayReplacementEndsExactController(t *testing.T) {
	for _, action := range []string{"replacement", "repin", "disconnect", "controller"} {
		t.Run(action, func(t *testing.T) {
			r := ownedRelay(t)
			ext := pinRelay(t, r, 1)
			cdp := dialWS(t, r.CDPURL())
			t.Cleanup(func() { _ = cdp.Close() })
			writeCli(t, cdp, `{"id":2,"method":"Runtime.evaluate","params":{"expression":"effect()"}}`)
			if got := readSrv(t, ext); !strings.Contains(got, "effect()") {
				t.Fatal(got)
			}
			switch action {
			case "replacement":
				pinRelay(t, r, 2)
			case "repin":
				writeCli(t, ext, `{"method":"whip.attached","params":{"tabId":2,"url":"https://other.example"}}`)
			case "disconnect":
				_ = ext.Close()
			case "controller":
				replacement := dialWS(t, r.CDPURL())
				defer replacement.Close()
				assertRelayPeerClosed(t, ext)
				writeCli(t, replacement, `{"id":2,"method":"Runtime.evaluate","params":{"expression":"next()"}}`)
				if got := readSrv(t, replacement); !strings.Contains(got, "no browser tab attached") {
					t.Fatal(got)
				}
			}
			assertRelayPeerClosed(t, cdp)
		})
	}
}

func TestRelayCDPRequiresToken(t *testing.T) {
	r := ownedRelay(t)
	for _, path := range []string{"/cdp", "/cdp?token=wrong"} {
		response, err := http.Get("http://" + r.Addr() + path)
		if err != nil {
			t.Fatal(err)
		}
		_ = response.Body.Close()
		if response.StatusCode != http.StatusUnauthorized {
			t.Fatal(response.StatusCode)
		}
	}
}

func TestRelayBoundsLogsAndMessages(t *testing.T) {
	r := ownedRelay(t)
	for i := range maxLogs + 4 {
		request := httptest.NewRequest(http.MethodPost, "http://relay/swlog?token="+r.Token(), strings.NewReader(strconv.Itoa(i)))
		response := httptest.NewRecorder()
		r.handleSWLog(response, request)
		if response.Code != http.StatusNoContent {
			t.Fatal(response.Code)
		}
	}
	logs := r.SWLogs()
	if len(logs) != maxLogs || logs[0] != "4" {
		t.Fatal(logs)
	}
	request := httptest.NewRequest(http.MethodPost, "http://relay/swlog?token="+r.Token(), strings.NewReader(strings.Repeat("x", maxLogBytes+1)))
	response := httptest.NewRecorder()
	r.handleSWLog(response, request)
	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatal(response.Code)
	}
	ext := pinRelay(t, r, 1)
	// Oversized header is rejected before its declared body is allocated/read.
	if err := ws.WriteHeader(ext.nc, ws.Header{Fin: true, OpCode: ws.OpText, Masked: true, Length: maxMessageBytes + 1}); err != nil {
		t.Fatal(err)
	}
	assertRelayPeerClosed(t, ext)
}

func TestRelayBoundedHandlerAdmissionAndClose(t *testing.T) {
	r := ownedRelay(t)
	entered := make(chan struct{}, maxHandlers)
	release := make(chan struct{})
	done := make(chan struct{}, maxHandlers)
	handler := r.owned(func(http.ResponseWriter, *http.Request) { entered <- struct{}{}; <-release })
	for range maxHandlers {
		go func() {
			handler(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
			done <- struct{}{}
		}()
	}
	for range maxHandlers {
		<-entered
	}
	rejected := httptest.NewRecorder()
	handler(rejected, httptest.NewRequest(http.MethodGet, "/", nil))
	if rejected.Code != http.StatusServiceUnavailable {
		t.Fatal(rejected.Code)
	}
	close(release)
	for range maxHandlers {
		<-done
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	after := httptest.NewRecorder()
	handler(after, httptest.NewRequest(http.MethodGet, "/", nil))
	if after.Code != http.StatusServiceUnavailable {
		t.Fatal(after.Code)
	}
}

func TestRelayWaitRequiresExactPinnedMetadata(t *testing.T) {
	r := ownedRelay(t)
	ext := dialWS(t, "ws://"+r.Addr()+"/ext?token="+r.Token())
	defer ext.Close()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := r.WaitAttached(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	writeCli(t, ext, `{"method":"whip.attached","params":{"tabId":1,"url":"https://example.com"}}`)
	ctx, cancel = context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if err := r.WaitAttached(ctx); err != nil {
		t.Fatal(err)
	}
}
