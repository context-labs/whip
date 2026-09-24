package daemon

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/context-labs/whip/internal/tools/bashrun"
)

type daemonInteractiveRunner struct {
	emit func(string, StreamEvent)
	next atomic.Uint64
	mu   sync.Mutex
	id   string
	keys chan []byte
}

func newDaemonInteractiveRunner(emit func(string, StreamEvent)) *daemonInteractiveRunner {
	return &daemonInteractiveRunner{emit: emit}
}

func (r *daemonInteractiveRunner) Run(ctx context.Context, options bashrun.Options) string {
	id := "terminal-" + strconv.FormatUint(r.next.Add(1), 10)
	keys := make(chan []byte, 32)
	r.mu.Lock()
	if r.keys != nil {
		r.mu.Unlock()
		return "interactive terminal is already in use"
	}
	r.id, r.keys = id, keys
	r.mu.Unlock()
	r.emit("stream.terminal.started", StreamEvent{ID: id, Name: "bash"})

	options.Interactive = true
	options.InactivityTimeout = 15 * time.Second
	options.Keys = keys
	options.OnOutput = func(chunk string) {
		r.emit("stream.terminal.output", StreamEvent{ID: id, Text: chunk})
	}
	options.OnAwaitInput = func(seconds int) {
		r.emit("stream.terminal.awaiting", StreamEvent{ID: id, Text: strconv.Itoa(seconds)})
	}
	result := bashrun.Run(ctx, options)

	r.mu.Lock()
	if r.id == id {
		r.id, r.keys = "", nil
	}
	r.mu.Unlock()
	r.emit("stream.terminal.completed", StreamEvent{ID: id, Result: result.Exit})
	return interactiveResult(result)
}

func (r *daemonInteractiveRunner) Send(id string, input []byte) error {
	r.mu.Lock()
	activeID, keys := r.id, r.keys
	r.mu.Unlock()
	if id == "" || id != activeID || keys == nil {
		return errors.New("interactive terminal is no longer active")
	}
	copyOfInput := append([]byte(nil), input...)
	select {
	case keys <- copyOfInput:
		return nil
	default:
		return errors.New("interactive terminal input buffer is full")
	}
}

func (r *AgentSession) SendTerminalInput(id string, input []byte) error {
	if r.interactive == nil {
		return errors.New("interactive terminal is unavailable")
	}
	return r.interactive.Send(id, input)
}

func interactiveResult(result bashrun.Result) string {
	switch {
	case result.Output == "" && result.Exit == "":
		return "(no output)"
	case result.Exit == "":
		return result.Output
	default:
		return result.Output + "\n(" + result.Exit + ")"
	}
}
