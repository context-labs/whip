package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/context-labs/whip/internal/buildinfo"

	"github.com/context-labs/whip/internal/agent"
	"github.com/context-labs/whip/internal/agentdef"
	"github.com/context-labs/whip/internal/browser"
	"github.com/context-labs/whip/internal/computer"
	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/daemon"
	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/lsp"
	"github.com/context-labs/whip/internal/mcp"
	"github.com/context-labs/whip/internal/openaiauth"
	"github.com/context-labs/whip/internal/rlm"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/tools"
	"github.com/context-labs/whip/internal/tui"
)

var (
	restartDaemonBinary = daemon.RestartSelfDaemon
	daemonKernelCommand []string
)

func daemonCLI(args []string) error {
	signals, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return runDaemon(signals, args)
}

func runDaemon(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("_daemon", flag.ContinueOnError)
	maintenanceFD := fs.Int("maintenance-fd", 0, "inherited backend maintenance descriptor")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("hidden daemon mode does not accept arguments")
	}
	network, err := daemonNetworkEnvironment()
	if err != nil {
		return err
	}
	dir, err := config.Dir()
	if err != nil {
		return err
	}
	paths, err := daemon.Paths(dir)
	if err != nil {
		return err
	}
	startup, err := daemon.AcquireStartup(paths, *maintenanceFD)
	if err != nil {
		return err
	}
	owner, err := daemon.AcquireOwner(paths.Lock)
	_ = startup.Close()
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
	store.SetGlobalPermissionRules(cfg.Permissions.Allow)
	generation, err := store.BeginDaemonGeneration(context.Background(), version)
	if err != nil {
		_ = store.Close()
		return err
	}
	limits := rlmLimits(cfg.RLM)
	providers := daemon.NewProviderService(ctx, strconv.FormatInt(generation, 10))
	defer providers.Close()
	if discovered, discoveryErr := providers.DiscoverProviders(ctx, "", ""); discoveryErr != nil {
		config.LogEvent("provider.discovery", discoveryErr.Error())
	} else if discovered.DiscoveryError != "" {
		config.LogEvent("provider.discovery", discovered.DiscoveryError)
	}
	kernels := rlm.NewManager(limits.MaxWorkers)
	defer kernels.Close()
	factory := func(_ context.Context, meta session.Meta, history []llm.Message) (daemon.Components, error) {
		runtimeCfg, err := config.Load()
		if err != nil {
			return daemon.Components{}, err
		}
		// Hand edits to the allowlist take effect on the next session.
		store.SetGlobalPermissionRules(runtimeCfg.Permissions.Allow)
		if meta.Kind == session.SessionKindToolHost {
			services := daemonToolServices(runtimeCfg, meta, "mcp", agentdef.Capabilities)
			return daemon.Components{Runner: daemon.NewToolRunner(services)}, nil
		}
		definition, ok, err := daemon.DefinitionFor(meta)
		if err != nil {
			return daemon.Components{}, err
		}
		if !ok {
			return daemon.Components{}, fmt.Errorf("unsupported session kind %q", meta.Kind)
		}
		route, model, err := resolveRuntimeModel(runtimeCfg, meta.Model, meta.Provider, providers)
		if err != nil {
			return daemon.Components{}, err
		}
		services := daemonToolServices(runtimeCfg, meta, route.Model, definition.Capabilities)
		ag := agent.NewRuntime(route.Client, route.Model, route.MaxTokens, "", services)
		ag.ModelName, ag.Provider = route.ModelName, route.Provider
		ag.Pricing = route.Pricing
		ag.WorkingDir = meta.CWD
		ag.ContextLimit = route.ContextLimit
		if model.SamplingParams != nil {
			ag.Temperature, ag.TopP = model.SamplingParams.Temperature, model.SamplingParams.TopP
		}
		if route.Vision {
			services.SetScreenshotSink(func(images [][]byte) {
				ag.SteerImages("browser/computer screenshots attached:", screenshotParts(images))
			})
		}
		ag.Vision = route.Vision
		effort := meta.Effort
		if effort == "" {
			effort = definition.Model.Effort
		}
		ag.Effort = resolvedRuntimeEffort(providers.Catalogs(), route.Provider, route.Model, effort, runtimeCfg.DefaultEffort)
		ag.ResolveModel = func(model, provider string) (agent.ModelRoute, error) {
			currentCfg, loadErr := config.Load()
			if loadErr != nil {
				return agent.ModelRoute{}, loadErr
			}
			resolved, _, resolveErr := resolveRuntimeModel(currentCfg, model, provider, providers)
			return resolved, resolveErr
		}
		// Compaction defaults: the definition first, then host configuration.
		compactName, compactProvider := definition.Compaction.Model, definition.Compaction.Provider
		if compactName == "" {
			compactName, compactProvider = runtimeCfg.CompactModel, runtimeCfg.CompactProvider
		}
		if compactName == "" {
			compactName = config.DefaultCompactModel
		}
		if compact, _, resolveErr := resolveRuntimeModel(runtimeCfg, compactName, compactProvider, providers); resolveErr == nil {
			ag.CompactClient = compact.Client
			ag.CompactModel, ag.CompactProvider = compact.Model, compact.Provider
			ag.CompactPricing = compact.Pricing
		}
		ag.CompactThreshold = definition.Compaction.Threshold
		if ag.CompactThreshold == 0 {
			compactPct := runtimeCfg.CompactPct
			if compactPct == 0 {
				compactPct = config.DefaultCompactPct
			}
			ag.CompactThreshold = float64(min(max(compactPct, 10), 90)) / 100
		}
		var mcpManager *mcp.Manager
		discovery := mcp.LoadMergedFiltered(meta.CWD, mcp.FromConfigMap(runtimeCfg.MCPServers), mcp.ImportPolicyFrom(runtimeCfg.MCPImport))
		if len(discovery.Merged) > 0 || len(discovery.Blocked) > 0 {
			mcpManager = mcp.NewManager(discovery.Merged)
			mcpManager.SetBlocked(discovery.Blocked)
		}
		runtime, err := daemon.NewRecursiveRuntime(daemon.RecursiveRuntimeOptions{
			Engine: meta.ExecutionEngine, Definition: definition, Agent: ag, History: history, Limits: limits, Kernels: kernels,
			KernelCommand: daemonKernelCommand,
		})
		if err != nil {
			if mcpManager != nil {
				mcpManager.Close()
			}
			services.Close()
			return daemon.Components{}, err
		}
		components := daemon.Components{
			Runner: runtime.RootSession(), Runtime: runtime, Bind: runtime.Bind, Definition: definition,
		}
		if mcpManager != nil {
			components.MCP = mcpManager
		}
		return components, nil
	}
	ownerDaemon, err := daemon.New(store, factory, providers)
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
		BuildID: version, Generation: generation, RuntimeDir: paths.Runtime, Network: network,
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

// resolveRuntimeModel snapshots the actual endpoint and its catalog rates together.
// All model purposes use this resolver so a default provider cannot accidentally
// supply another endpoint's price or output limits.
func resolveRuntimeModel(cfg *config.Config, modelName, providerName string, services ...*daemon.ProviderService) (agent.ModelRoute, config.Model, error) {
	providerName, provider, model, apiID, err := cfg.ResolveRoute(modelName, providerName)
	if err != nil {
		return agent.ModelRoute{}, config.Model{}, err
	}
	if modelName == "" {
		modelName = cfg.DefaultModel
	}
	var providers *daemon.ProviderService
	if len(services) > 0 {
		providers = services[0]
	}
	catalog := providers.CatalogsFor(cfg)[providerName]
	if len(model.Providers) > 0 && !slices.Contains(model.Providers, providerName) && catalog.Find(apiID) == nil {
		return agent.ModelRoute{}, config.Model{}, fmt.Errorf("model %q is not advertised by provider %q; select a model on that provider", apiID, providerName)
	}
	client, err := providers.ModelClient(provider, cfg)
	if err != nil {
		return agent.ModelRoute{}, config.Model{}, err
	}
	contextLimit, maxOutput := catalog.ModelLimits(apiID, model)
	if provider.API == openaiauth.Provider {
		maxOutput = llm.SubscriptionOutputLimit(apiID)
		if maxOutput == 0 || (model.MaxOut > 0 && model.MaxOut < maxOutput) {
			return agent.ModelRoute{}, config.Model{}, errors.New("ChatGPT subscription route requires a verified model limit and cannot enforce a smaller maxOut")
		}
	}
	vision := model.Vision
	if advertised, found := catalog.SupportsVision(apiID); found {
		vision = advertised
	}
	client.MaxRetries = cfg.MaxRetries
	pricing := llm.Pricing{}
	if provider.API != openaiauth.Provider && strings.TrimRight(catalog.BaseURL, "/") == client.BaseURL {
		pricing = catalog.ModelPricing(apiID)
	}
	return agent.ModelRoute{
		Client: client, ModelName: modelName, Provider: providerName, Model: apiID,
		ContextLimit: contextLimit, MaxTokens: maxOutput, Vision: vision,
		Pricing: pricing,
	}, model, nil
}

// daemonToolServices wires host integrations for one session. Browser and
// computer automation follow the definition's capabilities; the host
// configuration can still disable them.
func daemonToolServices(cfg *config.Config, meta session.Meta, apiID string, capabilities []string) *tools.Services {
	services := tools.NewServices()
	services.SetExternalPermissions(true)
	services.SetProcessMarkers(meta.ID, apiID)
	if cfg.Browser.CDPURL != "" {
		services.SetProcessEnvironment(map[string]string{"WHIP_CDP_URL": cfg.Browser.CDPURL})
	}
	if slices.Contains(capabilities, "browser") && (cfg.Browser.Enabled == nil || *cfg.Browser.Enabled) {
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
	if slices.Contains(capabilities, "computer") && (cfg.Computer.Enabled == nil || *cfg.Computer.Enabled) {
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
	if value.MaxConcurrentHostCalls != 0 {
		limits.MaxConcurrentHostCalls = value.MaxConcurrentHostCalls
	}
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

// screenshotParts bounds captures like every other image entering history: a
// HiDPI full-display shot exceeds both the dimension and byte caps.
func screenshotParts(images [][]byte) []llm.ContentPart {
	parts := make([]llm.ContentPart, 0, len(images))
	for _, image := range images {
		ext, data := llm.NormalizeImage("jpg", image)
		parts = append(parts, llm.ImagePart(ext, data))
	}
	return parts
}

// Network settings are inherited by explicit starts, automatic starts, and
// binary replacement. An ordinary invocation never enables a TCP listener.
func daemonNetworkEnvironment() (daemon.NetworkOptions, error) {
	options := daemon.NetworkOptions{Address: strings.TrimSpace(os.Getenv(buildinfo.Env("LISTEN")))}
	options.Enabled = options.Address != ""
	if value := os.Getenv(buildinfo.Env("NETWORK")); value != "" {
		enabled, err := strconv.ParseBool(value)
		if err != nil {
			return daemon.NetworkOptions{}, fmt.Errorf("%s must be a boolean: %w", buildinfo.Env("NETWORK"), err)
		}
		options.Enabled = enabled
	}
	parseList := func(value string) []string {
		result := []string{}
		for item := range strings.SplitSeq(value, ",") {
			if item = strings.TrimSpace(item); item != "" {
				result = append(result, item)
			}
		}
		return result
	}
	options.AllowedOrigins = parseList(os.Getenv(buildinfo.Env("ALLOWED_ORIGINS")))
	options.AllowedHosts = parseList(os.Getenv(buildinfo.Env("ALLOWED_HOSTS")))
	return options, nil
}
