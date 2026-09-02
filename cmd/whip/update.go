package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/daemon"
	"github.com/context-labs/whip/internal/update"
)

// installURL is the same curl-pipe-sh installer the README documents; update
// just re-runs it — the script resolves the latest release, verifies the
// checksum, and swaps the binary in place.
const installURL = "https://raw.githubusercontent.com/context-labs/whip/main/install.sh"

// updateCLI implements `whip update`: re-run the install script to get the
// latest release.
func updateCLI() error {
	fmt.Printf("whip %s — updating to the latest release via\n  curl -fsSL %s | sh\n\n", version, installURL)
	cmd := exec.CommandContext(context.Background(), "sh", "-c", "curl -fsSL "+installURL+" | sh")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("update failed: %w", err)
	}
	update.Acknowledge() // the pending startup notice is now satisfied
	if err := restartDaemonAfterUpdate(); err != nil {
		fmt.Fprintln(os.Stderr, "whip: updated, but daemon restart was not confirmed:", err)
	}
	fmt.Println("\nwhip updated — the local daemon will reconnect on the new version.")
	return nil
}

var restartDaemonAfterUpdate = func() error {
	dir, err := config.Dir()
	if err != nil {
		return err
	}
	paths, err := daemon.Paths(dir)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client, err := daemon.DialClient(ctx, paths, daemon.InitializeParams{
		ProtocolMajor: daemon.ProtocolMajor, ClientID: fmt.Sprintf("update-%d", os.Getpid()), ClientKind: "automation",
	})
	if err != nil {
		return nil // no responsive daemon means the next client starts the installed build
	}
	defer func() { _ = client.Close() }()
	payload, _ := json.Marshal(map[string]string{"reason": "binary updated"})
	result, err := client.Command(ctx, daemon.CommandParams{
		CommandID: fmt.Sprintf("update-%d", time.Now().UnixNano()), Scope: "daemon",
		Operation: "daemon.checkpoint", Payload: payload,
	})
	if err != nil {
		return err
	}
	var notice daemon.RestartNotice
	if err := json.Unmarshal([]byte(result.Output), &notice); err != nil {
		return err
	}
	return client.RequestRestart(ctx, notice.Generation)
}
