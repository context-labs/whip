// whip is a minimal coding agent harness.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/context-labs/whip/internal/agent"
	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/tui"
	"github.com/context-labs/whip/internal/update"
)

var version = "dev" // set via -ldflags "-X main.version=..."

func systemPrompt() string {
	wd, _ := os.Getwd()
	prompt := "You are an expert coding assistant operating inside whip, a coding agent harness. You help users by reading files, executing commands, editing code, and writing new files.\n\nAvailable tools:\n- read: Read file contents\n- bash: Execute bash commands (ls, grep, find, etc.)\n- edit: Make precise file edits with exact text replacement\n- write: Create or overwrite files\n- task: Delegate a self-contained task to a subagent with fresh context\n\nGuidelines:\n- Use bash for file operations like ls, rg, find\n- Use read to examine files instead of cat or sed\n- Use edit for precise changes (old_string must match exactly and be unique, or set replace_all)\n- Use write only for new files or complete rewrites\n- When the user tags a file with @, a note lists the tagged paths — inspect them with your tools as needed\n- Be concise in your responses\n- Show file paths clearly when working with files\n\nOperating rules:\n- The tool set changes turn to turn: MCP servers connect and drop, skills come and go. Never assume a tool exists because it did earlier — check the current set before calling it.\n- Bias toward acting on reasonable assumptions. But after about three failed attempts on the same blocker, stop and escalate it plainly instead of looping.\n- When the user shares a durable preference or fact about themselves, save it with remember; drop stale entries with forget.\n- Git hygiene: review the staged diff for secrets before committing, never run git add . — stage only the files you intend — and never force-push.\n\nCurrent working directory: " + wd
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

	// `whip run ...` — non-interactive one-turn mode for scripting.
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

	// `whip auth ...` — provider onboarding (OpenRouter keys or Codex OAuth).
	if flag.NArg() > 0 && flag.Arg(0) == "auth" {
		if err := authCLI(flag.Args()[1:]); err != nil {
			fmt.Fprintln(os.Stderr, "whip:", err)
			os.Exit(1)
		}
		return
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
		_ = agent.New(llm.New(prov.BaseURL, "bench"), id, mdl.MaxTokens, systemPrompt())
		return
	}

	// Update check: concurrent with the trust prompt and agent setup, so its
	// ~1 RTT is usually free — and when startup wins the race, the recorded
	// notice still shows on the next launch.
	go update.Check(version)
	tui.Version = version // /report names the build in the bug-report bundle
	sessionID, err := tui.Run(cfg, *modelFlag, *providerFlag, systemPrompt(), *resumeFlag, *cautiousFlag)
	if err != nil {
		fmt.Fprintln(os.Stderr, "whip:", err)
		os.Exit(1)
	}
	if sessionID != "" {
		fmt.Printf("session %s — resume with: whip --resume %s\n", sessionID, sessionID)
	}
}
