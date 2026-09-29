// whipcode run is a native one-turn client. The backend owns execution,
// configuration, permissions, persistence, and cancellation settlement.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"math/big"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/context-labs/whip/internal/client"
	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/runclient"
)

func runCLI(args []string) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	format := fs.String("format", "text", "output format: text or newline-delimited JSON events")
	model := fs.String("m", "", "model name (default: native host or resumed session selection)")
	provider := fs.String("p", "", "native provider route (default: native host or resumed session selection)")
	maxCost := fs.String("max-cost", "0", "maximum whole-session model cost in USD (0 leaves the current cap unchanged)")
	maxTokens := fs.Int64("max-tokens", 0, "maximum whole-session model tokens (0 leaves the current cap unchanged)")
	effort := fs.String("effort", "", "reasoning effort for this session")
	permission := fs.String("permission-mode", "", "new session permission mode: prompt or automatic")
	engine := fs.String("rlm-engine", "", "execution language: starlark or quickjs (immutable on resume)")
	agent := fs.String("agent", "", "registered agent id or id@revision (default coding; also junior-developer and assistant; immutable on resume)")
	resume := fs.String("resume", "", "continue this native session id")
	system := fs.String("system", "", "override the system prompt for this run")
	systemFile := fs.String("system-file", "", "read the system prompt from this file (wins over -system)")
	maxTurns := fs.Int("max-turns", 0, "maximum tool-call rounds (0 uncapped); one final no-tools answer at the cap")
	timeout := fs.Duration("timeout", 0, "wall-clock limit (0 unlimited); expiration explicitly cancels this input")
	quiet := fs.Bool("quiet", false, "suppress stderr tool/session notes")
	noSession := fs.Bool("no-session", false, "delete this session and its children after the run")
	cacheKey := fs.String("cache-key", "", "provider cache affinity (defaults to the session id)")
	recordPath := fs.String("record", "", "save the exact run admission to this new private file")
	recoverPath := fs.String("recover", "", "check and observe a saved run admission without resubmitting it")
	retry := fs.Bool("retry", false, "with -recover, explicitly retry the exact original input only if its receipt is absent")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: whipcode run [flags] \"prompt\" | whipcode run -recover file [-retry]")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	cost, err := runCost(*maxCost)
	if err != nil {
		return err
	}
	if *format != "text" && *format != "json" {
		return fmt.Errorf("unknown --format %q (want text|json)", *format)
	}
	if *engine != "" && *engine != "starlark" && *engine != "quickjs" {
		return fmt.Errorf("unknown --rlm-engine %q", *engine)
	}
	if *permission != "" && *permission != "prompt" && *permission != "automatic" {
		return fmt.Errorf("unknown --permission-mode %q", *permission)
	}
	if *resume != "" && *permission != "" {
		return errors.New("--permission-mode selects a new session; cannot combine with --resume")
	}
	if *maxTokens < 0 || *maxTurns < 0 || *maxTurns > 1000000 || *timeout < 0 {
		return errors.New("run limits must be nonnegative; max-turns must not exceed 1000000")
	}
	if *retry && *recoverPath == "" {
		return errors.New("--retry requires --recover")
	}
	if *recoverPath != "" {
		if fs.NArg() != 0 || *model != "" || *provider != "" || *effort != "" || *permission != "" || *engine != "" || *agent != "" || *resume != "" || *system != "" || *systemFile != "" || *maxTurns != 0 || cost != 0 || *maxTokens != 0 || *cacheKey != "" || *recordPath != "" {
			return errors.New("--recover preserves the original input; prompt/configuration/record flags cannot be combined with it")
		}
	}
	prompt := ""
	var recovered []byte
	if *recoverPath != "" {
		recovered, err = readRunRecord(*recoverPath)
		if err != nil {
			return err
		}
	} else {
		prompt = strings.Join(fs.Args(), " ")
		if info, err := os.Stdin.Stat(); err == nil && info.Mode()&os.ModeCharDevice == 0 {
			data, err := io.ReadAll(io.LimitReader(os.Stdin, (1<<20)+1))
			if err != nil {
				return fmt.Errorf("read prompt: %w", err)
			}
			if len(data) > 1<<20 {
				return errors.New("piped prompt exceeds 1 MiB")
			}
			if piped := strings.TrimSpace(string(data)); piped != "" {
				if prompt != "" {
					prompt += "\n\n"
				}
				prompt += piped
			}
		}
		if prompt == "" {
			return errors.New("no prompt given (pass one as an argument or pipe it on stdin)")
		}
		if len(prompt) > 1<<20 {
			return errors.New("prompt exceeds 1 MiB")
		}
		if *systemFile != "" {
			data, err := readRunSystemFile(*systemFile)
			if err != nil {
				return fmt.Errorf("-system-file: %w", err)
			}
			*system = string(data)
		}
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if *timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, *timeout)
		defer cancel()
	}
	c, err := connectNativeRuntime(ctx)
	if err != nil {
		return runContextError(err, *timeout)
	}
	defer func() { _ = c.Close() }()
	output, err := runclient.NewOutput(*format, *quiet, os.Stdout, os.Stderr)
	if err != nil {
		return err
	}
	var result runclient.Result
	defaultRecord := false
	savedPath := ""
	if *recoverPath != "" {
		savedPath = *recoverPath
		result, err = runclient.Recover(ctx, c, recovered, *retry, output)
	} else {
		result, err = runclient.Run(ctx, c, runclient.Options{Resume: protocol.ID(*resume), Agent: *agent, Engine: *engine, WorkingDirectory: cwd(), Model: *model, Provider: *provider, Effort: *effort, PermissionMode: *permission, Configuration: protocol.RunConfiguration{System: *system, MaxTurns: *maxTurns, CacheKey: *cacheKey}, Prompt: prompt, CostNanoUSD: cost, Tokens: protocol.Counter(*maxTokens), Record: func(record client.InputRecord) error {
			if savedPath != "" {
				return nil
			} // Original exact bytes suffice for receipt recovery.
			selected := *recordPath
			if selected == "" {
				paths, pathErr := nativeRuntimePaths()
				if pathErr != nil {
					return pathErr
				}
				var params protocol.SubmitParams
				if err := json.Unmarshal(record.Params, &params); err != nil {
					return err
				}
				directory := filepath.Join(filepath.Dir(paths.Directory), "client-v4", "runs")
				if err := prepareRunRecordDirectory(directory); err != nil {
					return err
				}
				selected = filepath.Join(directory, string(params.Identity.RequestID)+".json")
				defaultRecord = true
			}
			if err := saveRunRecord(selected, record); err != nil {
				return err
			}
			savedPath = selected
			return nil
		}}, output)
	}
	if ctx.Err() != nil {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		cancelErr := runclient.Cancel(cleanup, c, result)
		if cancelErr == nil && result.Record != nil {
			var params protocol.SubmitParams
			if json.Unmarshal(result.Record.Params, &params) == nil {
				result.Admission, cancelErr = c.Wait(cleanup, params.Identity)
				if result.Admission.Input != nil {
					result.SessionID = result.Admission.Input.SessionID
				}
			}
		}
		cancel()
		err = errors.Join(runContextError(ctx.Err(), *timeout), cancelErr)
	}
	terminal := result.Admission.Turn != nil && result.Admission.Turn.FinishedAt != nil || result.Admission.Input != nil && result.Admission.Input.State == "cancelled"
	if *noSession && result.SessionID != "" && result.Admission.Input != nil {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		deleteErr := deleteNativeRun(cleanup, c, result.SessionID)
		cancel()
		err = errors.Join(err, deleteErr)
		if deleteErr == nil {
			terminal = true
		}
	} else if result.SessionID != "" {
		output.Note("session %s — resume with: whipcode run -resume %s \"…\"", result.SessionID, result.SessionID)
	}
	if defaultRecord && terminal && savedPath != "" {
		err = errors.Join(err, os.Remove(savedPath))
	} else if savedPath != "" {
		if err != nil && !terminal {
			err = fmt.Errorf("%w; check this run with whipcode run -recover %q", err, savedPath)
		}
		output.Note("run recovery: whipcode run -recover %q", savedPath)
	}
	err = errors.Join(err, output.Finish(err))
	return err
}

func runContextError(err error, timeout time.Duration) error {
	if errors.Is(err, context.DeadlineExceeded) && timeout > 0 {
		return fmt.Errorf("run timed out after %s", timeout)
	}
	return err
}

func runCost(text string) (protocol.Counter, error) {
	if len(text) > 128 {
		return 0, errors.New("--max-cost is too large")
	}
	approximate, parseErr := strconv.ParseFloat(text, 64)
	if math.IsInf(approximate, 1) || approximate > float64(math.MaxInt64)/1e9 {
		return 0, errors.New("--max-cost is too large")
	}
	if parseErr != nil || math.IsNaN(approximate) || math.IsInf(approximate, -1) || approximate < 0 || strings.ContainsAny(text, "xX_/") {
		return 0, errors.New("--max-cost must be finite and nonnegative")
	}
	if approximate == 0 {
		mantissa, _, _ := strings.Cut(strings.ToLower(text), "e")
		if strings.Trim(mantissa, "+-.0") != "" {
			return 0, errors.New("--max-cost must be at least $0.000001 when nonzero")
		}
		return 0, nil
	}
	value, ok := new(big.Rat).SetString(text)
	if !ok || value.Sign() < 0 || strings.Contains(text, "/") {
		return 0, errors.New("--max-cost must be finite and nonnegative")
	}
	if value.Sign() != 0 && value.Cmp(big.NewRat(1, 1000000)) < 0 {
		return 0, errors.New("--max-cost must be at least $0.000001 when nonzero")
	}
	value.Mul(value, big.NewRat(1000000000, 1))
	// Round positive values to the nearest nano-USD without float overflow.
	value.Add(value, big.NewRat(1, 2))
	nano := new(big.Int).Quo(value.Num(), value.Denom())
	if !nano.IsInt64() {
		return 0, errors.New("--max-cost is too large to represent in nano-USD")
	}
	return protocol.Counter(nano.Int64()), nil
}
