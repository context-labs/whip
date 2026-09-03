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
	"github.com/context-labs/whip/internal/skills"
	"github.com/context-labs/whip/internal/tools"
	"github.com/context-labs/whip/internal/tui"
)

var restartDaemonBinary = daemon.RestartSelfDaemon
var daemonKernelCommand []string

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
	store, err := session.Open(filepath.Join(paths.Home, "sessions.db"))
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
	defer kernels.Close()
	factory := func(_ context.Context, meta session.Meta, history []llm.Message) (daemon.Components, error) {
		runtimeCfg, err := config.Load()
		if err != nil {
			return daemon.Components{}, err
		}
		switch meta.Kind {
		case session.SessionKindToolHost:
			services := daemonToolServices(runtimeCfg, meta, "mcp")
			return daemon.Components{Runner: daemon.NewToolRunner(services)}, nil
		case session.SessionKindAgent:
		default:
			return daemon.Components{}, fmt.Errorf("unsupported session kind %q", meta.Kind)
		}
		prov, model, apiID, err := runtimeCfg.Resolve(meta.Model, meta.Provider)
		if err != nil {
			return daemon.Components{}, err
		}
		key, err := prov.ResolveKey()
		if err != nil || key == "" {
			return daemon.Components{}, errors.Join(err, fmt.Errorf("no API key for provider %q", meta.Provider))
		}
		client := llm.New(prov.BaseURL, key)
		client.MaxRetries = runtimeCfg.MaxRetries
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
		prompt := rlm.BuildPrompt(meta.CWD, nil) + skills.PromptBlock(skills.Scan(skills.DirsFor(meta.CWD)...))
		services := daemonToolServices(runtimeCfg, meta, apiID)
		ag := agent.NewRuntime(client, apiID, maxOutput, prompt, services)
		ag.ModelName, ag.Provider = meta.Model, meta.Provider
		ag.WorkingDir = meta.CWD
		ag.ContextLimit = contextLimit
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
		ag.Vision = vision
		ag.Effort = resolvedRuntimeEffort(catalogs, meta.Provider, apiID, meta.Effort, runtimeCfg.DefaultEffort)
		ag.ResolveModel = func(model, provider string) (agent.ModelRoute, error) {
			currentCfg, loadErr := config.Load()
			if loadErr != nil {
				return agent.ModelRoute{}, loadErr
			}
			resolvedProvider, resolvedModel, apiID, resolveErr := currentCfg.Resolve(model, provider)
			if resolveErr != nil {
				return agent.ModelRoute{}, resolveErr
			}
			key, keyErr := resolvedProvider.ResolveKey()
			if keyErr != nil {
				return agent.ModelRoute{}, keyErr
			}
			if key == "" {
				return agent.ModelRoute{}, fmt.Errorf("no API key for the provider serving %q", model)
			}
			childClient := llm.New(resolvedProvider.BaseURL, key)
			childClient.MaxRetries = currentCfg.MaxRetries
			resolvedProviderName := provider
			if resolvedProviderName == "" {
				resolvedProviderName = currentCfg.DefaultProvider
			}
			if resolvedProviderName == "" && len(resolvedModel.Providers) > 0 {
				resolvedProviderName = resolvedModel.Providers[0]
			}
			resolvedVision := resolvedModel.Vision
			if currentCatalog, ok := config.LoadCatalogs()[resolvedProviderName]; ok {
				if advertised, found := currentCatalog.SupportsVision(apiID); found {
					resolvedVision = advertised
				}
			}
			return agent.ModelRoute{
				Client: childClient, ModelName: model, Provider: resolvedProviderName, Model: apiID,
				ContextLimit: resolvedModel.ContextWindow(), MaxTokens: resolvedModel.MaxOut,
				Vision: resolvedVision,
			}, nil
		}
		compactName := runtimeCfg.CompactModel
		if compactName == "" {
			compactName = config.DefaultCompactModel
		}
		if compactProvider, _, compactID, resolveErr := runtimeCfg.Resolve(compactName, runtimeCfg.CompactProvider); resolveErr == nil {
			if compactKey, keyErr := compactProvider.ResolveKey(); keyErr == nil && compactKey != "" {
				ag.CompactClient = llm.New(compactProvider.BaseURL, compactKey)
				ag.CompactClient.MaxRetries = runtimeCfg.MaxRetries
				ag.CompactModel = compactID
			}
		}
		compactPct := runtimeCfg.CompactPct
		if compactPct == 0 {
			compactPct = config.DefaultCompactPct
		}
		ag.CompactThreshold = float64(min(max(compactPct, 10), 90)) / 100
		ag.BrowserDisabled = runtimeCfg.Browser.Enabled != nil && !*runtimeCfg.Browser.Enabled
		ag.ComputerDisabled = runtimeCfg.Computer.Enabled != nil && !*runtimeCfg.Computer.Enabled
		var mcpManager *mcp.Manager
		discovery := mcp.LoadMergedFiltered(meta.CWD, mcp.FromConfigMap(runtimeCfg.MCPServers), mcp.ImportPolicyFrom(runtimeCfg.MCPImport))
		if len(discovery.Merged) > 0 || len(discovery.Blocked) > 0 {
			mcpManager = mcp.NewManager(discovery.Merged)
			mcpManager.SetBlocked(discovery.Blocked)
		}
		ag.Messages = append(ag.Messages[:1], rlm.FocusedHistory(history)...)
		inputPrice, outputPrice, cacheReadPrice := 0.0, 0.0, 0.0
		if hasCatalog {
			inputPrice, outputPrice, cacheReadPrice, _ = catalog.Pricing(apiID)
		}
		runtime, err := daemon.NewRecursiveRuntime(daemon.RecursiveRuntimeOptions{
			Agent: ag, History: history, Limits: limits, Kernels: kernels, MCP: mcpManager,
			KernelCommand: daemonKernelCommand,
			InputPrice:    inputPrice, OutputPrice: outputPrice, CacheReadPrice: cacheReadPrice,
		})
		if err != nil {
			if mcpManager != nil {
				mcpManager.Close()
			}
			services.Close()
			return daemon.Components{}, err
		}
		components := daemon.Components{
			Runner: runtime.RootSession(), Runtime: runtime, Bind: runtime.Bind,
		}
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
	lifecycleRequested := make(chan bool, 1)
	var lifecycleOnce sync.Once
	requestLifecycle := func(restart bool) {
		lifecycleOnce.Do(func() { lifecycleRequested <- restart })
	}
	server, err := daemon.NewServer(ownerDaemon, daemon.ServerOptions{
		BuildID: version, Generation: generation, RuntimeDir: paths.Runtime,
		Restart: func() { requestLifecycle(true) }, Stop: func() { requestLifecycle(false) },
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
	case restart := <-lifecycleRequested:
		status := "stopping"
		if restart {
			status = "restarting"
		}
		_ = store.SetDaemonStatus(context.Background(), generation, status)
		if err := server.Close(); err != nil {
			return err
		}
		_ = os.Remove(paths.Socket)
		if err := owner.Close(); err != nil {
			return err
		}
		if restart {
			return restartDaemonBinary()
		}
		return nil
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
		manager := browser.NewManager(mode)
		if cfg.Browser.Driver != "" {
			manager.SwitchDriver(cfg.Browser.Driver)
		}
		services.SetBrowser(manager, cfg.Browser.AllowPrivateURLs)
	}
	if cfg.Computer.Enabled == nil || *cfg.Computer.Enabled {
		defaultDeny := cfg.Computer.DefaultDeny != nil && *cfg.Computer.DefaultDeny
		services.SetComputerPolicy(computer.NewPolicy(cfg.Computer.Allow, cfg.Computer.Deny, defaultDeny))
	}
	services.SetDiagnostics(lsp.NewManager(lsp.FromConfigMap(cfg.LSPServers)))
	return services
}

func resolvedRuntimeEffort(catalogs map[string]config.Catalog, provider, modelID, stored, defaultEffort string) string {
	if stored == "off" {
		return ""
	}
	if stored != "" {
		return stored
	}
	return tui.DefaultEffortFor(catalogs, provider, modelID, defaultEffort)
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
