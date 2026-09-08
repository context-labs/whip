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
	"time"

	"github.com/context-labs/whip/internal/buildinfo"

	"github.com/context-labs/whip/internal/daemon"
)

var (
	probeWebDaemon = probeDaemon
	openWebBrowser = openBrowser
)

// webCLI attaches to the current daemon; it never changes runtime ownership or
// network configuration. Starting/replacing a daemon is an explicit CLI action.
func webCLI(args []string) error {
	flags := flag.NewFlagSet(buildinfo.Text("whip web"), flag.ContinueOnError)
	noOpen := flags.Bool("no-open", false, "print the URL without opening a browser")
	publishedURL := flags.String("url", "", "explicit web URL when using a trusted-network HTTPS proxy")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 {
		return errors.New(buildinfo.Text("usage: whip web [--no-open] [--url https://whip.example]"))
	}
	paths, err := daemonRuntimePaths()
	if err != nil {
		return err
	}
	status, client := probeWebDaemon(paths, time.Second)
	if client != nil {
		_ = client.Close()
	}
	endpoint, err := webEndpoint(status, *publishedURL)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := checkWebAssets(ctx, endpoint); err != nil {
		return err
	}
	fmt.Fprintln(os.Stdout, endpoint+"/")
	if !*noOpen && !openWebBrowser(endpoint+"/") {
		fmt.Fprintln(os.Stderr, buildinfo.Text("whip: could not open the browser; open the URL above"))
	}
	return nil
}

func webEndpoint(status daemonStatus, publishedURL string) (string, error) {
	if status.State == "stopped" {
		return "", errors.New(buildinfo.Text("daemon is stopped; run `WHIP_NETWORK=1 whip daemon start`, then `whip web`"))
	}
	if status.State != "running" {
		return "", fmt.Errorf(buildinfo.Text("daemon is unavailable: %s; inspect `whip daemon status` and `whip daemon logs`; ")+
			buildinfo.Text("replace it explicitly with `WHIP_NETWORK=1 whip daemon restart`"), status.Error)
	}
	if status.NetworkEndpoint == "" {
		return "", errors.New("the running daemon has networking disabled; enable it explicitly with " +
			buildinfo.Text("`WHIP_NETWORK=1 whip daemon restart`, then `whip web` (restart interrupts active work)"))
	}
	endpoint := status.NetworkEndpoint
	if publishedURL != "" {
		endpoint = publishedURL
	}
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return "", errors.New("web endpoint must be an HTTP or HTTPS origin")
	}
	if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil ||
		(parsed.Path != "" && parsed.Path != "/") || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errors.New("web endpoint must be an HTTP or HTTPS origin without credentials, path, query, or fragment")
	}
	if ip := net.ParseIP(parsed.Hostname()); ip != nil && ip.IsUnspecified() {
		return "", errors.New(buildinfo.Text("the daemon is bound to a wildcard address; use `whip web --url http://HOST:PORT` " +
			"with an exact WHIP_ALLOWED_HOSTS entry, or bind WHIP_LISTEN to the intended host address"))
	}
	return parsed.Scheme + "://" + parsed.Host, nil
}

func checkWebAssets(ctx context.Context, endpoint string) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"/api/v3/web", nil)
	if err != nil {
		return err
	}
	client := &http.Client{
		Timeout:       5 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("connect to web endpoint: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		if response.StatusCode == http.StatusForbidden {
			return errors.New(buildinfo.Text("web host is not allowed; configure the exact WHIP_ALLOWED_HOSTS value and explicitly restart the daemon"))
		}
		return fmt.Errorf(buildinfo.Text("daemon does not expose the web app (HTTP %d); build with `npm ci && task build`, "+
			"then explicitly restart with `WHIP_NETWORK=1 whip daemon restart`"), response.StatusCode)
	}
	var info struct {
		Available     bool   `json:"available"`
		ProtocolMajor int    `json:"protocol_major"`
		WebSocketPath string `json:"websocket_path"`
		ContentPath   string `json:"content_path"`
	}
	dec := json.NewDecoder(io.LimitReader(response.Body, 4096))
	if err := dec.Decode(&info); err != nil {
		return fmt.Errorf("invalid daemon web discovery: %w", err)
	}
	if info.ProtocolMajor != daemon.ProtocolMajor || info.WebSocketPath != "/api/v3/ws" || info.ContentPath != "/api/v3/content/" {
		return errors.New("daemon web protocol is incompatible; update/build " + buildinfo.Name + " and explicitly restart the daemon")
	}
	if !info.Available {
		return errors.New(buildinfo.Text("this daemon was built without web assets; run `npm ci && task build`, " +
			"then explicitly restart with `WHIP_NETWORK=1 ./whip daemon restart`"))
	}
	return nil
}
