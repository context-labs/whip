package main

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"slices"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/context-labs/whip/internal/client"
	"github.com/context-labs/whip/internal/protocol"
)

// MCP configuration and discovery belong to the selected native host. Commands
// never read private host configuration or expose connection credentials.
func mcpCLI(args []string, version string) error {
	if len(args) == 0 {
		return errors.New("usage: whipcode mcp <list|add|remove|import|serve|test>")
	}
	switch args[0] {
	case "serve":
		if len(args) != 1 {
			return errors.New("usage: whipcode mcp serve")
		}
		return mcpServe(version)
	case "test":
		if len(args) != 2 {
			return errors.New("usage: whipcode mcp test <name>")
		}
		return mcpTestCLI(args[1])
	case "import":
		return mcpImportCLI(args[1:])
	case "list":
		if len(args) != 1 {
			return errors.New("usage: whipcode mcp list")
		}
		return withMCPOwner(nil, func(ctx context.Context, owner *mcpCLIOwner) error {
			var status protocol.MCPStatusResult
			if err := owner.client.Call(ctx, "mcp.status", protocol.SessionParams{SessionID: owner.session.ID}, &status); err != nil {
				return err
			}
			if len(status.Items) == 0 {
				fmt.Println("no MCP servers configured")
			}
			for _, entry := range status.Items {
				if entry.State == "unreadable" {
					fmt.Fprintf(os.Stderr, "mcp: %s: %s\n", entry.Source, entry.Note)
					continue
				}
				fmt.Printf("%-20s %-12s %s\n", entry.Name, entry.State, entry.Source)
			}
			return nil
		})
	case "add":
		if len(args) < 4 {
			return errors.New("usage: whipcode mcp add <name> -- <cmd...> | whipcode mcp add <name> --url <url>")
		}
		server := protocol.MCPServerInput{Command: []string{}, Env: map[string]string{}, Headers: map[string]string{}}
		switch {
		case len(args) == 4 && args[2] == "--url":
			server.URL = args[3]
		case args[2] == "--":
			server.Command = args[3:]
		default:
			return errors.New("usage: whipcode mcp add <name> -- <cmd...> | whipcode mcp add <name> --url <url>")
		}
		return withMCPClient(func(ctx context.Context, c *client.Client) error {
			var current, updated protocol.MCPConfiguration
			if err := c.Call(ctx, "mcp.configuration", struct{}{}, &current); err != nil {
				return err
			}
			if err := c.Call(ctx, "mcp.configure", protocol.ConfigureMCPParams{Revision: current.Revision, Name: args[1], Server: &server}, &updated); err != nil {
				return err
			}
			fmt.Printf("added mcp server %q — explicitly reload existing sessions to use changes\n", args[1])
			return nil
		})
	case "remove":
		if len(args) != 2 {
			return errors.New("usage: whipcode mcp remove <name>")
		}
		return withMCPOwner(nil, func(ctx context.Context, owner *mcpCLIOwner) error {
			var current, updated protocol.MCPConfiguration
			if err := owner.client.Call(ctx, "mcp.configuration", struct{}{}, &current); err != nil {
				return err
			}
			if !slices.ContainsFunc(current.Servers, func(s protocol.MCPDeclaration) bool { return s.Name == args[1] }) {
				candidates, err := owner.candidates(ctx)
				if err != nil {
					return err
				}
				for _, candidate := range candidates.Candidates {
					if candidate.Name == args[1] {
						if candidate.Gated || candidate.State == "excluded" {
							return fmt.Errorf("%q is blocked by MCP import policy", args[1])
						}
						return fmt.Errorf("%q comes from %s configuration — edit that file to remove it", args[1], candidate.Source)
					}
				}
				return fmt.Errorf("no mcp server named %q", args[1])
			}
			if err := owner.client.Call(ctx, "mcp.configure", protocol.ConfigureMCPParams{Revision: current.Revision, Name: args[1], Remove: true}, &updated); err != nil {
				return err
			}
			fmt.Printf("removed mcp server %q\n", args[1])
			return nil
		})
	default:
		return fmt.Errorf("unknown mcp subcommand %q (list|add|remove|import|serve|test)", args[0])
	}
}

func withMCPClient(fn func(context.Context, *client.Client) error) error {
	signalContext, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(signalContext, 320*time.Second)
	defer cancel()
	c, err := connectNativeRuntime(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()
	return fn(ctx, c)
}

type mcpCLIOwner struct {
	client  *client.Client
	session protocol.Session
}

// The temporary owner gives project discovery an explicit canonical directory.
// It has no model and cannot admit provider turns. Its deletion never targets a
// user session. Creation recovery observes the original identity without resend.
func newMCPOwner(ctx context.Context, c *client.Client, selection *protocol.MCPSelection, modules []protocol.ID) (*mcpCLIOwner, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	var definition protocol.DefinitionRef
	for _, candidate := range c.Builtins() {
		if candidate.ID == "coding" {
			definition = candidate
			break
		}
	}
	if definition.ID == "" {
		return nil, errors.New("native coding definition unavailable")
	}
	id := protocol.ID(rand.Text())
	var created protocol.CreateTreeResult
	err = c.Call(ctx, "trees.create", protocol.CreateTreeParams{CreationID: id, Definition: definition, PermissionMode: new("prompt"), WorkingDirectory: cwd, Overrides: protocol.ConfigPatch{Model: &protocol.ModelSelection{}, Modules: modules, MCPServers: selection, AutomaticTitle: new(false), GoalsEnabled: new(false)}}, &created)
	if err != nil {
		original := err
		if err = c.Call(ctx, "trees.creation", protocol.TreeCreationParams{CreationID: id}, &created); err != nil {
			return nil, fmt.Errorf("MCP owner creation %s outcome unknown: %w", id, errors.Join(original, err))
		}
	}
	if created.Creation.ID != id || created.Root == nil || created.Deleted || created.Root.ID != created.Creation.RootID || created.Root.TreeID != created.Creation.TreeID || created.Root.ParentID != nil {
		return nil, errors.New("MCP owner creation is unavailable")
	}
	return &mcpCLIOwner{client: c, session: *created.Root}, nil
}

func (o *mcpCLIOwner) close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	err := deleteNativeRun(ctx, o.client, o.session.ID)
	if err != nil {
		return fmt.Errorf("MCP temporary session %s cleanup failed: %w", o.session.ID, err)
	}
	return nil
}

func withMCPOwner(selection *protocol.MCPSelection, fn func(context.Context, *mcpCLIOwner) error) error {
	return withMCPClient(func(ctx context.Context, c *client.Client) (err error) {
		owner, err := newMCPOwner(ctx, c, selection, []protocol.ID{})
		if err != nil {
			return err
		}
		defer func() { err = errors.Join(err, owner.close()) }()
		return fn(ctx, owner)
	})
}

func (o *mcpCLIOwner) candidates(ctx context.Context) (protocol.MCPImportCandidatesResult, error) {
	var result protocol.MCPImportCandidatesResult
	err := o.client.Call(ctx, "mcp.import.candidates", protocol.MCPImportCandidatesParams{SessionID: &o.session.ID}, &result)
	return result, err
}

func mcpImportCLI(args []string) error {
	dryRun := len(args) == 1 && args[0] == "--dry-run"
	if len(args) > 0 && !dryRun {
		return errors.New("usage: whipcode mcp import [--dry-run]")
	}
	return withMCPOwner(nil, func(ctx context.Context, owner *mcpCLIOwner) error {
		candidates, err := owner.candidates(ctx)
		if err != nil {
			return err
		}
		sources := make([]string, 0, len(candidates.SourceErrors))
		for source := range candidates.SourceErrors {
			sources = append(sources, source)
		}
		sort.Strings(sources)
		for _, source := range sources {
			fmt.Fprintf(os.Stderr, "mcp: %s: %s (its servers were not imported)\n", source, candidates.SourceErrors[source])
		}
		fingerprints := map[string]string{}
		for _, candidate := range candidates.Candidates {
			if candidate.State == "importable" && !candidate.Gated {
				fingerprints[candidate.Name] = candidate.Fingerprint
			}
		}
		if len(fingerprints) == 0 {
			fmt.Println("nothing to import — servers are already native, disabled, unsupported, or blocked by MCP import policy")
			return nil
		}
		if len(fingerprints) > 64 {
			return errors.New("import exceeds 64 servers; narrow the host import policy first")
		}
		if dryRun {
			fmt.Println("would import these servers as trusted host declarations; credentials and executable paths remain private:")
			for _, candidate := range candidates.Candidates {
				if _, ok := fingerprints[candidate.Name]; ok {
					fmt.Printf("%s (%s) %s\n", candidate.Name, candidate.Source, candidate.Fingerprint)
				}
			}
			return nil
		}
		var result protocol.MCPImportResult
		if err := owner.client.Call(ctx, "mcp.import.apply", protocol.MCPImportParams{SessionID: &owner.session.ID, Revision: candidates.Revision, Fingerprints: fingerprints}, &result); err != nil {
			return err
		}
		fmt.Printf("imported %d mcp server(s): %s\n", len(result.Added), strings.Join(result.Added, ", "))
		fmt.Println("they are now trusted host declarations; ordinary session permission policy still applies")
		return nil
	})
}

func mcpTestCLI(name string) error {
	return withMCPOwner(&protocol.MCPSelection{Servers: []string{name}}, func(ctx context.Context, owner *mcpCLIOwner) error {
		read := func() (protocol.MCPServerStatus, error) {
			var result protocol.MCPStatusResult
			if err := owner.client.Call(ctx, "mcp.status", protocol.SessionParams{SessionID: owner.session.ID}, &result); err != nil {
				return protocol.MCPServerStatus{}, err
			}
			for _, status := range result.Items {
				if status.Name == name {
					return status, nil
				}
			}
			return protocol.MCPServerStatus{}, fmt.Errorf("no mcp server named %q (try: whipcode mcp list)", name)
		}
		status, err := read()
		if err != nil {
			return err
		}
		if status.State == "blocked" || status.State == "disabled" {
			fmt.Printf("%s: %s\n", name, status.State)
			return fmt.Errorf("server %q is %s", name, status.State)
		}
		fmt.Printf("testing mcp server %q (%s)…\n", name, status.Source)
		start := time.Now()
		// This owner selects only the named server. Refresh cannot start any other
		// declared server; it is sent once, then only state is observed on lost ACK.
		var refresh protocol.MCPRefreshResult
		actionErr := owner.client.Call(ctx, "mcp.refresh", protocol.SessionParams{SessionID: owner.session.ID}, &refresh)
		if actionErr != nil {
			if _, ok := errors.AsType[*client.Error](actionErr); ok {
				return actionErr
			}
		}
		tick := time.NewTicker(50 * time.Millisecond)
		defer tick.Stop()
		for {
			status, err = read()
			if err != nil {
				return errors.Join(actionErr, err)
			}
			switch status.State {
			case "ready":
				var tools protocol.MCPToolsResult
				if err := owner.client.Call(ctx, "mcp.tools", protocol.MCPServerParams{SessionID: owner.session.ID, Server: name}, &tools); err != nil {
					return err
				}
				fmt.Printf("✓ connected in %s — %d tools\n", time.Since(start).Round(time.Millisecond), status.Tools)
				names := make([]string, 0, len(tools.Items))
				for _, tool := range tools.Items {
					names = append(names, tool.Name)
				}
				sort.Strings(names)
				if len(names) > 5 {
					names = append(names[:5], "…")
				}
				if len(names) > 0 {
					fmt.Println("  tools:", strings.Join(names, ", "))
				}
				return nil
			case "failed", "disabled", "blocked":
				failure := status.Note
				if status.Failure != nil {
					failure = *status.Failure
				}
				fmt.Printf("✗ failed after %s: %s\n", time.Since(start).Round(time.Millisecond), failure)
				if status.Note != "" {
					fmt.Println("  note:", status.Note)
				}
				fmt.Println("  source:", status.Source)
				return fmt.Errorf("server %q failed (%s)", name, status.State)
			}
			select {
			case <-ctx.Done():
				return errors.Join(ctx.Err(), actionErr)
			case <-tick.C:
			}
		}
	})
}
