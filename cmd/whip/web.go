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
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/context-labs/whip/internal/buildinfo"
	"github.com/context-labs/whip/internal/gateway"
	"github.com/context-labs/whip/internal/localruntime"
	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/webassets"
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
	paths, err := nativeRuntimePaths()
	if err != nil {
		return err
	}
	return runNativeGateway(ctx, paths, func(endpoint string) error { ready(endpoint); return nil })
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
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"/api/v4/web", nil)
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
	raw, err := io.ReadAll(io.LimitReader(response.Body, 4097))
	if err != nil || len(raw) > 4096 {
		return errors.New("invalid or oversized gateway web discovery")
	}
	if err := protocol.Validate("GatewayDiscovery", raw); err != nil {
		return fmt.Errorf("invalid gateway web discovery: %w", err)
	}
	var info protocol.GatewayDiscovery
	if err := json.Unmarshal(raw, &info); err != nil {
		return err
	}
	if info.Major != protocol.Major || info.WebSocketPath != "/api/v4/ws" || info.ContentPath != "/api/v4/content/" {
		return errors.New("gateway web protocol is incompatible; update the gateway executable")
	}
	if !info.Available {
		return errors.New("this gateway was built without web assets; run `npm ci && task build` and start the gateway again")
	}
	return nil
}

// The foreground listener observes one exact owner. It never creates runtime
// files, changes host policy, or adopts a replacement after connection loss.
func runNativeGateway(ctx context.Context, paths localruntime.Paths, ready func(string) error) error {
	status := localruntime.Inspect(ctx, paths)
	if status.State != "running" || status.Process == nil {
		return fmt.Errorf("native runtime is %s: %s; inspect whipcode daemon status or run whipcode daemon start explicitly", status.State, status.Error)
	}
	if !gatewayAssetsAvailable() {
		return errors.New("this executable was built without web assets; run npm ci && task build, then run whipcode web again")
	}
	list := func(value string) []string {
		var result []string
		for item := range strings.SplitSeq(value, ",") {
			if item = strings.TrimSpace(item); item != "" {
				result = append(result, item)
			}
		}
		return result
	}
	server, err := gateway.Start(ctx, gateway.Options{
		Address:      strings.TrimSpace(os.Getenv(buildinfo.Env("LISTEN"))),
		AllowedHosts: list(os.Getenv(buildinfo.Env("ALLOWED_HOSTS"))), AllowedOrigins: list(os.Getenv(buildinfo.Env("ALLOWED_ORIGINS"))),
		SocketPath: paths.Socket, RuntimeID: status.Process.RuntimeID, ProcessEpoch: status.Process.ProcessEpoch, Assets: webassets.Handler(),
	})
	if err != nil {
		return err
	}
	defer func() { _ = server.Close() }()
	if err := ready(server.Endpoint()); err != nil {
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
