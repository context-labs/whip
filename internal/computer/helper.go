// helper.go — the Go client for the embedded whip-computer Swift helper:
// spawn, handshake-token, version check, JSON-RPC over stdio, crash restart.
// The wire contract lives in driver/Sources/WhipComputerCore (both sides pin
// protocolVersion; a mismatch hard-fails — codex's CodexComputerUseIPC-4
// lesson). Design: .ai-docs/plans/computer-use-native/README.md.

package computer

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/context-labs/whip/internal/capability"
)

// ProtocolVersion must match Protocol.version in the Swift driver.
const ProtocolVersion = "whip-computer/1"

// tokenEnvVar must match Protocol.tokenEnvVar in the Swift driver.
const tokenEnvVar = "WHIP_COMPUTER_TOKEN"

// Application-level JSON-RPC error codes (mirror RPCErrorCode in the driver).
const (
	errCodeUnknownApp      = 1
	errCodeNoAXPermission  = 2
	errCodeNoScreenPerm    = 3
	errCodeStaleGeneration = 4
	errCodeIndexOutOfRange = 5
	errCodeNotActionable   = 6
	errCodeScreenLocked    = 7
	errCodeBadToken        = 8
)

// rpcError is a JSON-RPC error object from the helper.
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *rpcError) Error() string { return e.Message }

// StaleError reports that the AX tree changed since the caller's generation
// (the per-action state gate). The model should re-read state and retry.
type StaleError struct{ Msg string }

func (e *StaleError) Error() string { return e.Msg }

// rpcRequest / rpcResponse are the stdio frames (newline-delimited JSON).
type rpcRequest struct {
	JSONRPC string         `json:"jsonrpc"`
	ID      int64          `json:"id"`
	Method  string         `json:"method"`
	Params  map[string]any `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *rpcError       `json:"error"`
}

// AXElement is one indexed node in a state()/ax() response.
type AXElement struct {
	Index    int       `json:"index"`
	Role     string    `json:"role"`
	Subrole  string    `json:"subrole,omitempty"`
	Title    string    `json:"title,omitempty"`
	Value    string    `json:"value,omitempty"`
	Desc     string    `json:"desc,omitempty"`
	RoleDesc string    `json:"roleDescription,omitempty"`
	Position []float64 `json:"position,omitempty"` // [x, y] points
	Size     []float64 `json:"size,omitempty"`     // [w, h] points
	Actions  []string  `json:"actions,omitempty"`
	Focused  bool      `json:"focused"`
	Enabled  bool      `json:"enabled"`
}

// Screenshot is an inline JPEG frame normalized to point resolution.
type Screenshot struct {
	JPEGBase64 string `json:"jpegBase64,omitempty"`
	Bytes      int    `json:"bytes,omitempty"`
	Err        string `json:"error,omitempty"`
}

// AppState is the result of state(): fresh AX tree + screenshot in-call.
type AppState struct {
	Generation int         `json:"generation"`
	App        string      `json:"app"`
	Elements   []AXElement `json:"elements"`
	Screenshot *Screenshot `json:"screenshot,omitempty"`
}

// RunningApp is one entry of apps().
type RunningApp struct {
	Name     string `json:"name"`
	BundleID string `json:"bundleId"`
	PID      int    `json:"pid"`
	Active   bool   `json:"active"`
}

// TCCStatus reports the helper's permission grants.
type TCCStatus struct {
	Accessibility   bool   `json:"accessibility"`
	ScreenRecording bool   `json:"screenRecording"`
	Pending         bool   `json:"pending,omitempty"`
	Hint            string `json:"hint,omitempty"`
}

// Helper manages one whip-computer subprocess. The zero value is unusable;
// get it via Shared(). Thread-safe; restarts the helper on crash.
type Helper struct {
	mu        sync.Mutex
	cmd       *exec.Cmd
	process   *capability.Process
	processes *capability.ProcessManager
	rootID    string
	cwd       string
	env       map[string]string
	stdin     io.WriteCloser
	reader    *bufio.Reader
	token     string
	nextID    int64
}

var shared = struct {
	mu  sync.Mutex
	h   *Helper
	err error
}{}

// Shared returns the process-wide helper, spawning it on first use. On
// non-macOS or when no helper binary is embedded/found, returns an error —
// callers fall back to the osascript tier.
func Shared() (*Helper, error) {
	shared.mu.Lock()
	defer shared.mu.Unlock()
	if shared.h != nil {
		return shared.h, nil
	}
	if shared.err != nil {
		return nil, shared.err // don't respawn-loop a permanently-missing binary
	}
	h := &Helper{}
	if err := h.spawn(); err != nil {
		shared.err = err
		return nil, err
	}
	shared.h = h
	return h, nil
}

func NewManagedHelper(processes *capability.ProcessManager, rootID, cwd string, env map[string]string) (*Helper, error) {
	h := &Helper{processes: processes, rootID: rootID, cwd: cwd, env: env}
	if err := h.spawn(); err != nil {
		return nil, err
	}
	return h, nil
}

// ResetShared drops the cached helper (tests).
func ResetShared() {
	shared.mu.Lock()
	defer shared.mu.Unlock()
	if shared.h != nil {
		shared.h.kill()
	}
	shared.h, shared.err = nil, nil
}

// helperPath resolves the binary: embedded copy extracted to ~/.whip/bin,
// or WHIP_COMPUTER_BIN / the driver build tree for dev.
func helperPath() (string, error) {
	if p := os.Getenv("WHIP_COMPUTER_BIN"); p != "" {
		return p, nil
	}
	return ensureHelperBinary()
}

// spawn starts the helper and performs the handshake (token + version).
func (h *Helper) spawn() error {
	path, err := helperPath()
	if err != nil {
		return err
	}
	tok := make([]byte, 16)
	if _, err := rand.Read(tok); err != nil {
		return err
	}
	h.token = hex.EncodeToString(tok)

	var stdin io.WriteCloser
	var stdout io.ReadCloser
	if h.processes != nil {
		env := make(map[string]string, len(h.env)+1)
		maps.Copy(env, h.env)
		env[tokenEnvVar] = h.token
		h.process, stdin, stdout, err = h.processes.StartPiped(context.Background(), h.rootID, path, nil, capability.ProcessOptions{
			Cwd: h.cwd, Env: env, Stderr: io.Discard,
		})
	} else {
		h.cmd = exec.CommandContext(context.Background(), path)
		h.cmd.Env = append(os.Environ(), tokenEnvVar+"="+h.token)
		stdin, err = h.cmd.StdinPipe()
		if err == nil {
			stdout, err = h.cmd.StdoutPipe()
		}
		if err == nil {
			err = h.cmd.Start()
		}
	}
	if err != nil {
		return fmt.Errorf("spawn whip-computer: %w", err)
	}
	h.stdin = stdin
	h.reader = bufio.NewReaderSize(stdout, 4<<20)

	// First line is the version announcement (plain text, not JSON).
	announce, err := h.readLineTimeout(10 * time.Second)
	if err != nil {
		h.kill()
		return fmt.Errorf("whip-computer did not announce: %w", err)
	}
	if announce != ProtocolVersion {
		h.kill()
		return fmt.Errorf("whip-computer protocol mismatch: got %q, want %q (rebuild the driver: task driver)", announce, ProtocolVersion)
	}

	// Handshake RPC validates the token both ways.
	var hs struct {
		Version string `json:"version"`
	}
	if err := h.callLocked(context.Background(), "handshake", map[string]any{"token": h.token}, &hs); err != nil {
		h.kill()
		return fmt.Errorf("whip-computer handshake: %w", err)
	}
	if hs.Version != ProtocolVersion {
		h.kill()
		return fmt.Errorf("whip-computer handshake version mismatch: %q", hs.Version)
	}
	return nil
}

// readLineTimeout reads one \n-terminated line, honoring the deadline by
// racing the read against a timer (bufio has no deadlines on pipes).
func (h *Helper) readLineTimeout(d time.Duration) (string, error) {
	type res struct {
		s   string
		err error
	}
	ch := make(chan res, 1)
	go func() {
		s, err := h.reader.ReadString('\n')
		ch <- res{s, err}
	}()
	select {
	case r := <-ch:
		if r.err != nil {
			return "", r.err
		}
		// Trim trailing newline.
		for len(r.s) > 0 && (r.s[len(r.s)-1] == '\n' || r.s[len(r.s)-1] == '\r') {
			r.s = r.s[:len(r.s)-1]
		}
		return r.s, nil
	case <-time.After(d):
		return "", errors.New("timeout waiting for whip-computer")
	}
}

func (h *Helper) kill() {
	if h.stdin != nil {
		_ = h.stdin.Close()
	}
	if h.process != nil {
		_ = h.process.Kill()
		_ = h.process.Wait()
		h.process = nil
	}
	if h.cmd != nil && h.cmd.Process != nil {
		_ = h.cmd.Process.Kill()
		_, _ = h.cmd.Process.Wait()
	}
	h.cmd = nil
	h.stdin = nil
}

func (h *Helper) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.kill()
}

// restartLocked respawns after a crash. Caller holds h.mu.
func (h *Helper) restartLocked() error {
	h.kill()
	return h.spawn()
}

// Call invokes method with params and unmarshals the result into out.
// Restarts the helper once on transport failure (crash restart, plan §2).
func (h *Helper) Call(ctx context.Context, method string, params map[string]any, out any) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if params == nil {
		params = map[string]any{}
	}
	params["token"] = h.token
	err := h.callLocked(ctx, method, params, out)
	if err == nil {
		return nil
	}
	if rpcErr, ok := errors.AsType[*rpcError](err); ok {
		if rpcErr.Code == errCodeStaleGeneration {
			return &StaleError{Msg: rpcErr.Message}
		}
		return rpcErr
	}
	// Transport failure: restart once and retry.
	if rerr := h.restartLocked(); rerr != nil {
		return fmt.Errorf("whip-computer crashed and restart failed: %w (restart: %w)", err, rerr)
	}
	params["token"] = h.token // token changed on respawn
	err = h.callLocked(ctx, method, params, out)
	var rpcErr2 *rpcError
	if errors.As(err, &rpcErr2) && rpcErr2.Code == errCodeStaleGeneration {
		return &StaleError{Msg: rpcErr2.Message}
	}
	return err
}

// callLocked performs one round-trip. Caller holds h.mu and set the token.
func (h *Helper) callLocked(ctx context.Context, method string, params map[string]any, out any) error {
	if h.cmd == nil && h.process == nil {
		return errors.New("whip-computer not running")
	}
	h.nextID++
	req := rpcRequest{JSONRPC: "2.0", ID: h.nextID, Method: method, Params: params}
	data, err := json.Marshal(req)
	if err != nil {
		return err
	}
	if _, err := h.stdin.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("write to whip-computer: %w", err)
	}

	timeout := 30 * time.Second
	if method == "permissions.request" {
		timeout = 150 * time.Second // the in-turn TCC wait
	}
	if dl, ok := ctx.Deadline(); ok && time.Until(dl) < timeout {
		timeout = time.Until(dl)
	}
	line, err := h.readLineTimeout(timeout)
	if err != nil {
		return fmt.Errorf("read from whip-computer: %w", err)
	}
	var resp rpcResponse
	if err := json.Unmarshal([]byte(line), &resp); err != nil {
		return fmt.Errorf("bad frame from whip-computer: %q", line[:min(len(line), 120)])
	}
	if resp.Error != nil {
		return resp.Error
	}
	if out != nil && len(resp.Result) > 0 {
		return json.Unmarshal(resp.Result, out)
	}
	return nil
}

// DecodeScreenshot turns a Screenshot into JPEG bytes (nil when absent).
func (s *Screenshot) Decode() ([]byte, error) {
	if s == nil || s.JPEGBase64 == "" {
		return nil, nil
	}
	return base64.StdEncoding.DecodeString(s.JPEGBase64)
}
