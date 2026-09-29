package computer

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/context-labs/whip/internal/browser"
	"github.com/context-labs/whip/internal/computerconfig"
)

const MaxBatchOutputBytes = 64 << 10

// Observations belongs to the existing live session kernel. Never serialize or
// copy it into a fork/checkpoint. Index authority also binds connection and PID.
type Observations struct {
	mu     sync.Mutex
	values map[string]observedApp
}
type observedApp struct {
	epoch           string
	pid, generation int
}

func (o *Observations) generation(epoch, app string, pid int) int {
	o.mu.Lock()
	defer o.mu.Unlock()
	v := o.values[app]
	if v.epoch == epoch && v.pid == pid {
		return v.generation
	}
	return 0
}

func (o *Observations) note(epoch, app string, pid, generation int) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.values == nil {
		o.values = map[string]observedApp{}
	}
	if _, exists := o.values[app]; !exists && len(o.values) >= 64 {
		return errors.New("computer observed-application limit reached; discard kernel to refresh")
	}
	o.values[app] = observedApp{epoch: epoch, pid: pid, generation: generation}
	return nil
}

func (o *Observations) forget(app string) { o.mu.Lock(); defer o.mu.Unlock(); delete(o.values, app) }

func (o *Observations) clear() { o.mu.Lock(); defer o.mu.Unlock(); o.values = nil }

// BatchResult preserves the completed prefix, including screenshots, on failure.
// Screenshot bytes must be published as owner-scoped content, never JSON/base64
// tool output. There is no continuation token or resumable execution cursor.
type BatchResult struct {
	Text        string
	Screenshots [][]byte
}

// Run executes only after the caller commits operation dispatch. Recheck must
// verify current host policy and grant lifetime before each native/script effect.
func (l *Lease) Run(ctx context.Context, observations *Observations, recheck func(context.Context) error) (result BatchResult, runErr error) {
	l.runMu.Lock()
	if l.ran || l.closed {
		l.runMu.Unlock()
		return BatchResult{}, errors.New("computer batch already attempted or released; replay prohibited")
	}
	l.ran = true
	l.done = make(chan struct{})
	done := l.done
	l.runMu.Unlock()
	defer close(done)
	if observations == nil || recheck == nil {
		return BatchResult{}, errors.New("computer batch requires observations and authority recheck")
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(l.ctx, cancel)
	defer stop()
	check := func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := l.check(); err != nil {
			return err
		}
		return recheck(ctx)
	}
	result = BatchResult{Screenshots: [][]byte{}}
	defer func() {
		if runErr != nil {
			observations.clear()
		}
	}()
	var output strings.Builder
	totalImages := 0
	for index, step := range l.capture.batch.steps {
		if err := check(); err != nil {
			result.Text = output.String()
			return result, err
		}
		if len(result.Screenshots) >= 8 && step.method != "ax" && step.method != "apps" && step.method != "permissions.request" && step.method != "print" && step.method != "tell" && !strings.HasPrefix(step.method, "chrome_") {
			result.Text = output.String()
			return result, errors.New("computer batch screenshot limit reached before next action")
		}
		value, shot, err := l.runStep(ctx, step, observations, check)
		if value != "" {
			if !utf8.ValidString(value) || output.Len()+len(value)+1 > MaxBatchOutputBytes {
				result.Text = output.String()
				return result, errors.New("computer batch text limit exceeded after completed action")
			}
			output.WriteString(value)
			output.WriteByte('\n')
		}
		if len(shot) > 0 {
			if len(result.Screenshots) >= 8 || totalImages+len(shot) > 16<<20 {
				result.Text = output.String()
				return result, errors.New("computer batch screenshot limit exceeded after completed action")
			}
			result.Screenshots = append(result.Screenshots, shot)
			totalImages += len(shot)
		}
		if err != nil {
			result.Text = output.String()
			return result, fmt.Errorf("computer statement %d: %w", index+1, err)
		}
	}
	result.Text = output.String()
	return result, nil
}

func (l *Lease) runStep(ctx context.Context, step computerStep, observations *Observations, check func() error) (string, []byte, error) {
	if step.method == "print" {
		return step.text, nil, nil
	}
	if step.method == "tell" || strings.HasPrefix(step.method, "chrome_") {
		value, err := l.scriptStep(ctx, step, check)
		return value, nil, err
	}
	connection := l.capture.epoch.connection
	if connection == nil {
		return "", nil, ErrGenerationRetired
	}
	call := func(method string, params map[string]any) (json.RawMessage, error) {
		if err := check(); err != nil {
			return nil, err
		}
		raw, err := json.Marshal(params)
		if err != nil {
			return nil, err
		}
		return connection.Call(ctx, method, raw)
	}
	if step.method == "permissions.request" {
		raw, err := call(step.method, map[string]any{})
		if err != nil {
			return "", nil, err
		}
		var status TCCStatus
		if json.Unmarshal(raw, &status) != nil {
			return "", nil, errors.New("invalid helper permission response")
		}
		status.Hint = ""
		encoded, _ := json.Marshal(status)
		return string(encoded), nil, nil
	}
	raw, err := call("apps", map[string]any{})
	if err != nil {
		return "", nil, err
	}
	apps, err := decodeRunningApps(raw)
	if err != nil {
		return "", nil, err
	}
	if step.method == "apps" {
		encoded, _ := json.Marshal(apps)
		return string(encoded), nil, nil
	}
	app, err := exactRunningApp(apps, step.app)
	if err != nil {
		return "", nil, err
	}
	if err := l.capture.epoch.config.Check(app.Name, app.BundleID); err != nil {
		return "", nil, err
	}
	params := maps.Clone(step.params)
	params["app"] = app.BundleID
	if _, indexed := params["index"]; indexed {
		generation := observations.generation(connection.Epoch(), app.BundleID, app.PID)
		if generation <= 0 {
			return "", nil, errors.New("computer element requires current state observed by this session")
		}
		params["gen"] = generation
	}
	raw, err = call(step.method, params)
	if err != nil {
		observations.forget(app.BundleID)
		return "", nil, err
	}
	if step.method == "screenshot" {
		var shot Screenshot
		if json.Unmarshal(raw, &shot) != nil {
			return "", nil, errors.New("invalid helper screenshot response")
		}
		jpeg, err := computerJPEG(ctx, &shot)
		if err != nil {
			return "", nil, err
		}
		if len(jpeg) == 0 {
			return "screenshot unavailable", nil, nil
		}
		return "screenshot captured", jpeg, nil
	}
	var ack struct {
		Action           string `json:"action"`
		StateUnavailable string `json:"stateUnavailable"`
	}
	if json.Unmarshal(raw, &ack) == nil && ack.StateUnavailable != "" {
		observations.forget(app.BundleID)
		return "action posted; state unavailable; read fresh state before indexed actions", nil, nil
	}
	var state AppState
	if json.Unmarshal(raw, &state) != nil || state.Generation <= 0 || len(state.Elements) > 2048 {
		observations.forget(app.BundleID)
		return "", nil, errors.New("invalid or oversized helper application state")
	}
	// The request's exact identity is authority; helper-provided display labels are not.
	if err := observations.note(connection.Epoch(), app.BundleID, app.PID, state.Generation); err != nil {
		return "", nil, err
	}
	shot, err := computerJPEG(ctx, state.Screenshot)
	screenshotUnavailable := state.Screenshot != nil && state.Screenshot.Err != ""
	state.Screenshot = nil
	encoded, encodeErr := json.Marshal(state)
	if encodeErr != nil {
		return "", shot, errors.New("invalid helper state presentation")
	}
	text := string(encoded)
	if screenshotUnavailable {
		text += "\nscreenshot unavailable"
	}
	return text, shot, err
}

func decodeRunningApps(raw json.RawMessage) ([]RunningApp, error) {
	var apps []RunningApp
	if len(raw) > 256<<10 || json.Unmarshal(raw, &apps) != nil || len(apps) > 512 {
		return nil, errors.New("invalid or oversized helper application inventory")
	}
	for _, app := range apps {
		if _, err := computerconfig.CanonicalApp(app.Name); err != nil {
			return nil, errors.New("invalid helper application name")
		}
		if _, err := computerconfig.CanonicalApp(app.BundleID); err != nil || app.PID <= 0 {
			return nil, errors.New("invalid helper application identity")
		}
	}
	return apps, nil
}

func exactRunningApp(apps []RunningApp, label string) (RunningApp, error) {
	var match RunningApp
	matches := 0
	for _, app := range apps {
		name, _ := computerconfig.CanonicalApp(app.Name)
		bundle, _ := computerconfig.CanonicalApp(app.BundleID)
		if name == label || bundle == label {
			match = app
			matches++
		}
	}
	if matches != 1 {
		return RunningApp{}, errors.New("computer application must match one exact running name or bundle ID")
	}
	return match, nil
}

func computerJPEG(ctx context.Context, shot *Screenshot) ([]byte, error) {
	if shot == nil || shot.JPEGBase64 == "" {
		return nil, nil
	}
	if len(shot.JPEGBase64) > base64.StdEncoding.EncodedLen(8<<20) {
		return nil, errors.New("computer screenshot exceeds input bound")
	}
	data, err := base64.StdEncoding.DecodeString(shot.JPEGBase64)
	if err != nil {
		return nil, errors.New("invalid computer screenshot encoding")
	}
	data, err = browser.BoundJPEG(ctx, data, 2048)
	if err != nil {
		return nil, errors.New("invalid or oversized computer screenshot")
	}
	if len(data) > 4<<20 {
		return nil, errors.New("computer screenshot exceeds content bound")
	}
	return data, nil
}
