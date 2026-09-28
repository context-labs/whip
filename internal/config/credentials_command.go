package config

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
)

const credentialCommandTimeout = 10 * time.Second

func runCredentialCommand(ctx context.Context, declaration CredentialCommand, lookup func(string) (string, bool)) (string, error) {
	environment := make([]string, 0, len(declaration.Environment))
	size := 0
	for _, name := range declaration.Environment {
		if lookup == nil {
			return "", errors.New("credential command environment lookup is required")
		}
		value, ok := lookup(name)
		size += len(name) + len(value) + 1
		if !ok || strings.ContainsRune(value, 0) || size > maxCredentialBytes {
			return "", errors.New("credential command environment is missing or exceeds bounds")
		}
		environment = append(environment, name+"="+value)
	}
	ctx, cancel := context.WithTimeout(ctx, credentialCommandTimeout)
	defer cancel()
	command := exec.CommandContext(ctx, declaration.Executable, declaration.Arguments...)
	command.Dir = "/"
	command.Env = environment
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.Cancel = func() error { return syscall.Kill(-command.Process.Pid, syscall.SIGKILL) }
	// A descendant keeping a pipe open cannot strand Wait after the leader exits.
	command.WaitDelay = 100 * time.Millisecond
	output := &credentialOutput{cancel: cancel}
	command.Stdout = credentialWriter{output: output, retain: true}
	command.Stderr = credentialWriter{output: output}
	err := command.Run()
	if command.Process != nil {
		// Also terminate descendants after a successful or failed leader exit.
		// Run has joined the leader and both bounded pipe-copy goroutines.
		_ = syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
	}
	if output.overflow {
		return "", errors.New("credential command output exceeds size limit")
	}
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if err != nil {
		return "", errors.New("credential command failed")
	}
	return output.stdout.String(), nil
}

type credentialOutput struct {
	mu       sync.Mutex
	stdout   bytes.Buffer
	bytes    int
	overflow bool
	cancel   context.CancelFunc
}

type credentialWriter struct {
	output *credentialOutput
	retain bool
}

func (w credentialWriter) Write(raw []byte) (int, error) {
	w.output.mu.Lock()
	defer w.output.mu.Unlock()
	if w.output.overflow || len(raw) > maxCredentialBytes-w.output.bytes {
		w.output.overflow = true
		w.output.cancel()
		return 0, errors.New("credential command output exceeds size limit")
	}
	w.output.bytes += len(raw)
	if w.retain {
		return w.output.stdout.Write(raw)
	}
	return len(raw), nil
}
