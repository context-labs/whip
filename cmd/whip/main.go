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

	"github.com/context-labs/whip/internal/agent"
	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/llm"
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
	prompt := `You are an expert coding assistant operating inside whip, a coding agent harness. You help users by reading files, executing commands, editing code, and writing new files.

Available tools:
- read: Read file contents
- bash: Execute bash commands (ls, grep, find, etc.)
- edit: Make precise file edits with exact text replacement
- write: Create or overwrite files
- task: Delegate a self-contained task to a subagent with fresh context

Guidelines:
- Use bash for file operations like ls, rg, find
- Use read to examine files instead of cat or sed
- Use edit for precise changes (old_string must match exactly and be unique, or set replace_all)
- Use write only for new files or complete rewrites
- When the user tags a file with @, a note lists the tagged paths — inspect them with your tools as needed
- Be concise in your responses
- Show file paths clearly when working with files

Operating rules:
- The tool set changes turn to turn: MCP servers connect and drop, skills come and go. Never assume a tool exists because it did earlier — check the current set before calling it.
- Bias toward acting on reasonable assumptions. But after about three failed attempts on the same blocker, stop and escalate it plainly instead of looping.
- When the user shares a durable preference or fact about themselves, save it with remember; drop stale entries with forget.
- Git hygiene: review the staged diff for secrets before committing, never run git add . — stage only the files you intend — and never force-push.
- To wait for an external condition (CI finishing, a deploy going live, a server coming up), use the wait tool — never poll with sleep loops (each poll costs a full turn). You will be notified once when the condition changes.

whip's own docs (features, tools, configuration, MCP servers, skills) live at https://github.com/context-labs/whip/tree/main/docs — consult them when the user asks how to configure or extend whip itself.

Here is some useful information about the environment you are running in:
<env>
  Working directory: ` + wd + `
  Platform: ` + runtime.GOOS + `
  Current date/time: ` + now.Format("Mon Jan 2, 2006 15:04:05 MST (UTC-07:00)") + `
  User: ` + username() + `
</env>`
	if extra := config.MeInstructions(); extra != "" {
		prompt += "\n\nStanding instructions from the user (~/.whip/me.md — treat as user rules):\n" + extra
	}
	// the skills block is appended fresh each turn by the TUI, so newly added
	// skills are picked up without restarting
	return prompt
}

func main() {
	modelFlag := flag.String("m", "", "model name from ~/.whip/config.json (default: defaultModel)")
	providerFlag := flag.String("p", "", "provider to route the model through (default: model's first provider)")
	versionFlag := flag.Bool("version", false, "print version")
	resumeFlag := flag.String("resume", "", "resume a previous session by id (or unique prefix)")
	benchFlag := flag.Bool("bench", false, "do full startup init (config, routing, key, agent) then exit; for `task benchmark`")
	cautiousFlag := flag.Bool("cautious", false, "ask before running commands / writing files")
	flag.Parse()

	if *versionFlag {
		fmt.Println("whip", version)
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
		prov, mdl, id, err := cfg.Resolve(*modelFlag, *providerFlag)
		if err != nil {
			fmt.Fprintln(os.Stderr, "whip:", err)
			os.Exit(1)
		}
		_ = prov.Key()
		_ = agent.New(llm.New(prov.BaseURL, "bench"), id, mdl.MaxTokens, systemPrompt(cwd(), time.Now()))
		return
	}

	// Update check: concurrent with the trust prompt and agent setup, so its
	// ~1 RTT is usually free — and when startup wins the race, the recorded
	// notice still shows on the next launch.
	go update.Check(version)
	tui.Version = version // /report names the build in the bug-report bundle
	sessionID, err := tui.Run(cfg, *modelFlag, *providerFlag, systemPrompt(cwd(), time.Now()), *resumeFlag, *cautiousFlag, firstRun, initialPrompt)
	if err != nil {
		fmt.Fprintln(os.Stderr, "whip:", err)
		os.Exit(1)
	}
	if sessionID != "" {
		fmt.Printf("session %s — resume with: whip --resume %s\n", sessionID, sessionID)
	}
}
