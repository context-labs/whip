package secretref

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"sync"

	"github.com/context-labs/whip/internal/capability"
)

const maxCommandBytes = 64 << 10

func runCommand(ctx context.Context, fields []string) (string, error) {
	if len(fields) > 128 || len(strings.Join(fields, " ")) > maxCommandBytes {
		return "", errors.New("secret command exceeds argument bounds")
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	output := &commandOutput{cancel: cancel}
	processes := capability.NewProcessManager()
	defer func() { _ = processes.Close() }()
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	env := map[string]string{}
	for _, entry := range os.Environ() {
		if key, value, ok := strings.Cut(entry, "="); ok {
			// Keep the shared process policy authoritative for inherited values.
			if _, err := processes.ChildEnvironment(map[string]string{key: value}); err == nil {
				env[key] = value
			}
		}
	}
	process, err := processes.Start(ctx, "secret-helper", fields[0], fields[1:], capability.ProcessOptions{Cwd: cwd, Env: env, Stdin: strings.NewReader(""), Stdout: commandWriter{output: output, retain: true}, Stderr: commandWriter{output: output}})
	if err == nil {
		err = process.Wait()
	}
	if output.overflow {
		return "", errors.New("secret command output exceeds size limit")
	}
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if err != nil {
		return "", errors.New("secret command failed")
	}
	return strings.TrimSpace(output.stdout.String()), nil
}

type commandOutput struct {
	mu       sync.Mutex
	stdout   bytes.Buffer
	bytes    int
	overflow bool
	cancel   context.CancelFunc
}
type commandWriter struct {
	output *commandOutput
	retain bool
}

func (w commandWriter) Write(data []byte) (int, error) {
	w.output.mu.Lock()
	defer w.output.mu.Unlock()
	if w.output.overflow || len(data) > maxCommandBytes-w.output.bytes {
		w.output.overflow = true
		w.output.cancel()
		return 0, errors.New("secret command output exceeds size limit")
	}
	w.output.bytes += len(data)
	if w.retain {
		return w.output.stdout.Write(data)
	}
	return len(data), nil
}
