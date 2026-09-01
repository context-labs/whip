// `whip acp` — serve whip as an Agent Client Protocol agent over stdio, so
// ACP clients (Zed, other editors) can drive the agent loop. One process
// speaks newline-delimited JSON-RPC 2.0 on stdin/stdout; nothing but protocol
// frames may touch stdout, so all logging goes to stderr + the event log.
//
// The agent side of the protocol lives in internal/acp (bridge/translate/
// permission); this file is only process wiring: config → model → bridge
// factory → SDK connection → clean shutdown.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	acpsdk "github.com/coder/acp-go-sdk"

	"github.com/context-labs/whip/internal/acp"
	"github.com/context-labs/whip/internal/agent"
	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/lsp"
	"github.com/context-labs/whip/internal/mcp"
	"github.com/context-labs/whip/internal/memory"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/skills"
	"github.com/context-labs/whip/internal/tools"
	"github.com/context-labs/whip/internal/tui"
)

func acpCLI(args []string) error {
	fs := flag.NewFlagSet("acp", flag.ContinueOnError)
	modelFlag := fs.String("m", "", "model name from ~/.whip/config.json (default: defaultModel)")
	providerFlag := fs.String("p", "", "provider to route the model through (default: model's first provider)")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: whip acp [-m model] [-p provider]")
		fmt.Fprintln(os.Stderr, "serve whip as an ACP agent over stdio (for editors like Zed)")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	prov, mdl, apiID, err := tui.ResolveWithRefresh(cfg, *modelFlag, *providerFlag)
	if err != nil {
		return err
	}
	modelName, provName := *modelFlag, *providerFlag
	if modelName == "" {
		modelName = cfg.DefaultModel
	}
	if provName == "" {
		provName = cfg.DefaultProvider
		if provName == "" && len(mdl.Providers) > 0 {
			provName = mdl.Providers[0]
		}
	}
	key, err := prov.ResolveKey()
	if err != nil {
		return err
	}
	if key == "" {
		return fmt.Errorf("no API key for provider %q (set apiKey/apiKeyEnv in ~/.whip/config.json)", provName)
	}

	vision := acpSupportsVision(cfg, modelName, apiID, provName)
	acp.SetEventLog(func(format string, args ...any) { config.LogEvent("acp", fmt.Sprintf(format, args...)) })

	// The session store is also the capability ledger; execution must not
	// continue without it.
	dir, err := config.Dir()
	if err != nil {
		return fmt.Errorf("session directory: %w", err)
	}
	store, err := session.Open(dir + "/sessions.db")
	if err != nil {
		return fmt.Errorf("session store: %w", err)
	}
	defer func() { _ = store.Close() }()

	// Resolved once: the catalog values buildAgent re-derives per session are
	// constant for the process lifetime (same provider + model).
	cat, hasCat := config.LoadCatalogs()[provName]
	ctxLimit := mdl.ContextWindow()
	if hasCat {
		if n := cat.ContextLength(apiID); n > 0 {
			ctxLimit = n
		}
	}
	maxOut := mdl.MaxOut
	if maxOut <= 0 && hasCat {
		maxOut = cat.MaxCompletionTokens(apiID)
	}
	if maxOut <= 0 {
		maxOut = ctxLimit // generous default; provider clamps if too high
	}

	// Editor sessions have no consent prompt of their own outside ACP
	// permissions: computer_exec stays off (same posture as `whip run`).
	factory := func(ctx context.Context, wd string, servers map[string]mcp.ServerConfig) (*agent.Agent, *mcp.Manager, error) {
		client := llm.New(prov.BaseURL, key)
		client.MaxRetries = cfg.MaxRetries
		toolServices := tools.NewServices()
		lspMgr := lsp.NewManager(lsp.FromConfigMap(cfg.LSPServers))
		toolServices.SetDiagnostics(lspMgr)
		ag := agent.NewWithServices(client, apiID, maxOut, systemPrompt(wd, time.Now()), toolServices)
		ag.ModelName, ag.Provider = modelName, provName
		ag.ComputerDisabled = true
		ag.ContextLimit = ctxLimit
		// Reasoning effort: explicit cfg.DefaultEffort wins; "" resolves
		// model-aware — "low" when the model advertises it, else the lowest
		// supported level, else off — so a non-reasoning model never sends an
		// effort parameter the provider would reject.
		ag.Effort = tui.DefaultEffortFor(config.LoadCatalogs(), provName, ag.Model, cfg.DefaultEffort)

		// Skills + memory ride the system prompt. The TUI refreshes these per
		// turn; ACP sessions refresh at session creation — a new skill lands
		// with the next session/new, not mid-conversation.
		ag.Messages[0].Content += skills.PromptBlock(skills.Scan(skills.DefaultDirs()...))
		ag.Messages[0].Content += memory.PromptBlock(memory.Installation(), memory.Session("acp"))

		// MCP: whip's merged config (own + claude/codex imports, gated) plus
		// the client's session servers (already merged by the bridge — whip
		// config wins clashes).
		mgr := mcp.NewManager(servers)
		mgr.SetOnChange(func() { ag.SetMCPTools(mgr.Tools()) })
		ag.SetMCPTools(mgr.Tools())
		if ib := mgr.InstructionsBlock(); ib != "" {
			ag.Messages[0].Content += ib
		}
		return ag, mgr, nil
	}

	bridge := acp.NewBridge(version, factory, store, vision, acpBaseMCP(cfg))
	conn := acpsdk.NewAgentSideConnection(bridge, os.Stdout, os.Stdin)
	bridge.SetAgentConnection(conn)

	// SIGINT/SIGTERM close the connection: Done() unblocks main so deferred
	// cleanup (MCP/LSP shutdown, KillAll) runs instead of orphaning children.
	sigCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	select {
	case <-conn.Done():
	case <-sigCtx.Done():
	}

	// Client gone or signal: stop every in-flight/queued turn and shut each
	// session's scoped MCP, LSP, browser, and child processes down.
	bridge.CloseAll()
	return nil
}

// acpSupportsVision mirrors the TUI's modelSupportsVision (tui.go): the
// provider-advertised input_modalities win; else the config's per-model
// vision flag. ponytail: duplicated rather than exported from the UI package.
func acpSupportsVision(cfg *config.Config, modelName, modelID, provName string) bool {
	if cat, ok := config.LoadCatalogs()[provName]; ok {
		if vision, found := cat.SupportsVision(modelID); found {
			return vision
		}
	}
	if mc, ok := cfg.Models[modelName]; ok {
		return mc.Vision
	}
	return false
}

// acpBaseMCP is whip's own merged MCP config (config + .mcp.json + codex
// imports, gated by mcpImport policy) — the per-session floor that client
// servers are layered over. Discovery is rooted at the process cwd, matching
// the TUI's startup.
func acpBaseMCP(cfg *config.Config) map[string]mcp.ServerConfig {
	disc := mcp.LoadMergedFiltered(cwd(), mcp.FromConfigMap(cfg.MCPServers), mcp.ImportPolicyFrom(cfg.MCPImport))
	for src, err := range disc.Errs {
		config.LogEvent("acp", fmt.Sprintf("mcp discovery: %s: %s", src, err))
	}
	return disc.Merged
}
