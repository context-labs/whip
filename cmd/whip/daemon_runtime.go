package main

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

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
	providersvc "github.com/context-labs/whip/internal/provider"
	"github.com/context-labs/whip/internal/rlm"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/tools"
)

// daemonRuntimeFactory borrows startup resources and constructs session components
// for the daemon to own.
func daemonRuntimeFactory(
	store *session.Store,
	providers *providersvc.ProviderService,
	kernels *rlm.Manager,
	limits rlm.Limits,
) daemon.Factory {
	return func(ctx context.Context, meta session.Meta, history []llm.Message) (daemon.Components, error) {
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
		definition, ok, err := daemon.DefinitionFor(ctx, store, meta)
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
				ag.SteerImages("images attached (browser/computer screenshots or MCP results):", screenshotParts(images))
			})
		}
		ag.Vision = route.Vision
		// The saved effort is concrete: the daemon resolves it at creation and
		// upgrades older blank rows at open. "off" becomes an omitted parameter
		// at the request boundary.
		ag.Effort = meta.Effort
		ag.ResolveModel = func(model, provider string) (agent.ModelRoute, error) {
			currentCfg, loadErr := config.Load()
			if loadErr != nil {
				return agent.ModelRoute{}, loadErr
			}
			resolved, _, resolveErr := resolveRuntimeModel(currentCfg, model, provider, providers)
			return resolved, resolveErr
		}
		configureRuntimeCompaction(ag, runtimeCfg, definition.Compaction, providers)
		var mcpManager *mcp.Manager
		discovery := mcp.LoadMergedFiltered(meta.CWD, mcp.FromConfigMap(runtimeCfg.MCPServers), mcp.ImportPolicyFrom(runtimeCfg.MCPImport))
		// The definition names the servers it uses; everything else the host
		// configured stays out of this session. Attachment applies the same step.
		discovery = mcp.Select(discovery, definition.MCP.Servers)
		for source, err := range discovery.Errs {
			config.LogEvent("mcp", fmt.Sprintf("discovery: %s: %v", source, err))
		}
		if len(discovery.Merged) > 0 || len(discovery.Blocked) > 0 || len(discovery.Errs) > 0 {
			mcpManager = mcp.NewManager(discovery.Merged)
			mcpManager.SetBlocked(discovery.Blocked)
			mcpManager.SetSourceErrors(discovery.Errs)
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
			LoadMCP: func(ctx context.Context) (mcp.Filtered, error) {
				if err := ctx.Err(); err != nil {
					return mcp.Filtered{}, err
				}
				fresh, err := config.Load()
				if err != nil {
					return mcp.Filtered{}, err
				}
				return mcp.LoadMergedFiltered(meta.CWD, mcp.FromConfigMap(fresh.MCPServers), mcp.ImportPolicyFrom(fresh.MCPImport)), ctx.Err()
			},
		}
		if mcpManager != nil {
			components.MCP = mcpManager
		}
		return components, nil
	}
}

// configureRuntimeCompaction leaves Automatic on the conversation's actual route.
func configureRuntimeCompaction(
	ag *agent.Agent,
	cfg *config.Config,
	defaults agentdef.CompactionDefaults,
	providers *providersvc.ProviderService,
) {
	ag.CompactThreshold = defaults.Threshold
	if ag.CompactThreshold == 0 {
		percent := cfg.CompactPct
		if percent == 0 {
			percent = config.DefaultCompactPct
		}
		ag.CompactThreshold = float64(min(max(percent, 10), 90)) / 100
	}
	model, provider := defaults.Model, defaults.Provider
	if model == "" {
		model, provider = cfg.CompactModel, cfg.CompactProvider
	}
	if model == "" {
		return
	}
	compact, _, err := resolveRuntimeModel(cfg, model, provider, providers)
	if err != nil {
		ag.CompactFallback = "Custom summarizer unavailable; using this conversation’s model."
		config.LogEvent("compaction.fallback", fmt.Sprintf(
			"compact model %q (provider %q) unavailable, summaries run on the conversation model: %v",
			model, provider, err,
		))
		return
	}
	ag.CompactClient = compact.Client
	ag.CompactModel, ag.CompactProvider = compact.Model, compact.Provider
	ag.CompactPricing = compact.Pricing
	ag.CompactContextLimit, ag.CompactMaxTokens = compact.ContextLimit, compact.MaxTokens
}

// resolveRuntimeModel snapshots the actual endpoint and its catalog rates together.
// All model purposes use this resolver so a default provider cannot accidentally
// supply another endpoint's price or output limits.
func resolveRuntimeModel(cfg *config.Config, modelName, providerName string, services ...*providersvc.ProviderService) (agent.ModelRoute, config.Model, error) {
	providerName, provider, model, apiID, err := cfg.ResolveRoute(modelName, providerName)
	if err != nil {
		return agent.ModelRoute{}, config.Model{}, err
	}
	if modelName == "" {
		modelName = cfg.DefaultModel
	}
	var providers *providersvc.ProviderService
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
