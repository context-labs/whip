package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/context-labs/whip/internal/buildinfo"

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
	if buildinfo.UpdateOwner == "desktop" {
		fmt.Println("This whipcode installation is updated by Whip desktop. Open Whip → Check for Updates to update the app and backend together.")
		return nil
	}
	url := installURL
	if buildinfo.Name == "whipcode" {
		url = "https://raw.githubusercontent.com/context-labs/whip/whip-rlm/install-whipcode.sh"
	}
	fmt.Printf("%s %s — updating via %s\n\n", buildinfo.Name, buildinfo.Version(version), url)
	if err := runInstaller(url); err != nil {
		return fmt.Errorf("update failed: %w", err)
	}
	update.Acknowledge()
	if err := restartDaemonAfterUpdate(); err != nil {
		fmt.Fprintln(os.Stderr, buildinfo.Name+": updated, but daemon restart was not confirmed:", err)
	}
	fmt.Printf("\n%s updated — the local daemon will reconnect on the new version.\n", buildinfo.Name)
	return nil
}

// Download first: a failing curl piped into sh otherwise looks like success.
func runInstaller(url string) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	self, err = filepath.EvalSymlinks(self)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", "-c", `script=$(mktemp) || exit 1
trap 'rm -f "$script"' EXIT
curl -fSL --connect-timeout 10 --max-time 60 "$1" -o "$script" || exit 1
sh "$script"`, buildinfo.Name+"-update", url)
	// os/exec uses the last duplicate environment entry. Remove any old value
	// explicitly so the selected executable's directory always wins.
	key := buildinfo.Env("BIN_DIR") + "="
	env := []string{}
	for _, value := range os.Environ() {
		if !strings.HasPrefix(value, key) {
			env = append(env, value)
		}
	}
	cmd.Env = append(env, key+filepath.Dir(self))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
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
