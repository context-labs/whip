package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/context-labs/whip/internal/client"
	"github.com/context-labs/whip/internal/openaiauth"
	"github.com/context-labs/whip/internal/protocol"
)

func authOpenAICLI(args []string) error {
	operation := "login"
	if len(args) > 0 {
		operation = args[0]
	}
	if len(args) > 0 && operation == "flow" {
		return openAIFlowCLI(args[1:])
	}
	if len(args) > 1 {
		return openAIUsage()
	}
	switch operation {
	case "login", "status", "logout", "setup", "flows":
	default:
		return openAIUsage()
	}
	if operation == "login" {
		return providerDeviceLogin(openaiauth.Provider, openaiauth.DeviceLifetime)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	c, err := connectNativeRuntime(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()
	if operation == "flows" {
		var flows protocol.OpenAIFlowsResult
		if err := c.Call(ctx, "accounts.openai.list", protocol.EmptyParams{}, &flows); err != nil {
			return err
		}
		for _, flow := range flows.Items {
			printOpenAIFlow(flow)
		}
		return nil
	}
	var status protocol.OpenAIAccountStatus
	if err := c.Call(ctx, "accounts.openai."+operation, protocol.EmptyParams{}, &status); err != nil {
		return err
	}
	printOpenAIStatus(status)
	return nil
}

func openAIUsage() error {
	return errors.New("usage: whipcode auth openai-codex [login | status | logout | setup | flows | flow <get|wait|cancel> <id>]")
}

func printOpenAIStatus(status protocol.OpenAIAccountStatus) {
	fmt.Println("OpenAI (ChatGPT subscription)")
	fmt.Println("  Status  " + status.AuthState)
	fmt.Println("  Route   " + status.RouteState)
	if status.Email != nil {
		fmt.Println("  Account " + *status.Email)
	}
	if status.Plan != nil {
		fmt.Println("  Plan    " + *status.Plan)
	}
	accountWarning(status.Failure)
}

func printOpenAIFlow(flow protocol.OpenAILoginFlow) {
	fmt.Printf("%s  %s\n", flow.ID, flow.State)
	accountWarning(flow.Failure)
}

func openAIFlowCLI(args []string) error {
	if len(args) != 2 || args[0] != "get" && args[0] != "wait" && args[0] != "cancel" {
		return openAIUsage()
	}
	ctx, cancel := context.WithTimeout(context.Background(), openaiauth.DeviceLifetime)
	defer cancel()
	c, err := connectNativeRuntime(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()
	method := "get"
	if args[0] == "cancel" {
		method = "cancel"
	}
	var flow protocol.OpenAILoginFlow
	if err := c.Call(ctx, "accounts.openai."+method, protocol.OpenAIFlowParams{FlowID: args[1]}, &flow); err != nil {
		return err
	}
	if args[0] == "wait" {
		return waitOpenAIFlow(ctx, c, flow)
	}
	printOpenAIFlow(flow)
	return nil
}

func waitOpenAIFlow(ctx context.Context, c *client.Client, flow protocol.OpenAILoginFlow) error {
	fmt.Println("Authorization flow:", flow.ID)
	previous := ""
	for {
		showApproval(flow.VerificationURL, flow.UserCode, &previous)
		switch flow.State {
		case "succeeded":
			var status protocol.OpenAIAccountStatus
			if err := c.Call(ctx, "accounts.openai.status", protocol.EmptyParams{}, &status); err != nil {
				return err
			}
			printOpenAIStatus(status)
			return nil
		case "authorizing":
			if err := accountPoll(ctx); err != nil {
				return fmt.Errorf("observation ended for %s; inspect auth openai-codex flow get %s: %w", flow.ID, flow.ID, err)
			}
			if err := c.Call(ctx, "accounts.openai.get", protocol.OpenAIFlowParams{FlowID: flow.ID}, &flow); err != nil {
				return err
			}
		case "setup_required":
			return fmt.Errorf("authorization saved; run whipcode auth openai-codex setup: %s", accountText(flow.Failure))
		default:
			return fmt.Errorf("authorization %s (%s): %s", flow.State, flow.ID, accountText(flow.Failure))
		}
	}
}
