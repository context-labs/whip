package main

import (
	"os"
	"testing"

	nativeconfig "github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/session"
)

func TestACPSubscriptionDoesNotRequireAnAPIKey(t *testing.T) {
	useNativeAuth(t, func(directory string) {
		host := nativeconfig.Default()
		host.Providers["subscription"] = nativeconfig.Provider{Kind: "openai-codex"}
		host.Defaults.Model = session.ModelSelection{Provider: "subscription", Name: "gpt-6-astra"}
		if err := nativeconfig.Save(directory, host); err != nil {
			t.Fatal(err)
		}
	})
	input, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	_ = writer.Close() // EOF ends the stdio bridge after its startup validation.
	previous := os.Stdin
	os.Stdin = input
	t.Cleanup(func() { os.Stdin = previous; _ = input.Close() })
	if err := acpCLI(nil); err != nil {
		t.Fatalf("subscription ACP startup required an API key: %v", err)
	}
}
