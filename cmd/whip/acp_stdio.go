package main

import (
	"errors"
	"fmt"
	"os"
	"sync"
	"syscall"
	"time"
)

// The SDK accepts an io.Writer and cannot interrupt a blocked Write using a
// request context. Own pollable copies of stdio so deadlines and Close release
// blocked editor I/O before the bridge joins its observers.
type acpStdio struct {
	input, output *os.File
	timeout       time.Duration
	regularOutput bool
	once          sync.Once
}

func newACPStdio(input, output *os.File, timeout time.Duration) (*acpStdio, error) {
	in, err := acpPollableCopy(input)
	if err != nil {
		return nil, err
	}
	out, err := acpPollableCopy(output)
	if err != nil {
		_ = in.Close()
		return nil, err
	}
	info, err := out.Stat()
	if err != nil {
		_ = in.Close()
		_ = out.Close()
		return nil, err
	}
	return &acpStdio{input: in, output: out, timeout: timeout, regularOutput: info.Mode().IsRegular()}, nil
}

func acpPollableCopy(file *os.File) (*os.File, error) {
	fd, err := syscall.Dup(int(file.Fd()))
	if err != nil {
		return nil, fmt.Errorf("copy ACP stdio: %w", err)
	}
	syscall.CloseOnExec(fd)
	if err := syscall.SetNonblock(fd, true); err != nil {
		_ = syscall.Close(fd)
		return nil, fmt.Errorf("configure ACP stdio: %w", err)
	}
	return os.NewFile(uintptr(fd), "acp-stdio"), nil
}

func (s *acpStdio) Write(data []byte) (int, error) {
	// Match the SDK's inbound line bound; never emit an unreadable frame.
	if len(data) > 10<<20 {
		s.Close()
		return 0, errors.New("ACP output exceeds the 10 MiB frame limit")
	}
	if !s.regularOutput {
		if err := s.output.SetWriteDeadline(time.Now().Add(s.timeout)); err != nil {
			s.Close()
			return 0, err
		}
	}
	n, err := s.output.Write(data)
	if err != nil {
		s.Close()
	}
	return n, err
}

func (s *acpStdio) Close() {
	s.once.Do(func() { _ = s.input.Close(); _ = s.output.Close() })
}
