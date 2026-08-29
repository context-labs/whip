// Package bashrun executes shell commands (via the user's shell, see
// userShell) for the agent, with optional PTY
// support for interactive programs (sudo, ssh, gpg) that prompt on the
// controlling terminal.
//
// The default (non-interactive) path runs the command in a new session with no
// controlling terminal, so a program that wants to read a password from /dev/tty
// fails fast ("a terminal is required") instead of hanging indefinitely on
// whip's terminal — which is what used to lock up the whole agent.
//
// The interactive path runs the command in a PTY. Keystrokes the user types are
// forwarded to the PTY and PTY output streams back to the caller. If the child
// goes quiet for a while (likely waiting for input), the caller is told to show
// a countdown; if input is still absent after the inactivity timeout, the
// command is killed so whip never hangs forever.
package bashrun

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
)

// userShell resolves the user's login shell: $SHELL first, then the passwd
// entry, then bash. `-c` semantics are POSIX, so zsh/fish/etc. all run the
// same command strings bash would.
func userShell() string {
	if sh := os.Getenv("SHELL"); sh != "" {
		return sh
	}
	if sh := passwdShell(); sh != "" {
		return sh
	}
	return "bash"
}

// passwdShell reads the current user's shell field from /etc/passwd (last
// colon-separated field of their entry). Empty when unresolvable — NIS/LDAP
// users fall through to bash, same as before this change.
func passwdShell() string {
	u, err := user.Current()
	if err != nil {
		return ""
	}
	data, err := os.ReadFile("/etc/passwd")
	if err != nil {
		return ""
	}
	for line := range strings.Lines(string(data)) {
		fields := strings.Split(strings.TrimRight(line, "\n"), ":")
		if len(fields) == 7 && fields[2] == u.Uid {
			return fields[6]
		}
	}
	return ""
}

// Result is the outcome of one command run.
type Result struct {
	// Output is the combined stdout+stderr captured for the model.
	Output string
	// Stdout and Stderr are populated when Options.SeparateOutput is true.
	// Output remains populated too so callers can keep using the combined
	// stream while protocols inspect their decision and diagnostic channels
	// independently.
	Stdout string
	Stderr string
	// Exit is the human-readable exit status fed back to the model. It is
	// empty for a clean exit 0.
	Exit string
	// ExitCode is the process exit status. It is -1 when the process did not
	// start, timed out, was cancelled, or died by signal.
	ExitCode int
	// Truncated reports MaxOutputBytes capped at least one captured stream.
	Truncated bool
	// TimedOut reports the command exceeded its wall-clock timeout.
	TimedOut bool
	// Killed reports the command was killed by us (timeout, inactivity
	// timeout, or cancellation) rather than exiting on its own.
	Killed bool
	// Interactive reports whether the interactive PTY path was used.
	Interactive bool
}

// Options configure a single run.
type Options struct {
	Command string
	// Stdin is delivered to the command in non-interactive mode. nil keeps the
	// existing /dev/null behavior; an empty non-nil slice delivers immediate
	// EOF after a zero-byte input.
	Stdin []byte
	// Env overlays the inherited environment. Per-run values replace ambient
	// keys instead of leaving duplicate entries whose lookup semantics vary by
	// platform.
	Env []string
	// Dir sets the command's working directory. Empty inherits the process cwd.
	Dir string
	// SeparateOutput captures stdout and stderr independently in Result while
	// still maintaining Output as the combined stream.
	SeparateOutput bool
	// MaxOutputBytes caps each captured stream. <=0 leaves capture unbounded.
	MaxOutputBytes int
	// Timeout is the hard wall-clock cap. <=0 means 120s.
	Timeout time.Duration
	// Interactive runs the command in a PTY so sudo/ssh-like password prompts
	// work. Requires Keys, OnOutput, OnAwaitInput to be wired by the caller.
	Interactive bool
	// InactivityTimeout is the interactive-mode cap: if the child produces no
	// output and receives no forwarded keystroke for this long, the command is
	// killed as "timed out waiting for input". <=0 means 15s.
	InactivityTimeout time.Duration
	// OnOutput streams PTY stdout/stderr deltas back to the caller (live
	// transcript). Interactive only; safe to call from the run goroutine.
	OnOutput func(chunk string)
	// OnUpdate reports the accumulated combined output while a non-interactive
	// command runs, throttled to at most one call per ~100ms (pi's bash
	// onUpdate). Invoked from the run's own goroutines; must not block.
	// The final output is delivered via Result, not OnUpdate; one trailing
	// call may land after Run returns if a tick was in flight.
	OnUpdate func(outputSoFar string)
	// OnAwaitInput is called once per second while the child is quiet and
	// likely waiting for input; secLeft is the seconds remaining before the
	// inactivity timeout fires. Interactive only.
	OnAwaitInput func(secLeft int)
	// Keys is the channel the caller pushes keystrokes into for forwarding to
	// the PTY. The runner drains it until the command ends, then closes it.
	// Interactive only; may be nil for a fire-and-forget interactive run.
	Keys <-chan []byte
}

// Run executes the command and returns its result.
//
// In non-interactive mode Run blocks until the command finishes or its timeout
// fires. In interactive mode Run blocks until the command finishes, the hard
// timeout fires, or the inactivity timeout fires.
func Run(ctx context.Context, opts Options) Result {
	if opts.Timeout <= 0 {
		opts.Timeout = 120 * time.Second
	}
	if opts.Interactive && opts.InactivityTimeout <= 0 {
		opts.InactivityTimeout = 15 * time.Second
	}

	ctx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, userShell(), "-c", opts.Command)
	cmd.Env = mergeEnv(os.Environ(), markersSnapshot(), opts.Env)
	cmd.Dir = opts.Dir
	if opts.Stdin != nil {
		cmd.Stdin = bytes.NewReader(opts.Stdin)
	}

	if opts.Interactive {
		return runInteractive(ctx, cmd, opts)
	}
	return runPiped(ctx, cmd, opts)
}

// mergeEnv applies overlays in order while emitting each key once. Values
// without '=' are preserved as distinct entries; exec.Cmd accepts them and
// callers outside the hook protocol may already rely on that behavior.
func mergeEnv(base []string, overlays ...[]string) []string {
	out := make([]string, 0, len(base))
	index := make(map[string]int, len(base))
	apply := func(env []string) {
		for _, item := range env {
			key, _, ok := strings.Cut(item, "=")
			if !ok {
				out = append(out, item)
				continue
			}
			if i, found := index[key]; found {
				out[i] = item
				continue
			}
			index[key] = len(out)
			out = append(out, item)
		}
	}
	apply(base)
	for _, overlay := range overlays {
		apply(overlay)
	}
	return out
}

// runPiped runs the command with stdout/stderr captured, stdin wired to
// /dev/null, and a fresh session with no controlling terminal. A program that
// tries to open /dev/tty for a password fails fast rather than hanging on
// whip's terminal.
//
// The subtlety that justifies hand-rolling Start/Wait: a detached grandchild
// (nohup, `sleep 30 &`, a daemonized server) inherits the stdout/stderr pipes
// and keeps them open after the direct child exits. cmd.Run / cmd.Wait would
// block on io.Copy waiting for pipe EOF that never comes — the agent hangs
// even though the command "finished". We capture via explicit pipes and close
// our read ends the moment the process exits, so a lingering grandchild can't
// stall us. (We don't get the grandchild's later output, which is correct —
// it outlived the command.)
// updateInterval is the minimum gap between OnUpdate calls while a command
// runs (pi throttles its bash onUpdate at 100ms too).
const updateInterval = 100 * time.Millisecond

func runPiped(ctx context.Context, cmd *exec.Cmd, opts Options) Result {
	// Hand-rolled pipes, NOT cmd.StdoutPipe: Wait() closes StdoutPipe's read
	// ends the moment the child exits, discarding kernel-buffered output the
	// drain goroutines haven't read yet (lost output on fast commands). With
	// our own pipes Wait touches nothing and we control when reads end.
	stdout, outW, err := os.Pipe()
	if err != nil {
		return Result{Exit: "pipe: " + err.Error(), ExitCode: -1}
	}
	stderr, errW, err := os.Pipe()
	if err != nil {
		_ = stdout.Close()
		_ = outW.Close()
		return Result{Exit: "pipe: " + err.Error(), ExitCode: -1}
	}
	cmd.Stdout = outW
	cmd.Stderr = errW
	if cmd.Stdin == nil {
		devNull := openDevNull()
		if devNull != nil {
			cmd.Stdin = devNull
			defer devNull.Close()
		}
	}
	// Setsid gives the child a new session with no controlling terminal, so a
	// program that insists on /dev/tty fails immediately instead of grabbing
	// whip's terminal and blocking its input loop.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	if err := cmd.Start(); err != nil {
		_ = stdout.Close()
		_ = outW.Close()
		_ = stderr.Close()
		_ = errW.Close()
		return Result{Exit: exitString(err), ExitCode: -1}
	}
	// Drop our copies of the write ends: the drains must see EOF when the
	// child (and any grandchildren holding the pipes) are done writing.
	_ = outW.Close()
	_ = errW.Close()
	track(cmd) // register for KillAll on whip exit
	defer untrack(cmd)

	// Drain both pipes concurrently; the readers finish on pipe EOF (process
	// exit) OR when we close them below after Wait returns.
	combinedLimit := opts.MaxOutputBytes
	if opts.SeparateOutput && combinedLimit > 0 && combinedLimit <= int(^uint(0)>>1)/2 {
		// stdout and stderr each receive their own budget below. Give the
		// auxiliary combined view both budgets too, otherwise two individually
		// valid streams could make Result.Truncated report a false positive.
		combinedLimit *= 2
	}
	out := cappedBuffer{max: combinedLimit}
	stdoutOnly := cappedBuffer{max: opts.MaxOutputBytes}
	stderrOnly := cappedBuffer{max: opts.MaxOutputBytes}
	var mu sync.Mutex
	var wg sync.WaitGroup
	wg.Add(2)
	drain := func(r io.Reader, separate *cappedBuffer) {
		defer wg.Done()
		buf := make([]byte, 4096)
		for {
			n, rerr := r.Read(buf)
			if n > 0 {
				mu.Lock()
				if opts.SeparateOutput {
					before := separate.Len()
					_, _ = separate.Write(buf[:n])
					retained := separate.Len() - before
					_, _ = out.Write(buf[:retained])
				} else {
					_, _ = out.Write(buf[:n])
				}
				mu.Unlock()
			}
			if rerr != nil {
				return
			}
		}
	}
	go drain(stdout, &stdoutOnly)
	go drain(stderr, &stderrOnly)
	// Stream throttled snapshots of the accumulated output to the caller so
	// in-flight progress is visible before the command exits. One goroutine,
	// owned by this run, exits when updatesDone closes below.
	var updatesDone chan struct{}
	if opts.OnUpdate != nil {
		updatesDone = make(chan struct{})
		defer close(updatesDone)
		go func() {
			ticker := time.NewTicker(updateInterval)
			defer ticker.Stop()
			for {
				select {
				case <-updatesDone:
					return
				case <-ticker.C:
					mu.Lock()
					snap := out.String()
					mu.Unlock()
					opts.OnUpdate(snap)
				}
			}
		}()
	}
	// Kill the process group if the run context is cancelled/times out.
	watchDone := make(chan struct{})
	defer close(watchDone)
	go func() {
		select {
		case <-ctx.Done():
			if cmd.Process != nil {
				_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			}
		case <-watchDone:
		}
	}()

	waitErr := cmd.Wait()
	// The direct child exited. On the common path the drains hit EOF at once
	// (all write ends are closed) and finish having read everything. The timer
	// bounds the detached-grandchild case only: a lingering writer holds the
	// pipe open, so the drains never see EOF and we cut them off below.
	drained := make(chan struct{})
	go func() { wg.Wait(); close(drained) }()
	graceTimer := time.NewTimer(500 * time.Millisecond)
	select {
	case <-drained:
		graceTimer.Stop()
	case <-graceTimer.C:
	}
	// Close our read ends so any still-blocked drain goroutines see EOF (a
	// detached grandchild holding the write end must not stall us).
	_ = stdout.Close()
	_ = stderr.Close()
	wg.Wait()

	res := Result{
		Output:    out.String(),
		ExitCode:  processExitCode(waitErr),
		Truncated: out.truncated || stdoutOnly.truncated || stderrOnly.truncated,
	}
	if opts.SeparateOutput {
		res.Stdout = stdoutOnly.String()
		res.Stderr = stderrOnly.String()
	}
	if ctx.Err() == context.DeadlineExceeded {
		res.TimedOut = true
		res.Killed = true
		res.Exit = "timed out"
		res.ExitCode = -1
		return res
	}
	if isCancelled(ctx, waitErr) {
		res.Killed = true
		res.Exit = "cancelled"
		res.ExitCode = -1
		return res
	}
	res.Exit = exitString(waitErr)
	if waitErr != nil {
		res.Killed = isKilledBySignal(waitErr)
	}
	return res
}

// cappedBuffer accepts every write while retaining at most max bytes. It is
// used under runPiped's capture mutex, so it needs no synchronization of its
// own and preserves io.Writer's "accepted all bytes" contract for io.Copy.
type cappedBuffer struct {
	bytes.Buffer
	max       int
	truncated bool
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	if b.max <= 0 {
		_, _ = b.Buffer.Write(p)
		return n, nil
	}
	remaining := b.max - b.Len()
	if remaining <= 0 {
		b.truncated = b.truncated || n > 0
		return n, nil
	}
	if len(p) > remaining {
		_, _ = b.Buffer.Write(p[:remaining])
		b.truncated = true
		return n, nil
	}
	_, _ = b.Buffer.Write(p)
	return n, nil
}

// runInteractive runs the command in a PTY. sudo, ssh, gpg and friends detect a
// real terminal and prompt normally; the password is never echoed into the
// transcript because the PTY slave's ECHO is off for the master and the runner
// forwards raw bytes, not display text.
func runInteractive(ctx context.Context, cmd *exec.Cmd, opts Options) Result {
	// Setsid + Setctty make pty.Start give the child a controlling terminal
	// that is the pty slave — exactly what sudo wants.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true}

	ptmx, err := pty.Start(cmd)
	if err != nil {
		// Fall back to the safe non-interactive path; an interactive failure
		// must never hang the agent.
		return runPiped(ctx, cmd, opts)
	}
	defer ptmx.Close()
	track(cmd) // register for KillAll on whip exit
	defer untrack(cmd)

	// Kill the whole process group (bash + any children) on timeout/cancel so
	// nothing outlives the run.
	stop := sync.OnceFunc(func() {
		if cmd.Process != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
	})
	go func() {
		<-ctx.Done()
		stop()
	}()

	var buf bytes.Buffer
	outCh := make(chan []byte, 16)

	// Output pump: copy PTY -> caller + buffer; on read error the child has
	// exited (or the PTY closed), so we signal end-of-stream with a nil chunk.
	// Every send guards on ctx.Done so the pump can never block forever after
	// the main loop has returned (deferred ptmx.Close fires ctx cancel via
	// Run's deferred cancel, unblocking any in-flight send too).
	go func() {
		tmp := make([]byte, 4096)
		for {
			n, rerr := ptmx.Read(tmp)
			if n > 0 {
				cp := make([]byte, n)
				copy(cp, tmp[:n])
				select {
				case outCh <- cp:
				case <-ctx.Done():
					return
				}
			}
			if rerr != nil {
				select {
				case outCh <- nil:
				case <-ctx.Done():
				}
				return
			}
		}
	}()

	// Quiet clock: any output or forwarded keystroke resets it. When the clock
	// exceeds InactivityTimeout we kill the command.
	quiet := time.Now()
	var quietMu sync.Mutex
	touch := func() {
		quietMu.Lock()
		quiet = time.Now()
		quietMu.Unlock()
	}

	// Key forwarder: write bytes to the PTY master; any keystroke counts as
	// activity and disarms the inactivity timer.
	keyStop := make(chan struct{})
	defer close(keyStop)
	if opts.Keys != nil {
		go func() {
			for {
				select {
				case b, ok := <-opts.Keys:
					if !ok {
						return
					}
					if len(b) > 0 {
						_, _ = ptmx.Write(b)
					}
					touch()
				case <-keyStop:
					return
				}
			}
		}()
	}

	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case chunk, ok := <-outCh:
			// ok==false OR nil chunk => the output pump ended (PTY closed,
			// command exited). Wait for the child and return.
			if !ok || chunk == nil {
				waitErr := cmd.Wait()
				res := Result{Output: buf.String(), ExitCode: processExitCode(waitErr), Interactive: true}
				if ctx.Err() == context.DeadlineExceeded {
					res.TimedOut = true
					res.Killed = true
					res.Exit = "timed out"
					res.ExitCode = -1
					return res
				}
				if isCancelled(ctx, waitErr) {
					res.Killed = true
					res.Exit = "cancelled"
					res.ExitCode = -1
					return res
				}
				res.Exit = exitString(waitErr)
				return res
			}
			buf.Write(chunk)
			if opts.OnOutput != nil {
				opts.OnOutput(string(chunk))
			}
			touch()
		case <-ticker.C:
			quietMu.Lock()
			idle := time.Since(quiet)
			quietMu.Unlock()
			// A context kill outranks the inactivity timer. Without this the
			// kill races the output pump's end-of-stream: whichever arm of
			// this select won decided whether the same event was reported as
			// "timed out"/"cancelled" or as "timed out waiting for input".
			if ctxErr := ctx.Err(); ctxErr != nil {
				stop()
				_ = cmd.Wait()
				res := Result{Output: buf.String(), ExitCode: -1, Killed: true, Interactive: true}
				if errors.Is(ctxErr, context.DeadlineExceeded) {
					res.TimedOut = true
					res.Exit = "timed out"
				} else {
					res.Exit = "cancelled"
				}
				return res
			}
			if idle >= opts.InactivityTimeout {
				stop()
				_ = cmd.Wait()
				res := Result{
					Output:      buf.String(),
					Exit:        "timed out waiting for input",
					ExitCode:    -1,
					Killed:      true,
					Interactive: true,
				}
				res.Output += fmt.Sprintf(
					"\n[whip: interactive command killed after %s with no input]",
					opts.InactivityTimeout.Round(time.Second),
				)
				return res
			}
			if opts.OnAwaitInput != nil {
				secs := max(int((opts.InactivityTimeout-idle+time.Second-1)/time.Second), 0)
				opts.OnAwaitInput(secs)
			}
		}
	}
}

// exitString renders the exit status the way the existing bash tool did: empty
// for a clean exit 0, "(exit: N)" or "(exit: signal X)" otherwise.
func exitString(err error) string {
	if err == nil {
		return ""
	}
	if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
		return fmt.Sprintf("(exit: %s)", exitErr)
	}
	return fmt.Sprintf("(exit: %v)", err)
}

func processExitCode(err error) int {
	if err == nil {
		return 0
	}
	if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
		return exitErr.ExitCode()
	}
	return -1
}

// isKilledBySignal reports whether the error was a kill-by-signal.
func isKilledBySignal(err error) bool {
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return false
	}
	if ws, ok := exitErr.Sys().(syscall.WaitStatus); ok {
		return ws.Signaled()
	}
	return false
}

// isCancelled reports whether the context was cancelled (user interrupt) and
// the error reflects that.
func isCancelled(ctx context.Context, _ error) bool {
	return errors.Is(ctx.Err(), context.Canceled)
}

// openDevNull returns /dev/null for a child's stdin, or nil on failure.
func openDevNull() *os.File {
	f, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return nil
	}
	return f
}

// Registry of in-flight child processes so KillAll can guarantee none outlive
// whip. track is called right after a successful Start; untrack after Wait.
var (
	trackMu sync.Mutex
	tracked = map[int]*exec.Cmd{}
)

func track(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	trackMu.Lock()
	tracked[cmd.Process.Pid] = cmd
	trackMu.Unlock()
}

func untrack(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	trackMu.Lock()
	delete(tracked, cmd.Process.Pid)
	trackMu.Unlock()
}

// KillAll SIGKILLs every tracked child process group and waits briefly for
// them to die. Called on whip exit so an agent-spawned server or watcher
// never outlives the harness. Safe to call more than once.
func KillAll() {
	trackMu.Lock()
	procs := make([]*exec.Cmd, 0, len(tracked))
	for _, c := range tracked {
		procs = append(procs, c)
	}
	trackMu.Unlock()
	for _, c := range procs {
		if c.Process != nil {
			// negative pid kills the whole process group (bash + children)
			_ = syscall.Kill(-c.Process.Pid, syscall.SIGKILL)
		}
	}
	// Give the kernel a moment to reap; don't block exit indefinitely.
	deadline := time.Now().Add(2 * time.Second)
	for _, c := range procs {
		if c.Process == nil {
			continue
		}
		for time.Now().Before(deadline) {
			if err := c.Process.Signal(syscall.Signal(0)); err != nil {
				break // process gone
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
}

// KeyBytes converts a small set of named special keys to their terminal byte
// sequences. Plain text (KeyRunes) should be forwarded as the raw UTF-8 bytes
// of the runes, not via this helper.
const (
	KeyEnter = "\r"
	KeyEsc   = "\x1b"
	KeyTab   = "\t"
	KeyBS    = "\x7f"
	KeyUp    = "\x1b[A"
	KeyDown  = "\x1b[B"
	KeyRight = "\x1b[C"
	KeyLeft  = "\x1b[D"
)

func KeyBytes(name string) string {
	switch name {
	case "enter":
		return KeyEnter
	case "esc":
		return KeyEsc
	case "tab":
		return KeyTab
	case "backspace", "delete":
		return KeyBS
	case "up":
		return KeyUp
	case "down":
		return KeyDown
	case "right":
		return KeyRight
	case "left":
		return KeyLeft
	}
	return ""
}
