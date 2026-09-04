package bashrun

import (
	"context"
	"errors"
	"strings"
	"syscall"
	"testing"
	"time"
)

func waitJob(t *testing.T, job *Job) {
	t.Helper()
	select {
	case <-job.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("job did not finish")
	}
}

func TestJobStartsTailsAndKillsItsGroup(t *testing.T) {
	job, err := Start(context.Background(), Options{Command: "echo start; sleep 30; echo end", Cwd: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if !job.Running() || job.PID() <= 0 {
		t.Fatalf("job not running: pid=%d", job.PID())
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		if out, _ := job.Output(0); strings.Contains(out, "start") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("job output never arrived while running")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := job.Kill(); err != nil {
		t.Fatal(err)
	}
	waitJob(t, job)
	if job.Running() || !job.Killed() || job.Exit() != "killed" || job.Ended().IsZero() {
		t.Fatalf("killed job state: running=%v killed=%v exit=%q ended=%v", job.Running(), job.Killed(), job.Exit(), job.Ended())
	}
	if out, _ := job.Output(0); strings.Contains(out, "end") {
		t.Fatalf("killed job kept running: %q", out)
	}
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(-job.PID(), 0); errors.Is(err, syscall.ESRCH) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("process group survived Kill")
}

func TestJobExitStatusTimeoutAndBoundedOutput(t *testing.T) {
	job, err := Start(context.Background(), Options{Command: "echo hi; exit 3", Cwd: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	waitJob(t, job)
	if out, total := job.Output(0); out != "hi\n" || total != 3 || job.Killed() || !strings.Contains(job.Exit(), "3") {
		t.Fatalf("exit job: out=%q total=%d exit=%q killed=%v", out, total, job.Exit(), job.Killed())
	}

	slow, err := Start(context.Background(), Options{Command: "sleep 5", Cwd: t.TempDir(), Timeout: 200 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	waitJob(t, slow)
	if slow.Exit() != "timed out" || !slow.Killed() {
		t.Fatalf("timed out job: exit=%q killed=%v", slow.Exit(), slow.Killed())
	}

	big, err := Start(context.Background(), Options{Command: "head -c 3000000 /dev/zero | tr '\\0' x", Cwd: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	waitJob(t, big)
	out, total := big.Output(0)
	if total != 3_000_000 || len(out) != jobOutputBytes {
		t.Fatalf("bounded output: kept=%d total=%d", len(out), total)
	}
	if tail, _ := big.Output(10); tail != "xxxxxxxxxx" {
		t.Fatalf("tail = %q", tail)
	}
}
