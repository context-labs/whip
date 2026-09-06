package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"syscall"
	"time"

	"golang.org/x/term"

	"github.com/context-labs/whip/internal/config"
)

// authCLI implements `whip auth …`: turn a provider API key into a ready
// provider entry + pre-fetched model catalog, so `/model` just works.
//
//	whip auth openrouter [--env] [<key>]
//
// The key comes from (first hit): the positional arg, OPENROUTER_API_KEY in
// the environment, or a masked prompt. It is validated against the live
// OpenRouter API before anything is written — a bad key never reaches the
// config file.
//
// Storage: by default the key is written as a literal apiKey in
// ~/.whip/config.json (0600). --env instead records apiKeyEnv:
// OPENROUTER_API_KEY. The
// execution host must have the variable exported before its daemon starts.
func authCLI(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: whip auth <provider> [<args>]\n  providers: inference-net (login [flags] | status | logout | key rotate), openrouter [--env] [<key>]")
	}
	switch args[0] {
	case "inference-net", "inference":
		return authInferenceNetCLI(args[1:])
	case "openrouter":
		return authOpenRouterCLI(args[1:])
	default:
		return fmt.Errorf("unknown provider %q (supported: inference-net, openrouter)", args[0])
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
	if err := authOpenRouter(key, *envMode); err != nil {
		fmt.Println("failed")
		return err
	}
	fmt.Println("ok")

	fmt.Println("openrouter provider configured.")
	fmt.Println("  run `whip`, then /model and pick from the full OpenRouter catalog — e.g. /model openai/gpt-5 openrouter")
	return nil
}

// authOpenRouter sends an ephemeral secret request to the execution host.
func authOpenRouter(key string, envMode bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	return setupProviderCLI(ctx, "openrouter", key, envMode)
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
