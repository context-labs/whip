package hostview

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/context-labs/whip/internal/capability"
)

var (
	ErrUnavailable  = errors.New("native directory picker unavailable")
	ErrPickerLimit  = errors.New("directory picker limit reached")
	ErrPickerClosed = errors.New("directory picker closed")
)

type PickResult struct {
	Path      *string `json:"path"`
	Cancelled bool    `json:"cancelled"`
}

// Picker owns only bounded, human-requested dialogs. Cancelling observation can
// close the dialog: it admits no session work or external filesystem mutation.
type Picker struct {
	mu        sync.Mutex
	processes *capability.ProcessManager
	calls     sync.WaitGroup
	active    int
	closed    bool
	cwd       string
	env       map[string]string
}

func NewPicker(cwd string) *Picker {
	env := map[string]string{}
	for _, name := range []string{"DISPLAY", "WAYLAND_DISPLAY", "XDG_RUNTIME_DIR", "XAUTHORITY", "DBUS_SESSION_BUS_ADDRESS"} {
		if value := os.Getenv(name); len(value) <= 4096 && value != "" {
			env[name] = value
		}
	}
	return &Picker{processes: capability.NewProcessManager(), cwd: cwd, env: env}
}

func (p *Picker) Close() error {
	p.mu.Lock()
	p.closed = true
	p.mu.Unlock()
	err := p.processes.Close()
	p.calls.Wait()
	return err
}

func (p *Picker) Pick(ctx context.Context, start string) (PickResult, error) {
	if len(start) > 4096 || !utf8.ValidString(start) || strings.ContainsRune(start, 0) || (start != "" && !filepath.IsAbs(start)) {
		return PickResult{}, invalid("directory pick requires an absolute start path of at most 4096 bytes")
	}
	if err := ctx.Err(); err != nil {
		return PickResult{}, err
	}
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return PickResult{}, ErrPickerClosed
	}
	if p.active == 2 {
		p.mu.Unlock()
		return PickResult{}, ErrPickerLimit
	}
	p.active++
	p.calls.Add(1)
	p.mu.Unlock()
	defer func() { p.mu.Lock(); p.active--; p.mu.Unlock(); p.calls.Done() }()
	name, args, cancelled := pickCommand(runtime.GOOS, start)
	if name == "" {
		return PickResult{}, ErrUnavailable
	}
	owned, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	stdout, stderr := &pickerOutput{cancel: cancel}, &pickerOutput{cancel: cancel}
	process, err := p.processes.Start(owned, "host_directory_picker", name, args, capability.ProcessOptions{Cwd: p.cwd, Env: p.env, Stdin: strings.NewReader(""), Stdout: stdout, Stderr: stderr})
	if err != nil {
		return PickResult{}, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	err = process.Wait()
	process.Stop()
	if stdout.overflow || stderr.overflow {
		return PickResult{}, ErrPickerLimit
	}
	if owned.Err() != nil {
		return PickResult{}, owned.Err()
	}
	output := strings.TrimSuffix(stdout.text.String(), "\n")
	if runtime.GOOS == "windows" {
		output = strings.TrimSuffix(output, "\r")
	}
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) && cancelled(exit.ExitCode(), output+stderr.text.String()) {
			return PickResult{Cancelled: true}, nil
		}
		return PickResult{}, fmt.Errorf("%w: chooser failed: %w", ErrUnavailable, err)
	}
	if runtime.GOOS == "windows" && output == "" {
		return PickResult{Cancelled: true}, nil
	}
	if output == "" || len(output) > 4096 || !utf8.ValidString(output) || strings.ContainsRune(output, 0) || !filepath.IsAbs(output) {
		return PickResult{}, invalid("folder picker returned no usable absolute path")
	}
	// Clean preserves whitespace and filesystem roots, unlike TrimSpace/TrimRight.
	path := filepath.Clean(output)
	return PickResult{Path: &path}, nil
}

type pickerOutput struct {
	text     strings.Builder
	cancel   context.CancelFunc
	overflow bool
}

func (o *pickerOutput) Write(data []byte) (int, error) {
	if len(data) > 8192-o.text.Len() {
		o.overflow = true
		o.cancel()
		return len(data), nil
	}
	_, _ = o.text.Write(data)
	return len(data), nil
}

func pickCommand(goos, start string) (string, []string, func(int, string) bool) {
	switch goos {
	case "darwin":
		script := `choose folder with prompt "Choose a working directory"`
		if start != "" {
			script += ` default location (POSIX file ` + strconv.Quote(start) + `)`
		}
		return "osascript", []string{"-e", `POSIX path of (` + script + `)`}, func(_ int, output string) bool { return strings.Contains(output, "User canceled") }
	case "linux":
		cancelled := func(code int, _ string) bool { return code == 1 }
		if path, err := exec.LookPath("zenity"); err == nil {
			args := []string{"--file-selection", "--directory", "--title=Choose a working directory"}
			if start != "" {
				args = append(args, "--filename="+start+string(filepath.Separator))
			}
			return path, args, cancelled
		}
		if path, err := exec.LookPath("kdialog"); err == nil {
			args := []string{"--getexistingdirectory"}
			if start != "" {
				args = append(args, start)
			}
			return path, args, cancelled
		}
	case "windows":
		return "powershell", []string{"-NoProfile", "-NonInteractive", "-Command", `Add-Type -AssemblyName System.Windows.Forms; $d = New-Object System.Windows.Forms.FolderBrowserDialog; $d.Description = 'Choose a working directory'; $d.RootFolder = 4; if ($d.ShowDialog() -eq 'OK') { $d.SelectedPath }`}, func(_ int, _ string) bool { return false }
	}
	return "", nil, nil
}
