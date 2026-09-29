package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/context-labs/whip/internal/protocol"
)

const nativeBrowserUsage = `usage: whipcode browser <install|status|configure|list|reconnect|disconnect>
  install [--directory PATH]            explicitly write unpacked extension assets
  status [--json]                       passive host configuration and revision
  configure --revision HASH --mode MODE [--executable PATH | --live-endpoint URL | --live-profile PATH] [--allow-private-urls]
  list [--json] SESSION                 passive root-owned connections; children can inspect
  reconnect ROOT NAME GENERATION        prepare a fresh generation; requires an active root
  disconnect ROOT NAME GENERATION       retire the exact displayed generation
Modes: disabled, live, dedicated, headless, extension. configure replaces the complete declaration.
Use an absolute host executable for dedicated/headless; live needs one literal-loopback endpoint or absolute host profile.
Omitted source fields are cleared; private URLs default to denied. Configuration creates no agent grant.
After an unconfirmed edit, inspect status/list before another explicit action; commands never replay automatically`

func nativeBrowserCLI(args []string, out io.Writer) error {
	if len(args) == 0 {
		return errors.New(nativeBrowserUsage)
	}
	flags := flag.NewFlagSet("browser "+args[0], flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var jsonOutput bool
	var configure protocol.ConfigureExternalBrowserParams
	switch args[0] {
	case "status", "list":
		flags.BoolVar(&jsonOutput, "json", false, "write native metadata as JSON")
	case "configure":
		flags.StringVar(&configure.ExpectedRevision, "revision", "", "exact revision from browser status")
		flags.StringVar(&configure.Configuration.Mode, "mode", "", "disabled, live, dedicated, headless, extension")
		flags.StringVar(&configure.Configuration.Executable, "executable", "", "absolute executable on the host")
		flags.StringVar(&configure.Configuration.LiveEndpoint, "live-endpoint", "", "literal loopback HTTP or WebSocket endpoint")
		flags.StringVar(&configure.Configuration.LiveProfile, "live-profile", "", "absolute existing profile on the host")
		flags.BoolVar(&configure.Configuration.AllowPrivateURLs, "allow-private-urls", false, "allow private destination URLs")
	case "reconnect", "disconnect":
	default:
		return errors.New(nativeBrowserUsage)
	}
	if err := flags.Parse(args[1:]); err != nil {
		return fmt.Errorf("%w\n%s", err, nativeBrowserUsage)
	}
	count := map[string]int{"status": 0, "configure": 0, "list": 1, "reconnect": 3, "disconnect": 3}[args[0]]
	if flags.NArg() != count || args[0] == "configure" && (configure.ExpectedRevision == "" || configure.Configuration.Mode == "") {
		return errors.New(nativeBrowserUsage)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	c, err := connectNativeRuntime(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()
	switch args[0] {
	case "status", "configure":
		var result protocol.ExternalBrowserStatus
		method, params := "host.external_browser", any(protocol.EmptyParams{})
		if args[0] == "configure" {
			method, params = "host.set_external_browser", configure
		}
		if err := c.Call(ctx, method, params, &result); err != nil {
			return fmt.Errorf("external Chrome configuration not confirmed; browser status reads current evidence before another explicit edit: %w", err)
		}
		if args[0] == "configure" && result.Configuration != configure.Configuration {
			return errors.New("external Chrome configuration acknowledgement mismatch; inspect browser status")
		}
		if jsonOutput {
			return json.NewEncoder(out).Encode(result)
		}
		fmt.Fprintf(out, "External Chrome: %s\nRevision: %s\nDriver: %s (pinned: %t)\nExecutable: %q\nLive endpoint: %q\nLive profile: %q\nPrivate URLs: %t\n", result.Configuration.Mode, result.Revision, result.Driver, result.DriverPinned, result.Configuration.Executable, result.Configuration.LiveEndpoint, result.Configuration.LiveProfile, result.Configuration.AllowPrivateURLs)
		fmt.Fprintln(out, "Passive declarations only. Configuration does not launch Chrome, publish a relay, or grant agent access.")
	case "list":
		var result protocol.ExternalBrowserSessions
		if err := c.Call(ctx, "browser.external_sessions", protocol.SessionParams{SessionID: protocol.ID(flags.Arg(0))}, &result); err != nil {
			return err
		}
		if jsonOutput {
			return json.NewEncoder(out).Encode(result)
		}
		fmt.Fprintln(out, "External Chrome connections (metadata only; children are read-only):")
		for _, row := range result.Items {
			printExternalBrowserConnection(out, row)
		}
		if len(result.Items) == 0 {
			fmt.Fprintln(out, "No prepared connections. An agent operation prepares the first named connection; browser access requires separate permission.")
		}
	case "reconnect", "disconnect":
		params := protocol.ExternalBrowserConnectionParams{RootID: protocol.ID(flags.Arg(0)), Name: flags.Arg(1), Generation: protocol.ID(flags.Arg(2))}
		var result protocol.ExternalBrowserSession
		if err := c.Call(ctx, "browser."+args[0]+"_external", params, &result); err != nil {
			return fmt.Errorf("external Chrome action not confirmed; browser list reads current generations before another explicit action: %w", err)
		}
		if result.RootID != params.RootID || result.Name != params.Name {
			return errors.New("external Chrome action response ownership mismatch; inspect browser list")
		}
		printExternalBrowserConnection(out, result)
		fmt.Fprintln(out, "No browser was opened or operation replayed. Agent access remains subject to permission checks for this generation.")
	}
	return nil
}

func printExternalBrowserConnection(out io.Writer, row protocol.ExternalBrowserSession) {
	fmt.Fprintf(out, "%s  root=%s  %s/%s  %s  generation=%s\n", row.Name, row.RootID, row.Mode, row.Driver, row.State, row.Generation)
}
