package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"slices"
	"strconv"
	"syscall"
	"time"

	"github.com/context-labs/whip/internal/daemon"
	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/webassets"
	"github.com/context-labs/whip/internal/webgateway"
)

var (
	openWebBrowser         = openBrowser
	gatewayAssetsAvailable = webassets.Available
)

// webCLI owns only the foreground gateway, never the daemon or its work.
func webCLI(args []string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return runWeb(ctx, args)
}

func runWeb(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("whipcode web", flag.ContinueOnError)
	noOpen := flags.Bool("no-open", false, "print the ready URL without opening a browser")
	publishedURL := flags.String("url", "", "check/open an existing HTTP(S) endpoint instead of serving")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("usage: whipcode web [--no-open] [--url https://whip.example]")
	}
	ready := func(endpoint string) {
		fmt.Fprintln(os.Stdout, endpoint+"/")
		if !*noOpen && !openWebBrowser(endpoint+"/") {
			fmt.Fprintln(os.Stderr, "whipcode: could not open the browser; open the URL above")
		}
	}
	if *publishedURL != "" {
		endpoint, err := validateWebEndpoint(*publishedURL)
		if err != nil {
			return err
		}
		if err := checkWebAssets(ctx, endpoint); err != nil {
			return err
		}
		ready(endpoint)
		return nil
	}
	paths, err := daemonStatusPaths()
	if err != nil {
		return err
	}
	return runGateway(ctx, paths, nil, func(record gatewayReady) error { ready(record.Endpoint); return nil })
}

// Both public foreground and private managed modes use this runner. The Open
// factory always narrows socket privileges and fails closed on old daemons.
func runGateway(ctx context.Context, paths daemon.RuntimePaths, expected *gatewayReady, ready func(gatewayReady) error) error {
	options := gatewayEnvironment()
	options.SocketPath = paths.Socket
	options.Open = func(ctx context.Context) (webgateway.Client, error) {
		client, err := dialGatewayClient(ctx, paths)
		if err != nil {
			return nil, err
		}
		if expected != nil {
			init := client.InitializeResult()
			if init.RuntimeID != expected.RuntimeID || init.Generation != expected.Generation {
				_ = client.Close()
				return nil, errors.New("daemon generation changed; start the gateway again")
			}
		}
		return client, nil
	}
	// Check compatibility before assets, so an old/stopped runtime has an
	// actionable error even in an unpackaged development binary.
	checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	client, err := options.Open(checkCtx)
	cancel()
	if err != nil {
		return err
	}
	initializedCheck := client.InitializeResult()
	_ = client.Close()
	if expected == nil {
		expected = &gatewayReady{RuntimeID: initializedCheck.RuntimeID, Generation: initializedCheck.Generation}
	}
	if !gatewayAssetsAvailable() {
		return errors.New("this executable was built without web assets; run `npm ci && task build`, then run `whipcode web` again")
	}
	server, err := webgateway.Start(ctx, options)
	if err != nil {
		return err
	}
	defer func() { _ = server.Close() }()
	initialized := server.InitializeResult()
	if err := ready(gatewayReady{Endpoint: server.Endpoint(), RuntimeID: initialized.RuntimeID, Generation: initialized.Generation}); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return nil
	case <-server.Done():
		if ctx.Err() != nil {
			return nil
		}
		return server.Err()
	}
}

func dialGatewayClient(ctx context.Context, paths daemon.RuntimePaths) (*daemon.Client, error) {
	client, err := daemon.DialClient(ctx, paths, daemon.InitializeParams{
		ProtocolMajor: daemon.ProtocolMajor, BuildID: version,
		ClientID: daemonClientID("web-gateway"), ClientKind: "automation",
		Capabilities: []string{protocol.NetworkClientCapability},
	})
	if err != nil {
		return nil, fmt.Errorf("connect to running daemon: %w; inspect `whipcode daemon status` or run `whipcode daemon start` explicitly", err)
	}
	if !slices.Contains(client.InitializeResult().NegotiatedCapabilities, protocol.NetworkClientCapability) {
		_ = client.Close()
		return nil, errors.New("the running daemon does not support the network-client safety capability; update it and explicitly run `whipcode daemon restart` (interrupts active work)")
	}
	return client, nil
}

func validateWebEndpoint(endpoint string) (string, error) {
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return "", errors.New("web endpoint must be an HTTP or HTTPS origin")
	}
	if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" || parsed.User != nil ||
		(parsed.Path != "" && parsed.Path != "/") || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" {
		return "", errors.New("web endpoint must be an HTTP or HTTPS origin without credentials, path, query, or fragment")
	}
	if port := parsed.Port(); port != "" {
		number, err := strconv.Atoi(port)
		if err != nil || number < 1 || number > 65535 {
			return "", errors.New("web endpoint port must be between 1 and 65535")
		}
	}
	if ip := net.ParseIP(parsed.Hostname()); ip != nil && ip.IsUnspecified() {
		return "", errors.New("web endpoint is a wildcard address; use `whipcode web --url http://HOST:PORT` with an exact WHIPCODE_ALLOWED_HOSTS entry")
	}
	return parsed.Scheme + "://" + parsed.Host, nil
}

func checkWebAssets(ctx context.Context, endpoint string) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"/api/v3/web", nil)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("connect to web endpoint: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		if response.StatusCode == http.StatusForbidden {
			return errors.New("web host is not allowed; configure the gateway's exact WHIPCODE_ALLOWED_HOSTS value")
		}
		return fmt.Errorf("endpoint does not expose the web app (HTTP %d); check the URL and gateway build", response.StatusCode)
	}
	var info struct {
		Available     bool   `json:"available"`
		ProtocolMajor int    `json:"protocol_major"`
		WebSocketPath string `json:"websocket_path"`
		ContentPath   string `json:"content_path"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&info); err != nil {
		return fmt.Errorf("invalid gateway web discovery: %w", err)
	}
	if info.ProtocolMajor != daemon.ProtocolMajor || info.WebSocketPath != "/api/v3/ws" || info.ContentPath != "/api/v3/content/" {
		return errors.New("gateway web protocol is incompatible; update the gateway executable")
	}
	if !info.Available {
		return errors.New("this gateway was built without web assets; run `npm ci && task build` and start the gateway again")
	}
	return nil
}
