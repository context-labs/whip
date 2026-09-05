// whip is a minimal coding agent harness.
package main

import (
	"flag"
	"fmt"
	"os"
	"os/user"
	"runtime"
	"strings"
	"time"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/rlm"
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

// username is the OS login name, or "unknown" when it can't be resolved
// (e.g. inside a container without a passwd entry).
func username() string {
	u, err := user.Current()
	if err != nil || u.Username == "" {
		return "unknown"
	}
	return u.Username
}

// systemPrompt builds the base system prompt rooted at wd. The TUI and
// `whip run` pass the process cwd; `whip acp` passes the client-provided
// session cwd (the editor spawns whip wherever it likes).
func systemPrompt(wd string, now time.Time) string {
	prompt := rlm.BuildPrompt(wd, nil) + `

Operating rules:
- When the user tags a file with @, inspect the listed path with files.read.
- Bias toward acting on reasonable assumptions. After repeated failures on one blocker, escalate it plainly instead of looping.
- Use explicit messages for child collaboration; do not assume a child's ordinary response reaches its parent.
- Git hygiene: inspect staged changes for secrets, stage intentional files only, and never force-push.

Environment:
<env>
  Platform: ` + runtime.GOOS + `
  Current date/time: ` + now.Format("Mon Jan 2, 2006 15:04:05 MST (UTC-07:00)") + `
  User: ` + username() + `
</env>`
	if extra := config.MeInstructions(); extra != "" {
		prompt += "\n\nStanding instructions from the user (~/.whip/me.md — treat as user rules):\n" + extra
	}
	return prompt
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "_kernel" {
		if err := kernelCLI(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "whip kernel:", err)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "_daemon" {
		if err := daemonCLI(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "whip daemon:", err)
			os.Exit(1)
		}
		return
	}
	modelFlag := flag.String("m", "", "model name from ~/.whip/config.json (default: defaultModel)")
	providerFlag := flag.String("p", "", "provider to route the model through (default: model's first provider)")
	versionFlag := flag.Bool("version", false, "print version")
	resumeFlag := flag.String("resume", "", "resume a previous session by id (or unique prefix)")
	benchFlag := flag.Bool("bench", false, "measure configuration and provider routing startup, then exit; for `task benchmark`")
	cautiousFlag := flag.Bool("cautious", false, "ask before running commands / writing files")
	yoloFlag := flag.Bool("yolo", false, "approve every permission prompt automatically in this TUI's sessions")
	flag.Parse()
	if *cautiousFlag && *yoloFlag {
		fmt.Fprintln(os.Stderr, "whip: --cautious and --yolo are mutually exclusive")
		os.Exit(2)
	}

	if *versionFlag {
		fmt.Println("whip", version)
		return
	}

	// `whip daemon ...` — inspect and manage the local runtime daemon.
	if flag.NArg() > 0 && flag.Arg(0) == "daemon" {
		if err := daemonManageCLI(flag.Args()[1:]); err != nil {
			fmt.Fprintln(os.Stderr, "whip:", err)
			os.Exit(1)
		}
		return
	}

	// `whip mcp ...` — server management and the MCP server mode.
	if flag.NArg() > 0 && flag.Arg(0) == "mcp" {
		if err := mcpCLI(flag.Args()[1:], version); err != nil {
			fmt.Fprintln(os.Stderr, "whip:", err)
			os.Exit(1)
		}
		return
	}

	// `whip skills ...` — list and import SKILL.md skills (incl. from other
	// harnesses' dirs, deduped against what whip already loads).
	if flag.NArg() > 0 && flag.Arg(0) == "skills" {
		if err := skillsCLI(flag.Args()[1:]); err != nil {
			fmt.Fprintln(os.Stderr, "whip:", err)
			os.Exit(1)
		}
		return
	}

	// `whip run ...` — non-interactive one-turn mode for scripting; no TTY or
	// trust prompt required (headless use implies trusted automation).
	// `whip acp` — ACP agent over stdio for editors (Zed et al.).
	if flag.NArg() > 0 && flag.Arg(0) == "acp" {
		if err := acpCLI(flag.Args()[1:]); err != nil {
			fmt.Fprintln(os.Stderr, "whip acp:", err)
			os.Exit(1)
		}
		return
	}

	if flag.NArg() > 0 && flag.Arg(0) == "run" {
		if err := runCLI(flag.Args()[1:]); err != nil {
			fmt.Fprintln(os.Stderr, "whip:", err)
			os.Exit(1)
		}
		return
	}

	// `whip browser ...` — browser tooling (install the drive-my-tab extension).
	// `whip sessions` — list stored sessions (the scriptable companion to run).
	if flag.NArg() > 0 && flag.Arg(0) == "sessions" {
		if err := sessionsCLI(); err != nil {
			fmt.Fprintln(os.Stderr, "whip:", err)
			os.Exit(1)
		}
		return
	}

	if flag.NArg() > 0 && flag.Arg(0) == "browser" {
		if err := browserCLI(flag.Args()[1:]); err != nil {
			fmt.Fprintln(os.Stderr, "whip:", err)
			os.Exit(1)
		}
		return
	}

	// `whip update` — re-run the install script to get the latest release.
	if flag.NArg() > 0 && flag.Arg(0) == "update" {
		if err := updateCLI(); err != nil {
			fmt.Fprintln(os.Stderr, "whip:", err)
			os.Exit(1)
		}
		return
	}

	// `whip auth ...` — provider key onboarding (openrouter).
	if flag.NArg() > 0 && flag.Arg(0) == "auth" {
		if err := authCLI(flag.Args()[1:]); err != nil {
			fmt.Fprintln(os.Stderr, "whip:", err)
			os.Exit(1)
		}
		return
	}

	// The setup wizard triggers on "no config file AND no setup-done marker":
	// Load creates the config on first run, so only a pre-Load stat can tell
	// this install has never launched — and the marker keeps a subcommand's
	// Load (whip auth/run/mcp/…) from permanently consuming the first run.
	firstRun := !config.Exists() && !config.SetupDone()

	// `whip up <words...>`: flag.Parse stops at "up", so flags go before it
	// (whip -m kimi up …) and the prompt may start with "-" untouched.
	initialPrompt := ""
	if flag.NArg() > 0 && flag.Arg(0) == "up" {
		initialPrompt = strings.Join(flag.Args()[1:], " ")
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "whip:", err)
		os.Exit(1)
	}

	if *benchFlag {
		prov, _, _, err := cfg.Resolve(*modelFlag, *providerFlag)
		if err != nil {
			fmt.Fprintln(os.Stderr, "whip:", err)
			os.Exit(1)
		}
		_ = prov.Key()
		return
	}

	// Update check: concurrent with the trust prompt and agent setup, so its
	// ~1 RTT is usually free — and when startup wins the race, the recorded
	// notice still shows on the next launch.
	go update.Check(version)
	tui.Version = version // /report names the build in the bug-report bundle
	sessionID, err := tui.Run(cfg, *modelFlag, *providerFlag, systemPrompt(cwd(), time.Now()), *resumeFlag, *cautiousFlag, *yoloFlag, firstRun, initialPrompt)
	if err != nil {
		fmt.Fprintln(os.Stderr, "whip:", err)
		os.Exit(1)
	}
	if sessionID != "" {
		fmt.Printf("session %s — resume with: whip --resume %s\n", sessionID, sessionID)
	}
}
