package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/context-labs/whip/internal/buildinfo"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/daemon"
)

// authInferenceNetCLI implements `whip auth inference-net …`: first-class
// Inference.net sign-in. The default path is a browser device-authorization
// login that provisions a machine API key automatically — no key handling.
// BYOK is supported via login --key / --env.
//
//	whip auth inference-net login [--key <apikey> | --env]
//	whip auth inference-net status
//	whip auth inference-net logout
//	whip auth inference-net key rotate
func authInferenceNetCLI(args []string) error {
	sub := "login"
	if len(args) > 0 {
		sub = args[0]
		args = args[1:]
	}
	switch sub {
	case "login":
		return inferenceNetLoginCLI(args)
	case "status":
		return inferenceNetStatusCLI()
	case "logout":
		return inferenceNetLogoutCLI()
	case "key":
		if len(args) > 0 && args[0] == "rotate" {
			return inferenceNetKeyRotateCLI()
		}
		return errors.New(buildinfo.Text("usage: whip auth inference-net key rotate"))
	default:
		return fmt.Errorf("unknown inference-net subcommand %q (login | status | logout | key rotate)", sub)
	}
}

func inferenceNetLoginCLI(args []string) error {
	fs := flag.NewFlagSet("auth inference-net login", flag.ContinueOnError)
	key := fs.String("key", "", "bring your own Inference.net API key instead of the browser login")
	envMode := fs.Bool("env", false, "store the BYOK key as apiKeyEnv: "+config.InferenceNetEnvVar+" instead of a literal in config.json")
	if err := fs.Parse(args); err != nil {
		return err
	}
	k := config.TrimKey(*key)
	if k == "" {
		k = config.TrimKey(os.Getenv(config.InferenceNetEnvVar))
	}
	// An explicit --key (even empty) or --env, or an env-provided key, takes the
	// BYOK path — so we never surprise the user with a browser flow they didn't
	// ask for (and a missing key errors instead of polling the device endpoint).
	keyFlagSet := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "key" {
			keyFlagSet = true
		}
	})
	if keyFlagSet || *envMode || k != "" {
		return inferenceNetBYOK(k, *envMode)
	}
	return inferenceNetDeviceLogin()
}

// inferenceNetBYOK validates and saves credentials on the execution host.
func inferenceNetBYOK(key string, envMode bool) error {
	if key == "" && !envMode {
		return errors.New("no API key provided (set " + config.InferenceNetEnvVar + " or pass --key)")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := setupProviderCLI(ctx, config.InferenceNetProvider, key, envMode); err != nil {
		return err
	}
	fmt.Println("inference-net provider configured on the execution host.")
	return nil
}

func inferenceNetDeviceLogin() error {
	return providerDeviceLogin(config.InferenceNetProvider, 10*time.Minute)
}

func providerDeviceLogin(provider string, lifetime time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), lifetime)
	defer cancel()
	client, err := connectProviderDaemon(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()
	status, err := client.BeginProviderLogin(ctx, provider)
	if err != nil {
		return err
	}
	verificationURL := ""
	fmt.Println("Starting provider authorization on the execution host…")
	for {
		if status.VerificationURL != "" && verificationURL != status.VerificationURL {
			verificationURL = status.VerificationURL
			fmt.Printf("Approve in your browser:\n  %s\n  Code: %s\n", verificationURL, status.UserCode)
			openBrowser(verificationURL)
		}
		switch status.State {
		case "choose_team":
			id, chooseErr := chooseProviderID("workspace", status.Teams)
			if chooseErr != nil {
				return chooseErr
			}
			status, err = client.SelectLoginTeam(ctx, status.FlowID, id)
		case "choose_project":
			choices := append(append([]daemon.ProviderChoice{}, status.Projects...), daemon.ProviderChoice{ID: "", Name: "+ Create new project"})
			id, chooseErr := chooseProviderID("project", choices)
			if chooseErr != nil {
				return chooseErr
			}
			if id != "" {
				status, err = client.SelectLoginProject(ctx, status.FlowID, id)
			} else {
				name, chooseErr := cliChooser("name", "new project", nil)
				if chooseErr != nil {
					return chooseErr
				}
				status, err = client.CreateLoginProject(ctx, status.FlowID, name)
			}
		case "succeeded":
			fmt.Printf("✓ Signed in as %s. Provider configured on the execution host.\n", status.Email)
			if status.Error != "" {
				fmt.Fprintln(os.Stderr, status.Error)
			}
			if status.ProjectID != "" {
				fmt.Println("  Project " + status.ProjectID)
			}
			return nil
		case "failed", "interrupted", "expired", "cancelled":
			return fmt.Errorf("provider login %s: %s", status.State, status.Error)
		default:
			timer := time.NewTimer(200 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
			status, err = client.LoginStatus(ctx, status.FlowID)
		}
		if err != nil {
			return err
		}
	}
}

func chooseProviderID(kind string, choices []daemon.ProviderChoice) (string, error) {
	if len(choices) == 0 {
		return "", errors.New("provider returned no choices")
	}
	if len(choices) == 1 && choices[0].ID != "" {
		return choices[0].ID, nil
	}
	labels := make([]string, len(choices))
	for i, choice := range choices {
		labels[i] = fmt.Sprintf("%s [%s]", choice.Name, choice.ID)
	}
	selected, err := cliChooser(kind, kind, labels)
	if err != nil {
		return "", err
	}
	for i, label := range labels {
		if label == selected {
			return choices[i].ID, nil
		}
	}
	return "", errors.New("invalid provider choice")
}

// cliChooser is the interactive picker for team/project selection. For a list
// it prints a numbered menu and reads a number (default 1); for a name prompt
// (no options) it reads free text.
func cliChooser(kind, title string, options []string) (string, error) {
	fmt.Println("\n" + title + ":")
	if len(options) == 0 {
		fmt.Print("  name: ")
		return readLine()
	}
	for i, o := range options {
		fmt.Printf("  %d) %s\n", i+1, o)
	}
	fmt.Printf("  pick [1-%d, default 1]: ", len(options))
	line, err := readLine()
	if err != nil {
		return "", err
	}
	if line == "" {
		return options[0], nil
	}
	n, err := strconv.Atoi(line)
	if err != nil || n < 1 || n > len(options) {
		return "", fmt.Errorf("invalid choice %q", line)
	}
	return options[n-1], nil
}

func readLine() (string, error) {
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && line == "" {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

func inferenceNetStatusCLI() error {
	return providerAccountCLI("status")
}

func inferenceNetLogoutCLI() error {
	return providerAccountCLI("logout")
}

func inferenceNetKeyRotateCLI() error {
	return providerAccountCLI("rotate")
}

func providerAccountCLI(operation string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	client, err := connectProviderDaemon(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()
	var status daemon.ProviderStatus
	switch operation {
	case "status":
		status, err = client.ProviderStatus(ctx, config.InferenceNetProvider)
	case "logout":
		status, err = client.LogoutProvider(ctx, config.InferenceNetProvider)
	case "rotate":
		status, err = client.RotateProviderKey(ctx, config.InferenceNetProvider)
	}
	if err != nil {
		return err
	}
	fmt.Println("Inference.net")
	if status.Email != "" {
		fmt.Println("  Account     " + status.Email)
	} else {
		fmt.Println(buildinfo.Text("  Account     not signed in (whip auth inference-net login)"))
	}
	if status.ProjectID != "" {
		fmt.Println("  Project     " + status.ProjectName + " (" + status.ProjectID + ")")
	}
	if status.MachineKeyName != "" {
		fmt.Println("  Machine key " + status.MachineKeyName)
	}
	fmt.Println("  Provider    " + status.KeySource)
	for _, warning := range status.Warnings {
		fmt.Fprintln(os.Stderr, buildinfo.Text("whip:"), warning)
	}
	return nil
}

// openBrowser opens url in the default browser; false when it can't.
func openBrowser(url string) bool {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.CommandContext(context.Background(), "open", url)
	case "windows":
		cmd = exec.CommandContext(context.Background(), "rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.CommandContext(context.Background(), "xdg-open", url)
	}
	return cmd.Start() == nil
}
