//go:build !unix

package rlm

import (
	"errors"
	"os/exec"
)

func applyMemoryLimit(uint64) error { return errors.New("RLM memory limits require a Unix host") }

func residentBytes(int) (uint64, error) {
	return 0, errors.New("RLM RSS observation requires a Unix host")
}

func configureCommand(*exec.Cmd) {}

func killProcessGroup(int) error { return nil }
