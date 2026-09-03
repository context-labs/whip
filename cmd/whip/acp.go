// `whip acp` is an editor-facing protocol adapter. It owns the ACP stdio
// connection and reconnecting daemon clients, never agent execution or
// persistence.
package main

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	acpsdk "github.com/coder/acp-go-sdk"

	"github.com/context-labs/whip/internal/acp"
	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/daemon"
	"github.com/context-labs/whip/internal/mcp"
	"github.com/context-labs/whip/internal/session"
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
	provider, model, apiID, err := tui.ResolveWithRefresh(cfg, *modelFlag, *providerFlag)
	if err != nil {
		return err
	}
	modelName, providerName := *modelFlag, *providerFlag
	if modelName == "" {
		modelName = cfg.DefaultModel
	}
	if providerName == "" {
		providerName = cfg.DefaultProvider
		if providerName == "" && len(model.Providers) > 0 {
			providerName = model.Providers[0]
		}
	}
	key, err := provider.ResolveKey()
	if err != nil {
		return err
	}
	if key == "" {
		return fmt.Errorf("no API key for provider %q (set apiKey/apiKeyEnv in ~/.whip/config.json)", providerName)
	}
	credentials, err := loadACPClientCredentials()
	if err != nil {
		return fmt.Errorf("ACP identity: %w", err)
	}
	backend := &acpDaemonBackend{
		clientID: credentials.ClientID, privateKey: credentials.PrivateKey,
		model: modelName, provider: providerName,
	}
	backend.refreshPaired(context.Background())

	vision := acpSupportsVision(cfg, modelName, apiID, providerName)
	acp.SetEventLog(func(format string, args ...any) { config.LogEvent("acp", fmt.Sprintf(format, args...)) })
	bridge := acp.NewBridge(version, backend, vision, acpBaseMCP(cfg))
	connection := acpsdk.NewAgentSideConnection(bridge, os.Stdout, os.Stdin)
	bridge.SetAgentConnection(connection)

	signalContext, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	select {
	case <-connection.Done():
	case <-signalContext.Done():
	}
	bridge.CloseAll()
	return nil
}

var loadACPClientCredentials = func() (daemon.ClientCredentials, error) {
	return daemon.LoadOrCreateClientCredentials(daemon.SystemKeyStore(), "acp")
}

type acpDaemonBackend struct {
	clientID   string
	privateKey ed25519.PrivateKey
	model      string
	provider   string
	paired     atomic.Bool
}

func (b *acpDaemonBackend) NewRoot(ctx context.Context, cwd string, servers map[string]mcp.ServerConfig) (*daemon.RootClient, error) {
	return b.root(ctx, "", cwd, servers)
}

func (b *acpDaemonBackend) LoadRoot(ctx context.Context, rootID, _ string, servers map[string]mcp.ServerConfig) (*daemon.RootClient, error) {
	return b.root(ctx, rootID, "", servers)
}

func (b *acpDaemonBackend) root(ctx context.Context, rootID, cwd string, servers map[string]mcp.ServerConfig) (*daemon.RootClient, error) {
	options := daemon.RootClientOptions{
		ClientID: b.clientID, PrivateKey: b.privateKey, RootID: rootID,
		Connector: daemonConnector("acp", b.clientID),
	}
	if rootID == "" {
		options.Create = &daemon.CreateSession{CWD: cwd, Model: b.model, Provider: b.provider}
	}
	client, err := daemon.NewRootClient(options)
	if err != nil {
		return nil, err
	}
	client.Start()
	if err := client.WaitLive(ctx); err != nil {
		_ = client.Close()
		return nil, err
	}
	if len(servers) > 0 {
		action, err := client.NewAction("mcp.attach", map[string]any{"servers": servers})
		if err != nil {
			_ = client.Close()
			return nil, err
		}
		result, err := client.Command(ctx, action)
		if err != nil {
			_ = client.Close()
			return nil, err
		}
		if result.Status != "succeeded" {
			_ = client.Close()
			return nil, errors.New(result.Error)
		}
	}
	return client, nil
}

func (b *acpDaemonBackend) ListSessions(ctx context.Context, limit int) ([]session.Meta, error) {
	connection, err := connectDaemon(ctx, "acp", b.clientID, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = connection.Close() }()
	payload, err := json.Marshal(map[string]int{"limit": limit})
	if err != nil {
		return nil, err
	}
	result, err := connection.Command(ctx, daemon.CommandParams{
		CommandID: daemonCommandID(b.clientID, "list"), Scope: string(session.CommandScopeDaemon),
		Operation: "session.list", Payload: payload,
	})
	if err != nil {
		return nil, err
	}
	if result.Status != "succeeded" {
		return nil, errors.New(result.Error)
	}
	var metas []session.Meta
	if err := json.Unmarshal([]byte(result.Output), &metas); err != nil {
		return nil, err
	}
	return metas, nil
}

func (b *acpDaemonBackend) Paired(ctx context.Context) bool {
	b.refreshPaired(ctx)
	return b.paired.Load()
}

func (b *acpDaemonBackend) refreshPaired(ctx context.Context) {
	query, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	connection, err := connectDaemon(query, "acp", b.clientID, nil)
	if err != nil {
		return
	}
	defer func() { _ = connection.Close() }()
	identity, ok := connection.(interface {
		IdentityStatus(context.Context) (daemon.IdentityStatusResult, error)
	})
	if !ok {
		return
	}
	status, err := identity.IdentityStatus(query)
	if err == nil {
		b.paired.Store(status.Paired)
	}
}

func acpSupportsVision(cfg *config.Config, modelName, modelID, providerName string) bool {
	if catalog, ok := config.LoadCatalogs()[providerName]; ok {
		if vision, found := catalog.SupportsVision(modelID); found {
			return vision
		}
	}
	if model, ok := cfg.Models[modelName]; ok {
		return model.Vision
	}
	return false
}

func acpBaseMCP(cfg *config.Config) map[string]mcp.ServerConfig {
	discovery := mcp.LoadMergedFiltered(cwd(), mcp.FromConfigMap(cfg.MCPServers), mcp.ImportPolicyFrom(cfg.MCPImport))
	for source, err := range discovery.Errs {
		config.LogEvent("acp", fmt.Sprintf("mcp discovery: %s: %s", source, err))
	}
	return discovery.Merged
}
