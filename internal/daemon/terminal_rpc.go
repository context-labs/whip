package daemon

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/context-labs/whip/internal/buildinfo"
	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/terminal"
	"github.com/context-labs/whip/internal/tools/bashrun"
)

// maxTerminalCwdBytes matches the host directory operations' path bound.
const maxTerminalCwdBytes = 4096

// handleTerminal serves workspace terminal tabs. Every operation is scoped to
// the calling connection: output goes to whichever connection attached last,
// and a connection that drops is detached without ending its shell. Network
// clients are refused unless the operator enabled terminals for the listener.
func (s *Server) handleTerminal(connection *serverConn, request rpcMessage) (any, *RPCError, bool) {
	if !strings.HasPrefix(request.Method, "terminal.") {
		return nil, nil, false
	}
	manager := s.daemon.terminals
	if manager == nil {
		return nil, rpcFailure(-32003, "terminals are unavailable on this daemon"), true
	}
	if connection.network && !s.options.Network.Terminals {
		return nil, rpcFailure(-32012, "terminals are disabled for network clients; start the daemon with "+buildinfo.Env("NETWORK_TERMINALS")+"=1 to allow them"), true
	}
	switch request.Method {
	case "terminal.open":
		var params protocol.TerminalOpenParams
		if err := decodeProviderParams(request.Params, &params); err != nil {
			return nil, rpcFailure(-32602, err.Error()), true
		}
		if !validTerminalDimension(params.Cols) || !validTerminalDimension(params.Rows) {
			return nil, rpcFailure(-32602, "terminal size must be within 1..1000 columns and rows"), true
		}
		cwd, err := s.terminalCwd(connection.ctx, params)
		if err != nil {
			return nil, rpcFailure(-32602, err.Error()), true
		}
		shell := bashrun.UserShell()
		term, err := manager.Open(terminal.Options{
			Shell: shell, Args: []string{"-l"}, Cwd: cwd,
			Env:  bashrun.ChildEnvironment(map[string]string{"TERM": "xterm-256color", "COLORTERM": "truecolor"}),
			Cols: uint16(params.Cols), Rows: uint16(params.Rows), //nolint:gosec // G115: both values were bounded to 1..1000 above.
		})
		if err != nil {
			return nil, terminalFailure(err), true
		}
		return protocol.TerminalOpenResult{ID: term.ID, Shell: shell, Cwd: cwd}, nil, true
	case "terminal.attach":
		var params protocol.TerminalAttachParams
		if err := decodeProviderParams(request.Params, &params); err != nil {
			return nil, rpcFailure(-32602, err.Error()), true
		}
		status, from, err := manager.Attach(params.ID, params.Cursor, connection)
		if err != nil {
			return nil, terminalFailure(err), true
		}
		return protocol.TerminalAttachResult{
			Cursor: from, Cwd: status.Cwd, Cols: int(status.Cols), Rows: int(status.Rows),
			Exited: status.Exited, ExitCode: status.ExitCode, Signal: status.Signal,
		}, nil, true
	case "terminal.write":
		var params protocol.TerminalWriteParams
		if err := decodeProviderParams(request.Params, &params); err != nil {
			return nil, rpcFailure(-32602, err.Error()), true
		}
		term, ok := manager.Get(params.ID)
		if !ok {
			return nil, terminalFailure(terminal.ErrNotFound), true
		}
		if err := term.Write(params.Bytes); err != nil {
			return nil, terminalFailure(err), true
		}
		return protocol.Accepted{Accepted: true}, nil, true
	case "terminal.resize":
		var params protocol.TerminalResizeParams
		if err := decodeProviderParams(request.Params, &params); err != nil {
			return nil, rpcFailure(-32602, err.Error()), true
		}
		if !validTerminalDimension(params.Cols) || !validTerminalDimension(params.Rows) {
			return nil, rpcFailure(-32602, "terminal size must be within 1..1000 columns and rows"), true
		}
		term, ok := manager.Get(params.ID)
		if !ok {
			return nil, terminalFailure(terminal.ErrNotFound), true
		}
		if err := term.Resize(uint16(params.Cols), uint16(params.Rows)); err != nil { //nolint:gosec // G115: both values were bounded to 1..1000 above.
			return nil, terminalFailure(err), true
		}
		return protocol.Accepted{Accepted: true}, nil, true
	case "terminal.close":
		var params protocol.TerminalIDParams
		if err := decodeProviderParams(request.Params, &params); err != nil {
			return nil, rpcFailure(-32602, err.Error()), true
		}
		if err := manager.Close(params.ID); err != nil {
			return nil, terminalFailure(err), true
		}
		return protocol.Accepted{Accepted: true}, nil, true
	}
	return nil, nil, false
}

func validTerminalDimension(value int) bool { return value >= 1 && value <= terminal.MaxDimension }

func terminalFailure(err error) *RPCError {
	switch {
	case errors.Is(err, terminal.ErrNotFound), errors.Is(err, terminal.ErrExited), errors.Is(err, terminal.ErrClosed):
		return rpcFailure(-32003, err.Error())
	case errors.Is(err, terminal.ErrLimit):
		return rpcFailure(-32009, err.Error())
	}
	return rpcFailure(-32602, err.Error())
}

// terminalCwd resolves the shell's directory: an explicit absolute path, else
// the named session's directory when it still exists, else the daemon user's
// home. The session lookup is a hint, so a deleted directory is not an error.
func (s *Server) terminalCwd(ctx context.Context, params protocol.TerminalOpenParams) (string, error) {
	if params.Cwd != "" {
		if !filepath.IsAbs(params.Cwd) || len(params.Cwd) > maxTerminalCwdBytes {
			return "", errors.New("terminal cwd must be an absolute path of at most 4096 bytes")
		}
		return params.Cwd, nil
	}
	if params.RootID != "" && s.daemon.store != nil {
		if metadata, err := s.daemon.store.SessionMetadata(ctx, params.RootID); err == nil && filepath.IsAbs(metadata.CWD) {
			if info, statErr := os.Stat(metadata.CWD); statErr == nil && info.IsDir() {
				return metadata.CWD, nil
			}
		}
	}
	return os.UserHomeDir()
}

var _ terminal.Sink = (*serverConn)(nil)

// Output delivers one chunk as a notification. A terminal flood must not fill
// the connection's outbound queue, because send closes the whole connection
// when it overflows; instead Output waits for the writer to drain, which
// stalls the shell through the terminal's bounded queue.
func (c *serverConn) Output(id string, cursor int64, data []byte) bool {
	for {
		c.mu.Lock()
		pending, closed := c.outBytes, c.closed
		c.mu.Unlock()
		if closed {
			return false
		}
		if pending < c.server.options.MaxOutboundBytes/2 {
			break
		}
		select {
		case <-c.done:
			return false
		case <-time.After(5 * time.Millisecond):
		}
	}
	return c.notify("terminal.output", protocol.TerminalOutputParams{ID: id, Cursor: cursor, Bytes: data})
}

func (c *serverConn) Exited(id string, code int, signal string) {
	c.notify("terminal.exited", protocol.TerminalExitedParams{ID: id, ExitCode: code, Signal: signal})
}

func (c *serverConn) Detached(id string) {
	c.notify("terminal.detached", protocol.TerminalDetachedParams{ID: id})
}
