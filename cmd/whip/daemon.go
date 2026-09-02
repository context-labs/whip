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
	"github.com/context-labs/whip/internal/rlm"
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
	defaultMode := configuredSessionMode(cfg)
	store, err := session.OpenWithDefaultMode(filepath.Join(dir, "sessions.db"), defaultMode)
	if err != nil {
		return err
	}
	generation, err := store.BeginDaemonGeneration(context.Background(), version)
	if err != nil {
		_ = store.Close()
		return err
	}
	limits := rlmLimits(cfg.RLM)
	kernels := rlm.NewManager(limits.MaxWorkers)
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
		catalogs := config.LoadCatalogs()
		catalog, hasCatalog := catalogs[meta.Provider]
		contextLimit := model.ContextWindow()
		if hasCatalog {
			if value := catalog.ContextLength(apiID); value > 0 {
				contextLimit = value
			}
		}
		maxOutput := model.MaxOut
		if maxOutput <= 0 && hasCatalog {
			maxOutput = catalog.MaxCompletionTokens(apiID)
		}
		if maxOutput <= 0 {
			maxOutput = contextLimit
		}
		prompt := systemPrompt(meta.CWD, time.Now())
		if meta.Mode == session.ModeRLM {
			prompt = rlm.BuildPrompt(meta.CWD, nil)
		}
		ag := agent.New(client, apiID, maxOutput, prompt)
		ag.ModelName, ag.Provider = meta.Model, meta.Provider
		ag.ContextLimit = contextLimit
		ag.Effort = tui.DefaultEffortFor(catalogs, meta.Provider, apiID, cfg.DefaultEffort)
		if meta.Mode == session.ModeRLM {
			ag.Messages = append(ag.Messages[:1], rlm.FocusedHistory(history)...)
			host := newDaemonRLMHost(ag, history)
			if hasCatalog {
				input, output, cacheRead, _ := catalog.Pricing(apiID)
				host.SetPricing(input, output, cacheRead)
			}
			kernel, err := rlm.NewKernel(rlm.KernelOptions{Limits: limits, Manager: kernels, Host: host})
			if err != nil {
				return daemon.Components{}, err
			}
			ag.SetExclusiveTool(rlm.Tool(kernel), "rlm")
			return daemon.Components{
				Runner: daemon.NewAgentRunner(ag), Runtime: daemonRLMRuntime{host: host, kernel: kernel},
				Bind: host.Bind,
			}, nil
		}
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

func configuredSessionMode(cfg *config.Config) session.Mode {
	if cfg != nil && cfg.RLMEnabled() {
		return session.ModeRLM
	}
	return session.ModeClassic
}

func rlmLimits(value config.RLMConfig) rlm.Limits {
	limits := rlm.DefaultLimits()
	if value.Steps > 0 {
		limits.Steps = value.Steps
	}
	if value.HostRequests > 0 {
		limits.HostRequests = value.HostRequests
	}
	if value.WallMillis > 0 {
		limits.Wall = time.Duration(value.WallMillis) * time.Millisecond
	}
	if value.MemoryMiB > 0 {
		limits.MemoryBytes = uint64(value.MemoryMiB) << 20
	}
	if value.OutputBytes > 0 {
		limits.OutputBytes = value.OutputBytes
	}
	if value.FrameBytes > 0 {
		limits.FrameBytes = value.FrameBytes
	}
	if value.MaxWorkers > 0 {
		limits.MaxWorkers = value.MaxWorkers
	}
	return limits
}
