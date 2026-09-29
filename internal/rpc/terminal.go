package rpc

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/context-labs/whip/internal/bashrun"
	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/runtime"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/terminal"
)

var errTerminalWriteUncertain = errors.New("terminal input was not acknowledged; do not replay keystrokes")

func terminalDispatch(ctx context.Context, r *runtime.Runtime, host HostServices, method string, raw json.RawMessage) (any, error) {
	if host.Terminals == nil {
		return nil, terminal.ErrClosed
	}
	// The operation schema has already validated this required field. Check it
	// before any lookup or shell creation, including reads and uncertain retries.
	var epoch struct {
		ProcessEpoch protocol.ID `json:"process_epoch"`
	}
	if err := json.Unmarshal(raw, &epoch); err != nil {
		return nil, session.ErrInvalid
	}
	if string(epoch.ProcessEpoch) != r.ProcessEpoch() {
		return nil, ErrIdentity
	}
	manager := host.Terminals
	find := func(id protocol.ID) (*terminal.Terminal, error) {
		value, ok := manager.Get(string(id))
		if !ok {
			return nil, terminal.ErrNotFound
		}
		return value, nil
	}
	switch method {
	case "terminal.open":
		return decode(raw, func(p protocol.TerminalOpenParams) (any, error) {
			if !filepath.IsAbs(p.Cwd) || len(p.Cwd) > 4096 || strings.IndexByte(p.Cwd, 0) >= 0 {
				return nil, fmt.Errorf("%w: terminal cwd must be an absolute path of at most 4096 bytes", session.ErrInvalid)
			}
			cwd, err := filepath.EvalSymlinks(p.Cwd)
			if err != nil {
				return nil, fmt.Errorf("%w: terminal working directory is unavailable", session.ErrInvalid)
			}
			shell, err := exec.LookPath(bashrun.UserShell())
			if err != nil {
				return nil, fmt.Errorf("%w: host login shell is unavailable", session.ErrInvalid)
			}
			shell, err = filepath.Abs(shell)
			if err != nil {
				return nil, session.ErrInvalid
			}
			value, err := manager.Open(terminal.Options{Shell: shell, Args: []string{"-l"}, Cwd: cwd, Env: map[string]string{"TERM": "xterm-256color", "COLORTERM": "truecolor"}, Cols: p.Cols, Rows: p.Rows})
			if err != nil {
				return nil, err
			}
			return terminalInfo(r.ProcessEpoch(), value.Status()), nil
		})
	case "terminal.list":
		items := manager.List()
		result := protocol.TerminalList{ProcessEpoch: protocol.ID(r.ProcessEpoch()), Items: make([]protocol.TerminalInfo, 0, len(items))}
		for _, value := range items {
			result.Items = append(result.Items, terminalInfo(r.ProcessEpoch(), value))
		}
		return result, nil
	case "terminal.read":
		return decode(raw, func(p protocol.TerminalReadParams) (any, error) {
			value, err := find(p.ID)
			if err != nil {
				return nil, err
			}
			page, err := value.Read(int64(p.Cursor), p.Limit)
			if err != nil {
				return nil, err
			}
			return protocol.TerminalPage{Terminal: terminalInfo(r.ProcessEpoch(), page.Status), From: protocol.Counter(page.From), Next: protocol.Counter(page.Next), End: protocol.Counter(page.End), Truncated: page.Truncated, DataBase64: base64.StdEncoding.EncodeToString(page.Data)}, nil
		})
	case "terminal.write":
		return decode(raw, func(p protocol.TerminalWriteParams) (any, error) {
			data, err := base64.StdEncoding.Strict().DecodeString(p.DataBase64)
			if err != nil || len(data) == 0 || len(data) > terminal.MaxWriteBytes {
				return nil, fmt.Errorf("%w: terminal input requires 1..16384 encoded bytes", session.ErrInvalid)
			}
			value, err := find(p.ID)
			if err != nil {
				return nil, err
			}
			if err := value.WriteContext(ctx, data); err != nil {
				if errors.Is(err, terminal.ErrExited) {
					return nil, err
				}
				return nil, errTerminalWriteUncertain
			}
			return protocol.TerminalAccepted{Accepted: true}, nil
		})
	case "terminal.resize":
		return decode(raw, func(p protocol.TerminalResizeParams) (any, error) {
			value, err := find(p.ID)
			if err != nil {
				return nil, err
			}
			if err := value.Resize(p.Cols, p.Rows); err != nil {
				return nil, err
			}
			return terminalInfo(r.ProcessEpoch(), value.Status()), nil
		})
	case "terminal.close":
		return decode(raw, func(p protocol.TerminalRef) (any, error) {
			err := manager.Close(string(p.ID))
			return protocol.TerminalAccepted{Accepted: err == nil}, err
		})
	}
	return nil, ErrMethod
}

func terminalInfo(epoch string, value terminal.Status) protocol.TerminalInfo {
	return protocol.TerminalInfo{ProcessEpoch: protocol.ID(epoch), ID: protocol.ID(value.ID), Cwd: value.Cwd, Shell: value.Shell, Cols: int(value.Cols), Rows: int(value.Rows), Closing: value.Closing, Exited: value.Exited, ExitCode: value.ExitCode, Signal: value.Signal, Start: protocol.Counter(value.Start), End: protocol.Counter(value.End), CreatedAt: value.CreatedAt.Format(time.RFC3339Nano)}
}
