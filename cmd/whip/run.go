// `whip run` is a one-turn automation client for the daemon. It preserves the
// text and NDJSON contracts while the daemon owns provider execution, tools,
// persistence, permissions, schedules, and child processes.
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
	"time"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/daemon"
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
	noSessionFlag := fs.Bool("no-session", false, "run without retaining a session (one-off jobs don't clutter whip sessions)")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: whip run [--format text|json] [-m model] [-p provider] [-resume id] [-system text | -system-file path] [-max-turns N] [-timeout dur] [-quiet] [-no-session] \"prompt\"")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *format != "text" && *format != "json" {
		return fmt.Errorf("unknown --format %q (want text|json)", *format)
	}
	if *maxTurnsFlag < 0 {
		return errors.New("-max-turns cannot be negative")
	}

	prompt := strings.Join(fs.Args(), " ")
	if fi, err := os.Stdin.Stat(); err == nil && fi.Mode()&os.ModeCharDevice == 0 {
		if data, readErr := io.ReadAll(os.Stdin); readErr == nil {
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
	_, model, _, err := tui.ResolveWithRefresh(cfg, *modelFlag, *providerFlag)
	if err != nil {
		return err
	}
	modelName, providerName := *modelFlag, *providerFlag
	if modelName == "" {
		modelName = cfg.DefaultModel
	}
	if providerName == "" {
		providerName = cfg.DefaultProvider
		if providerName == "" && len(model.Providers) > 0 {
			providerName = model.Providers[0]
		}
	}

	system := ""
	if *systemFlag != "" {
		system = *systemFlag
	}
	if *systemFileFlag != "" {
		data, readErr := os.ReadFile(*systemFileFlag)
		if readErr != nil {
			return fmt.Errorf("-system-file: %w", readErr)
		}
		system = string(data)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if *timeoutFlag > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, *timeoutFlag)
		defer cancel()
	}

	clientID := daemonClientID("run")
	options := daemon.RootClientOptions{
		ClientID:  clientID,
		RootID:    *resumeFlag,
		Connector: daemonConnector("automation", clientID),
	}
	if *resumeFlag == "" {
		options.Create = &daemon.CreateSession{CWD: cwd(), Model: modelName, Provider: providerName}
	}
	client, err := daemon.NewRootClient(options)
	if err != nil {
		return err
	}
	client.Start()
	defer func() { _ = client.Close() }()
	if err := client.WaitLive(ctx); err != nil {
		return runContextError(err, *timeoutFlag)
	}

	configure, err := client.NewAction("run.configure", map[string]any{
		"system": system, "max_turns": *maxTurnsFlag, "headless": true,
	})
	if err != nil {
		return err
	}
	configured, err := client.Command(ctx, configure)
	if err != nil {
		return runContextError(err, *timeoutFlag)
	}
	if configured.Status != "succeeded" {
		return errors.New(configured.Error)
	}

	baseline := client.Cursor()
	action, err := client.NewAction("submit", daemon.SubmitPayload{Text: prompt})
	if err != nil {
		return err
	}
	type commandReply struct {
		result daemon.CommandResult
		err    error
	}
	replies := make(chan commandReply, 1)
	go func() {
		result, commandErr := client.Command(ctx, action)
		replies <- commandReply{result: result, err: commandErr}
	}()

	output := newRunOutput(*format, *quietFlag)
	var reply commandReply
	terminalSeen := false
	for !terminalSeen || reply.result.CommandID == "" && reply.err == nil {
		select {
		case <-ctx.Done():
			cancelRun(client, action.RootID)
			err = runContextError(ctx.Err(), *timeoutFlag)
			output.finish("", err)
			if *noSessionFlag {
				_ = deleteDaemonSession(clientID, action.RootID)
			}
			return err
		case update, ok := <-client.Updates():
			if !ok {
				if reply.result.CommandID == "" && reply.err == nil {
					return errors.New("daemon client stopped before the run completed")
				}
				terminalSeen = true
				continue
			}
			if update.Event == nil || update.Event.Seq <= baseline {
				continue
			}
			output.event(*update.Event)
			if update.Event.Kind == "turn.succeeded" || update.Event.Kind == "turn.failed" {
				terminalSeen = true
			}
		case reply = <-replies:
			if reply.err != nil {
				terminalSeen = true
			}
		}
	}
	if reply.err != nil {
		err = runContextError(reply.err, *timeoutFlag)
	} else if reply.result.Status != "succeeded" {
		err = errors.New(reply.result.Error)
	}
	output.finish(reply.result.Output, err)

	rootID := client.RootID()
	if *noSessionFlag {
		if closeErr := client.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
		if deleteErr := deleteDaemonSession(clientID, rootID); deleteErr != nil && err == nil {
			err = deleteErr
		}
	} else {
		output.note("session %s — resume with: whip run -resume %s \"…\" · or interactively: whip --resume %s", rootID, rootID, rootID)
	}
	return err
}

type runOutput struct {
	quiet bool
	enc   *json.Encoder
}

func newRunOutput(format string, quiet bool) *runOutput {
	output := &runOutput{quiet: quiet}
	if format == "json" {
		output.enc = json.NewEncoder(os.Stdout)
	}
	return output
}

func (o *runOutput) event(event daemon.ProtocolEvent) {
	var stream daemon.StreamEvent
	if strings.HasPrefix(event.Kind, "stream.") {
		if err := json.Unmarshal(event.Payload, &stream); err != nil {
			return
		}
	}
	switch event.Kind {
	case "stream.text":
		if o.enc != nil {
			o.emit(map[string]string{"type": "text", "delta": stream.Text})
		} else {
			fmt.Fprint(os.Stdout, stream.Text)
		}
	case "stream.reasoning":
		if o.enc != nil {
			o.emit(map[string]string{"type": "reasoning", "delta": stream.Text})
		}
	case "stream.tool.started":
		if o.enc != nil {
			o.emit(map[string]string{"type": "tool_start", "name": stream.Name, "args": stream.Args})
		} else {
			o.note("⚒ %s", stream.Name)
		}
	case "stream.tool.completed":
		if o.enc != nil {
			o.emit(map[string]string{"type": "tool_end", "name": stream.Name, "result": stream.Result})
		}
	case "permission.pending":
		if o.enc != nil {
			o.emit(map[string]string{"type": "permission_pending", "detail": string(event.Payload)})
		} else {
			o.note("permission pending: %s", event.Payload)
		}
	}
}

func (o *runOutput) finish(final string, err error) {
	if o.enc != nil {
		if err != nil {
			o.emit(map[string]string{"type": "error", "error": err.Error()})
		} else {
			o.emit(map[string]string{"type": "done", "text": final})
		}
		return
	}
	fmt.Fprintln(os.Stdout)
}

func (o *runOutput) emit(value any) {
	if err := o.enc.Encode(value); err != nil {
		fmt.Fprintln(os.Stderr, "whip: json encode:", err)
	}
}

func (o *runOutput) note(format string, args ...any) {
	if !o.quiet {
		fmt.Fprintf(os.Stderr, format+"\n", args...)
	}
}

func runContextError(err error, timeout time.Duration) error {
	if errors.Is(err, context.DeadlineExceeded) && timeout > 0 {
		return fmt.Errorf("run timed out after %s", timeout)
	}
	return err
}

func cancelRun(client *daemon.RootClient, rootID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	action, err := client.NewAction("cancel", map[string]string{})
	if err != nil {
		return
	}
	action.RootID = rootID
	_, _ = client.Command(ctx, action)
}

func deleteDaemonSession(clientID, rootID string) error {
	if rootID == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cleanupID := daemonCommandID(clientID, "cleanup")
	connection, err := connectDaemon(ctx, "automation", cleanupID, nil)
	if err != nil {
		return err
	}
	defer func() { _ = connection.Close() }()
	payload, err := json.Marshal(map[string]string{"root_id": rootID})
	if err != nil {
		return err
	}
	result, err := connection.Command(ctx, daemon.CommandParams{
		CommandID: cleanupID + "-delete-" + rootID,
		Scope:     string(session.CommandScopeDaemon),
		Operation: "session.delete",
		Payload:   payload,
	})
	if err != nil {
		return err
	}
	if result.Status != "succeeded" {
		return errors.New(result.Error)
	}
	return nil
}
