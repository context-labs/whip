package computer

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/context-labs/whip/internal/computerconfig"
	"github.com/context-labs/whip/internal/helperprogram"
)

const MaxBatchApplications = 16

// BatchIntent is the complete static consent scope. Applications are canonical
// labels, resolved to unique exact app identities only inside approved execution.
// BroadScript explicitly means arbitrary AppleScript; its App argument is not a
// confinement boundary. Policy must reject it while any hard-deny rule exists.
type BatchIntent struct {
	Applications []string `json:"applications"`
	BroadScript  bool     `json:"broad_applescript"`
	Inventory    bool     `json:"application_inventory"`
	Permissions  bool     `json:"request_os_permissions"`
	Native       bool     `json:"native_helper"`
}

// Batch is a bounded immutable plan. It contains no helper, authority, process,
// live AX index or replay position. A caller must authorize the entire intent
// before executing its first step; partial failures cannot resume this plan.
type Batch struct {
	steps  []computerStep
	intent BatchIntent
}

type computerStep struct {
	method string
	app    string
	params map[string]any
	text   string
}

func (b *Batch) Intent() BatchIntent {
	intent := b.intent
	intent.Applications = slices.Clone(intent.Applications)
	return intent
}

// Resource binds ordinary exact-match SQL grants to a process/policy generation
// and full app set. The unhashed Intent must accompany permission arguments.
func (b *Batch) Resource(generation string) (string, error) {
	if generation == "" || len(generation) > 128 || strings.ContainsAny(generation, "\x00\r\n/") {
		return "", errors.New("computer generation required")
	}
	raw, _ := json.Marshal(b.intent)
	digest := sha256.Sum256(raw)
	return "computer:" + generation + ":" + hex.EncodeToString(digest[:]), nil
}

// CompileBatch ports the retained helper vocabulary without executing any
// statement, resolving an app, opening a helper or consulting permission state.
func CompileBatch(code string) (*Batch, error) {
	calls, err := helperprogram.Parse(code)
	if err != nil {
		return nil, err
	}
	batch := &Batch{intent: BatchIntent{Applications: []string{}}}
	for index, call := range calls {
		step, err := compileComputerStep(call)
		if err != nil {
			return nil, fmt.Errorf("computer statement %d: %w", index+1, err)
		}
		batch.steps = append(batch.steps, step)
		if step.app != "" && !slices.Contains(batch.intent.Applications, step.app) {
			if len(batch.intent.Applications) >= MaxBatchApplications {
				return nil, errors.New("computer batch application limit exceeded")
			}
			batch.intent.Applications = append(batch.intent.Applications, step.app)
		}
		switch step.method {
		case "print":
		case "tell":
			batch.intent.BroadScript = true
		case "apps":
			batch.intent.Inventory = true
			batch.intent.Native = true
		case "permissions.request":
			batch.intent.Permissions = true
			batch.intent.Native = true
		default:
			if !strings.HasPrefix(step.method, "chrome_") {
				batch.intent.Native = true
			}
		}
	}
	slices.Sort(batch.intent.Applications)
	return batch, nil
}

func compileComputerStep(call helperprogram.Call) (computerStep, error) {
	if call.Name == "print" {
		if nested, ok := call.Arguments[0].(helperprogram.Call); ok {
			return compileComputerStep(nested)
		}
		text, ok := call.Arguments[0].(string)
		if !ok {
			encoded, err := json.Marshal(call.Arguments[0])
			if err != nil {
				return computerStep{}, errors.New("invalid printed value")
			}
			text = string(encoded)
		}
		return computerStep{method: "print", text: text}, nil
	}
	step := computerStep{method: call.Name, params: map[string]any{}}
	count := len(call.Arguments)
	arity := func(minimum, maximum int) error {
		if count < minimum || count > maximum {
			return errors.New("wrong computer helper argument count")
		}
		return nil
	}
	text := func(index, limit int) (string, error) {
		if index >= count {
			return "", errors.New("missing computer helper text")
		}
		value, ok := call.Arguments[index].(string)
		if !ok || len(value) > limit || !utf8.ValidString(value) || strings.ContainsRune(value, 0) {
			return "", errors.New("invalid computer helper text")
		}
		return value, nil
	}
	number := func(index int, minimum, maximum int64) (int64, error) {
		if index >= count {
			return 0, errors.New("missing computer helper integer")
		}
		value, ok := call.Arguments[index].(json.Number)
		if !ok {
			return 0, errors.New("computer helper requires an integer")
		}
		integer, err := strconv.ParseInt(value.String(), 10, 64)
		if err != nil || integer < minimum || integer > maximum {
			return 0, errors.New("computer helper integer is out of range")
		}
		return integer, nil
	}
	application := func() error {
		value, err := text(0, 256)
		if err != nil {
			return err
		}
		value, err = computerconfig.CanonicalApp(value)
		if err != nil {
			return err
		}
		step.app = value
		return nil
	}
	switch call.Name {
	case "apps", "permissions", "chrome_state", "chrome_tabs", "chrome_back", "chrome_reload":
		if err := arity(0, 0); err != nil {
			return step, err
		}
		if call.Name == "permissions" {
			step.method = "permissions.request"
		}
	case "state", "ax", "screenshot":
		if err := arity(1, 1); err != nil {
			return step, err
		}
		if err := application(); err != nil {
			return step, err
		}
	case "tell", "type", "press":
		if err := arity(2, 2); err != nil {
			return step, err
		}
		if err := application(); err != nil {
			return step, err
		}
		limit := helperprogram.MaxBytes
		if call.Name == "press" {
			limit = 128
		}
		value, err := text(1, limit)
		if err != nil {
			return step, err
		}
		key := map[string]string{"tell": "script", "type": "text", "press": "key"}[call.Name]
		step.params[key] = value
	case "click":
		if err := arity(2, 3); err != nil {
			return step, err
		}
		if err := application(); err != nil {
			return step, err
		}
		if count == 2 {
			index, err := number(1, 0, 100000)
			if err != nil {
				return step, err
			}
			step.params["index"] = index
		} else {
			x, err := number(1, -1000000, 1000000)
			if err != nil {
				return step, err
			}
			y, err := number(2, -1000000, 1000000)
			if err != nil {
				return step, err
			}
			step.params["x"], step.params["y"] = x, y
		}
	case "scroll", "set", "select", "menu":
		minimum, maximum := 3, 3
		if call.Name == "scroll" {
			minimum, maximum = 2, 4
		}
		if call.Name == "select" {
			minimum = 2
		}
		if err := arity(minimum, maximum); err != nil {
			return step, err
		}
		if err := application(); err != nil {
			return step, err
		}
		index, err := number(1, 0, 100000)
		if err != nil {
			return step, err
		}
		step.params["index"] = index
		if count >= 3 {
			value, err := text(2, helperprogram.MaxBytes)
			if err != nil {
				return step, err
			}
			key := map[string]string{"scroll": "dir", "set": "value", "select": "target", "menu": "action"}[call.Name]
			if call.Name == "scroll" && !slices.Contains([]string{"up", "down", "left", "right"}, value) {
				return step, errors.New("invalid computer scroll direction")
			}
			step.params[key] = value
		}
		if count == 4 {
			clicks, err := number(3, 1, 1000)
			if err != nil {
				return step, err
			}
			step.params["clicks"] = clicks
		}
	case "chrome_goto", "chrome_new_tab", "chrome_js", "chrome_find":
		if err := arity(1, 1); err != nil {
			return step, err
		}
		value, err := text(0, helperprogram.MaxBytes)
		if err != nil {
			return step, err
		}
		if call.Name == "chrome_goto" || call.Name == "chrome_new_tab" {
			parsed, err := url.Parse(value)
			if err != nil || parsed.Host == "" || parsed.User != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
				return step, errors.New("computer browser navigation requires an HTTP(S) URL without credentials")
			}
		}
		step.params["value"] = value
	case "chrome_activate", "chrome_close":
		if err := arity(2, 2); err != nil {
			return step, err
		}
		window, err := number(0, 1, 100000)
		if err != nil {
			return step, err
		}
		index, err := number(1, 1, 100000)
		if err != nil {
			return step, err
		}
		step.params["window"], step.params["index"] = window, index
	default:
		return step, errors.New("unknown computer helper")
	}
	if strings.HasPrefix(call.Name, "chrome_") {
		step.app = "google chrome"
	}
	return step, nil
}
