package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/context-labs/whip/internal/buildinfo"

	"github.com/context-labs/whip/internal/browser/extrelay"
)

// browserCLI implements `whip browser <install>`.
//
//	install    write the unpacked extension to ~/.whip/browser/extension,
//	           generate a relay token, and open chrome://extensions + the
//	           folder so the user can load it (Chrome forbids programmatic
//	           install — the three clicks are on the user).
func browserCLI(args []string) error {
	if len(args) == 0 {
		return errors.New(buildinfo.Text("usage: whip browser <install>"))
	}
	switch args[0] {
	case "install":
		return browserInstall()
	default:
		return fmt.Errorf(buildinfo.Text("unknown whip browser subcommand %q (want: install)"), args[0])
	}
}

func browserInstall() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	dir := extrelay.ExtensionDir(home)
	written, err := extrelay.WriteExtension(dir)
	if err != nil {
		return fmt.Errorf("write extension: %w", err)
	}

	// Fresh relay token for the extension to authenticate with. The address
	// is filled in for real when the agent starts the relay (ephemeral port);
	// we write the token now and a placeholder addr that the running agent
	// overwrites via WriteRelayState on startup.
	r, err := extrelay.NewRelay()
	if err != nil {
		return fmt.Errorf("start relay to mint token: %w", err)
	}
	defer func() { _ = r.Close() }()
	statePath, err := extrelay.WriteRelayState(home, r.Addr(), r.Token())
	if err != nil {
		return fmt.Errorf("write relay state: %w", err)
	}

	fmt.Println(buildinfo.Text("whip browser extension written:"))
	for _, f := range written {
		fmt.Println("  ", f)
	}
	fmt.Printf("  %s (relay address + token, 0600)\n\n", statePath)

	fmt.Println("Load it into Chrome (3 clicks — Chrome doesn't allow programmatic install):")
	fmt.Println("  1. In chrome://extensions, toggle ON \"Developer mode\" (top right).")
	fmt.Println("  2. Click \"Load unpacked\".")
	fmt.Printf("  3. Select this folder:\n       %s\n\n", dir)

	fmt.Println(buildinfo.Text("Then, to let whip drive a tab:"))
	fmt.Println(buildinfo.Text("  - Set \"browser\": { \"mode\": \"extension\" } in ~/.whip/config.json."))
	fmt.Println("  - Open the tab you want, click the whip extension icon (a green ● appears).")
	fmt.Println("  - Click the icon again to detach.")
	fmt.Println()
	fmt.Println("Note: while pinned, Chrome shows a \"whip is debugging this browser\" bar —")
	fmt.Println(buildinfo.Text("that's chrome.debugger, the mechanism that lets whip drive your real session."))

	openInstallTargets(dir)
	return nil
}

// openInstallTargets opens chrome://extensions and the extension folder so
// the manual load is one switch away. Best-effort; failure isn't fatal.
func openInstallTargets(dir string) {
	var urlCmd, dirCmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		urlCmd = exec.CommandContext(context.Background(), "open", "-a", "Google Chrome", "chrome://extensions")
		dirCmd = exec.CommandContext(context.Background(), "open", dir)
	case "windows":
		urlCmd = exec.CommandContext(context.Background(), "cmd", "/c", "start", "", "chrome://extensions")
		dirCmd = exec.CommandContext(context.Background(), "explorer", dir)
	default: // linux
		urlCmd = exec.CommandContext(context.Background(), "xdg-open", "chrome://extensions")
		dirCmd = exec.CommandContext(context.Background(), "xdg-open", dir)
	}
	if urlCmd != nil {
		_ = urlCmd.Start()
	}
	if dirCmd != nil {
		_ = dirCmd.Start()
	}
	fmt.Println()
	fmt.Printf("(opened chrome://extensions and %s for you)\n", filepath.Clean(dir))
}
