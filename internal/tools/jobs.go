package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/tools/bashrun"
)

// Background jobs are per agent: a node sees only the jobs it started. At most
// maxRunningJobs run at once; finished jobs stay pollable until the retained
// set exceeds maxRetainedJobs, oldest first.
const (
	maxRunningJobs  = 8
	maxRetainedJobs = 32
)

var ErrTooManyJobs = fmt.Errorf("at most %d background jobs may run at once; kill or wait for one first", maxRunningJobs)

// JobStatus is the model-facing view of one background job.
type JobStatus struct {
	ID        string    `json:"id"`
	PID       int       `json:"pid"`
	Command   string    `json:"command"`
	Running   bool      `json:"running"`
	Exit      string    `json:"exit,omitempty"`
	Killed    bool      `json:"killed,omitempty"`
	Bytes     int       `json:"bytes"`
	StartedAt time.Time `json:"started_at"`
	EndedAt   time.Time `json:"ended_at,omitzero"`
}

// StartJob launches a background shell command owned by this agent.
func (s *Services) StartJob(ctx context.Context, command, cwd string, timeout time.Duration) (JobStatus, error) {
	opts := s.ProcessOptions()
	opts.Command, opts.Timeout = command, timeout
	if cwd != "" {
		opts.Cwd = cwd
	}
	s.mu.Lock()
	running := 0
	for _, job := range s.jobs {
		if job.Running() {
			running++
		}
	}
	if running >= maxRunningJobs {
		s.mu.Unlock()
		return JobStatus{}, ErrTooManyJobs
	}
	s.mu.Unlock()
	job, err := bashrun.Start(ctx, opts)
	if err != nil {
		return JobStatus{}, err
	}
	id, err := randomID()
	if err != nil {
		_ = job.Kill()
		return JobStatus{}, err
	}
	id = "job-" + id[:8]
	s.mu.Lock()
	if s.jobs == nil {
		s.jobs = make(map[string]*bashrun.Job)
	}
	s.jobs[id] = job
	s.jobOrder = append(s.jobOrder, id)
	s.pruneJobsLocked()
	s.mu.Unlock()
	return jobStatus(id, job), nil
}

// pruneJobsLocked forgets the oldest finished jobs past the retention cap.
func (s *Services) pruneJobsLocked() {
	for len(s.jobOrder) > maxRetainedJobs {
		removed := false
		for index, id := range s.jobOrder {
			if job := s.jobs[id]; job != nil && !job.Running() {
				delete(s.jobs, id)
				s.jobOrder = append(s.jobOrder[:index], s.jobOrder[index+1:]...)
				removed = true
				break
			}
		}
		if !removed {
			return
		}
	}
}

// Job returns one of this agent's background jobs.
func (s *Services) Job(id string) (*bashrun.Job, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	job, ok := s.jobs[id]
	return job, ok
}

// JobStatus reports one job; ok is false for an unknown id.
func (s *Services) JobStatus(id string) (JobStatus, bool) {
	job, ok := s.Job(id)
	if !ok {
		return JobStatus{}, false
	}
	return jobStatus(id, job), true
}

// Jobs lists this agent's retained jobs, oldest first.
func (s *Services) Jobs() []JobStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]JobStatus, 0, len(s.jobOrder))
	for _, id := range s.jobOrder {
		if job := s.jobs[id]; job != nil {
			result = append(result, jobStatus(id, job))
		}
	}
	sort.SliceStable(result, func(i, j int) bool { return result[i].StartedAt.Before(result[j].StartedAt) })
	return result
}

// KillJob terminates one job's process group.
func (s *Services) KillJob(id string) error {
	job, ok := s.Job(id)
	if !ok {
		return errors.New("no such job")
	}
	return job.Kill()
}

// killJobs terminates every running job; used when the agent closes.
func (s *Services) killJobs() {
	s.mu.RLock()
	jobs := make([]*bashrun.Job, 0, len(s.jobs))
	for _, job := range s.jobs {
		jobs = append(jobs, job)
	}
	s.mu.RUnlock()
	for _, job := range jobs {
		_ = job.Kill()
	}
}

func jobStatus(id string, job *bashrun.Job) JobStatus {
	_, total := job.Output(0)
	return JobStatus{
		ID: id, PID: job.PID(), Command: job.Command, Running: job.Running(), Exit: job.Exit(), Killed: job.Killed(),
		Bytes: total, StartedAt: job.Started, EndedAt: job.Ended(),
	}
}

// shellStartTool starts a background job. It is admitted exactly like bash
// (shell capability, writer capability, permission prompt) and returns the
// job id and pid instead of waiting for output.
func shellStartTool(services *Services) Tool {
	return Tool{
		Def: llm.NewTool("shell_start",
			"Start a bash command in the background and return its job id. Use for servers, builds, and anything longer than a few seconds; poll, tail, wait for, or kill it afterwards.",
			`{"type":"object","properties":{"command":{"type":"string","description":"The bash command to start"},"timeout":{"type":"number","description":"Optional wall-clock cap in seconds; 0 means none"}},"required":["command"]}`),
		Run: func(ctx context.Context, args json.RawMessage) (string, error) {
			var a struct {
				Command string  `json:"command"`
				Timeout float64 `json:"timeout"`
			}
			if err := json.Unmarshal(args, &a); err != nil {
				return "", err
			}
			if strings.TrimSpace(a.Command) == "" {
				return "", errors.New("command is required")
			}
			call, dispatched := dispatchCall(ctx)
			if !dispatched {
				if deny := services.CheckGate(ctx, "bash", a.Command); deny != "" {
					return "", errors.New(deny)
				}
			}
			cwd := call.WorkingDir
			if cwd == "" {
				cwd = call.CanonicalRoot
			}
			status, err := services.StartJob(ctx, a.Command, cwd, time.Duration(a.Timeout*float64(time.Second)))
			if err != nil {
				return "", err
			}
			encoded, err := json.Marshal(status)
			return string(encoded), err
		},
	}
}
