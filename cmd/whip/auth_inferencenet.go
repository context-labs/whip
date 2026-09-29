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

	"github.com/context-labs/whip/internal/client"
	"github.com/context-labs/whip/internal/protocol"
)

// authInferenceNetCLI implements `whipcode auth inference-net …`: first-class
// Inference.net sign-in. The default path is a browser device-authorization
// login that provisions a machine API key automatically — no key handling.
// BYOK is supported via login --key / --env.
//
//	whipcode auth inference-net login [--key <apikey> | --env]
//	whipcode auth inference-net status
//	whipcode auth inference-net logout
//	whipcode auth inference-net key rotate
func authInferenceNetCLI(args []string) error {
	sub := "login"
	if len(args) > 0 {
		sub = args[0]
		args = args[1:]
	}
	switch sub {
	case "login":
		return inferenceNetLoginCLI(args)
	case "flow":
		return inferenceFlowCLI(args)
	case "flows", "cleanup":
		return inferenceReadCLI(sub, args)
	case "setup":
		if len(args) != 0 {
			return errors.New("usage: whipcode auth inference-net setup")
		}
		return providerAccountCLI("setup")
	case "status":
		if len(args) != 0 {
			return errors.New("usage: whipcode auth inference-net status")
		}
		return inferenceNetStatusCLI()
	case "logout":
		if len(args) != 0 {
			return errors.New("usage: whipcode auth inference-net logout")
		}
		return inferenceNetLogoutCLI()
	case "key":
		if len(args) == 1 && args[0] == "rotate" {
			return inferenceNetKeyRotateCLI()
		}
		return errors.New("usage: whipcode auth inference-net key rotate")
	default:
		return fmt.Errorf("unknown inference-net subcommand %q (login | status | logout | key rotate)", sub)
	}
}

func inferenceNetLoginCLI(args []string) error {
	fs := flag.NewFlagSet("auth inference-net login", flag.ContinueOnError)
	key := fs.String("key", "", "bring your own Inference.net API key instead of the browser login")
	envMode := fs.Bool("env", false, "use the execution host environment variable "+inferenceEnvironment+" instead of publishing a private key file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("usage: whipcode auth inference-net login [--key <key> | --env]")
	}
	k := strings.TrimSpace(*key)
	if k == "" {
		k = strings.TrimSpace(os.Getenv(inferenceEnvironment))
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
	if keyFlagSet && *envMode {
		return errors.New("--key and --env are mutually exclusive")
	}
	if keyFlagSet || *envMode || k != "" {
		return inferenceNetBYOK(k, *envMode)
	}
	return inferenceNetDeviceLogin()
}

// inferenceNetBYOK validates and saves credentials on the execution host.
func inferenceNetBYOK(key string, envMode bool) error {
	if key == "" && !envMode {
		return errors.New("no API key provided (set " + inferenceEnvironment + " or pass --key)")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := setupProviderCLI(ctx, inferenceProvider, key, envMode); err != nil {
		return err
	}
	fmt.Println("inference-net provider configured on the execution host.")
	return nil
}

func inferenceNetDeviceLogin() error {
	return providerDeviceLogin(inferenceProvider, 10*time.Minute)
}

func providerDeviceLogin(provider string, lifetime time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), lifetime)
	defer cancel()
	c, err := connectNativeRuntime(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()
	if provider == "openai-codex" {
		var flow protocol.OpenAILoginFlow
		if err := c.Call(ctx, "accounts.openai.begin", protocol.EmptyParams{}, &flow); err != nil {
			return err
		}
		return waitOpenAIFlow(ctx, c, flow)
	}
	if provider != inferenceProvider {
		return errors.New("unsupported account provider")
	}
	var flow protocol.InferenceFlow
	if err := c.Call(ctx, "accounts.inference.begin", protocol.EmptyParams{}, &flow); err != nil {
		return err
	}
	return waitInferenceFlow(ctx, c, flow)
}

func waitInferenceFlow(ctx context.Context, c *client.Client, flow protocol.InferenceFlow) error {
	fmt.Println("Authorization flow:", flow.ID)
	previous := ""
	for {
		showApproval(flow.VerificationURL, flow.UserCode, &previous)
		var err error
		switch flow.State {
		case "choose_team":
			choices := make([]providerChoice, len(flow.Teams))
			for i, team := range flow.Teams {
				choices[i] = providerChoice{ID: team.ID, Name: team.Name}
			}
			id, chooseErr := chooseProviderID("workspace", choices)
			if chooseErr != nil {
				return chooseErr
			}
			err = c.Call(ctx, "accounts.inference.team", protocol.InferenceTeamParams{FlowID: flow.ID, TeamID: id}, &flow)
		case "choose_project":
			choices := make([]providerChoice, 0, len(flow.Projects)+1)
			for _, project := range flow.Projects {
				choices = append(choices, providerChoice{ID: project.ID, Name: project.Name})
			}
			choices = append(choices, providerChoice{Name: "+ Create new project"})
			id, chooseErr := chooseProviderID("project", choices)
			if chooseErr != nil {
				return chooseErr
			}
			if id != "" {
				err = c.Call(ctx, "accounts.inference.project", protocol.InferenceProjectParams{FlowID: flow.ID, ProjectID: id}, &flow)
			} else {
				name, chooseErr := cliChooser("name", "new project", nil)
				if chooseErr != nil {
					return chooseErr
				}
				err = c.Call(ctx, "accounts.inference.create_project", protocol.InferenceCreateProjectParams{FlowID: flow.ID, Name: name}, &flow)
			}
		case "succeeded":
			var status protocol.InferenceAccountStatus
			if err := c.Call(ctx, "accounts.inference.status", protocol.EmptyParams{}, &status); err != nil {
				return err
			}
			fmt.Printf("✓ Signed in as %s. Provider configured on the execution host.\n", accountText(status.Email))
			printInferenceStatus(status)
			return nil
		case "authorizing", "loading_projects", "creating_project", "provisioning":
			if err := accountPoll(ctx); err != nil {
				return fmt.Errorf("observation ended for %s; inspect auth inference-net flow get %s: %w", flow.ID, flow.ID, err)
			}
			err = c.Call(ctx, "accounts.inference.get", protocol.InferenceFlowParams{FlowID: flow.ID}, &flow)
		default:
			return fmt.Errorf("authorization %s (%s): %s; inspect with whipcode auth inference-net flow get %s", flow.State, flow.ID, accountText(flow.Failure), flow.ID)
		}
		if err != nil {
			return err
		}
	}
}

type providerChoice struct{ ID, Name string }

func chooseProviderID(kind string, choices []providerChoice) (string, error) {
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
	c, err := connectNativeRuntime(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()
	if operation == "rotate" {
		var flow protocol.InferenceFlow
		if err := c.Call(ctx, "accounts.inference.rotate", protocol.EmptyParams{}, &flow); err != nil {
			return err
		}
		return waitInferenceFlow(ctx, c, flow)
	}
	var status protocol.InferenceAccountStatus
	if operation == "logout" {
		var result protocol.InferenceLogoutResult
		if err := c.Call(ctx, "accounts.inference.logout", protocol.EmptyParams{}, &result); err != nil {
			return err
		}
		status = result.Status
		accountWarning(result.LocalFailure)
		accountWarning(result.CleanupFailure)
		for _, entry := range result.Cleanup {
			fmt.Printf("  Cleanup %s key=%s session=%s\n", entry.ID, entry.KeyState, entry.SessionState)
		}
		printInferenceStatus(status)
		if result.LocalFailure != nil {
			return errors.New("local sign-out requires attention; inspect status before retrying")
		}
		return nil
	}
	if err := c.Call(ctx, "accounts.inference."+operation, protocol.EmptyParams{}, &status); err != nil {
		return err
	}
	printInferenceStatus(status)
	if operation == "status" {
		var inventory protocol.ProviderInventory
		if err := c.Call(ctx, "providers.list", protocol.EmptyParams{}, &inventory); err != nil {
			return err
		}
		for _, route := range inventory.Routes {
			if route.ID == inferenceProvider {
				fmt.Printf("  Provider credential %s (%s)\n", route.Credential.Source, route.Credential.State)
			}
		}
	}
	return nil
}

func printInferenceStatus(status protocol.InferenceAccountStatus) {
	fmt.Println("Inference.net")
	fmt.Println("  Management " + status.ManagementState)
	fmt.Println("  Managed key " + status.InferenceState)
	fmt.Println("  Route      " + status.RouteState)
	if status.Email != nil {
		fmt.Println("  Account " + *status.Email)
	}
	if status.ProjectID != nil {
		fmt.Println("  Project " + *status.ProjectID + " " + accountText(status.ProjectName))
	}
	if status.CleanupPending {
		fmt.Println("  Remote cleanup pending; inspect auth inference-net cleanup")
	}
	accountWarning(status.Failure)
}

func inferenceFlowCLI(args []string) error {
	if len(args) != 2 || args[0] != "get" && args[0] != "wait" && args[0] != "cancel" && args[0] != "retry" {
		return errors.New("usage: whipcode auth inference-net flow <get|wait|cancel|retry> <id>")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	c, err := connectNativeRuntime(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()
	method := args[0]
	if method == "wait" {
		method = "get"
	}
	var flow protocol.InferenceFlow
	if err := c.Call(ctx, "accounts.inference."+method, protocol.InferenceFlowParams{FlowID: args[1]}, &flow); err != nil {
		return err
	}
	if args[0] == "wait" || args[0] == "retry" {
		return waitInferenceFlow(ctx, c, flow)
	}
	fmt.Printf("%s  %s\n", flow.ID, flow.State)
	accountWarning(flow.Failure)
	return nil
}

func inferenceReadCLI(operation string, args []string) error {
	if len(args) > 1 || len(args) == 1 && (operation != "cleanup" || args[0] != "--retry") {
		return errors.New("usage: whipcode auth inference-net <flows | cleanup [--retry]>")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	c, err := connectNativeRuntime(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()
	if operation == "flows" {
		var result protocol.InferenceFlowsResult
		if err := c.Call(ctx, "accounts.inference.list", protocol.EmptyParams{}, &result); err != nil {
			return err
		}
		for _, flow := range result.Items {
			fmt.Printf("%s  %s\n", flow.ID, flow.State)
			accountWarning(flow.Failure)
		}
		return nil
	}
	method := "cleanup"
	if len(args) == 1 {
		method = "retry_cleanup"
	}
	var result protocol.InferenceCleanupResult
	if err := c.Call(ctx, "accounts.inference."+method, protocol.EmptyParams{}, &result); err != nil {
		return err
	}
	for _, entry := range result.Items {
		fmt.Printf("%s key=%s session=%s\n", entry.ID, entry.KeyState, entry.SessionState)
		accountWarning(entry.Failure)
	}
	accountWarning(result.Failure)
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
	if err := cmd.Start(); err != nil {
		return false
	}
	go func() { _ = cmd.Wait() }()
	return true
}
