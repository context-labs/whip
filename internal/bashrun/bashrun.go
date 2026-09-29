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
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"os/user"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"

	"github.com/context-labs/whip/internal/capability"
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
	// StartError means no command was launched; a nonzero process exit is separate.
	StartError error
	// Output is the combined stdout+stderr captured for the model.
	Output string
	// TotalBytes includes bytes discarded from the bounded retained tail.
	TotalBytes int64
	Truncated  bool
	// Exit is the human-readable exit status fed back to the model. It is
	// empty for a clean exit 0.
	Exit string
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
	Command     string
	Cwd         string
	CwdIdentity os.FileInfo
	RootID      string
	Processes   *capability.ProcessManager
	Env         map[string]string
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
	// The final output is delivered via Result. All callbacks finish before Run returns.
	OnUpdate func(outputSoFar string)
	// OnAwaitInput is called once per second while the child is quiet and
	// likely waiting for input; secLeft is the seconds remaining before the
	// inactivity timeout fires. Interactive only.
	OnAwaitInput func(secLeft int)
	// Keys is the channel the caller pushes keystrokes into for forwarding to
	// the PTY. The runner stops receiving when the command ends; the caller owns the channel.
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

	// Cancellation below kills the process group; CommandContext would kill only the shell.
	cmd := exec.CommandContext(context.WithoutCancel(ctx), userShell(), "-c", opts.Command)
	cmd.Dir = opts.Cwd

	if opts.Interactive {
		return runInteractive(ctx, cmd, opts)
	}
	return runPiped(ctx, cmd, opts)
}

// A caller without an owner receives a private manager with the same bounded,
// allowlisted environment and joined process-group lifetime as owned runs.
func startProcess(ctx context.Context, cmd *exec.Cmd, opts Options, controllingTTY bool) (*capability.Process, func(), error) {
	cleanup := func() {}
	manager := opts.Processes
	rootID := opts.RootID
	if manager == nil {
		manager = capability.NewProcessManager()
		rootID = "shell"
		cleanup = func() { _ = manager.Close() }
	}
	cwd := cmd.Dir
	if cwd == "" {
		var err error
		cwd, err = os.Getwd()
		if err != nil {
			cleanup()
			return nil, func() {}, err
		}
	}
	env := map[string]string{"WHIP": "1", "WHIP_PID": strconv.Itoa(os.Getpid())}
	maps.Copy(env, opts.Env)
	process, err := manager.Start(ctx, rootID, cmd.Path, cmd.Args[1:], capability.ProcessOptions{
		Cwd: cwd, CwdIdentity: opts.CwdIdentity, Env: env, Stdin: cmd.Stdin, Stdout: cmd.Stdout, Stderr: cmd.Stderr, ControllingTTY: controllingTTY,
	})
	if err != nil {
		cleanup()
		return nil, func() {}, err
	}
	return process, cleanup, nil
}

func childEnvironment(overrides map[string]string) []string {
	values := make(map[string]string, len(os.Environ())+len(overrides)+2)
	for _, entry := range os.Environ() {
		if name, value, ok := strings.Cut(entry, "="); ok {
			values[name] = value
		}
	}
	values["WHIP"] = "1"
	values["WHIP_PID"] = strconv.Itoa(os.Getpid())
	maps.Copy(values, overrides)
	env := make([]string, 0, len(values))
	for name, value := range values {
		env = append(env, name+"="+value)
	}
	sort.Strings(env)
	return env
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
		return Result{StartError: err, Exit: "pipe: " + err.Error()}
	}
	stderr, errW, err := os.Pipe()
	if err != nil {
		_ = stdout.Close()
		_ = outW.Close()
		return Result{StartError: err, Exit: "pipe: " + err.Error()}
	}
	cmd.Stdout = outW
	cmd.Stderr = errW
	if devNull := openDevNull(); devNull != nil {
		cmd.Stdin = devNull
		defer devNull.Close()
	}
	process, cleanup, err := startProcess(ctx, cmd, opts, false)
	if err != nil {
		_ = stdout.Close()
		_ = outW.Close()
		_ = stderr.Close()
		_ = errW.Close()
		return Result{StartError: err, Exit: exitString(err)}
	}
	defer cleanup()
	// Drop our copies of the write ends: the drains must see EOF when the
	// child (and any grandchildren holding the pipes) are done writing.
	_ = outW.Close()
	_ = errW.Close()
	// Drain both pipes concurrently; the readers finish on pipe EOF (process
	// exit) OR when we close them below after Wait returns.
	var out outputBuffer
	var mu sync.Mutex
	var wg sync.WaitGroup
	wg.Add(2)
	drain := func(r io.Reader) {
		defer wg.Done()
		buf := make([]byte, 4096)
		for {
			n, rerr := r.Read(buf)
			if n > 0 {
				mu.Lock()
				out.Write(buf[:n])
				mu.Unlock()
			}
			if rerr != nil {
				return
			}
		}
	}
	go drain(stdout)
	go drain(stderr)
	// Stream throttled snapshots of the accumulated output to the caller so
	// in-flight progress is visible before the command exits. One goroutine,
	// owned by this run, exits when updatesDone closes below.
	var updatesDone chan struct{}
	if opts.OnUpdate != nil {
		updatesDone = make(chan struct{})
		updatesJoined := make(chan struct{})
		defer func() { close(updatesDone); <-updatesJoined }()
		go func() {
			defer close(updatesJoined)
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
	waitErr := process.Wait()
	// Foreground descendants belong to this invocation, even when the shell
	// exits before them. Join the complete group before publishing completion.
	process.Stop()
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

	res := Result{Output: out.String(), TotalBytes: out.total, Truncated: out.truncated()}
	if ctx.Err() == context.DeadlineExceeded {
		res.TimedOut = true
		res.Killed = true
		res.Exit = "timed out"
		return res
	}
	if isCancelled(ctx, waitErr) {
		res.Killed = true
		res.Exit = "cancelled"
		return res
	}
	res.Exit = exitString(waitErr)
	if waitErr != nil {
		res.Killed = isKilledBySignal(waitErr)
	}
	return res
}

// runInteractive runs the command in a PTY. sudo, ssh, gpg and friends detect a
// real terminal and prompt normally. The child controls terminal echo; password
// programs must disable echo themselves. The runner never records input separately,
// but anything the child echoes is ordinary output.
func runInteractive(ctx context.Context, cmd *exec.Cmd, opts Options) Result {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	ptmx, tty, err := pty.Open()
	if err != nil {
		fallback := exec.CommandContext(context.WithoutCancel(ctx), userShell(), "-c", opts.Command)
		fallback.Dir = opts.Cwd
		fallback.Env = cmd.Env
		return runPiped(ctx, fallback, opts)
	}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = tty, tty, tty
	process, cleanup, err := startProcess(ctx, cmd, opts, true)
	_ = tty.Close()
	if err != nil {
		_ = ptmx.Close()
		fallback := exec.CommandContext(context.WithoutCancel(ctx), userShell(), "-c", opts.Command)
		fallback.Dir = opts.Cwd
		fallback.Env = cmd.Env
		return runPiped(ctx, fallback, opts)
	}
	var workers sync.WaitGroup
	defer func() {
		cancel()
		process.Stop()
		_ = ptmx.Close()
		workers.Wait()
		cleanup()
	}()
	stop := process.Stop
	// End a foreground invocation when its leader exits, including descendants
	// that keep the PTY open. Wait is safe to call from the result path as well.
	workers.Go(func() {
		_ = process.Wait()
		process.Stop()
	})

	var buf outputBuffer
	outCh := make(chan []byte, 16)

	// Closing the PTY and cancelling sends joins the output pump on every exit.
	workers.Go(func() {
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
	})

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
	if opts.Keys != nil {
		workers.Go(func() {
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
				case <-ctx.Done():
					return
				}
			}
		})
	}

	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case chunk, ok := <-outCh:
			// ok==false OR nil chunk => the output pump ended (PTY closed,
			// command exited). Wait for the child and return.
			if !ok || chunk == nil {
				waitErr := process.Wait()
				res := Result{Output: buf.String(), TotalBytes: buf.total, Truncated: buf.truncated(), Interactive: true}
				if ctx.Err() == context.DeadlineExceeded {
					res.TimedOut = true
					res.Killed = true
					res.Exit = "timed out"
					return res
				}
				if isCancelled(ctx, waitErr) {
					res.Killed = true
					res.Exit = "cancelled"
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
				_ = process.Wait()
				res := Result{Output: buf.String(), TotalBytes: buf.total, Truncated: buf.truncated(), Killed: true, Interactive: true}
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
				_ = process.Wait()
				res := Result{
					Output:      buf.String(),
					TotalBytes:  buf.total,
					Truncated:   buf.truncated(),
					Exit:        "timed out waiting for input",
					Killed:      true,
					Interactive: true,
				}
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

// openDevNull returns read-only /dev/null for a child's stdin, or nil on failure.
func openDevNull() *os.File {
	f, err := os.Open(os.DevNull)
	if err != nil {
		return nil
	}
	return f
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

// UserShell exposes login-shell resolution to other process owners, such as
// workspace terminals, so every shell Whip starts is the same program.
func UserShell() string { return userShell() }

// ChildEnvironment exposes the daemon's child environment with the WHIP markers
// the bash tool sets, so shells started elsewhere look identical to the model's.
func ChildEnvironment(overrides map[string]string) []string { return childEnvironment(overrides) }
