package computer

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"

	"github.com/context-labs/whip/internal/browser"
	"github.com/context-labs/whip/internal/capability"
)

type boundedScriptOutput struct {
	mu       sync.Mutex
	value    []byte
	overflow bool
}

func (b *boundedScriptOutput) Write(data []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	count := len(data)
	remaining := (256 << 10) - len(b.value)
	if count > remaining {
		b.overflow = true
		data = data[:remaining]
	}
	b.value = append(b.value, data...)
	return count, nil
}

func (l *Lease) scriptRunner(check func() error) CommandRunner {
	return func(ctx context.Context, _ string, args ...string) ([]byte, error) {
		if err := check(); err != nil {
			return nil, err
		}
		c := l.capture.controller
		if c.scriptCommand != nil {
			output, err := c.scriptCommand(ctx, "/usr/bin/osascript", args...)
			if err != nil {
				if ctx.Err() != nil {
					return nil, ctx.Err()
				}
				return nil, errors.New("computer AppleScript failed; effects may have occurred")
			}
			if len(output) > 256<<10 {
				return nil, errors.New("computer AppleScript output exceeded bound; effects may have occurred")
			}
			return output, nil
		}
		output := &boundedScriptOutput{}
		process, err := c.processes.Start(ctx, c.owner, "/usr/bin/osascript", args, capability.ProcessOptions{Cwd: c.directory, Env: c.environment, Stdin: strings.NewReader(""), Stdout: output, Stderr: io.Discard})
		if err != nil {
			return nil, errors.New("cannot start computer AppleScript process")
		}
		err = process.Wait()
		process.Stop()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if err != nil {
			return nil, errors.New("computer AppleScript failed; effects may have occurred")
		}
		output.mu.Lock()
		defer output.mu.Unlock()
		if output.overflow {
			return nil, errors.New("computer AppleScript output exceeded bound; effects may have occurred")
		}
		return output.value, nil
	}
}

func (l *Lease) scriptStep(ctx context.Context, step computerStep, check func() error) (string, error) {
	if !Available() && l.capture.controller.scriptCommand == nil {
		return "", ErrUnsupportedPlatform
	}
	if err := l.capture.epoch.config.Check(step.app); err != nil {
		return "", err
	}
	if strings.HasPrefix(step.method, "chrome_") {
		if err := l.capture.epoch.config.Check("com.google.Chrome"); err != nil {
			return "", err
		}
	}
	automation := Automation{Context: ctx, Run: l.scriptRunner(check)}
	value, _ := step.params["value"].(string)
	completed := func(text string, err error) (string, error) {
		if err != nil {
			return "", err
		}
		return text, nil
	}
	switch step.method {
	case "tell":
		return automation.Tell(step.app, step.params["script"].(string))
	case "chrome_state":
		return ChromeState(automation)
	case "chrome_tabs":
		tabs, err := ChromeTabs(automation)
		if err != nil {
			return "", err
		}
		raw, _ := json.Marshal(tabs)
		return string(raw), nil
	case "chrome_goto", "chrome_new_tab":
		if err := browser.CheckURL(ctx, value); err != nil {
			return "", errors.New("computer browser destination is blocked or unavailable")
		}
		var err error
		if step.method == "chrome_goto" {
			err = ChromeGoto(value, automation)
		} else {
			err = ChromeNewTab(value, automation)
		}
		if err != nil {
			return "", err
		}
		return "navigation dispatched", nil
	case "chrome_activate":
		return completed("tab activated", ChromeActivateTab(int(step.params["window"].(int64)), int(step.params["index"].(int64)), automation))
	case "chrome_close":
		return completed("tab closed", ChromeCloseTab(int(step.params["window"].(int64)), int(step.params["index"].(int64)), automation))
	case "chrome_back":
		return completed("back dispatched", ChromeBack(automation))
	case "chrome_reload":
		return completed("reload dispatched", ChromeReload(automation))
	case "chrome_js":
		return ChromeJS(value, automation)
	case "chrome_find":
		tab, err := ChromeFindTab(value, automation)
		if err != nil {
			return "", err
		}
		raw, _ := json.Marshal(tab)
		return string(raw), nil
	default:
		return "", errors.New("unsupported computer script operation")
	}
}
