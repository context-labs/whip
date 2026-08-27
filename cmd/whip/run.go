// `whip run` — non-interactive (headless) mode: one turn of the agent with
// no TUI and no trust prompt, for trusted automation and scripting. Piped
// stdin is appended to the prompt. --format json emits the raw event stream
// as newline-delimited JSON; the final event is {"type":"done",...} or
// {"type":"error",...}. Exit code 0 on success, 1 on error.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/context-labs/whip/internal/agent"
	"github.com/context-labs/whip/internal/codexauth"
	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/session"
)

func runCLI(args []string) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	format := fs.String("format", "text", "output format: text (stream the reply) or json (newline-delimited event stream)")
	modelFlag := fs.String("m", "", "model name from ~/.whip/config.json (default: defaultModel)")
	providerFlag := fs.String("p", "", "provider to route the model through (default: model's first provider)")
	resumeFlag := fs.String("resume", "", "continue this session id (see `whip sessions`) instead of starting fresh")
	systemFlag := fs.String("system", "", "override the system prompt for this run")
	systemFileFlag := fs.String("system-file", "", "read the system prompt from this file (wins over -system)")
	maxTurnsFlag := fs.Int("max-turns", 0, "cap the tool-call loop at N rounds (0 = uncapped); a capped run exits non-zero")
	timeoutFlag := fs.Duration("timeout", 0, "wall-clock cap on the whole run (e.g. 30s, 5m); 0 = no timeout")
	quietFlag := fs.Bool("quiet", false, "suppress the stderr tool/session notes (clean stdout for -format json piping)")
	noSessionFlag := fs.Bool("no-session", false, "run without persisting a session (one-off jobs don't clutter whip sessions)")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: whip run [--format text|json] [-m model] [-p provider] [-resume id] [-system text | -system-file path] [-max-turns N] [-timeout dur] [-quiet] [-no-session] \"prompt\"")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	switch *format {
	case "text", "json":
	default:
		return fmt.Errorf("unknown --format %q (want text|json)", *format)
	}

	prompt := strings.Join(fs.Args(), " ")
	// Piped stdin is appended to the prompt (both matter: e.g.
	// `git diff | whip run "review this"`). Read only when stdin is not a
	// TTY, so interactive `whip run "…"` never blocks on it.
	if fi, err := os.Stdin.Stat(); err == nil && fi.Mode()&os.ModeCharDevice == 0 {
		if data, err := io.ReadAll(os.Stdin); err == nil {
			if piped := strings.TrimSpace(string(data)); piped != "" {
				if prompt != "" {
					prompt += "\n\n"
				}
				prompt += piped
			}
		}
	}
	if prompt == "" {
		fs.Usage()
		return errors.New("no prompt given (pass one as an argument or pipe it on stdin)")
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	prov, mdl, apiID, err := cfg.Resolve(*modelFlag, *providerFlag)
	if err != nil {
		return err
	}
	modelName, provName := *modelFlag, *providerFlag
	if modelName == "" {
		modelName = cfg.DefaultModel
	}
	if provName == "" {
		provName = cfg.DefaultProvider
		if provName == "" && len(mdl.Providers) > 0 {
			provName = mdl.Providers[0]
		}
	}
	client, err := runClientForProvider(prov, provName, cfg.MaxRetries)
	if err != nil {
		return err
	}

	// System prompt: -system-file wins over -system (a file is the deliberate
	// choice; a stray -system alongside it is almost certainly stale).
	sys := systemPrompt()
	if *systemFlag != "" {
		sys = *systemFlag
	}
	if *systemFileFlag != "" {
		data, err := os.ReadFile(*systemFileFlag)
		if err != nil {
			return fmt.Errorf("-system-file: %w", err)
		}
		sys = string(data)
	}

	maxOut := mdl.MaxOut
	if maxOut == 0 {
		maxOut = mdl.ContextWindow()
	}
	ag := agent.New(client, apiID, maxOut, sys)
	ag.ModelName, ag.Provider = modelName, provName
	// Headless runs have no one to answer a consent prompt: computer_exec
	// stays disabled (no interactive approver is ever installed).
	ag.ComputerDisabled = true
	ag.ContextLimit = mdl.ContextWindow()
	ag.Effort = cfg.DefaultEffort
	if ag.Effort == "" {
		ag.Effort = "medium"
	}
	ag.MaxTurns = *maxTurnsFlag

	// Session: resume an existing one, or create a fresh one — unless
	// -no-session (a one-off cron job shouldn't clutter whip sessions).
	var store *session.Store
	var sessionID string
	if !*noSessionFlag {
		if dir, derr := config.Dir(); derr == nil {
			if st, serr := session.Open(dir + "/sessions.db"); serr == nil {
				store = st
				defer func() { _ = st.Close() }()
			}
		}
	}
	if store != nil {
		if *resumeFlag != "" {
			meta, msgs, lerr := store.Load(*resumeFlag)
			if lerr != nil {
				return fmt.Errorf("-resume: %w", lerr)
			}
			sessionID = meta.ID
			ag.Messages = append(ag.Messages[:1], msgs[1:]...) // keep our system prompt, replay the rest
		} else if cwd, cerr := os.Getwd(); cerr == nil {
			if id, ierr := store.Create(cwd, modelName, provName); ierr == nil {
				sessionID = id
			}
		}
	}

	// ctrl+c cancels the turn; -timeout caps the whole run.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if *timeoutFlag > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, *timeoutFlag)
		defer cancel()
	}

	ev := agent.Events{}
	var emit func(any) // set only for --format json
	note := func(format string, a ...any) {
		if !*quietFlag {
			fmt.Fprintf(os.Stderr, format+"\n", a...)
		}
	}
	if *format == "json" {
		enc := json.NewEncoder(os.Stdout)
		emit = func(v any) {
			if err := enc.Encode(v); err != nil {
				fmt.Fprintln(os.Stderr, "whip: json encode:", err)
			}
		}
		ev.OnText = func(d string) { emit(map[string]string{"type": "text", "delta": d}) }
		ev.OnToolStart = func(_, name, args string) {
			emit(map[string]string{"type": "tool_start", "name": name, "args": args})
		}
		ev.OnToolEnd = func(_, name, result string) {
			emit(map[string]string{"type": "tool_end", "name": name, "result": result})
		}
	} else {
		ev.OnText = func(d string) { fmt.Fprint(os.Stdout, d) }
		ev.OnToolStart = func(_, name, args string) { note("⚒ %s", name) }
	}

	final, err := ag.Turn(ctx, prompt, ev)
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		err = fmt.Errorf("run timed out after %s", *timeoutFlag)
	}
	if emit != nil {
		if err != nil {
			emit(map[string]string{"type": "error", "error": err.Error()})
		} else {
			emit(map[string]string{"type": "done", "text": final})
		}
	} else {
		fmt.Fprintln(os.Stdout) // end the streamed reply's line
	}

	// Best-effort persistence (the TUI's persist does the same each turn).
	// Save from index 0: Load re-derives the system-prompt slot, so a resumed
	// conversation must not skip it (saving from 1 shifts everything off).
	if store != nil && sessionID != "" {
		if serr := store.Save(sessionID, 0, ag.MessagesSnapshot(), modelName, provName); serr != nil {
			config.LogEvent("session.save", "run FAILED id="+sessionID+": "+serr.Error())
		}
		note("session %s — resume with: whip run -resume %s \"…\" · or interactively: whip --resume %s", sessionID, sessionID, sessionID)
	}
	return err
}

func runClientForProvider(prov config.Provider, name string, maxRetries int) (llm.Client, error) {
	switch prov.API {
	case "", "openai-completions":
		key, err := prov.ResolveKey()
		if err != nil {
			return nil, err
		}
		if key == "" {
			return nil, fmt.Errorf("no API key for provider %q (set apiKey/apiKeyEnv in ~/.whip/config.json)", name)
		}
		client := llm.New(prov.BaseURL, key)
		client.MaxRetries = maxRetries
		return client, nil
	case "openai-codex-responses":
		if prov.Auth != "codex" {
			return nil, fmt.Errorf("codex provider %q requires auth:\"codex\"", name)
		}
		if strings.TrimRight(prov.BaseURL, "/") != config.CodexBaseURL {
			return nil, fmt.Errorf("codex provider %q must use %s", name, config.CodexBaseURL)
		}
		source := &codexauth.Source{}
		if err := source.Available(); err != nil {
			return nil, err
		}
		return llm.NewCodex(prov.BaseURL, source), nil
	default:
		return nil, fmt.Errorf("unsupported API %q for provider %q", prov.API, name)
	}
}
