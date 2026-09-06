package tui

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/daemon"
)

// setupWizard runs once, on the first launch (no config file existed), after
// the trust gate and before the TUI takes the terminal. It walks the
// install-time decisions the palette can't conveniently make for you —
// provider auth, thinking-token display, and whether claude/codex MCP configs
// are imported — and persists the answers. Defaults are opt-OUT (Enter = no /
// skip): a first run that only presses Enter ends with a clean, self-contained
// config that imports nothing and signs into nothing.
//
// The wizard only runs interactively. Non-terminal stdin (piped runs, tests,
// headless launches) skips silently and keeps the shipped defaults. r is the
// caller's shared stdin reader (checkTrust just used it — a fresh bufio here
// would lose its buffered read-ahead).
func setupWizard(cfg *config.Config, r *bufio.Reader) error {
	// A failed Stat means we can't tell whether stdin is a terminal — treat it
	// as non-terminal and skip, rather than error out of install.
	st, statErr := os.Stdin.Stat()
	if statErr != nil || st.Mode()&os.ModeCharDevice == 0 {
		return nil //nolint:nilerr // no terminal to ask on: keep defaults
	}
	return runSetupWizard(cfg, r, os.Stderr)
}

// runSetupWizard is the wizard body with injectable I/O so tests can drive it
// (os.Stdin can't be swapped for a pipe without losing the terminal check).
// stdin is any reader here — the production path passes the shared
// *bufio.Reader; tests pass a strings.Reader.
func runSetupWizard(cfg *config.Config, stdin io.Reader, stderr io.Writer) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	host, err := connectSetupHost(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = host.Close() }()
	return runSetupWizardOnHost(ctx, host, cfg, stdin, stderr)
}

func runSetupWizardOnHost(ctx context.Context, host setupHost, cfg *config.Config, stdin io.Reader, stderr io.Writer) error {

	r, ok := stdin.(*bufio.Reader)
	if !ok {
		r = bufio.NewReader(stdin)
	}
	w := stderr

	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Welcome to whip! First-run setup (Enter = skip/keep default).")
	fmt.Fprintln(w, "Every choice is reversible later: /auth, ctrl+p, ~/.whip/config.json.")
	fmt.Fprintln(w, "")

	setupProvider(ctx, host, r, w)

	if !askYN(r, w, "Show thinking (reasoning) tokens in the transcript?", true) {
		off := false
		cfg.Thinking = &off
	}

	setupMCPImports(cfg, r, w)

	current, err := host.ReadConfiguration(ctx)
	if err != nil {
		return err
	}
	if _, err := host.UpdateConfiguration(ctx, daemon.ConfigurationUpdate{Revision: current.Revision,
		ImportClaude: cfg.MCPImport.Claude.Enabled, ImportCodex: cfg.MCPImport.Codex.Enabled}); err != nil {
		return fmt.Errorf("saving host setup choices: %w", err)
	}
	if err := cfg.SavePreferences(); err != nil {
		return fmt.Errorf("saving client preferences: %w", err)
	}
	config.MarkSetupDone() // only on success: an aborted wizard offers again
	fmt.Fprintln(w, "Setup complete — starting whip.")
	fmt.Fprintln(w, "")
	return nil
}

// askYN asks a yes/no question with an explicit default: Enter takes it,
// y/yes/n/no parse, anything else re-asks once and then takes the default
// (a wizard answer is never worth a validation loop).
func askYN(r *bufio.Reader, w io.Writer, question string, def bool) bool {
	hint := "[y/N]"
	if def {
		hint = "[Y/n]"
	}
	for attempt := 0; ; attempt++ {
		fmt.Fprintf(w, "%s %s ", question, hint)
		line, err := r.ReadString('\n')
		if err != nil && line == "" {
			return def // EOF: take the default rather than wedge install
		}
		switch strings.ToLower(strings.TrimSpace(line)) {
		case "":
			return def
		case "y", "yes":
			return true
		case "n", "no":
			return false
		}
		if attempt > 0 {
			return def // second unrecognized answer: stop asking
		}
		fmt.Fprintln(w, "  (answer y or n)")
	}
}

// setupProvider asks which inference provider to connect. Enter skips (the
// shipped config already routes to inference-net and resolves its machine key
// whenever the user signs in later via /auth).
func setupProvider(ctx context.Context, host setupHost, r *bufio.Reader, w io.Writer) {
	fmt.Fprintln(w, "Connect a model provider:")
	fmt.Fprintln(w, "  1) Inference.net — browser sign-in, no key handling (recommended)")
	fmt.Fprintln(w, "  2) OpenRouter    — paste an API key from https://openrouter.ai/keys")
	fmt.Fprintln(w, "  3) Skip          — set up later with /auth")
	fmt.Fprint(w, "Choose [1/2/3, default 3]: ")
	line, _ := r.ReadString('\n')
	switch strings.TrimSpace(line) {
	case "1", "inference-net", "inference":
		setupInferenceNet(ctx, host, r, w)
	case "2", "openrouter":
		setupOpenRouter(ctx, host, r, w)
	default:
		fmt.Fprintln(w, "  skipped — /auth inference-net or /auth openrouter later.")
	}
	fmt.Fprintln(w, "")
}

// setupInferenceNet renders provider choices while the daemon owns the flow.
func setupInferenceNet(ctx context.Context, host setupHost, r *bufio.Reader, w io.Writer) {
	status, err := host.BeginLogin(ctx)
	url := ""
	for err == nil {
		if status.VerificationURL != "" && status.VerificationURL != url {
			url = status.VerificationURL
			fmt.Fprintf(w, "  Approve in your browser:\n  %s\n  Code: %s\n", url, status.UserCode)
			openBrowserURL(url)
		}
		switch status.State {
		case "choose_team":
			var id string
			id, err = wizardProviderChoice(r, w, "workspace", status.Teams)
			if err == nil {
				status, err = host.SelectLoginTeam(ctx, status.FlowID, id)
			}
		case "choose_project":
			choices := append(append([]daemon.ProviderChoice{}, status.Projects...), daemon.ProviderChoice{Name: "+ Create new project"})
			var id string
			id, err = wizardProviderChoice(r, w, "project", choices)
			if err == nil {
				if id != "" {
					status, err = host.SelectLoginProject(ctx, status.FlowID, id)
				} else {
					var name string
					name, err = wizardChoose(r, w, "new project", nil)
					if err == nil {
						status, err = host.CreateLoginProject(ctx, status.FlowID, name)
					}
				}
			}
		case "succeeded":
			fmt.Fprintf(w, "  ✓ signed in as %s on the execution host\n", status.Email)
			return
		case "failed", "expired", "interrupted", "cancelled":
			err = fmt.Errorf("provider login %s: %s", status.State, status.Error)
		default:
			timer := time.NewTimer(200 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				err = ctx.Err()
			case <-timer.C:
				status, err = host.LoginStatus(ctx, status.FlowID)
			}
		}
	}
	fmt.Fprintln(w, "  sign-in failed: "+err.Error()+"; retry later with /auth inference-net")
}

func wizardProviderChoice(r *bufio.Reader, w io.Writer, title string, choices []daemon.ProviderChoice) (string, error) {
	if len(choices) == 1 && choices[0].ID != "" {
		return choices[0].ID, nil
	}
	labels := providerChoiceLabels(choices)
	choice, err := wizardChoose(r, w, title, labels)
	if err != nil {
		return "", err
	}
	for i, label := range labels {
		if choice == label {
			return choices[i].ID, nil
		}
	}
	return "", fmt.Errorf("invalid provider choice")
}

func setupOpenRouter(ctx context.Context, host setupHost, r *bufio.Reader, w io.Writer) {
	fmt.Fprint(w, "  paste your OpenRouter key (visible while typing): ")
	line, _ := r.ReadString('\n')
	key := config.TrimKey(line)
	if key == "" {
		fmt.Fprintln(w, "  skipped — /auth openrouter later.")
		return
	}
	current, err := host.ReadConfiguration(ctx)
	if err == nil {
		_, err = host.SetProviderKey(ctx, daemon.ProviderKeySetup{Revision: current.Revision, Provider: "openrouter", Key: key})
	}
	if err != nil {
		fmt.Fprintln(w, "  host provider setup failed: "+err.Error())
		return
	}
	fmt.Fprintln(w, "  ✓ openrouter configured on the execution host")
}

// wizardChoose is the numbered-list ChooseFunc for the wizard's plain
// terminal: Enter takes the first option, a number picks, a bare name matches.
func wizardChoose(r *bufio.Reader, w io.Writer, title string, options []string) (string, error) {
	fmt.Fprintln(w, "  "+title+":")
	if len(options) == 0 {
		fmt.Fprint(w, "  name: ")
		line, err := r.ReadString('\n')
		return strings.TrimSpace(line), err
	}
	for i, o := range options {
		fmt.Fprintf(w, "    %d) %s\n", i+1, o)
	}
	fmt.Fprintf(w, "  pick [1-%d, default 1]: ", len(options))
	line, err := r.ReadString('\n')
	if err != nil && line == "" {
		return "", err
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return options[0], nil
	}
	if n, err := strconv.Atoi(line); err == nil && n >= 1 && n <= len(options) {
		return options[n-1], nil
	}
	return "", fmt.Errorf("invalid choice %q", line)
}

// setupMCPImports asks which external MCP configs whip should import. Enter =
// no for both (opt-in): nothing from another harness's config is picked up
// unless the user says so. The answers always land in the mcpImport block so
// the install has an explicit record — and ctrl+p → MCPs flips them later.
func setupMCPImports(cfg *config.Config, r *bufio.Reader, w io.Writer) {
	fmt.Fprintln(w, "whip can import MCP servers from other tools' configs.")
	claude := askYN(r, w, "Import MCP servers from Claude? (~/.claude.json, .mcp.json)", false)
	codex := askYN(r, w, "Import MCP servers from Codex? (~/.codex/config.toml)", false)
	cfg.MCPImport = &config.MCPImport{
		Claude: &config.MCPImportSource{Enabled: &claude},
		Codex:  &config.MCPImportSource{Enabled: &codex},
	}
	state := func(on bool) string {
		if on {
			return "on"
		}
		return "off"
	}
	fmt.Fprintf(w, "  MCP imports: claude %s, codex %s (toggle anytime: ctrl+p → MCPs)\n", state(claude), state(codex))
	fmt.Fprintln(w, "")
}
