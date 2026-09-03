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
	"github.com/context-labs/whip/internal/browser"
	"github.com/context-labs/whip/internal/computer"
	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/daemon"
	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/lsp"
	"github.com/context-labs/whip/internal/mcp"
	"github.com/context-labs/whip/internal/rlm"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/tools"
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
		if meta.Model == "mcp" && meta.Provider == "local" {
			services := daemonToolServices(cfg, meta, "mcp")
			ag := agent.NewWithServices(llm.New("", ""), "mcp", 1, "", services)
			ag.ModelName, ag.Provider = meta.Model, meta.Provider
			ag.WorkingDir = meta.CWD
			ag.ComputerDisabled = true
			return daemon.Components{Runner: daemon.NewAgentRunner(ag)}, nil
		}
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
		services := daemonToolServices(cfg, meta, apiID)
		ag := agent.NewWithServices(client, apiID, maxOutput, prompt, services)
		ag.ModelName, ag.Provider = meta.Model, meta.Provider
		ag.WorkingDir = meta.CWD
		ag.ContextLimit = contextLimit
		ag.WorktreeSubagents = cfg.WorktreeSubagents != nil && *cfg.WorktreeSubagents
		if model.SamplingParams != nil {
			ag.Temperature, ag.TopP = model.SamplingParams.Temperature, model.SamplingParams.TopP
		}
		vision := model.Vision
		if catalog, ok := catalogs[meta.Provider]; ok {
			if advertised, found := catalog.SupportsVision(apiID); found {
				vision = advertised
			}
		}
		if vision {
			services.SetScreenshotSink(func(images [][]byte) {
				parts := make([]llm.ContentPart, 0, len(images))
				for _, image := range images {
					parts = append(parts, llm.ImagePart("jpg", image))
				}
				ag.SteerImages("browser/computer screenshots attached:", parts)
			})
		}
		ag.Effort = meta.Effort
		if ag.Effort == "" {
			ag.Effort = tui.DefaultEffortFor(catalogs, meta.Provider, apiID, cfg.DefaultEffort)
		}
		configSnapshot := cfg.Snapshot()
		ag.ResolveModel = func(model, provider string) (agent.SubModel, error) {
			return tui.SubModelFor(configSnapshot, model, provider)
		}
		if taskDefault, taskErr := tui.TaskDefaultFor(configSnapshot); taskErr == nil {
			ag.TaskDefault = taskDefault
		}
		ag.BrowserDisabled = cfg.Browser.Enabled != nil && !*cfg.Browser.Enabled
		ag.ComputerDisabled = cfg.Computer.Enabled != nil && !*cfg.Computer.Enabled
		if ag.BrowserDisabled || ag.ComputerDisabled {
			filtered := ag.Tools[:0]
			for _, tool := range ag.Tools {
				name := tool.Def.Function.Name
				if ag.BrowserDisabled && name == "browser_exec" || ag.ComputerDisabled && name == "computer_exec" {
					continue
				}
				filtered = append(filtered, tool)
			}
			ag.Tools = filtered
		}
		var mcpManager *mcp.Manager
		discovery := mcp.LoadMergedFiltered(meta.CWD, mcp.FromConfigMap(cfg.MCPServers), mcp.ImportPolicyFrom(cfg.MCPImport))
		if len(discovery.Merged) > 0 || len(discovery.Blocked) > 0 {
			mcpManager = mcp.NewManager(discovery.Merged)
			mcpManager.SetBlocked(discovery.Blocked)
		}
		if meta.Mode == session.ModeRLM {
			ag.Messages = append(ag.Messages[:1], rlm.FocusedHistory(history)...)
			host := newDaemonRLMHost(ag, history)
			if hasCatalog {
				input, output, cacheRead, _ := catalog.Pricing(apiID)
				host.SetPricing(input, output, cacheRead)
			}
			kernel, err := rlm.NewKernel(rlm.KernelOptions{Limits: limits, Manager: kernels, Host: host})
			if err != nil {
				if mcpManager != nil {
					mcpManager.Close()
				}
				services.Close()
				return daemon.Components{}, err
			}
			ag.SetExclusiveTool(rlm.Tool(kernel), "rlm")
			components := daemon.Components{
				Runner: daemon.NewAgentRunner(ag), Runtime: daemonRLMRuntime{host: host, kernel: kernel},
				Bind: host.Bind,
			}
			if mcpManager != nil {
				components.MCP = mcpManager
			}
			return components, nil
		}
		if len(history) > 1 {
			ag.Messages = append(ag.Messages[:1], history[1:]...)
		}
		components := daemon.Components{Runner: daemon.NewAgentRunner(ag)}
		if mcpManager != nil {
			components.MCP = mcpManager
		}
		return components, nil
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

func daemonToolServices(cfg *config.Config, meta session.Meta, apiID string) *tools.Services {
	services := tools.NewServices()
	services.SetExternalPermissions(true)
	services.SetProcessMarkers(meta.ID, apiID)
	if cfg.Browser.CDPURL != "" {
		services.SetProcessEnvironment(map[string]string{"WHIP_CDP_URL": cfg.Browser.CDPURL})
	}
	if cfg.Browser.Enabled == nil || *cfg.Browser.Enabled {
		mode := browser.ModeLive
		switch cfg.Browser.Mode {
		case "dedicated":
			mode = browser.ModeDedicated
		case "headless":
			mode = browser.ModeHeadless
		case "extension":
			mode = browser.ModeExtension
		}
		services.SetBrowser(browser.NewManager(mode), cfg.Browser.AllowPrivateURLs)
	}
	if cfg.Computer.Enabled == nil || *cfg.Computer.Enabled {
		defaultDeny := cfg.Computer.DefaultDeny != nil && *cfg.Computer.DefaultDeny
		services.SetComputerPolicy(computer.NewPolicy(cfg.Computer.Allow, cfg.Computer.Deny, defaultDeny))
	}
	services.SetDiagnostics(lsp.NewManager(lsp.FromConfigMap(cfg.LSPServers)))
	return services
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
