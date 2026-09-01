// `whip run` — non-interactive (headless) mode: one turn of the agent with
// no TUI and no trust prompt, for trusted automation and scripting. Piped
// stdin is appended to the prompt. --format json emits the raw event stream
// as newline-delimited JSON: {"type":"reasoning","delta":...} for thinking
// tokens, {"type":"text","delta":...} for reply text, {"type":"tool_start"/
// "tool_end",...} for tool calls; the final event is {"type":"done",...} or
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
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/context-labs/whip/internal/agent"
	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/tui"
)

func runCLI(args []string) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	format := fs.String("format", "text", "output format: text (stream the reply) or json (newline-delimited event stream)")
	modelFlag := fs.String("m", "", "model name from ~/.whip/config.json (default: defaultModel)")
	providerFlag := fs.String("p", "", "provider to route the model through (default: model's first provider)")
	resumeFlag := fs.String("resume", "", "continue this session id (see `whip sessions`) instead of starting fresh")
	systemFlag := fs.String("system", "", "override the system prompt for this run")
	systemFileFlag := fs.String("system-file", "", "read the system prompt from this file (wins over -system)")
	maxTurnsFlag := fs.Int("max-turns", 0, "cap the tool-call loop at N rounds (0 = uncapped); on the cap, the model makes one final no-tools answer instead of erroring")
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
	prov, mdl, apiID, err := tui.ResolveWithRefresh(cfg, *modelFlag, *providerFlag)
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
	key := prov.Key()
	if key == "" {
		return fmt.Errorf("no API key for provider %q (set apiKey/apiKeyEnv in ~/.whip/config.json)", provName)
	}

	client := llm.New(prov.BaseURL, key)
	client.MaxRetries = cfg.MaxRetries

	// System prompt: -system-file wins over -system (a file is the deliberate
	// choice; a stray -system alongside it is almost certainly stale).
	sys := systemPrompt(cwd(), time.Now())
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

	ag := agent.New(client, apiID, mdl.MaxTokens, sys)
	ag.ModelName, ag.Provider = modelName, provName
	// Headless runs have no one to answer a consent prompt: computer_exec
	// stays disabled (no interactive approver is ever installed).
	ag.ComputerDisabled = true
	ag.ContextLimit = mdl.ContextWindow()
	// Reasoning effort: explicit cfg.DefaultEffort wins; "" resolves
	// model-aware — "low" when the model advertises it, else the lowest
	// supported level, else off — so a non-reasoning model never sends an
	// effort parameter the provider would reject.
	ag.Effort = tui.DefaultEffortFor(config.LoadCatalogs(), provName, ag.Model, cfg.DefaultEffort)
	ag.MaxTurns = *maxTurnsFlag

	// Session: persistent unless -no-session, where a temporary store still
	// supplies the capability ledger without cluttering session history.
	var store *session.Store
	var sessionID string
	persistSession := !*noSessionFlag
	if !*noSessionFlag {
		if dir, derr := config.Dir(); derr == nil {
			if st, serr := session.Open(dir + "/sessions.db"); serr == nil {
				store = st
				defer func() { _ = st.Close() }()
			}
		}
	}
	if store == nil && *resumeFlag == "" {
		tmp, terr := os.MkdirTemp("", "whip-run-")
		if terr != nil {
			return terr
		}
		defer func() { _ = os.RemoveAll(tmp) }()
		store, err = session.Open(filepath.Join(tmp, "sessions.db"))
		if err != nil {
			return err
		}
		defer func() { _ = store.Close() }()
		persistSession = false
	}
	if store == nil && *resumeFlag != "" {
		return errors.New("-resume: session store unavailable")
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
	if sessionID == "" {
		return errors.New("session unavailable")
	}
	authority, err := store.EnsureClassicAuthority(context.Background(), sessionID)
	if err != nil {
		return err
	}
	ag.Services.SetProcessMarkers(sessionID, ag.Model)
	if err := ag.Services.BindDispatcher(store, store.Workspaces(), store.Processes(), authority); err != nil {
		return err
	}
	defer ag.Services.Close()
	defer ag.Close()
	ag.SetSessionID(sessionID)
	ag.Tasks().SetSessionID(sessionID)

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
		ev.OnThink = func(d string) {
			emit(map[string]string{"type": "reasoning", "delta": d})
		}
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

	// Subagent routing: same default chain as the TUI (taskModel → built-in
	// default → catalog suffix → the run's own model). Headless runs never
	// mutate cfg, so no snapshot is needed for the resolver.
	ag.ResolveModel = func(model, provider string) (agent.SubModel, error) {
		return tui.SubModelFor(cfg, model, provider)
	}
	if o, terr := tui.TaskDefaultFor(cfg); terr == nil {
		ag.TaskDefault = o
	} else {
		note("task model: %v — subagents use the run's model", terr)
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
	if persistSession {
		if serr := store.Save(sessionID, 0, ag.MessagesSnapshot(), modelName, provName); serr != nil {
			config.LogEvent("session.save", "run FAILED id="+sessionID+": "+serr.Error())
		}
		note("session %s — resume with: whip run -resume %s \"…\" · or interactively: whip --resume %s", sessionID, sessionID, sessionID)
	}
	return err
}
