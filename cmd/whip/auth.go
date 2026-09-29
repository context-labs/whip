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
)

// authCLI implements `whipcode auth …`: turn a provider API key into a ready
// provider entry + pre-fetched model catalog, so `/model` just works.
//
//	whipcode auth openrouter [--env] [<key>]
//
// The key comes from (first hit): the positional arg, OPENROUTER_API_KEY in
// the environment, or a masked prompt. The host discovers compatible models
// and rejects an observed authentication error before writing. OpenRouter's
// public model catalog alone cannot verify a key; the host also checks its key
// endpoint. Inference is tested on the user's first send.
//
// Storage is owned by the native host. Pasted keys use a private credential
// file; --env retains an explicit host environment reference without copying it.
func authCLI(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: whipcode auth <provider> [<args>]\n  providers: openai-codex (login | status | logout), inference-net (login [flags] | status | logout | key rotate), openrouter [--env] [<key>]")
	}
	switch args[0] {
	case "inference-net", "inference":
		return authInferenceNetCLI(args[1:])
	case "openrouter":
		return authOpenRouterCLI(args[1:])
	case "openai-codex":
		return authOpenAICLI(args[1:])
	default:
		return fmt.Errorf("unknown provider %q (supported: inference-net, openrouter, openai-codex)", args[0])
	}
}

func authOpenRouterCLI(args []string) error {
	fs := flag.NewFlagSet("auth openrouter", flag.ContinueOnError)
	envMode := fs.Bool("env", false, "use the execution host environment variable "+openRouterEnvironment+" instead of publishing a private key file")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if fs.NArg() > 1 || *envMode && fs.NArg() != 0 {
		return errors.New("usage: whipcode auth openrouter [--env | <key>]")
	}
	key := strings.TrimSpace(fs.Arg(0))
	if key == "" {
		key = strings.TrimSpace(os.Getenv(openRouterEnvironment))
	}
	if key == "" && !*envMode {
		var err error
		key, err = promptKey("OpenRouter API key (sk-or-…): ")
		if err != nil {
			return err
		}
	}
	if key == "" && !*envMode {
		return errors.New("no API key provided (get one at https://openrouter.ai/keys)")
	}

	fmt.Print("configuring OpenRouter… ")
	if err := authOpenRouter(key, *envMode); err != nil {
		fmt.Println("needs attention")
		return err
	}
	fmt.Println("ok")

	fmt.Println("openrouter provider configured.")
	fmt.Println("  credential discovery succeeded; model inference has not been tested.")
	fmt.Println("  run `whipcode`, then /model to choose a supported chat model and send a prompt.")
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
		return strings.TrimSpace(string(b)), err
	}
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	return strings.TrimSpace(line), err
}
