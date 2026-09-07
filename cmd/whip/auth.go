package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"syscall"
	"time"

	"golang.org/x/term"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/llm"
)

// authCLI implements `whip auth …`: turn provider credentials into a ready
// provider route, so `/model` works immediately after sign-in.
//
//	whip auth openrouter [--env] [<key>]
//	whip auth codex
//
// The key comes from (first hit): the positional arg, OPENROUTER_API_KEY in
// the environment, or a masked prompt. It is validated against the live
// OpenRouter API before anything is written — a bad key never reaches the
// config file.
//
// Storage: by default the key is written as a literal apiKey in
// ~/.whip/config.json (0600). --env instead records apiKeyEnv:
// OPENROUTER_API_KEY and the key must be exported in the shell; with an
// interactive terminal we offer to append the export to the shell rc file.
func authCLI(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: whip auth <provider> [<args>]\n  providers: inference-net (login [flags] | status | logout | key rotate), openrouter [--env] [<key>], codex")
	}
	switch args[0] {
	case "inference-net", "inference":
		return authInferenceNetCLI(args[1:])
	case "openrouter":
		return authOpenRouterCLI(args[1:])
	case "codex":
		return authCodexCLI(args[1:])
	default:
		return fmt.Errorf("unknown provider %q (supported: inference-net, openrouter, codex)", args[0])
	}
}

func authOpenRouterCLI(args []string) error {
	fs := flag.NewFlagSet("auth openrouter", flag.ContinueOnError)
	envMode := fs.Bool("env", false, "store the key as apiKeyEnv: "+config.OpenRouterEnvVar+" instead of a literal in config.json")
	if err := fs.Parse(args); err != nil {
		return err
	}

	key := config.TrimKey(fs.Arg(0))
	if key == "" {
		key = config.TrimKey(os.Getenv(config.OpenRouterEnvVar))
	}
	if key == "" {
		var err error
		key, err = promptKey("OpenRouter API key (sk-or-…): ")
		if err != nil {
			return err
		}
	}
	if key == "" {
		return errors.New("no API key provided (get one at https://openrouter.ai/keys)")
	}

	fmt.Print("validating key against OpenRouter… ")
	if err := authOpenRouter(config.OpenRouterBaseURL, key, *envMode); err != nil {
		fmt.Println("failed")
		return err
	}
	fmt.Println("ok")

	if *envMode && os.Getenv(config.OpenRouterEnvVar) == "" {
		offerShellExport(key)
	}
	fmt.Println("openrouter provider configured.")
	fmt.Println("  run `whip`, then /model and pick from the full OpenRouter catalog — e.g. /model openai/gpt-5 openrouter")
	return nil
}

// authOpenRouter is the testable core: validate the key against baseURL's
// live /models, persist the provider entry, pre-fetch the catalog. The key
// is validated before anything is written — a bad key never reaches disk.
func authOpenRouter(baseURL, key string, envMode bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	infos, err := llm.New(baseURL, key).Models(ctx)
	cancel()
	if err != nil {
		return fmt.Errorf("OpenRouter rejected the key (config untouched): %w", err)
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	cfg.UpsertOpenRouter(key, envMode)
	if err := cfg.Save(); err != nil {
		return err
	}

	// Pre-fetch the catalog so the very next /model picker lists everything
	// (otherwise it waits for the TUI's 24h-TTL background refresh).
	if err := saveCatalog("openrouter", baseURL, infos); err != nil {
		fmt.Fprintln(os.Stderr, "whip: catalog prefetch failed (the TUI will retry on its TTL):", err)
	}
	return nil
}

// saveCatalog records a freshly fetched provider catalog. Model capability
// data stays out of config routes so an account's live catalog can evolve
// without rewriting users' overrides.
func saveCatalog(provider, baseURL string, infos []llm.ModelInfo) error {
	cats := config.LoadCatalogs()
	lites := make([]config.ModelInfoLite, len(infos))
	for i, mi := range infos {
		lites[i] = config.ModelInfoLite{
			ID:                  mi.ID,
			ContextLength:       mi.ContextLength,
			MaxCompletionTokens: mi.MaxCompletionTokens,
			ReasoningEfforts:    mi.ReasoningEfforts,
			InputModalities:     mi.InputModalities,
		}
		if mi.Pricing != nil {
			lites[i].InPrice, lites[i].OutPrice, lites[i].CacheReadPrice = mi.Pricing.Rates()
		}
	}
	cats[provider] = config.Catalog{FetchedAt: time.Now(), BaseURL: baseURL, Models: lites}
	return config.SaveCatalogs(cats)
}

// promptKey reads a key with echo disabled when stdin is a terminal,
// falling back to a plain line read (piped input, tests).
func promptKey(prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	if term.IsTerminal(syscall.Stdin) {
		b, err := term.ReadPassword(syscall.Stdin)
		fmt.Fprintln(os.Stderr)
		return config.TrimKey(string(b)), err
	}
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	return config.TrimKey(line), err
}

// offerShellExport appends the env export to the user's shell rc when they
// confirm. Best-effort; refusal or failure just prints the manual step.
func offerShellExport(key string) {
	rc := shellRC()
	fmt.Printf("export %s so whip can read the key on every run.\n", config.OpenRouterEnvVar)
	if rc == "" || !term.IsTerminal(syscall.Stdin) {
		fmt.Printf("  add to your shell profile:  export %s=%s\n", config.OpenRouterEnvVar, key)
		return
	}
	fmt.Printf("append to %s? [y/N] ", rc)
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(line)), "y") {
		fmt.Printf("  skipped — add it yourself:  export %s=%s\n", config.OpenRouterEnvVar, key)
		return
	}
	f, err := os.OpenFile(rc, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o600) //nolint:gosec // G304: rc is the user's own shell profile, detected by shellRC
	if err != nil {
		fmt.Fprintf(os.Stderr, "whip: couldn't open %s (%v) — add manually: export %s=%s\n", rc, err, config.OpenRouterEnvVar, key)
		return
	}
	defer func() { _ = f.Close() }()
	fmt.Fprintf(f, "\n# OpenRouter (whip auth openrouter)\nexport %s=%s\n", config.OpenRouterEnvVar, key)
	fmt.Println("  appended. Open a new shell (or `source " + rc + "`) before running whip.")
}

// shellRC picks the rc file to append to from $SHELL. "" when unknown.
func shellRC() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	switch {
	case strings.HasSuffix(os.Getenv("SHELL"), "zsh"):
		return home + "/.zshrc"
	case strings.HasSuffix(os.Getenv("SHELL"), "bash"):
		return home + "/.bashrc"
	default:
		return ""
	}
}
