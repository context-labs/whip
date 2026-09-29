package browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/context-labs/whip/internal/helperprogram"
)

const MaxProgramOutput = 64 << 10

// Program is a fully parsed immutable desktop helper batch. It contains no live
// browser, execution cursor, permission, or resumable effect state.
type Program struct {
	steps       []programStep
	screenshots int
}
type programStep struct {
	method  string
	text    []string
	numbers []float64
	flag    bool
}

func (p *Program) Screenshots() int { return p.screenshots }

// ValidateTarget rejects a stale/foreign literal tab before any batch effect.
func (p *Program) ValidateTarget(target string) error {
	for _, s := range p.steps {
		if s.method == "useTab" && s.text[0] != target {
			return errors.New("browser program may use only its attached tab")
		}
	}
	return nil
}

func CompileProgram(code string) (*Program, error) {
	calls, e := helperprogram.Parse(code)
	if e != nil {
		return nil, e
	}
	program := &Program{}
	for i, c := range calls {
		step, err := compileProgramStep(c)
		if err != nil {
			return nil, fmt.Errorf("browser statement %d: %w", i+1, err)
		}
		program.steps = append(program.steps, step)
		if step.method == "screenshot" {
			program.screenshots++
		}
	}
	return program, nil
}

func compileProgramStep(c helperprogram.Call) (programStep, error) {
	if c.Name == "print" {
		if nested, ok := c.Arguments[0].(helperprogram.Call); ok {
			return compileProgramStep(nested)
		}
		text, ok := c.Arguments[0].(string)
		if !ok {
			raw, e := json.Marshal(c.Arguments[0])
			if e != nil {
				return programStep{}, errors.New("invalid printed value")
			}
			text = string(raw)
		}
		if len(text) > MaxProgramOutput || !utf8.ValidString(text) || strings.ContainsRune(text, 0) {
			return programStep{}, errors.New("printed text exceeds bounds")
		}
		return programStep{method: "print", text: []string{text}}, nil
	}
	step := programStep{method: c.Name}
	count := len(c.Arguments)
	arity := func(minimum, maximum int) error {
		if count < minimum || count > maximum {
			return errors.New("wrong browser helper argument count")
		}
		return nil
	}
	text := func(i, limit int) error {
		value, ok := c.Arguments[i].(string)
		if !ok {
			raw, e := json.Marshal(c.Arguments[i])
			if e != nil {
				return errors.New("invalid browser helper text")
			}
			value = string(raw)
		}
		if len(value) > limit || !utf8.ValidString(value) || strings.ContainsRune(value, 0) {
			return errors.New("browser helper text exceeds bounds")
		}
		step.text = append(step.text, value)
		return nil
	}
	number := func(i int) error {
		value, ok := c.Arguments[i].(json.Number)
		if !ok {
			return errors.New("browser helper requires a number")
		}
		f, e := value.Float64()
		if e != nil || math.IsNaN(f) || math.IsInf(f, 0) || math.Abs(f) > 1e7 {
			return errors.New("browser helper number exceeds bounds")
		}
		step.numbers = append(step.numbers, f)
		return nil
	}
	boolean := func(i int) error {
		value, ok := c.Arguments[i].(bool)
		if !ok {
			return errors.New("browser helper requires a boolean")
		}
		step.flag = value
		return nil
	}
	switch c.Name {
	case "back", "info", "waitLoad", "ax", "tabs", "screenshot":
		if e := arity(0, 0); e != nil {
			return step, e
		}
	case "goto", "js", "type", "press", "useTab":
		if e := arity(1, 1); e != nil {
			return step, e
		}
		limit := 64 << 10
		if c.Name == "goto" {
			limit = 8192
		}
		if c.Name == "press" || c.Name == "useTab" {
			limit = 128
		}
		if e := text(0, limit); e != nil {
			return step, e
		}
		if c.Name == "goto" {
			u, e := url.Parse(step.text[0])
			if e != nil || u.User != nil || (step.text[0] != "about:blank" && ((u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "")) {
				return step, errors.New("navigation requires HTTP, HTTPS or about:blank without credentials")
			}
			host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
			if alwaysBlockedHosts[host] {
				return step, errors.New("cloud metadata navigation is unavailable")
			}
			if ip, err := netip.ParseAddr(host); err == nil && ipBlocked(ip.Unmap()) {
				return step, errors.New("cloud metadata navigation is unavailable")
			}
		}
		if c.Name == "press" {
			if _, named := keyDefs[step.text[0]]; !named && utf8.RuneCountInString(step.text[0]) != 1 {
				return step, errors.New("unknown browser key")
			}
		}
	case "fill":
		if e := arity(2, 2); e != nil {
			return step, e
		}
		if e := text(0, 4096); e != nil {
			return step, e
		}
		if e := text(1, 64<<10); e != nil {
			return step, e
		}
	case "click":
		if e := arity(2, 2); e != nil {
			return step, e
		}
		if e := number(0); e != nil {
			return step, e
		}
		if e := number(1); e != nil {
			return step, e
		}
	case "scroll":
		if e := arity(0, 1); e != nil {
			return step, e
		}
		if count == 0 {
			step.numbers = []float64{-300}
		} else if e := number(0); e != nil {
			return step, e
		}
	case "waitFor":
		if e := arity(1, 2); e != nil {
			return step, e
		}
		if e := text(0, 4096); e != nil {
			return step, e
		}
		if count == 2 {
			if e := boolean(1); e != nil {
				return step, e
			}
		}
	case "box":
		if e := arity(1, 1); e != nil {
			return step, e
		}
		value, ok := c.Arguments[0].(json.Number)
		if !ok {
			return step, errors.New("box requires an exact positive node integer")
		}
		n, e := value.Int64()
		if e != nil || n < 1 || n > math.MaxInt32 {
			return step, errors.New("box node integer exceeds bounds")
		}
		step.numbers = []float64{float64(n)}
	case "dialog":
		if e := arity(0, 2); e != nil {
			return step, e
		}
		step.flag = true
		step.text = []string{""}
		if count > 0 {
			if e := boolean(0); e != nil {
				return step, e
			}
		}
		if count == 2 {
			step.text = nil
			if e := text(1, 64<<10); e != nil {
				return step, e
			}
		}
	case "upload":
		return step, errors.New("desktop browser uploads require authorized bytes; filesystem paths are unavailable")
	default:
		return step, errors.New("unknown browser helper")
	}
	return step, nil
}

// ProgramLimits comes from the exact cell's remaining committed attachment
// budget, while its host image reservation is held. Direct calls use per-operation
// limits. The sink durably publishes each observed image before the next effect.
type ProgramLimits struct {
	Images     int
	ImageBytes int
}

func (p *Program) Run(ctx context.Context, b Backend, limits ProgramLimits, sink func(context.Context, []byte) error) (output string, err error) {
	if b == nil || limits.Images < 0 || limits.Images > 8 || limits.ImageBytes < 0 || limits.ImageBytes > 16<<20 || (p.screenshots > 0 && sink == nil) {
		return "", errors.New("invalid browser program execution bounds")
	}
	defer func() {
		note, policyErr := checkProgramFinalURL(ctx, b)
		err = errors.Join(err, policyErr)
		if note != "" {
			if len(note)+1 > MaxProgramOutput-len(output) {
				err = errors.Join(err, errors.New("browser output exceeds limit"))
				return
			}
			output += note + "\n"
		}
	}()
	var out strings.Builder
	images, bytes := 0, 0
	for i, s := range p.steps {
		if e := ctx.Err(); e != nil {
			return out.String(), e
		}
		if s.method == "screenshot" && (images >= limits.Images || bytes >= limits.ImageBytes) {
			return out.String(), errors.New("browser screenshot budget exhausted before capture")
		}
		text, image, e := s.run(ctx, b)
		if e != nil {
			return out.String(), fmt.Errorf("browser statement %d: %w", i+1, e)
		}
		if s.method == "screenshot" && len(image) == 0 {
			return out.String(), errors.New("browser screenshot contained no image")
		}
		if len(image) > 0 {
			if len(image) > 4<<20 || len(image) > limits.ImageBytes-bytes {
				return out.String(), errors.New("browser screenshot exceeds remaining byte budget")
			}
			if e = sink(ctx, image); e != nil {
				return out.String(), e
			}
			images++
			bytes += len(image)
		}
		if text != "" {
			if len(text)+1 > MaxProgramOutput-out.Len() {
				return out.String(), errors.New("browser output exceeds limit")
			}
			out.WriteString(text)
			out.WriteByte('\n')
		}
	}
	return out.String(), nil
}

func (s programStep) run(ctx context.Context, b Backend) (string, []byte, error) {
	switch s.method {
	case "print":
		return s.text[0], nil, nil
	case "goto":
		if e := CheckURL(ctx, s.text[0]); e != nil {
			return "", nil, e
		}
		return "", nil, b.Navigate(ctx, s.text[0])
	case "back":
		return "", nil, b.Back(ctx)
	case "info":
		v, e := b.Info(ctx)
		if e != nil {
			return "", nil, e
		}
		raw, e := json.Marshal(v)
		return string(raw), nil, e
	case "js":
		v, e := b.Eval(ctx, s.text[0])
		return v, nil, e
	case "click":
		return "", nil, b.ClickAt(ctx, s.numbers[0], s.numbers[1])
	case "type":
		return "", nil, b.TypeText(ctx, s.text[0])
	case "press":
		return "", nil, b.PressKey(ctx, s.text[0])
	case "fill":
		return "", nil, b.Fill(ctx, s.text[0], s.text[1])
	case "scroll":
		return "", nil, b.Scroll(ctx, s.numbers[0])
	case "waitLoad":
		return "", nil, b.WaitLoad(ctx)
	case "waitFor":
		v, e := b.WaitElement(ctx, s.text[0], s.flag)
		return strconv.FormatBool(v), nil, e
	case "ax":
		v, e := b.AXTree(ctx)
		return v, nil, e
	case "box":
		x, y, e := b.BoxModel(ctx, int(s.numbers[0]))
		if e != nil {
			return "", nil, e
		}
		return fmt.Sprintf(`{"x":%.1f,"y":%.1f}`, x, y), nil, nil
	case "tabs":
		v, e := b.Tabs(ctx)
		if e != nil {
			return "", nil, e
		}
		raw, e := json.Marshal(v)
		return string(raw), nil, e
	case "useTab":
		return "", nil, b.UseTab(ctx, s.text[0])
	case "dialog":
		return "", nil, b.HandleDialog(s.flag, s.text[0])
	case "screenshot":
		image, e := b.Screenshot(ctx, 1568)
		if e != nil {
			return "", nil, e
		}
		return fmt.Sprintf("(screenshot captured: %d bytes, jpeg, ≤1568px)", len(image)), image, nil
	default:
		return "", nil, errors.New("invalid compiled browser program")
	}
}

// Preserve the retained post-navigation metadata floor for js/click redirects.
// The live scoped transport still rechecks authority before Info and Navigate;
// this cleanup cannot bypass a revoked grant or replay the failed helper.
func checkProgramFinalURL(ctx context.Context, b Backend) (string, error) {
	if e := ctx.Err(); e != nil {
		return "", e
	}
	info, e := b.Info(ctx)
	if e != nil {
		return "", fmt.Errorf("browser post-navigation observation: %w", e)
	}
	if info.URL == "" || info.Dialog != nil {
		return "", nil
	}
	if e = CheckURL(ctx, info.URL); e == nil {
		return "", nil
	}
	if cleanupErr := b.Navigate(ctx, "about:blank"); cleanupErr != nil {
		return "", errors.Join(errors.New("browser post-navigation policy failed and neutralization could not be confirmed"), cleanupErr)
	}
	return "(post-navigation URL policy rejected the destination; page neutralized to about:blank)", nil
}
