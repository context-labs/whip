// Package extrelay turns the user's real, logged-in Chrome tab into a
// browser_exec backend: the whipcode browser extension holds an outbound
// WebSocket to this loopback relay and pipes raw CDP through chrome.debugger
// on the tab the user pinned. rod connects to the relay's /cdp endpoint and
// drives the tab unchanged — no second driver.
//
// The relay is a CDP pipe with one exception: a single-tab chrome.debugger
// session can't answer browser-level Target.* commands that rod's attach
// path needs, so the relay synthesizes those few responses (one attached
// page target) and forwards everything else verbatim to the tab.
//
// Security: loopback only, and the extension must present the per-process
// bearer token (written to ~/.whipcode/browser/extension/relay.json by
// `whipcode browser install`, 0600). Only a tab the user explicitly activated
// by clicking the extension icon is drivable.
package extrelay

import (
	"context"
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // G505: WebSocket handshake hash per RFC 6455, not a security primitive
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
)

// frame is the CDP wire envelope, for both requests and responses/events.
type frame struct {
	ID        int64           `json:"id,omitempty"`
	Method    string          `json:"method,omitempty"`
	Params    json.RawMessage `json:"params,omitempty"`
	SessionID string          `json:"sessionId,omitempty"`
	Result    json.RawMessage `json:"result,omitempty"`
	Error     json.RawMessage `json:"error,omitempty"`
}

// Relay bridges one extension connection and one rod (CDP) connection.
const (
	maxMessageBytes = 12 << 20
	maxHandlers     = 16
	maxLogs         = 64
	maxLogBytes     = 2048
)

var ErrClosed = errors.New("extension relay is closed")

type Relay struct {
	token     string
	ln        net.Listener
	mu        sync.Mutex
	ext       *conn    // the extension's /ext socket (nil until attached)
	cdpConn   *conn    // rod's /cdp socket
	tabInfo   tabInfo  // the pinned tab's identity, reported by the extension
	swlogs    []string // bounded diagnostic SW step logs (/swlog, test/debug)
	server    *http.Server
	done      chan struct{}
	closed    bool
	active    int
	peers     sync.WaitGroup
	closeOnce sync.Once
	closeErr  error
	httpConns map[net.Conn]struct{}
	httpMu    sync.Mutex
}

// conn is one WebSocket. Reads run on the handshake's buffered reader;
// writes go through lockedWriter so a control-frame reply (pong/close) that
// gobwas emits from inside the read loop shares the write mutex with data
// frames — a ping racing a write would otherwise interleave a pong header
// into a data frame body and corrupt the stream.
type conn struct {
	nc net.Conn
	r  io.Reader
	w  *lockedWriter
	wm sync.Mutex
}

// lockedWriter serializes all writes (data frames + control replies) on wm.
type lockedWriter struct {
	nc net.Conn
	mu *sync.Mutex
}

func (w *lockedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	_ = w.nc.SetWriteDeadline(time.Now().Add(5 * time.Second))
	defer func() { _ = w.nc.SetWriteDeadline(time.Time{}) }()
	return w.nc.Write(p)
}

// NewRelay starts a loopback relay on an ephemeral port with a fresh token.
func NewRelay() (*Relay, error) {
	tok, err := genToken()
	if err != nil {
		return nil, err
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	r := &Relay{token: tok, ln: ln, done: make(chan struct{}), httpConns: make(map[net.Conn]struct{})}
	mux := http.NewServeMux()
	mux.HandleFunc("/ext", r.owned(r.handleExt))
	mux.HandleFunc("/cdp", r.owned(r.handleCDP))
	mux.HandleFunc("/swlog", r.owned(r.handleSWLog)) // diagnostic: SW step logging (test/debug)
	r.server = &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 8192, ConnState: r.connectionState}
	go func() { defer close(r.done); _ = r.server.Serve(ln) }()
	return r, nil
}

// owned bounds concurrent handlers, including hijacked peers. Admission and
// Close share the same lock so Wait cannot race a late Add.
func (r *Relay) owned(handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		r.mu.Lock()
		if r.closed || r.active >= maxHandlers {
			r.mu.Unlock()
			http.Error(w, "relay unavailable", http.StatusServiceUnavailable)
			return
		}
		r.active++
		r.peers.Add(1)
		r.mu.Unlock()
		defer func() { r.mu.Lock(); r.active--; r.mu.Unlock(); r.peers.Done() }()
		handler(w, req)
	}
}

func (r *Relay) connectionState(c net.Conn, state http.ConnState) {
	r.httpMu.Lock()
	defer r.httpMu.Unlock()
	switch state {
	case http.StateClosed, http.StateHijacked:
		delete(r.httpConns, c)
	case http.StateNew:
		if len(r.httpConns) >= maxHandlers {
			_ = c.Close()
			return
		}
		r.httpConns[c] = struct{}{}
	}
}

func (r *Relay) authorized(w http.ResponseWriter, req *http.Request) bool {
	if subtle.ConstantTimeCompare([]byte(req.URL.Query().Get("token")), []byte(r.token)) != 1 || r.token == "" {
		http.Error(w, "bad token", http.StatusUnauthorized)
		return false
	}
	return true
}

// SWLogs returns the service worker's step logs posted to /swlog.
func (r *Relay) SWLogs() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.swlogs...)
}

// handleSWLog records a diagnostic line from the extension's service worker
// (POST body). Lets tests see exactly where autoAttach/pin succeed or fail —
// the SW's console is otherwise hard to capture (it runs and suspends before
// a debugger can attach to it).
func (r *Relay) handleSWLog(w http.ResponseWriter, req *http.Request) {
	if !r.authorized(w, req) {
		return
	}
	if req.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(io.LimitReader(req.Body, maxLogBytes+1))
	if err != nil || len(body) > maxLogBytes || !utf8.Valid(body) {
		http.Error(w, "log exceeds bounds", http.StatusRequestEntityTooLarge)
		return
	}
	r.mu.Lock()
	if len(r.swlogs) == maxLogs {
		copy(r.swlogs, r.swlogs[1:])
		r.swlogs = r.swlogs[:maxLogs-1]
	}
	r.swlogs = append(r.swlogs, string(body))
	r.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

// Addr is the relay's host:port for writing into relay.json.
func (r *Relay) Addr() string { return r.ln.Addr().String() }

// Token is the bearer the extension must present on /ext.
func (r *Relay) Token() string { return r.token }

// Close shuts the relay down.
func (r *Relay) Close() error {
	r.closeOnce.Do(func() {
		r.mu.Lock()
		r.closed = true
		ext, cdp := r.ext, r.cdpConn
		r.ext, r.cdpConn, r.tabInfo = nil, nil, tabInfo{}
		r.mu.Unlock()
		if ext != nil {
			ext.close()
		}
		if cdp != nil {
			cdp.close()
		}
		r.closeErr = r.server.Close()
		<-r.done
		r.peers.Wait()
	})
	return r.closeErr
}

// CDPURL is the ws:// URL rod should dial to drive the attached tab.
func (r *Relay) CDPURL() string { return "ws://" + r.ln.Addr().String() + "/cdp?token=" + r.token }

// Attached reports whether the extension is currently connected.
func (r *Relay) Attached() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return !r.closed && r.ext != nil
}

func genToken() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// handleExt is the extension's control+data socket. The token gates it.
func (r *Relay) handleExt(w http.ResponseWriter, req *http.Request) {
	if !r.authorized(w, req) {
		return
	}
	// HTTP 101 can reach the extension before upgrade returns. Keep observers
	// and the CDP forwarder behind publication, so an immediately sent command
	// cannot mistake this acknowledged connection for an absent extension.
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		http.Error(w, "relay closed", http.StatusServiceUnavailable)
		return
	}
	c, err := upgrade(w, req)
	if err != nil {
		r.mu.Unlock()
		return
	}
	if r.ext != nil {
		r.ext.close() // replacement retires the prior controller as well
	}
	if r.cdpConn != nil {
		r.cdpConn.close()
		r.cdpConn = nil
	}
	r.tabInfo = tabInfo{}
	r.ext = c
	r.mu.Unlock()
	r.serveExt(c)
}

// handleCDP is rod's socket: a browser-level CDP endpoint over the tunnel.
func (r *Relay) handleCDP(w http.ResponseWriter, req *http.Request) {
	if !r.authorized(w, req) {
		return
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		http.Error(w, "relay closed", http.StatusServiceUnavailable)
		return
	}
	c, err := upgrade(w, req)
	if err != nil {
		r.mu.Unlock()
		return
	}
	if r.cdpConn != nil {
		r.cdpConn.close()
		if r.ext != nil {
			r.ext.close()
			r.ext = nil
			r.tabInfo = tabInfo{}
		}
	}
	r.cdpConn = c
	r.mu.Unlock()
	r.serveCDP(c)
}

func upgrade(w http.ResponseWriter, req *http.Request) (*conn, error) {
	// Manual handshake: rod's CDP client sends the literal non-base64 key
	// "nil", which gobwas's ws.UpgradeHTTP rejects (it demands a 16-byte
	// base64 nonce) — and rewriting the key breaks the accept hash rod
	// verifies against the key IT sent. So we hijack and compute
	// Sec-WebSocket-Accept from whatever key arrived, then hand the conn to
	// gobwas for frames. On a loopback relay the key carries no security
	// weight; the accept just has to round-trip what the client expects.
	if !strings.EqualFold(req.Header.Get("Upgrade"), "websocket") {
		http.Error(w, "not a websocket upgrade", http.StatusBadRequest)
		return nil, errors.New("not a websocket upgrade")
	}
	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "no hijack", http.StatusInternalServerError)
		return nil, errors.New("response writer cannot hijack")
	}
	nc, rw, err := hj.Hijack()
	if err != nil {
		return nil, err
	}
	_ = nc.SetDeadline(time.Now().Add(5 * time.Second))
	key := req.Header.Get("Sec-WebSocket-Key")
	accept := wsAccept(key)
	if _, err := fmt.Fprintf(rw, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: %s\r\n\r\n", accept); err != nil {
		_ = nc.Close()
		return nil, err
	}
	if err := rw.Flush(); err != nil {
		_ = nc.Close()
		return nil, err
	}
	_ = nc.SetDeadline(time.Time{})
	c := &conn{nc: nc, r: rw.Reader}
	c.w = &lockedWriter{nc: nc, mu: &c.wm}
	return c, nil
}

// wsAccept computes Sec-WebSocket-Accept: base64(sha1(key + magic)), per
// RFC 6455 — accepting any key verbatim (including rod's "nil").
func wsAccept(key string) string {
	const magic = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"
	sum := sha1.Sum([]byte(key + magic)) //nolint:gosec // G401: RFC 6455 mandates SHA-1 for Sec-WebSocket-Accept
	return base64.StdEncoding.EncodeToString(sum[:])
}

func (c *conn) close() { _ = c.nc.Close() }

// writeText serializes the whole frame against read-loop control replies.
func (c *conn) writeText(b []byte) error {
	if len(b) > maxMessageBytes {
		return errors.New("relay message exceeds bounds")
	}
	c.wm.Lock()
	defer c.wm.Unlock()
	_ = c.nc.SetWriteDeadline(time.Now().Add(5 * time.Second))
	defer func() { _ = c.nc.SetWriteDeadline(time.Time{}) }()
	return wsutil.WriteServerMessage(c.nc, ws.OpText, b)
}

// readText is the blocking read used by the serve loops. gobwas's control
// handler writes bounded control replies through the same write lock.
func (c *conn) readText() ([]byte, error) {
	control := wsutil.ControlFrameHandler(c.w, ws.StateServerSide)
	fragments := 0
	reader := wsutil.Reader{Source: c.r, State: ws.StateServerSide, CheckUTF8: true, MaxFrameSize: maxMessageBytes, OnIntermediate: control, OnContinuation: func(ws.Header, io.Reader) error {
		fragments++
		if fragments > 256 {
			return errors.New("relay message fragments exceed bounds")
		}
		return nil
	}}
	for {
		h, err := reader.NextFrame()
		if err != nil {
			return nil, err
		}
		if h.OpCode.IsControl() {
			if err := control(h, &reader); err != nil {
				return nil, err
			}
			continue
		}
		if h.OpCode != ws.OpText {
			return nil, errors.New("relay requires text messages")
		}
		data, err := io.ReadAll(io.LimitReader(&reader, maxMessageBytes+1))
		if len(data) > maxMessageBytes {
			return nil, errors.New("relay message exceeds bounds")
		}
		return data, err
	}
}

// serveExt pumps extension → rod: CDP responses/events for the tab, plus
// relay control frames (tab info on attach). Blocks until disconnect.
func (r *Relay) serveExt(c *conn) {
	defer func() {
		r.mu.Lock()
		if r.ext == c {
			r.ext = nil
			r.tabInfo = tabInfo{}
			if r.cdpConn != nil {
				r.cdpConn.close()
				r.cdpConn = nil
			}
		}
		r.mu.Unlock()
		c.close()
	}()
	for {
		msg, err := c.readText()
		if err != nil {
			return
		}
		// Control frames are ours ("whip.*"); everything else is CDP for rod.
		if isControl(msg) {
			r.handleControl(c, msg)
			continue
		}
		r.mu.Lock()
		cdp := r.cdpLocked()
		if r.ext != c {
			r.mu.Unlock()
			return
		}
		r.mu.Unlock()
		if cdp != nil {
			_ = cdp.writeText(msg)
		}
	}
}

// serveCDP pumps rod → extension, synthesizing the browser-level Target.*
// answers the single-tab debugger can't provide. Blocks until disconnect.
func (r *Relay) serveCDP(c *conn) {
	r.mu.Lock()
	current := !r.closed && r.cdpConn == c
	r.mu.Unlock()
	if !current {
		c.close()
		return
	}

	defer func() {
		r.mu.Lock()
		if r.cdpConn == c {
			r.setCDPLocked(nil)
			// Outstanding replies belong to this controller. A later controller
			// requires a fresh human pin, never a reused stream of old request IDs.
			if r.ext != nil {
				r.ext.close()
				r.ext = nil
				r.tabInfo = tabInfo{}
			}
		}
		r.mu.Unlock()
		c.close()
	}()
	for {
		msg, err := c.readText()
		if err != nil {
			return
		}
		if r.handleSynth(c, msg) {
			continue
		}
		r.mu.Lock()
		ext := r.ext
		if r.cdpConn != c {
			r.mu.Unlock()
			return
		}
		r.mu.Unlock()
		if ext == nil {
			r.replyErr(c, msg, "no browser tab attached — click the whipcode extension icon on a tab")
			continue
		}
		_ = ext.writeText(msg)
	}
}

// --- CDP plumbing ---

// cdpLocked returns the rod socket; caller holds r.mu.
func (r *Relay) cdpLocked() *conn { return r.cdpConn }

// setCDPLocked sets the rod socket; caller holds r.mu.
func (r *Relay) setCDPLocked(c *conn) { r.cdpConn = c }

// handleSynth answers the browser-level commands rod's attach path issues
// that the tab session can't. Returns true if handled (not forwarded).
func (r *Relay) handleSynth(c *conn, msg []byte) bool {
	var f frame
	if err := json.Unmarshal(msg, &f); err != nil || f.ID == 0 || f.Method == "" {
		return false
	}
	switch f.Method {
	case "Target.setDiscoverTargets":
		// Discovery is browser-side; the tunnel has exactly one tab. Ack.
		r.reply(c, f.ID, `{}`)
		return true
	case "Target.getTargets":
		r.reply(c, f.ID, r.targetsJSON())
		return true
	case "Target.getTargetInfo":
		// rod's Page.Info() (called by attachPage) asks for the attached
		// target's info — describe the pinned tab.
		r.reply(c, f.ID, r.targetInfoJSON())
		return true
	case "Target.attachToTarget":
		// Single session: confirm with a fixed session id; subsequent
		// page-scoped commands carry it and we strip/forward as-is.
		r.reply(c, f.ID, `{"sessionId":"whip-ext"}`)
		return true
	case "Target.createTarget", "Browser.close", "Browser.getVersion":
		// No tab creation / no process control over the user's browser, and
		// no version query over the tunnel — answer minimally.
		switch f.Method {
		case "Target.createTarget":
			r.replyErr(c, msg, "extension mode drives the pinned tab only; it cannot open new tabs")
		case "Browser.close":
			r.reply(c, f.ID, `{}`)
		default:
			r.reply(c, f.ID, `{"product":"whip-extension-relay"}`)
		}
		return true
	}
	return false
}

// targetsJSON describes the one attached tab as a page target list.
func (r *Relay) targetsJSON() string {
	r.mu.Lock()
	ti := r.tabInfo
	r.mu.Unlock()
	tid, title, url := ti.ID, ti.Title, ti.URL
	if tid == "" {
		tid = "whip-ext-tab"
	}
	b, _ := json.Marshal(map[string]any{
		"targetInfos": []map[string]any{{
			"targetId": tid,
			"type":     "page",
			"title":    title,
			"url":      url,
			"attached": true,
		}},
	})
	return string(b)
}

// targetInfoJSON is the singular Target.getTargetInfo answer for the pinned
// tab (same shape as one entry of targetsJSON).
func (r *Relay) targetInfoJSON() string {
	r.mu.Lock()
	ti := r.tabInfo
	r.mu.Unlock()
	tid, title, url := ti.ID, ti.Title, ti.URL
	if tid == "" {
		tid = "whip-ext-tab"
	}
	b, _ := json.Marshal(map[string]any{
		"targetInfo": map[string]any{
			"targetId": tid,
			"type":     "page",
			"title":    title,
			"url":      url,
			"attached": true,
		},
	})
	return string(b)
}

func (r *Relay) reply(c *conn, id int64, result string) {
	_ = c.writeText([]byte(fmt.Sprintf(`{"id":%d,"result":%s}`, id, result)))
}

func (r *Relay) replyErr(c *conn, msg []byte, text string) {
	var f frame
	_ = json.Unmarshal(msg, &f)
	e, _ := json.Marshal(map[string]any{"code": -32000, "message": text})
	_ = c.writeText([]byte(fmt.Sprintf(`{"id":%d,"error":%s}`, f.ID, e)))
}

// --- relay control channel (extension → relay, not CDP) ---

type tabInfo struct{ ID, Title, URL string }

// handleControl processes "whip.*" frames: the extension reports the pinned
// tab's identity on attach so Target.getTargets can describe it.
func (r *Relay) handleControl(c *conn, msg []byte) {
	var f frame
	if json.Unmarshal(msg, &f) != nil || !strings.HasPrefix(f.Method, "whip.") {
		return
	}
	if f.Method == "whip.attached" {
		var p struct {
			TabID int    `json:"tabId"`
			Title string `json:"title"`
			URL   string `json:"url"`
		}
		if json.Unmarshal(f.Params, &p) != nil || p.TabID < 0 || len(p.Title) > 4096 || len(p.URL) > 8192 {
			c.close()
			return
		}
		r.mu.Lock()
		defer r.mu.Unlock()
		if r.ext != c {
			return
		}
		id := fmt.Sprintf("tab-%d", p.TabID)
		if r.tabInfo.ID != "" && r.tabInfo.ID != id {
			if r.cdpConn != nil {
				r.cdpConn.close()
				r.cdpConn = nil
			}
			c.close()
			r.ext = nil
			r.tabInfo = tabInfo{}
			return
		}
		r.tabInfo = tabInfo{ID: id, Title: p.Title, URL: p.URL}
	}
}

func isControl(msg []byte) bool {
	var f frame
	return json.Unmarshal(msg, &f) == nil && strings.HasPrefix(f.Method, "whip.")
}

// WaitAttached blocks until the extension connects or ctx expires — used by
// the backend open path to give a clear "click the icon" error.
func (r *Relay) WaitAttached(ctx context.Context) error {
	t := time.NewTicker(100 * time.Millisecond)
	defer t.Stop()
	for {
		r.mu.Lock()
		closed, pinned := r.closed, r.ext != nil && r.tabInfo.ID != ""
		r.mu.Unlock()
		if closed {
			return ErrClosed
		}
		if pinned {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("no tab attached: click the whipcode extension icon on the tab to drive: %w", ctx.Err())
		case <-t.C:
		}
	}
}
