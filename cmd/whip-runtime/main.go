// whip-runtime starts the new backend in an explicitly selected private directory.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/context-labs/whip/internal/engine/process"
	"github.com/context-labs/whip/internal/hostcmd"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "_kernel" {
		if err := process.WorkerMain(os.Args[2:], os.Stdin, os.Stdout, nil); err != nil {
			_, _ = fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := hostcmd.Run(ctx, os.Args[1:], os.Stdout, os.Stderr); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
