// whip-runtime starts the new backend in an explicitly selected private directory.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/context-labs/whip/internal/engine/process"
	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/rpc"
	"github.com/context-labs/whip/internal/runner"
	"github.com/context-labs/whip/internal/runtime"
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
	if err := run(ctx, os.Args[1:], os.Stdout, os.Stderr); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(parent context.Context, args []string, out, diagnostics io.Writer) (err error) {
	flags := flag.NewFlagSet("whip-runtime", flag.ContinueOnError)
	flags.SetOutput(diagnostics)
	directory := flags.String("directory", "", "required private runtime directory (fresh v4 storage)")
	scripted := flags.Bool("scripted", false, "use the deterministic fixture provider")
	delay := flags.Duration("scripted-delay", 0, "fixture response delay")
	workers := flags.Int("workers", 4, "maximum concurrent session turns")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 || *directory == "" {
		return errors.New("an explicit -directory is required; positional arguments are unsupported")
	}
	if !*scripted && *delay != 0 {
		return errors.New("-scripted-delay requires -scripted")
	}
	if *delay < 0 || *delay > time.Hour {
		return errors.New("scripted delay must be between zero and one hour")
	}
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	var provider runner.Provider = configuredProvider(*directory)
	if *scripted {
		provider = model.Scripted{Delay: *delay}
	}
	r, err := runtime.Open(ctx, *directory, provider, runtime.Options{Workers: *workers})
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, r.Close()) }()
	server, err := rpc.Listen(r)
	if err != nil {
		return err
	}
	defer func() { _ = server.Close() }()
	if err := r.Start(ctx); err != nil {
		return err
	}
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		select {
		case <-r.Done():
			cancel()
		case <-ctx.Done():
		}
	}()
	defer func() { cancel(); <-stopped }()
	if err := json.NewEncoder(out).Encode(map[string]any{"socket": r.SocketPath(), "runtime_id": r.Identity(), "major": protocol.Major}); err != nil {
		return err
	}
	return errors.Join(server.Serve(ctx), r.Err())
}
