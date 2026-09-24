package tools

import (
	"testing"
	"time"
)

func TestJobRetentionKeepsRunningAndNewestFinished(t *testing.T) {
	services := NewServices()
	t.Cleanup(services.Close)
	running, err := services.StartJob(t.Context(), "sleep 30", t.TempDir(), 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	var finished []string
	for range maxRetainedJobs + 2 {
		status, err := services.StartJob(t.Context(), "printf done", "", 0)
		if err != nil {
			t.Fatal(err)
		}
		job, ok := services.Job(status.ID)
		if !ok {
			t.Fatal("new job was pruned before its output could be read")
		}
		select {
		case <-job.Done():
		case <-time.After(5 * time.Second):
			t.Fatal("fixture job failed to finish")
		}
		finished = append(finished, status.ID)
	}
	if jobs := services.Jobs(); len(jobs) != maxRetainedJobs {
		t.Fatalf("retained %d jobs, want %d", len(jobs), maxRetainedJobs)
	}
	if status, ok := services.JobStatus(running.ID); !ok || !status.Running {
		t.Fatal("oldest running job must survive retention pruning")
	}
	for i, id := range finished {
		job, ok := services.Job(id)
		if i < 3 {
			if ok {
				t.Fatalf("old completed job %d was not pruned", i)
			}
			continue
		}
		if !ok {
			t.Fatalf("recent completed job %d was pruned", i)
		}
		if output, total := job.Output(0); output != "done" || total != 4 {
			t.Fatalf("retained job output=%q bytes=%d", output, total)
		}
	}
}
