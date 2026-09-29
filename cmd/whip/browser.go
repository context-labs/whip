package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/context-labs/whip/internal/browser/extrelay"
	"github.com/context-labs/whip/internal/buildinfo"
	"github.com/context-labs/whip/internal/localruntime"
)

// browserCLI explicitly installs native extension assets. Relay credentials are
// published only by an approved native browser operation, never by installation.
func browserCLI(args []string) error {
	if len(args) == 0 {
		return errors.New(nativeBrowserUsage)
	}
	switch args[0] {
	case "status", "configure", "list", "reconnect", "disconnect":
		return nativeBrowserCLI(args, os.Stdout)
	case "install":
		flags := flag.NewFlagSet("browser install", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		directory := flags.String("directory", "", "explicit native runtime directory")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if flags.NArg() != 0 {
			return errors.New("usage: whipcode browser install [--directory /absolute/runtime-directory]")
		}
		if *directory != "" && (!filepath.IsAbs(*directory) || filepath.Clean(*directory) != *directory) {
			return errors.New("browser install directory must be clean and absolute")
		}

		return browserInstall(*directory)
	default:
		return fmt.Errorf("unknown whipcode browser subcommand %q (want: install, status, configure, list, reconnect, disconnect)", args[0])
	}
}

func browserInstall(directory string) error {
	if directory == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		paths, err := localruntime.Resolve(buildinfo.Home(home))
		if err != nil {
			return err
		}
		directory = paths.Directory
	}
	dir := filepath.Join(directory, "browser", "extension")
	written, err := extrelay.WriteExtension(dir)
	if err != nil {
		return fmt.Errorf("write extension: %w", err)
	}

	fmt.Println("whipcode browser extension written:")
	for _, f := range written {
		fmt.Println("  ", f)
	}
	fmt.Println()

	fmt.Println("Load it into Chrome (3 clicks — Chrome doesn't allow programmatic install):")
	fmt.Println("  1. In chrome://extensions, toggle ON \"Developer mode\" (top right).")
	fmt.Println("  2. Click \"Load unpacked\".")
	fmt.Printf("  3. Select this folder:\n       %s\n\n", dir)

	fmt.Println("Then, to let whipcode drive a tab:")
	fmt.Printf("  - In %s, set external_browser.mode to extension (host version 21),\n", filepath.Join(directory, "host.json"))
	fmt.Println("    or use the native SDK hosts.setExternalBrowser configuration CAS.")
	fmt.Println("  - Submit and approve browser.run(session=\"default\", code=\"info()\").")
	fmt.Println("    The running native host then publishes its private relay address and token.")
	fmt.Println("  - Open the tab you want, click the whipcode extension icon (a green ● appears).")
	fmt.Println("  - Click the icon again to detach. Reconnect is explicit; no command is replayed.")
	fmt.Println()
	fmt.Println("Note: while pinned, Chrome shows a \"whipcode is debugging this browser\" bar —")
	fmt.Println("that's chrome.debugger, the mechanism that lets whipcode drive your real session.")

	openInstallTargets(dir)
	return nil
}

// openInstallTargets opens chrome://extensions and the extension folder so
// the manual load is one switch away. Best-effort; failure isn't fatal.
func openInstallTargets(dir string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var urlCmd, dirCmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		urlCmd = exec.CommandContext(ctx, "open", "-a", "Google Chrome", "chrome://extensions")
		dirCmd = exec.CommandContext(ctx, "open", dir)
	case "windows":
		urlCmd = exec.CommandContext(ctx, "cmd", "/c", "start", "", "chrome://extensions")
		dirCmd = exec.CommandContext(ctx, "explorer", dir)
	default: // linux
		urlCmd = exec.CommandContext(ctx, "xdg-open", "chrome://extensions")
		dirCmd = exec.CommandContext(ctx, "xdg-open", dir)
	}
	if urlCmd != nil && dirCmd != nil {
		if errors.Join(urlCmd.Run(), dirCmd.Run()) == nil {
			fmt.Printf("\n(opened chrome://extensions and %s)\n", filepath.Clean(dir))
			return
		}
	}
	fmt.Println("\nOpen chrome://extensions and the listed folder manually if they did not open.")
}
