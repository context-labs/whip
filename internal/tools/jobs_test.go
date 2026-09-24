package tools

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/session"
)

func TestShellStartJobsAreOwnedCappedAndClosedWithServices(t *testing.T) {
	root := t.TempDir()
	st, err := session.Open(filepath.Join(t.TempDir(), "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	rootID, err := st.Create(session.SessionKindAgent, root, "m", "p")
	if err != nil {
		t.Fatal(err)
	}
	authority, err := st.EnsureAuthority(context.Background(), rootID)
	if err != nil {
		t.Fatal(err)
	}
	services := NewServices()
	services.SetGate(func(context.Context, GateRequest) (GateDecision, string) { return GateAllowOnce, "" })
	if err := services.BindDispatcher(&countingLedger{Store: st}, st.Workspaces(), st.Processes(), authority); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(services.Close)

	output, err := services.Invoke(context.Background(), "shell_start", json.RawMessage(`{"command":"sleep 30"}`))
	if err != nil {
		t.Fatal(err)
	}
	var status JobStatus
	if err := json.Unmarshal([]byte(output), &status); err != nil {
		t.Fatal(err)
	}
	if status.ID == "" || status.PID <= 0 || !status.Running {
		t.Fatalf("started job = %+v", status)
	}
	if listed := services.Jobs(); len(listed) != 1 || listed[0].ID != status.ID {
		t.Fatalf("jobs = %+v", listed)
	}
	for range maxRunningJobs - 1 {
		if _, err := services.StartJob(context.Background(), "sleep 30", root, 0); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := services.StartJob(context.Background(), "sleep 30", root, 0); !errors.Is(err, ErrTooManyJobs) {
		t.Fatalf("cap error = %v", err)
	}
	if _, ok := services.JobStatus("job-missing"); ok {
		t.Fatal("unknown job reported")
	}
	services.Close()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		running := 0
		for _, job := range services.Jobs() {
			if job.Running {
				running++
			}
		}
		if running == 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("Close left background jobs running")
}
