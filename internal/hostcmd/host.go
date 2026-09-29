// Package hostcmd owns the native runtime command lifecycle shared by both binaries.
package hostcmd

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/context-labs/whip/internal/account"
	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/gateway"
	"github.com/context-labs/whip/internal/inferenceaccount"
	"github.com/context-labs/whip/internal/inferenceauth"
	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/openaiauth"
	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/providerhost"
	"github.com/context-labs/whip/internal/rpc"
	"github.com/context-labs/whip/internal/runner"
	"github.com/context-labs/whip/internal/runtime"
	"github.com/context-labs/whip/internal/terminal"
)

// Run starts the native backend in an explicitly selected private directory and
// joins every owned service before returning. It never selects legacy storage.
func Run(parent context.Context, args []string, out, diagnostics io.Writer) (err error) {
	flags := flag.NewFlagSet("whip-runtime", flag.ContinueOnError)
	flags.SetOutput(diagnostics)
	directory := flags.String("directory", "", "required private runtime directory (fresh v4 storage)")
	scripted := flags.Bool("scripted", false, "use the deterministic fixture provider")
	delay := flags.Duration("scripted-delay", 0, "fixture response delay")
	web := flags.Bool("web", false, "enable the browser gateway (trusted network or authenticated proxy required)")
	webAddress := flags.String("web-listen", "", "browser gateway address (default loopback)")
	webHosts := flags.String("web-hosts", "", "comma-separated exact allowed HTTP Host authorities")
	webOrigins := flags.String("web-origins", "", "comma-separated exact allowed browser origins")
	webTerminals := flags.Bool("web-terminals", false, "allow human terminal methods from network clients")
	workers := flags.Int("workers", 4, "maximum concurrent session turns")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 || *directory == "" {
		return errors.New("an explicit -directory is required; positional arguments are unsupported")
	}
	if !*web && (*webAddress != "" || *webHosts != "" || *webOrigins != "" || *webTerminals) {
		return errors.New("web options require -web")
	}
	if !*scripted && *delay != 0 {
		return errors.New("-scripted-delay requires -scripted")
	}
	if *delay < 0 || *delay > time.Hour {
		return errors.New("scripted delay must be between zero and one hour")
	}
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	*directory, err = filepath.Abs(*directory)
	if err != nil {
		return err
	}
	auth := openaiauth.New(ctx, *directory)
	defer auth.Close() // Registered before runtime.Close: owned refreshes join last.
	configured := configuredProvider(*directory, auth, nil)
	var provider runner.Provider = &configured
	if *scripted {
		provider = model.Scripted{Delay: *delay}
	}
	r, err := runtime.Open(ctx, *directory, provider, runtime.Options{Workers: *workers})
	if err != nil {
		return err
	}
	var inference *inferenceauth.Manager
	defer func() {
		err = errors.Join(err, r.Close())
		if inference != nil {
			err = errors.Join(err, inference.Close())
		}
	}()
	// Runtime.Open has acquired exclusive ownership and created the directory;
	// no provider runs before Start. The command owns this sole credential manager.
	inference, err = inferenceauth.New(ctx, *directory)
	if err != nil {
		return err
	}
	configured.InferenceAuth = inference
	authority := r.HostConfiguration()
	accounts, err := account.New(ctx, auth, authority)
	if err != nil {
		return err
	}
	defer accounts.Close() // Join device requests before closing the borrowed manager.
	inferenceAccounts, err := inferenceaccount.New(ctx, inference, nil, func(ctx context.Context) error {
		current, err := authority.Snapshot(ctx)
		if err != nil {
			return err
		}
		_, err = authority.Update(ctx, current.Revision, (*config.Host).EnsureInference)
		return err
	})
	if err != nil {
		return err
	}
	defer inferenceAccounts.Close()
	providers, err := providerhost.New(ctx, authority, nil, os.LookupEnv, auth, inference)
	if err != nil {
		return err
	}
	defer providers.Close()
	terminals := terminal.NewManager(ctx)
	defer terminals.Shutdown()
	server, err := rpc.Listen(r, rpc.HostServices{Terminals: terminals, NetworkTerminals: *webTerminals, OpenAI: accounts, Inference: inferenceAccounts, Config: authority, ProviderHost: providers})
	if err != nil {
		return err
	}
	defer func() { _ = server.Close() }()
	if err := r.Start(ctx); err != nil {
		return err
	}
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		select {
		case <-r.Done():
			cancel()
		case <-ctx.Done():
		}
	}()
	defer func() { cancel(); <-stopped }()
	served := make(chan error, 1)
	go func() { served <- server.Serve(ctx); cancel() }()
	defer func() { cancel(); err = errors.Join(err, <-served, r.Err()) }()
	ready := map[string]any{"socket": r.SocketPath(), "runtime_id": r.Identity(), "major": protocol.Major, "process_epoch": r.ProcessEpoch()}
	if *web {
		list := func(value string) []string {
			if value == "" {
				return nil
			}
			return strings.Split(value, ",")
		}
		browser, startErr := gateway.Start(ctx, gateway.Options{Address: *webAddress, AllowedHosts: list(*webHosts), AllowedOrigins: list(*webOrigins), SocketPath: r.SocketPath(), RuntimeID: protocol.ID(r.Identity()), ProcessEpoch: protocol.ID(r.ProcessEpoch()), BackendDone: r.Done()})
		if startErr != nil {
			return startErr
		}
		defer func() { _ = browser.Close() }()
		ready["web"] = browser.Endpoint()
		if err := json.NewEncoder(out).Encode(ready); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return nil
		case <-browser.Done():
			return browser.Err()
		}
	}
	if err := json.NewEncoder(out).Encode(ready); err != nil {
		return err
	}
	<-ctx.Done()
	return nil
}
