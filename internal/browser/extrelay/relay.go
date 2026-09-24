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
type Relay struct {
	token   string
	ln      net.Listener
	mu      sync.Mutex
	ext     *conn    // the extension's /ext socket (nil until attached)
	cdpConn *conn    // rod's /cdp socket
	tabInfo tabInfo  // the pinned tab's identity, reported by the extension
	swlogs  []string // diagnostic SW step logs (/swlog, test/debug)
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
	r := &Relay{token: tok, ln: ln}
	mux := http.NewServeMux()
	mux.HandleFunc("/ext", r.handleExt)
	mux.HandleFunc("/cdp", r.handleCDP)
	mux.HandleFunc("/swlog", r.handleSWLog) // diagnostic: SW step logging (test/debug)
	//nolint:gosec // G114: loopback-only relay for the local browser extension; long-lived CDP websockets make timeouts wrong
	go func() { _ = http.Serve(ln, mux) }()
	return r, nil
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
	if req.URL.Query().Get("token") != r.token {
		http.Error(w, "bad token", http.StatusUnauthorized)
		return
	}
	body, _ := io.ReadAll(io.LimitReader(req.Body, 1<<16))
	r.mu.Lock()
	r.swlogs = append(r.swlogs, string(body))
	r.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

// Addr is the relay's host:port for writing into relay.json.
func (r *Relay) Addr() string { return r.ln.Addr().String() }

// Token is the bearer the extension must present on /ext.
func (r *Relay) Token() string { return r.token }

// Close shuts the relay down.
func (r *Relay) Close() error { return r.ln.Close() }

// CDPURL is the ws:// URL rod should dial to drive the attached tab.
func (r *Relay) CDPURL() string { return "ws://" + r.ln.Addr().String() + "/cdp" }

// Attached reports whether the extension is currently connected.
func (r *Relay) Attached() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.ext != nil
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
	if req.URL.Query().Get("token") != r.token {
		http.Error(w, "bad token", http.StatusUnauthorized)
		return
	}
	c, err := upgrade(w, req)
	if err != nil {
		return
	}
	r.mu.Lock()
	if r.ext != nil {
		r.ext.close() // one extension at a time; newest wins
	}
	r.ext = c
	r.mu.Unlock()
	r.serveExt(c)
}

// handleCDP is rod's socket: a browser-level CDP endpoint over the tunnel.
func (r *Relay) handleCDP(w http.ResponseWriter, req *http.Request) {
	c, err := upgrade(w, req)
	if err != nil {
		return
	}
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

// splitRW adapts a separate reader (the handshake's buffered reader) and
// writer (the raw socket) into the io.ReadWriter wsutil wants. They never
// share a buffer, so a write's flush can't corrupt an in-flight read the way
// a single bufio.ReadWriter would.
// wsRW is the io.ReadWriter wsutil wants: reads from the handshake's
// buffered reader, writes through the locked writer (so control replies are
// serialized with data frames).
type wsRW struct {
	c *conn
}

func (s wsRW) Read(p []byte) (int, error)  { return s.c.r.Read(p) }
func (s wsRW) Write(p []byte) (int, error) { return s.c.w.Write(p) }

// writeText flushes a server frame. The locked writer inside wsRW serializes
// against any control reply emitted from the read loop.
func (c *conn) writeText(b []byte) error {
	return wsutil.WriteServerMessage(wsRW{c}, ws.OpText, b)
}

// readText is the blocking read used by the serve loops. gobwas's control
// handler may write pong/close replies to the rw — those go through wsRW's
// locked writer, staying serialized with data frames.
func (c *conn) readText() ([]byte, error) {
	return wsutil.ReadClientText(wsRW{c})
}

// serveExt pumps extension → rod: CDP responses/events for the tab, plus
// relay control frames (tab info on attach). Blocks until disconnect.
func (r *Relay) serveExt(c *conn) {
	defer func() {
		r.mu.Lock()
		if r.ext == c {
			r.ext = nil
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
			r.handleControl(msg)
			continue
		}
		r.mu.Lock()
		cdp := r.cdpLocked()
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
	if old := r.cdpConn; old != nil && old != c {
		old.close() // one rod client at a time; newest wins, don't leak the stale socket
	}
	r.setCDPLocked(c)
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		r.setCDPLocked(nil)
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
func (r *Relay) handleControl(msg []byte) {
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
		_ = json.Unmarshal(f.Params, &p)
		r.mu.Lock()
		r.tabInfo = tabInfo{ID: fmt.Sprintf("tab-%d", p.TabID), Title: p.Title, URL: p.URL}
		r.mu.Unlock()
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
		if r.Attached() {
			return nil
		}
		select {
		case <-ctx.Done():
			return errors.New("no tab attached: click the whipcode extension icon on the tab to drive")
		case <-t.C:
		}
	}
}
