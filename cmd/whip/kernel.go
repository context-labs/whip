package main

import (
	"os"

	"github.com/context-labs/whip/internal/rlm"
)

func kernelCLI(args []string) error {
	return rlm.WorkerMain(args, os.Stdin, os.Stdout)
}
