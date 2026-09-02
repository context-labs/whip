package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/context-labs/whip/internal/agent"
	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/daemon"
	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/tui"
)

var restartDaemonBinary = daemon.RestartSelfDaemon

func daemonCLI(args []string) error {
	signals, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return runDaemon(signals, args)
}

func runDaemon(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("_daemon", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("hidden daemon mode does not accept arguments")
	}
	dir, err := config.Dir()
	if err != nil {
		return err
	}
	paths, err := daemon.Paths(dir)
	if err != nil {
		return err
	}
	owner, err := daemon.AcquireOwner(paths.Lock)
	if err != nil {
		return err
	}
	defer func() { _ = owner.Close() }()

	// The database is not opened or inspected until cross-process ownership
	// is established above.
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	store, err := session.Open(filepath.Join(dir, "sessions.db"))
	if err != nil {
		return err
	}
	generation, err := store.BeginDaemonGeneration(context.Background(), version)
	if err != nil {
		_ = store.Close()
		return err
	}
	factory := func(_ context.Context, meta session.Meta, history []llm.Message) (daemon.Components, error) {
		prov, model, apiID, err := cfg.Resolve(meta.Model, meta.Provider)
		if err != nil {
			return daemon.Components{}, err
		}
		key, err := prov.ResolveKey()
		if err != nil || key == "" {
			return daemon.Components{}, errors.Join(err, fmt.Errorf("no API key for provider %q", meta.Provider))
		}
		client := llm.New(prov.BaseURL, key)
		client.MaxRetries = cfg.MaxRetries
		ag := agent.New(client, apiID, model.MaxTokens, systemPrompt(meta.CWD, time.Now()))
		ag.ModelName, ag.Provider = meta.Model, meta.Provider
		ag.ContextLimit = model.ContextWindow()
		ag.Effort = tui.DefaultEffortFor(config.LoadCatalogs(), meta.Provider, apiID, cfg.DefaultEffort)
		if len(history) > 1 {
			ag.Messages = append(ag.Messages[:1], history[1:]...)
		}
		return daemon.Components{Runner: daemon.NewAgentRunner(ag)}, nil
	}
	ownerDaemon, err := daemon.New(store, factory)
	if err != nil {
		_ = store.Close()
		return err
	}
	restartRequested := make(chan struct{})
	var restartOnce sync.Once
	server, err := daemon.NewServer(ownerDaemon, daemon.ServerOptions{
		BuildID: version, Generation: generation, RuntimeDir: paths.Runtime,
		Restart: func() { restartOnce.Do(func() { close(restartRequested) }) },
	})
	if err != nil {
		_ = ownerDaemon.Close()
		return err
	}
	defer func() {
		_ = server.Close()
		_ = os.Remove(paths.Socket)
	}()
	served := make(chan error, 1)
	go func() { served <- server.ListenAndServe(paths) }()
	select {
	case err := <-served:
		return err
	case <-restartRequested:
		_ = store.SetDaemonStatus(context.Background(), generation, "restarting")
		if err := server.Close(); err != nil {
			return err
		}
		_ = os.Remove(paths.Socket)
		if err := owner.Close(); err != nil {
			return err
		}
		return restartDaemonBinary()
	case <-ctx.Done():
		_ = store.SetDaemonStatus(context.Background(), generation, "stopping")
		return server.Close()
	}
}
