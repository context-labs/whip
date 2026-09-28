package main

import (
	"os"

	"github.com/context-labs/whip/internal/engine/process"
	"github.com/context-labs/whip/internal/rlm"
)

func kernelCLI(args []string) error {
	return process.WorkerMain(args, os.Stdin, os.Stdout, rlm.DescribeEngine)
}
