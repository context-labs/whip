package main

import (
	"os"

	"github.com/context-labs/whip/internal/engine/process"
)

func kernelCLI(args []string) error {
	return process.WorkerMain(args, os.Stdin, os.Stdout, nil)
}
