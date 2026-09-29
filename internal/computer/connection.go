package computer

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/context-labs/whip/internal/capability"
)

const (
	MaxConnectionFrameBytes   = 16 << 20
	MaxConnectionRequestBytes = 256 << 10
	MaxConnectionWaiters      = 4
)

var (
	ErrConnectionClosed   = errors.New("computer helper connection is closed; explicit new connection required")
	ErrConnectionCapacity = errors.New("computer helper call queue is full")
)

// ConnectionOptions explicitly names the already-installed executable. Opening
// never discovers, extracts, replaces, or repairs a helper. Processes is borrowed
// from the caller; this connection owns only the process it starts through it.
type ConnectionOptions struct {
	Processes   *capability.ProcessManager
	Owner       string
	Executable  string
	Directory   string
	Environment map[string]string
}

// Connection is one version-pinned helper lifetime, with one active call and at
// most four queued callers. Transport loss or active-call cancellation retires
// the entire generation. A failed action is never resent or automatically reopened.
// Human applications and their OS permissions remain independently owned.
type Connection struct {
	epoch     string
	ctx       context.Context
	cancel    context.CancelFunc
	process   *capability.Process
	stdin     io.WriteCloser
	stdout    io.ReadCloser
	reader    *bufio.Reader
	token     string
	gate      chan struct{}
	waiters   chan struct{}
	nextID    int64
	mu        sync.Mutex
	closed    bool
	io        sync.WaitGroup
	closeOnce sync.Once
	done      chan struct{}
}

// HelperError is a completed helper response, not a transport delivery failure.
// Error text is host-owned: frames, supplied tokens and arbitrary helper text
// never become diagnostics. Callers decide the operation's effect disposition.
type HelperError struct{ Code int }

func (e *HelperError) Error() string {
	switch e.Code {
	case errCodeUnknownApp:
		return "computer application is unavailable"
	case errCodeNoAXPermission:
		return "computer accessibility permission is unavailable"
	case errCodeNoScreenPerm:
		return "computer screen-recording permission is unavailable"
	case errCodeStaleGeneration:
		return "computer state changed; read current state"
	case errCodeIndexOutOfRange:
		return "computer element index is out of range"
	case errCodeNotActionable:
		return "computer element is not actionable"
	case errCodeScreenLocked:
		return "computer screen is locked"
	case errCodeBadToken:
		return "computer helper rejected authentication"
	default:
		return "computer helper rejected the request"
	}
}

// OpenConnection starts and handshakes one explicitly configured helper. The
// caller must Close it before closing the borrowed process manager. No background
// restart task, singleton cache or environment-based executable fallback exists.
func OpenConnection(ctx context.Context, options ConnectionOptions) (*Connection, error) {
	if options.Processes == nil || options.Owner == "" || !filepath.IsAbs(options.Executable) || !filepath.IsAbs(options.Directory) {
		return nil, errors.New("computer helper requires explicit process owner, absolute executable and directory")
	}
	if _, exists := options.Environment[tokenEnvVar]; exists {
		return nil, errors.New("computer helper token environment is reserved")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	lifetime, cancel := context.WithCancel(ctx)
	token := rand.Text() + rand.Text()
	environment := maps.Clone(options.Environment)
	if environment == nil {
		environment = map[string]string{}
	}
	environment[tokenEnvVar] = token
	process, stdin, stdout, err := options.Processes.StartPiped(lifetime, options.Owner, options.Executable, nil, capability.ProcessOptions{Cwd: options.Directory, Env: environment, Stderr: io.Discard})
	if err != nil {
		cancel()
		return nil, fmt.Errorf("start computer helper: %w", err)
	}
	connection := &Connection{
		epoch: "computer_" + rand.Text(), ctx: lifetime, cancel: cancel,
		process: process, stdin: stdin, stdout: stdout,
		reader: bufio.NewReaderSize(stdout, 64<<10), token: token,
		gate: make(chan struct{}, 1), waiters: make(chan struct{}, MaxConnectionWaiters),
		done: make(chan struct{}),
	}
	go func() {
		_ = process.Wait()
		connection.retire()
		close(connection.done)
	}()
	handshakeCtx, stop := context.WithTimeout(ctx, 10*time.Second)
	defer stop()
	var announcement []byte
	err = connection.roundtrip(handshakeCtx, func() error {
		var err error
		announcement, err = connection.readLine()
		return err
	})
	if err == nil && string(announcement) != ProtocolVersion {
		err = errors.New("computer helper protocol version mismatch")
	}
	if err == nil {
		var response json.RawMessage
		response, err = connection.Call(handshakeCtx, "handshake", json.RawMessage(`{}`))
		if err == nil {
			var result struct {
				Version string `json:"version"`
			}
			if json.Unmarshal(response, &result) != nil || result.Version != ProtocolVersion {
				err = errors.New("computer helper handshake version mismatch")
			}
		}
	}
	if err != nil {
		connection.Close()
		return nil, err
	}
	return connection, nil
}

// Epoch and Lifetime are inert local observations. They do not launch or check
// a native application and cannot revive an ended generation.
func (c *Connection) Epoch() string             { return c.epoch }
func (c *Connection) Lifetime() context.Context { return c.ctx }

// Close revokes the generation, closes both pipes, stops the exact owned process
// group and joins every in-flight reader/writer. It never closes human apps.
func (c *Connection) Close() {
	c.retire()
	<-c.done
}

func (c *Connection) retire() {
	c.closeOnce.Do(func() {
		c.mu.Lock()
		c.closed = true
		c.cancel()
		c.mu.Unlock()
		_ = c.stdin.Close()
		_ = c.stdout.Close()
		c.process.Stop()
		c.io.Wait()
	})
}

// Call accepts one JSON object and returns exact JSON result bytes. The caller
// owns action validation and authority. Helper errors keep the connection alive;
// malformed frames, mismatched IDs, cancellation and I/O failure retire it.
func (c *Connection) Call(ctx context.Context, method string, arguments json.RawMessage) (json.RawMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := c.ctx.Err(); err != nil {
		return nil, ErrConnectionClosed
	}
	if method == "" || len(method) > 64 || !utf8.ValidString(method) || strings.ContainsAny(method, "\x00\r\n") {
		return nil, errors.New("invalid computer helper method")
	}
	if len(arguments) > MaxConnectionRequestBytes || !utf8.Valid(arguments) {
		return nil, errors.New("computer helper request exceeds bounds")
	}
	var params map[string]json.RawMessage
	if json.Unmarshal(arguments, &params) != nil || params == nil {
		return nil, errors.New("computer helper arguments must be an object")
	}
	if _, exists := params["token"]; exists {
		return nil, errors.New("computer helper token argument is reserved")
	}
	// Reserve a finite queue slot only when another call owns serialization.
	select {
	case c.gate <- struct{}{}:
	default:
		select {
		case c.waiters <- struct{}{}:
		default:
			return nil, ErrConnectionCapacity
		}
		select {
		case c.gate <- struct{}{}:
			<-c.waiters
		case <-ctx.Done():
			<-c.waiters
			return nil, ctx.Err()
		case <-c.ctx.Done():
			<-c.waiters
			return nil, ErrConnectionClosed
		}
	}
	defer func() { <-c.gate }()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if c.ctx.Err() != nil {
		return nil, ErrConnectionClosed
	}
	if c.nextID == 1<<53-1 {
		c.Close()
		return nil, errors.New("computer helper request identity exhausted")
	}
	c.nextID++
	id := c.nextID
	encodedToken, _ := json.Marshal(c.token)
	params["token"] = encodedToken
	request, err := json.Marshal(struct {
		JSONRPC string                     `json:"jsonrpc"`
		ID      int64                      `json:"id"`
		Method  string                     `json:"method"`
		Params  map[string]json.RawMessage `json:"params"`
	}{"2.0", id, method, params})
	if err != nil {
		return nil, errors.New("invalid computer helper request")
	}
	if len(request) > MaxConnectionRequestBytes {
		return nil, errors.New("computer helper request exceeds bounds")
	}
	limit := 30 * time.Second
	if method == "permissions.request" {
		limit = 150 * time.Second
	}
	callCtx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	var raw []byte
	err = c.roundtrip(callCtx, func() error {
		if _, err := c.stdin.Write(append(request, '\n')); err != nil {
			return errors.New("computer helper write failed; outcome may be unknown")
		}
		var err error
		raw, err = c.readLine()
		return err
	})
	if err != nil {
		return nil, err
	}
	var response rpcResponse
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&response) != nil || decoder.Decode(new(any)) != io.EOF || response.JSONRPC != "2.0" || response.ID != id || (len(response.Result) == 0) == (response.Error == nil) {
		c.Close()
		return nil, errors.New("invalid computer helper response; outcome may be unknown")
	}
	if response.Error != nil {
		if len(response.Error.Message) > 4096 || !utf8.ValidString(response.Error.Message) {
			c.Close()
			return nil, errors.New("invalid computer helper error; outcome may be unknown")
		}
		return nil, &HelperError{Code: response.Error.Code}
	}
	return bytes.Clone(response.Result), nil
}

func (c *Connection) readLine() ([]byte, error) {
	var result []byte
	for {
		part, prefix, err := c.reader.ReadLine()
		if err != nil {
			return nil, errors.New("computer helper read failed; outcome may be unknown")
		}
		if len(part) > MaxConnectionFrameBytes-len(result) {
			return nil, errors.New("computer helper response exceeds bounds; outcome may be unknown")
		}
		result = append(result, part...)
		if !prefix {
			break
		}
	}
	if !utf8.Valid(result) {
		return nil, errors.New("computer helper response is not UTF-8; outcome may be unknown")
	}
	return result, nil
}

// A pipe has no portable write deadline. A single joined I/O task owns the
// roundtrip; cancellation closes the whole generation to unblock both sides.
func (c *Connection) roundtrip(ctx context.Context, run func() error) error {
	c.mu.Lock()
	if c.closed || c.ctx.Err() != nil {
		c.mu.Unlock()
		return ErrConnectionClosed
	}
	if err := ctx.Err(); err != nil {
		c.mu.Unlock()
		return err
	}
	c.io.Add(1)
	c.mu.Unlock()
	done := make(chan error, 1)
	go func() { defer c.io.Done(); done <- run() }()
	var err error
	select {
	case err = <-done:
	case <-ctx.Done():
		err = ctx.Err()
	case <-c.ctx.Done():
		err = ErrConnectionClosed
	}
	if err != nil {
		c.Close()
		return err
	}
	if err := ctx.Err(); err != nil {
		c.Close()
		return err
	}
	if c.ctx.Err() != nil {
		return ErrConnectionClosed
	}
	return nil
}
