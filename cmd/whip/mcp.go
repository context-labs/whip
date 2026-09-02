package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/daemon"
	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/mcp"
)

// mcpCLI implements `whip mcp <list|add|remove|serve|test|import>`.
//
//	list                        merged view of every configured server, its source, and blocked state
//	add <name> -- <cmd...>      register a stdio server
//	add <name> --url <url>      register a remote (streamable HTTP) server
//	remove <name>               drop a server from whip's own config
//	import [--dry-run]          materialize imported (claude/codex) servers into whip's config
//	serve                       run whip's tools as an MCP server over stdio
//
// add/remove/import write through config.Save (atomic, clobber-guarded).
// Servers imported from .mcp.json or codex can't be removed here (edit the
// source file); remove on an imported name explains that.
func mcpCLI(args []string, version string) error {
	if len(args) == 0 {
		return errors.New("usage: whip mcp <list|add|remove|import|serve|test>")
	}
	if args[0] == "serve" {
		return mcpServe(version)
	}
	if args[0] == "test" {
		if len(args) < 2 {
			return errors.New("usage: whip mcp test <name>")
		}
		return mcpTestCLI(args[1])
	}
	if args[0] == "import" {
		return mcpImportCLI(args[1:])
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	switch args[0] {
	case "list":
		wd, _ := os.Getwd()
		disc := mcp.LoadMergedFiltered(wd, mcp.FromConfigMap(cfg.MCPServers), mcp.ImportPolicyFrom(cfg.MCPImport))
		names := make([]string, 0, len(disc.Merged)+len(disc.Blocked))
		for name := range disc.Merged {
			names = append(names, name)
		}
		for name := range disc.Blocked {
			names = append(names, name)
		}
		sort.Strings(names)
		if len(names) == 0 {
			fmt.Println("no MCP servers configured")
		}
		for _, name := range names {
			status := "enabled"
			var c mcp.ServerConfig
			if b, ok := disc.Blocked[name]; ok {
				c, status = b, "blocked"
			} else {
				c = disc.Merged[name]
				if c.Disabled() {
					status = "disabled"
				}
			}
			fmt.Printf("%-20s %-9s %-30s %s\n", name, status, mcpTarget(c), disc.Sources[name]+" config")
		}
		for src, e := range disc.Errs {
			fmt.Fprintf(os.Stderr, "mcp: %s: %s\n", src, e)
		}
		return nil

	case "add":
		if len(args) < 2 {
			return errors.New("usage: whip mcp add <name> -- <cmd...> | whip mcp add <name> --url <url>")
		}
		name := args[1]
		entry := config.MCPServer{}
		rest := args[2:]
		switch {
		case len(rest) >= 2 && rest[0] == "--url":
			entry.URL = rest[1]
		case len(rest) >= 2 && rest[0] == "--":
			entry.Command = rest[1:]
		default:
			return errors.New("usage: whip mcp add <name> -- <cmd...> | whip mcp add <name> --url <url>")
		}
		sc := mcp.FromConfigMap(map[string]config.MCPServer{name: entry})[name]
		if msg := sc.Valid(); msg != "" {
			return fmt.Errorf("invalid server: %s", msg)
		}
		if cfg.MCPServers == nil {
			cfg.MCPServers = map[string]config.MCPServer{}
		}
		cfg.MCPServers[name] = entry
		if err := cfg.Save(); err != nil {
			return err
		}
		fmt.Printf("added mcp server %q — starts on next whip launch\n", name)
		return nil

	case "remove":
		if len(args) < 2 {
			return errors.New("usage: whip mcp remove <name>")
		}
		name := args[1]
		if _, ok := cfg.MCPServers[name]; !ok {
			// Maybe it's imported (or blocked by the import policy).
			wd, _ := os.Getwd()
			disc := mcp.LoadMergedFiltered(wd, mcp.FromConfigMap(cfg.MCPServers), mcp.ImportPolicyFrom(cfg.MCPImport))
			if _, imported := disc.Merged[name]; imported {
				return fmt.Errorf("%q comes from .mcp.json or ~/.codex/config.toml — edit that file to remove it", name)
			}
			if _, blocked := disc.Blocked[name]; blocked {
				return fmt.Errorf("%q is blocked by the mcpImport config — edit ~/.whip/config.json", name)
			}
			return fmt.Errorf("no mcp server named %q", name)
		}
		delete(cfg.MCPServers, name)
		if err := cfg.Save(); err != nil {
			return err
		}
		fmt.Printf("removed mcp server %q\n", name)
		return nil
	}
	return fmt.Errorf("unknown mcp subcommand %q (list|add|remove|import|serve|test)", args[0])
}

func mcpServe(version string) error {
	wd, err := os.Getwd()
	if err != nil {
		return err
	}
	ctx := context.Background()
	clientID := daemonClientID("mcp")
	client, err := daemon.NewRootClient(daemon.RootClientOptions{
		ClientID:  clientID,
		Create:    &daemon.CreateSession{CWD: wd, Model: "mcp", Provider: "local"},
		Connector: daemonConnector("automation", clientID),
	})
	if err != nil {
		return err
	}
	client.Start()
	defer func() { _ = client.Close() }()
	if err := client.WaitLive(ctx); err != nil {
		return err
	}
	configure, err := client.NewAction("tool.configure", map[string]bool{"deny_permissions": true})
	if err != nil {
		return err
	}
	if result, err := client.Command(ctx, configure); err != nil {
		return err
	} else if result.Status != "succeeded" {
		return errors.New(result.Error)
	}
	provider := daemonMCPTools{client: client}
	serveErr := mcp.Serve(ctx, version, provider)
	rootID := client.RootID()
	closeErr := client.Close()
	deleteErr := deleteDaemonSession(clientID, rootID)
	return errors.Join(serveErr, closeErr, deleteErr)
}

type daemonMCPTools struct{ client *daemon.RootClient }

func (p daemonMCPTools) ToolDefinitions(ctx context.Context) ([]llm.Tool, error) {
	action, err := p.client.NewAction("tool.schema", struct{}{})
	if err != nil {
		return nil, err
	}
	result, err := p.client.Command(ctx, action)
	if err != nil {
		return nil, err
	}
	if result.Status != "succeeded" {
		return nil, errors.New(result.Error)
	}
	var definitions []llm.Tool
	if err := json.Unmarshal([]byte(result.Output), &definitions); err != nil {
		return nil, fmt.Errorf("decode daemon tool schemas: %w", err)
	}
	return definitions, nil
}

func (p daemonMCPTools) CallTool(ctx context.Context, name string, arguments json.RawMessage) (string, error) {
	action, err := p.client.NewAction("tool.call", map[string]any{"tool": name, "arguments": arguments})
	if err != nil {
		return "", err
	}
	result, err := p.client.Command(ctx, action)
	if err != nil {
		return "", err
	}
	if result.Status != "succeeded" {
		return "", errors.New(result.Error)
	}
	return result.Output, nil
}

// mcpTestCLI is the doctor: connect to one configured server, report status,
// timing, tool names, and the stderr tail on failure. Exits non-zero when the
// server isn't usable, so CI can validate a .mcp.json before it ships.
func mcpTestCLI(name string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	wd, _ := os.Getwd()
	disc := mcp.LoadMergedFiltered(wd, mcp.FromConfigMap(cfg.MCPServers), mcp.ImportPolicyFrom(cfg.MCPImport))
	sc, ok := disc.Merged[name]
	if !ok {
		if _, blocked := disc.Blocked[name]; blocked {
			return fmt.Errorf("server %q is blocked by the mcpImport config — edit ~/.whip/config.json", name)
		}
		return fmt.Errorf("no mcp server named %q (try: whip mcp list)", name)
	}
	fmt.Printf("testing mcp server %q (%s)…\n", name, mcpTarget(sc))
	res := mcp.Probe(context.Background(), name, sc)
	switch res.Status {
	case mcp.StatusReady:
		fmt.Printf("✓ connected in %s — %d tools\n", res.Elapsed.Round(time.Millisecond), res.Tools)
		if len(res.ToolNames) > 0 {
			fmt.Println("  tools:", strings.Join(res.ToolNames, ", "))
		}
		return nil
	case mcp.StatusDisabled:
		fmt.Println("○ disabled — enable it in ~/.whip/config.json")
		return fmt.Errorf("server %q is disabled", name)
	default:
		fmt.Printf("✗ failed after %s: %s\n", res.Elapsed.Round(time.Millisecond), res.Err)
		if res.Note != "" {
			fmt.Println("  note:", res.Note)
		}
		if res.Source != "" {
			fmt.Println("  config:", res.Source)
		}
		return fmt.Errorf("server %q failed", name)
	}
}

func mcpTarget(c mcp.ServerConfig) string {
	if c.Remote() {
		return c.URL
	}
	return strings.Join(c.Command, " ")
}

// mcpImportCLI materializes imported (claude/codex) servers into whip's own
// config — mcp-polish item 6. Imported means: admitted by the mcpImport
// policy and not already in whip's config (idempotent; existing whip
// entries are never touched). --dry-run prints the JSONC fragment instead of
// writing.
func mcpImportCLI(args []string) error {
	dryRun := false
	for _, a := range args {
		if a == "--dry-run" {
			dryRun = true
		} else {
			return errors.New("usage: whip mcp import [--dry-run]")
		}
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	wd, _ := os.Getwd()
	disc := mcp.LoadMergedFiltered(wd, mcp.FromConfigMap(cfg.MCPServers), mcp.ImportPolicyFrom(cfg.MCPImport))
	add := map[string]config.MCPServer{}
	for name, sc := range disc.Merged {
		if _, owned := cfg.MCPServers[name]; owned {
			continue // already whip's own — importing is a no-op
		}
		add[name] = config.MCPServer{
			Command: sc.Command, Env: sc.Env, Cwd: sc.Cwd,
			URL: sc.URL, Headers: sc.Headers, Enabled: sc.Enabled,
			Note: sc.Note, StartupTimeout: sc.StartupTimeout, ToolTimeout: sc.ToolTimeout,
		}
	}
	if len(add) == 0 {
		fmt.Println("nothing to import — all servers are already in whip's config (or blocked by mcpImport)")
		return nil
	}
	if dryRun {
		body, err := json.MarshalIndent(add, "", "  ")
		if err != nil {
			return err
		}
		fmt.Printf("would add %d server(s) to ~/.whip/config.json under \"mcp\":\n%s\n", len(add), body)
		return nil
	}
	if cfg.MCPServers == nil {
		cfg.MCPServers = map[string]config.MCPServer{}
	}
	names := make([]string, 0, len(add))
	for name, entry := range add {
		cfg.MCPServers[name] = entry
		names = append(names, name)
	}
	if err := cfg.Save(); err != nil {
		return err
	}
	sort.Strings(names)
	fmt.Printf("imported %d mcp server(s) into ~/.whip/config.json: %s\n", len(names), strings.Join(names, ", "))
	return nil
}
