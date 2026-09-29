// whipcode is a minimal coding agent harness.
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/context-labs/whip/internal/buildinfo"
	"github.com/context-labs/whip/internal/session"

	"github.com/context-labs/whip/internal/tui"
	"github.com/context-labs/whip/internal/update"
)

var version = "dev" // set via -ldflags "-X main.version=..."

// cwd is the process working directory, or "." if it's somehow gone.
func cwd() string {
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return wd
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "_desktop-runtime-sync" {
		if err := desktopRuntimeSyncCLI(os.Args[2:], os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, "whipcode desktop:", err)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "_desktop-ssh" {
		os.Exit(desktopSSHCLI(os.Args[2:]))
	}
	if len(os.Args) > 1 && os.Args[1] == "_desktop-runtime-info" {
		if err := desktopRuntimeInfo(os.Args[2:], os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, "whipcode desktop: could not read runtime build metadata")
			os.Exit(1)
		}
		return
	}
	if os.Getenv("WHIP_DESKTOP_ASKPASS") == "1" {
		os.Exit(desktopAskpassCLI(os.Args[1:]))
	}
	if len(os.Args) > 1 && os.Args[1] == "_kernel" {
		if err := kernelCLI(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "whipcode kernel:", err)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) > 1 && (os.Args[1] == "_web-gateway" || os.Args[1] == "_daemon") {
		fmt.Fprintln(os.Stderr, "whipcode: retired private entry point; use daemon start with the native runtime")
		os.Exit(1)
	}
	if len(os.Args) > 1 && os.Args[1] == "_native-runtime" {
		if err := nativeRuntimeCLI(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "whipcode runtime:", err)
			os.Exit(1)
		}
		return
	}
	modelFlag := flag.String("m", "", "model name on the native provider route (default: host model)")
	providerFlag := flag.String("p", "", "configured native provider route (default: host provider)")
	versionFlag := flag.Bool("version", false, "print version")
	engineFlag := flag.String("rlm-engine", "", "session execution language: starlark or quickjs (immutable on resume)")
	agentFlag := flag.String("agent", "", "agent definition for a new session: a registered id, or built-in "+strings.Join(nativeBuiltinNames(), " or ")+" (default coding; immutable on resume)")
	resumeFlag := flag.String("resume", "", "resume a previous session by id (or unique prefix)")
	benchFlag := flag.Bool("bench", false, "measure read-only native host declaration loading and selection validation; no runtime, credentials, or network")
	benchInitFlag := flag.Bool("bench-init", false, "explicitly initialize runtime-v4/host.json, then validate declarations; no database or runtime")
	cautiousFlag := flag.Bool("cautious", false, "require approval and save this mode for the initial session")
	yoloFlag := flag.Bool("yolo", false, "use native automatic permission mode for the initial session; explicitly protected capabilities still require consent")
	flag.Parse()
	if *cautiousFlag && *yoloFlag {
		fmt.Fprintln(os.Stderr, "whipcode: --cautious and --yolo are mutually exclusive")
		os.Exit(2)
	}

	if *versionFlag {
		fmt.Println(buildinfo.Name, version)
		return
	}
	if *benchFlag || *benchInitFlag {
		if flag.NArg() != 0 {
			fmt.Fprintln(os.Stderr, "whipcode: --bench and --bench-init do not accept commands or prompt arguments")
			os.Exit(2)
		}
		if err := benchCLI(*benchInitFlag, *modelFlag, *providerFlag); err != nil {
			fmt.Fprintln(os.Stderr, "whipcode:", err)
			os.Exit(1)
		}
		return
	}

	// `whipcode daemon ...` — inspect and manage the local runtime daemon.
	if flag.NArg() > 0 && flag.Arg(0) == "daemon" {
		if err := daemonManageCLI(flag.Args()[1:]); err != nil {
			fmt.Fprintln(os.Stderr, "whipcode:", err)
			os.Exit(1)
		}
		return
	}

	// `whipcode web` serves the foreground gateway above an existing socket daemon.
	if flag.NArg() > 0 && flag.Arg(0) == "web" {
		if err := webCLI(flag.Args()[1:]); err != nil {
			fmt.Fprintln(os.Stderr, "whipcode:", err)
			os.Exit(1)
		}
		return
	}

	// `whipcode mcp ...` — server management and the MCP server mode.
	if flag.NArg() > 0 && flag.Arg(0) == "mcp" {
		if err := mcpCLI(flag.Args()[1:], version); err != nil {
			fmt.Fprintln(os.Stderr, "whipcode:", err)
			os.Exit(1)
		}
		return
	}

	// `whipcode skills ...` — list and import SKILL.md skills (incl. from other
	// harnesses' dirs, deduped against what whipcode already loads).
	if flag.NArg() > 0 && flag.Arg(0) == "skills" {
		if err := skillsCLI(flag.Args()[1:]); err != nil {
			fmt.Fprintln(os.Stderr, "whipcode:", err)
			os.Exit(1)
		}
		return
	}

	// `whipcode run ...` — non-interactive one-turn mode for scripting; no TTY required.
	// `whipcode acp` — ACP agent over stdio for editors (Zed et al.).
	if flag.NArg() > 0 && flag.Arg(0) == "acp" {
		if err := acpCLI(flag.Args()[1:]); err != nil {
			fmt.Fprintln(os.Stderr, "whipcode acp:", err)
			os.Exit(1)
		}
		return
	}

	if flag.NArg() > 0 && flag.Arg(0) == "run" {
		if err := runCLI(flag.Args()[1:]); err != nil {
			fmt.Fprintln(os.Stderr, "whipcode:", err)
			os.Exit(1)
		}
		return
	}

	// `whipcode browser ...` — browser tooling (install the drive-my-tab extension).
	// `whipcode sessions` — list stored sessions (the scriptable companion to run).
	if flag.NArg() > 0 && flag.Arg(0) == "sessions" {
		if err := sessionsCLI(); err != nil {
			fmt.Fprintln(os.Stderr, "whipcode:", err)
			os.Exit(1)
		}
		return
	}

	if flag.NArg() > 0 && flag.Arg(0) == "browser" {
		if err := browserCLI(flag.Args()[1:]); err != nil {
			fmt.Fprintln(os.Stderr, "whipcode:", err)
			os.Exit(1)
		}
		return
	}

	// `whipcode update` — re-run the install script to get the latest release.
	if flag.NArg() > 0 && flag.Arg(0) == "update" {
		if err := updateCLI(); err != nil {
			fmt.Fprintln(os.Stderr, "whipcode:", err)
			os.Exit(1)
		}
		return
	}

	// `whipcode auth ...` — provider key onboarding (openrouter).
	if flag.NArg() > 0 && flag.Arg(0) == "auth" {
		if err := authCLI(flag.Args()[1:]); err != nil {
			fmt.Fprintln(os.Stderr, "whipcode:", err)
			os.Exit(1)
		}
		return
	}

	// `whipcode up <words...>`: flag.Parse stops at "up", so flags go before it
	// (whipcode -m kimi up …) and the prompt may start with "-" untouched.
	initialPrompt := ""
	if flag.NArg() > 0 && flag.Arg(0) == "up" {
		initialPrompt = strings.Join(flag.Args()[1:], " ")
	}

	// Update check: concurrent with TUI and agent setup, so its
	// ~1 RTT is usually free — and when startup wins the race, the recorded
	// notice still shows on the next launch.
	go update.Check(version)
	tui.Version = version // /report names the build in the bug-report bundle
	sessionID, err := nativeTUI(tui.NativeOptions{Model: *modelFlag, Provider: *providerFlag, Resume: *resumeFlag, Cautious: *cautiousFlag, Automatic: *yoloFlag, InitialPrompt: initialPrompt, Engine: *engineFlag, Agent: *agentFlag})
	if err != nil {
		fmt.Fprintln(os.Stderr, "whipcode:", err)
		os.Exit(1)
	}
	if sessionID != "" {
		fmt.Printf("session %s — resume with: whipcode --resume %s\n", sessionID, sessionID)
	}
}

func nativeBuiltinNames() []string {
	documents := session.Builtins()
	names := make([]string, 0, len(documents))
	for _, document := range documents {
		names = append(names, document.ID)
	}
	return names
}
