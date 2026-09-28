package bashrun

import (
	"context"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"
)

// jobOutputBytes bounds the output a background job keeps in memory: the tail
// survives, the total byte count is tracked, and the head is dropped.
const jobOutputBytes = 1 << 20

// Job is a background shell command. Unlike Run it returns as soon as the
// process starts and keeps draining output until the process group is gone.
// A job outlives the cell and the turn that started it; the owning Services
// kills it when the agent closes, and the process manager kills it with the
// root.
type Job struct {
	Command string
	Started time.Time

	process processHandle
	pid     int
	done    chan struct{}

	mu     sync.Mutex
	output []byte
	total  int
	exit   string
	killed bool
	ended  time.Time
}

// Start launches a background job. ctx bounds only the start; the job's own
// lifetime is bounded by opts.Timeout when set, otherwise by Kill.
func Start(ctx context.Context, opts Options) (*Job, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(context.WithoutCancel(ctx), userShell(), "-c", opts.Command)
	cmd.Dir = opts.Cwd
	if opts.Processes == nil {
		cmd.Env = childEnvironment(opts.Env)
	}
	stdout, outW, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	stderr, errW, err := os.Pipe()
	if err != nil {
		_ = stdout.Close()
		_ = outW.Close()
		return nil, err
	}
	cmd.Stdout, cmd.Stderr = outW, errW
	devNull := openDevNull()
	if devNull != nil {
		cmd.Stdin = devNull
	}
	process, cleanup, err := startProcess(context.WithoutCancel(ctx), cmd, opts, false)
	_ = outW.Close()
	_ = errW.Close()
	if devNull != nil {
		_ = devNull.Close()
	}
	if err != nil {
		_ = stdout.Close()
		_ = stderr.Close()
		cleanup()
		return nil, err
	}
	job := &Job{Command: opts.Command, Started: time.Now(), process: process, pid: process.PID(), done: make(chan struct{})}
	go job.pump(stdout, stderr, cleanup, opts.Timeout)
	return job, nil
}

func (j *Job) pump(stdout, stderr *os.File, cleanup func(), timeout time.Duration) {
	defer cleanup()
	var wg sync.WaitGroup
	wg.Add(2)
	drain := func(r io.Reader) {
		defer wg.Done()
		buf := make([]byte, 4096)
		for {
			n, err := r.Read(buf)
			if n > 0 {
				j.append(buf[:n])
			}
			if err != nil {
				return
			}
		}
	}
	go drain(stdout)
	go drain(stderr)
	var timer *time.Timer
	timedOut := false
	if timeout > 0 {
		timer = time.AfterFunc(timeout, func() {
			j.mu.Lock()
			timedOut = true
			j.mu.Unlock()
			_ = j.process.Kill()
		})
	}
	waitErr := j.process.Wait()
	if timer != nil {
		timer.Stop()
	}
	drained := make(chan struct{})
	go func() { wg.Wait(); close(drained) }()
	grace := time.NewTimer(500 * time.Millisecond)
	select {
	case <-drained:
		grace.Stop()
	case <-grace.C:
	}
	_ = stdout.Close()
	_ = stderr.Close()
	wg.Wait()
	j.mu.Lock()
	switch {
	case timedOut:
		j.exit, j.killed = "timed out", true
	case j.killed:
		j.exit = "killed"
	default:
		j.exit = exitString(waitErr)
		if waitErr != nil && isKilledBySignal(waitErr) {
			j.killed = true
		}
	}
	j.ended = time.Now()
	j.mu.Unlock()
	close(j.done)
}

func (j *Job) append(chunk []byte) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.total += len(chunk)
	j.output = append(j.output, chunk...)
	if excess := len(j.output) - jobOutputBytes; excess > 0 {
		j.output = append([]byte(nil), j.output[excess:]...)
	}
}

// PID is the shell's process id; the whole group is killed with it.
func (j *Job) PID() int { return j.pid }

// Done closes when the process and its group have exited and output is final.
func (j *Job) Done() <-chan struct{} { return j.done }

func (j *Job) Running() bool {
	select {
	case <-j.done:
		return false
	default:
		return true
	}
}

// Kill terminates the process group. It is safe to call more than once.
func (j *Job) Kill() error {
	j.mu.Lock()
	if !j.Running() {
		j.mu.Unlock()
		return nil
	}
	j.killed = true
	j.mu.Unlock()
	return j.process.Kill()
}

// Exit is the human-readable exit status once the job has ended; "" while it
// runs or after a clean exit 0.
func (j *Job) Exit() string {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.exit
}

// Killed reports whether the job ended because it was killed or timed out.
func (j *Job) Killed() bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.killed
}

// Ended is when the job finished; zero while it runs.
func (j *Job) Ended() time.Time {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.ended
}

// Output returns the last tail bytes of combined output (all retained output
// when tail <= 0) and the total number of bytes the job has produced.
func (j *Job) Output(tail int) (string, int) {
	j.mu.Lock()
	defer j.mu.Unlock()
	out := j.output
	if tail > 0 && len(out) > tail {
		out = out[len(out)-tail:]
	}
	return string(out), j.total
}
