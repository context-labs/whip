package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/context-labs/whip/internal/buildinfo"
	"github.com/context-labs/whip/internal/daemon"
	"github.com/context-labs/whip/internal/openaiauth"
)

func authOpenAICLI(args []string) error {
	operation := "login"
	if len(args) > 0 {
		operation = args[0]
	}
	if len(args) > 1 || (operation != "login" && operation != "status" && operation != "logout") {
		return errors.New(buildinfo.Text("usage: whip auth openai-codex [login | status | logout]"))
	}
	if operation == "login" {
		return providerDeviceLogin(openaiauth.Provider, openaiauth.DeviceLifetime)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	client, err := connectProviderDaemon(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()
	var status daemon.ProviderStatus
	if operation == "logout" {
		status, err = client.LogoutProvider(ctx, openaiauth.Provider)
	} else {
		status, err = client.ProviderStatus(ctx, openaiauth.Provider)
	}
	if err != nil {
		return err
	}
	fmt.Println("OpenAI (ChatGPT subscription)")
	fmt.Println("  Status  " + status.AuthState)
	if status.Email != "" {
		fmt.Println("  Account " + status.Email)
	}
	if status.Plan != "" {
		fmt.Println("  Plan    " + status.Plan)
	}
	for _, warning := range status.Warnings {
		fmt.Fprintln(os.Stderr, warning)
	}
	return nil
}
