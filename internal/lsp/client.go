package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"
)

const (
	maxFrameBytes  = 1 << 20
	maxHeaderBytes = 8 << 10
)

// This file is the LSP/JSON-RPC wire layer: Content-Length framed messages
// over stdio (spec: microsoft.github.io/language-server-protocol, base
// protocol). opencode pulls in vscode-jsonrpc + vscode-languageserver-protocol
// for this (packages/opencode/src/lsp/client.ts); encoding/json + bufio cover
// the whole thing, so go.mod stays unchanged.

// rpcMessage is the union of a request, response, and notification.
type rpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *rpcError) Error() string { return fmt.Sprintf("rpc error %d: %s", e.Code, e.Message) }

// diagHandler receives publishDiagnostics pushes (uri, doc version, decoded
// diagnostics). Called on the client's read goroutine; must not block.
type diagHandler func(uri string, version int, diags []Diagnostic)

// client is one LSP server process speaking JSON-RPC over stdio.
//
// Concurrency (docs/concurrency.md): a single reader goroutine owns stdout
// and routes responses into per-request cap-1 channels; all writes funnel
// through the buffered `out` channel drained by one writer goroutine — no
// write locks. Shutdown is a close-to-broadcast on `dead`, which resolves
// every pending request with an error.
type client struct {
	stdin     io.Writer
	out       chan []byte // frames to write; pump owns stdin
	pend      sync.Map    // id (string form) -> chan rpcResponse, capacity 1
	nextID    atomic.Int64
	dead      chan struct{} // closed once when the connection dies/closes
	closeOnce sync.Once
	workers   sync.WaitGroup
	stdout    io.Reader
	onDiag    diagHandler
}

type rpcResponse struct {
	result json.RawMessage
	err    error
}

func newClient(stdin io.Writer, stdout io.Reader, onDiag diagHandler) *client {
	c := &client{
		stdin:  stdin,
		out:    make(chan []byte, 8),
		dead:   make(chan struct{}),
		onDiag: onDiag,
		stdout: stdout,
	}
	c.workers.Go(func() { c.readLoop(stdout) })
	c.workers.Go(c.writeLoop)
	return c
}

// shutdown stops the write pump. The read loop exits on stdout EOF (process
// death); both broadcast on `dead`.
func (c *client) shutdown() {
	c.closeOnce.Do(func() {
		close(c.dead)
		if stream, ok := c.stdin.(io.Closer); ok {
			_ = stream.Close()
		}
		if stream, ok := c.stdout.(io.Closer); ok {
			_ = stream.Close()
		}
	})
}

func (c *client) wait() { c.workers.Wait() }

// isDead reports whether the connection has been torn down.
func (c *client) isDead() bool {
	select {
	case <-c.dead:
		return true
	default:
		return false
	}
}

func (c *client) writeLoop() {
	for {
		select {
		case frame := <-c.out:
			if _, err := c.stdin.Write(frame); err != nil {
				c.shutdown()
				return
			}
		case <-c.dead:
			return
		}
	}
}

// readLoop parses Content-Length frames and dispatches: responses by id,
// publishDiagnostics to the handler, server→client requests answered with a
// minimal null result (opencode does the same for workDoneProgress/create,
// workspace/configuration, client/registerCapability — client.ts:173-208).
func (c *client) readLoop(r io.Reader) {
	defer c.shutdown()
	br := bufio.NewReader(r)
	for {
		body, err := readFrame(br)
		if err != nil {
			return // EOF or framing error: process is gone or wedged
		}
		var msg rpcMessage
		if err := json.Unmarshal(body, &msg); err != nil {
			continue
		}
		switch {
		case msg.Method == "textDocument/publishDiagnostics":
			var p struct {
				URI         string `json:"uri"`
				Version     int    `json:"version"`
				Diagnostics []struct {
					Range struct {
						Start struct {
							Line      int `json:"line"`
							Character int `json:"character"`
						} `json:"start"`
					} `json:"range"`
					Severity int    `json:"severity"`
					Message  string `json:"message"`
				} `json:"diagnostics"`
			}
			if err := json.Unmarshal(msg.Params, &p); err == nil && c.onDiag != nil {
				diags := make([]Diagnostic, min(len(p.Diagnostics), maxPerFile+1))
				for i, d := range p.Diagnostics[:len(diags)] {
					diags[i] = Diagnostic{
						Line:     d.Range.Start.Line + 1,
						Col:      d.Range.Start.Character + 1,
						Severity: d.Severity,
						Message:  d.Message,
					}
				}
				c.onDiag(p.URI, p.Version, diags)
			}
		case len(msg.ID) > 0 && msg.Method != "":
			// Server→client request: ack with null so the server isn't blocked.
			c.send(rpcMessage{JSONRPC: "2.0", ID: msg.ID, Result: json.RawMessage("null")})
		case len(msg.ID) > 0:
			key := string(msg.ID)
			if ch, ok := c.pend.LoadAndDelete(key); ok {
				res := rpcResponse{result: msg.Result}
				if msg.Error != nil {
					res.err = msg.Error
				}
				ch.(chan rpcResponse) <- res // cap 1, never blocks
			}
		}
	}
}

// readFrame reads one Content-Length framed message body.
func readFrame(br *bufio.Reader) ([]byte, error) {
	length := -1
	headers := 0
	for {
		raw, err := br.ReadSlice('\n')
		if err != nil {
			return nil, err
		}
		headers += len(raw)
		if headers > maxHeaderBytes {
			return nil, errors.New("language server headers exceed 8 KiB")
		}
		line := strings.TrimRight(string(raw), "\r\n")
		if line == "" {
			break // end of headers
		}
		if v, ok := strings.CutPrefix(line, "Content-Length: "); ok {
			n, err := strconv.Atoi(strings.TrimSpace(v))
			if err != nil || n < 0 || n > maxFrameBytes || length >= 0 {
				return nil, fmt.Errorf("bad Content-Length %q", v)
			}
			length = n
		}
	}
	if length < 0 {
		return nil, errors.New("missing Content-Length header")
	}
	body := make([]byte, length)
	if _, err := io.ReadFull(br, body); err != nil {
		return nil, err
	}
	return body, nil
}

// writeTimeout bounds how long send blocks on a full buffer: a wedged server
// that stops reading stdin must kill the client, not the turn (or whip's
// exit path, which also sends).
const writeTimeout = 5 * time.Second

// send enqueues one framed message. The writer pump owns stdin; if the
// buffer stays full past writeTimeout the server is wedged and the client is
// torn down — callers never block past that.
func (c *client) send(msg rpcMessage) { _ = c.sendContext(context.Background(), msg) }

func (c *client) sendContext(ctx context.Context, msg rpcMessage) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	msg.JSONRPC = "2.0"
	body, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	if len(body) > maxFrameBytes {
		return errors.New("language server frame exceeds 1 MiB")
	}
	frame := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(body))
	payload := append([]byte(frame), body...)
	timer := time.NewTimer(writeTimeout)
	defer timer.Stop()
	select {
	case c.out <- payload:
		return nil
	case <-c.dead:
		return errors.New("language server connection closed")
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		c.shutdown()
		return errors.New("language server write timed out")
	}
}

func diagnosticText(text string) string {
	text = strings.ToValidUTF8(text, "�")
	if len(text) <= maxMsgLen {
		return text
	}
	text = text[:maxMsgLen]
	for !utf8.ValidString(text) {
		text = text[:len(text)-1]
	}
	return text + "…"
}

// request issues a JSON-RPC request and waits for the response, bounded by
// ctx (tool-call ctx, so ctrl+c cancels).
func (c *client) request(ctx context.Context, method string, params, result any) error {
	n := c.nextID.Add(1)
	id := strconv.FormatInt(n, 10)
	ch := make(chan rpcResponse, 1)
	c.pend.Store(id, ch)
	defer c.pend.Delete(id)

	idRaw, _ := json.Marshal(n) // numeric id
	paramsRaw, err := json.Marshal(params)
	if err != nil {
		return err
	}
	select {
	case <-c.dead:
		return errors.New("lsp server connection closed")
	default:
	}
	if err := c.sendContext(ctx, rpcMessage{ID: idRaw, Method: method, Params: paramsRaw}); err != nil {
		return err
	}

	select {
	case res := <-ch:
		if res.err != nil {
			return res.err
		}
		if result != nil && len(res.result) > 0 {
			return json.Unmarshal(res.result, result)
		}
		return nil
	case <-c.dead:
		return errors.New("lsp server connection closed")
	case <-ctx.Done():
		return ctx.Err()
	}
}

// notify sends a notification (no response expected).
func (c *client) notify(method string, params any) {
	_ = c.notifyContext(context.Background(), method, params)
}

func (c *client) notifyContext(ctx context.Context, method string, params any) error {
	paramsRaw, err := json.Marshal(params)
	if err != nil {
		return err
	}
	return c.sendContext(ctx, rpcMessage{Method: method, Params: paramsRaw})
}
