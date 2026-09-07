package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"

	"github.com/context-labs/whip/internal/codexauth"
	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/llm"
)

// authCodexCLI implements `whip auth codex`.
func authCodexCLI(args []string) error {
	if len(args) != 0 {
		return errors.New("usage: whip auth codex")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	return authCodexAt(ctx, &codexauth.Source{}, os.Stdout, config.CodexBaseURL)
}

// authCodexAt has an injectable backend for the authenticated catalog test.
// Production always uses the fixed ChatGPT Codex endpoint.
func authCodexAt(ctx context.Context, source *codexauth.Source, out io.Writer, baseURL string) error {
	err := source.DeviceLogin(ctx, func(code codexauth.DeviceCode) {
		fmt.Fprint(out, deviceLoginPrompt(code))
	})
	if errors.Is(err, context.Canceled) {
		return errors.New("codex login cancelled")
	}
	if err != nil {
		return err
	}
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("configure Codex provider: %w", err)
	}
	cfg.UpsertCodex()
	if err := cfg.Save(); err != nil {
		return fmt.Errorf("configure Codex provider: %w", err)
	}

	client := llm.NewCodex(baseURL, source)
	if source.HTTP != nil { // lets device-login tests use their fake backend
		client.HTTP = source.HTTP
	}
	infos, catalogErr := client.Models(ctx)
	if catalogErr != nil {
		fmt.Fprintln(out, "Codex login saved to ~/.codex/auth.json. gpt-5.4 @ codex is ready in /model.")
		fmt.Fprintln(out, "Codex model catalog could not be fetched yet; run /model refresh after starting Whip:", catalogErr)
		return nil
	}
	if err := saveCatalog(config.CodexProviderName, baseURL, infos); err != nil {
		fmt.Fprintln(out, "Codex login saved to ~/.codex/auth.json. gpt-5.4 @ codex is ready in /model.")
		fmt.Fprintln(out, "Codex model catalog could not be cached; /model refresh will retry:", err)
		return nil
	}
	fmt.Fprintf(out, "Codex login saved to ~/.codex/auth.json. %d account models are ready in /model.\n", len(infos))
	return nil
}

func deviceLoginPrompt(code codexauth.DeviceCode) string {
	return fmt.Sprintf(`
Open this URL in any browser and sign in to ChatGPT:
  %s

Enter this one-time code (expires in 15 minutes):
  %s

Continue only if you started this login in Whip. Press ctrl+c to cancel.

`, code.VerificationURL, code.UserCode)
}
