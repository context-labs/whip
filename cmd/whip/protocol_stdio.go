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
type protocolStdio struct {
	input, output *os.File
	timeout       time.Duration
	maxFrame      int
	regularOutput bool
	once          sync.Once
}

func newProtocolStdio(input, output *os.File, timeout time.Duration, maxFrame int) (*protocolStdio, error) {
	in, err := protocolPollableCopy(input)
	if err != nil {
		return nil, err
	}
	out, err := protocolPollableCopy(output)
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
	return &protocolStdio{input: in, output: out, timeout: timeout, maxFrame: maxFrame, regularOutput: info.Mode().IsRegular()}, nil
}

func protocolPollableCopy(file *os.File) (*os.File, error) {
	fd, err := syscall.Dup(int(file.Fd()))
	if err != nil {
		return nil, fmt.Errorf("copy protocol stdio: %w", err)
	}
	syscall.CloseOnExec(fd)
	if err := syscall.SetNonblock(fd, true); err != nil {
		_ = syscall.Close(fd)
		return nil, fmt.Errorf("configure protocol stdio: %w", err)
	}
	return os.NewFile(uintptr(fd), "protocol-stdio"), nil
}

func (s *protocolStdio) Write(data []byte) (int, error) {
	// Match the SDK's inbound line bound; never emit an unreadable frame.
	if len(data) > s.maxFrame {
		_ = s.Close()
		return 0, errors.New("protocol output exceeds its frame limit")
	}
	if !s.regularOutput {
		if err := s.output.SetWriteDeadline(time.Now().Add(s.timeout)); err != nil {
			_ = s.Close()
			return 0, err
		}
	}
	n, err := s.output.Write(data)
	if err != nil {
		_ = s.Close()
	}
	return n, err
}

func (s *protocolStdio) Close() error {
	s.once.Do(func() { _ = s.input.Close(); _ = s.output.Close() })
	return nil
}
