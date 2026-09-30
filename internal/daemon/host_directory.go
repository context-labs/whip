package daemon

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"

	"github.com/context-labs/whip/internal/protocol"
)

func hostDirectories(ctx context.Context, p protocol.HostDirectoryParams) (protocol.HostDirectoryResult, error) {
	result := protocol.HostDirectoryResult{Entries: []protocol.HostDirectoryEntry{}}
	if p.Limit < 1 || p.Limit > 128 || len(p.Path) > 4096 || len(p.Prefix) > 256 || len(p.After) > 256 || strings.ContainsAny(p.Prefix+p.After, `/\`+"\x00") {
		return result, rpcFailure(-32602, "directories require limit 1..128, path up to 4096 bytes, and filename filters up to 256 bytes")
	}
	path := p.Path
	if path == "" || path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return result, err
		}
		path = filepath.Join(home, strings.TrimPrefix(path, "~/"))
		if p.Path == "" || p.Path == "~" {
			path = home
		}
	}
	if !filepath.IsAbs(path) {
		return result, rpcFailure(-32602, "directory path must be absolute or start with ~/")
	}
	result.Path, result.Parent = filepath.Clean(path), filepath.Dir(filepath.Clean(path))
	file, err := os.Open(path) //nolint:gosec // G304: the host directory picker intentionally lists the user-requested absolute directory
	if err != nil {
		return result, err
	}
	defer func() { _ = file.Close() }()
	entries := []fs.DirEntry{}
	for len(entries) < 20000 {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		batch, err := file.ReadDir(min(128, 20000-len(entries)))
		entries = append(entries, batch...)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return result, err
		}
	}
	result.Truncated = len(entries) == 20000
	slices.SortFunc(entries, func(a, b fs.DirEntry) int { return strings.Compare(a.Name(), b.Name()) })
	bytes := len(result.Path) + len(result.Parent)
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if entry.Name() <= p.After || !strings.HasPrefix(entry.Name(), p.Prefix) || !p.ShowHidden && strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		child := filepath.Join(result.Path, entry.Name())
		isDir := entry.IsDir()
		if entry.Type()&os.ModeSymlink != 0 {
			if info, err := os.Stat(child); err == nil {
				isDir = info.IsDir()
			}
		}
		if !isDir {
			continue
		}
		// JSON may escape every input byte; reserve worst-case encoded space.
		bytes += 6*(len(child)+len(entry.Name())) + 128
		if len(result.Entries) == p.Limit || bytes > 384<<10 {
			result.HasMore = true
			break
		}
		result.Entries = append(result.Entries, protocol.HostDirectoryEntry{Name: entry.Name(), Path: child})
	}
	if result.HasMore && len(result.Entries) > 0 {
		result.NextAfter = result.Entries[len(result.Entries)-1].Name
	}
	return result, nil
}

func hostDirectoryCreate(ctx context.Context, p protocol.HostDirectoryCreateParams) (protocol.HostDirectoryCreateResult, error) {
	result := protocol.HostDirectoryCreateResult{}
	if len(p.Parent) > 4096 || strings.ContainsRune(p.Parent, 0) || !filepath.IsAbs(p.Parent) {
		return result, rpcFailure(-32602, "parent must be an absolute directory path of at most 4096 bytes")
	}
	if strings.TrimSpace(p.Name) == "" || len(p.Name) > 255 || p.Name == "." || p.Name == ".." ||
		strings.ContainsAny(p.Name, "/\\\x00") || !filepath.IsLocal(p.Name) {
		return result, rpcFailure(-32602, "folder name must be one nonblank component of at most 255 bytes, without separators, NUL, dot or dot-dot")
	}
	// Windows otherwise normalizes trailing dots/spaces and accepts alternate streams.
	if runtime.GOOS == "windows" && (strings.ContainsAny(p.Name, `<>:"|?*`) ||
		strings.HasSuffix(p.Name, ".") || strings.HasSuffix(p.Name, " ") ||
		strings.ContainsFunc(p.Name, func(r rune) bool { return r < 32 })) {
		return result, rpcFailure(-32602, "folder name contains characters or aliases that are invalid on Windows")
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	path := filepath.Join(p.Parent, p.Name)
	// The trusted host picker intentionally creates only one user-requested directory.
	if err := os.Mkdir(path, 0o700); err != nil {
		return result, fmt.Errorf("create folder: %w", err)
	}
	result.Path = path
	return result, nil
}

// directoryPickCommand returns the native folder-chooser invocation for goos
// (osascript on darwin, zenity or kdialog on linux, PowerShell Forms on
// windows) and a predicate mapping a failure to a user cancellation.
func directoryPickCommand(goos, start string) (name string, args []string, cancelled func(output string) bool) {
	switch goos {
	case "darwin":
		script := `choose folder with prompt "Choose a working directory"`
		if start != "" {
			script += ` default location (POSIX file ` + strconv.Quote(start) + `)`
		}
		return "osascript", []string{"-e", `POSIX path of (` + script + `)`},
			func(output string) bool { return strings.Contains(output, "User canceled") }
	case "linux":
		if path, err := exec.LookPath("zenity"); err == nil {
			return path, []string{"--file-selection", "--directory", "--title=Choose a working directory"},
				func(output string) bool { return true } // zenity exits 1 only on cancel
		}
		if path, err := exec.LookPath("kdialog"); err == nil {
			args := []string{"--getexistingdirectory"}
			if start != "" {
				args = append(args, start)
			}
			return path, args, func(output string) bool { return true }
		}
	case "windows":
		// `4` is the SSF_DESKTOPDIRECTORY folder constant (numeric so quoting is moot).
		return "powershell", []string{
				"-NoProfile", "-NonInteractive", "-Command",
				`Add-Type -AssemblyName System.Windows.Forms; $d = New-Object System.Windows.Forms.FolderBrowserDialog; $d.Description = 'Choose a working directory'; $d.RootFolder = 4; if ($d.ShowDialog() -eq 'OK') { $d.SelectedPath }`,
			},
			func(output string) bool { return output == "" } // cancel prints no path
	}
	return "", nil, nil
}

// hostDirectoryPick opens the OS-native folder chooser on the execution
// machine. Only available where the daemon itself has a desktop session; a
// headless daemon (SSH, container, no zenity/kdialog) errors so the browser
// falls back to the web directory browser.
func hostDirectoryPick(ctx context.Context, p protocol.HostDirectoryPickParams) (protocol.HostDirectoryPickResult, error) {
	result := protocol.HostDirectoryPickResult{}
	if len(p.Start) > 4096 || (p.Start != "" && !filepath.IsAbs(p.Start)) {
		return result, rpcFailure(-32602, "directory pick requires an absolute start path of at most 4096 bytes")
	}
	name, args, cancelled := directoryPickCommand(runtime.GOOS, filepath.Clean(p.Start))
	if name == "" {
		return result, fmt.Errorf("no native folder picker available on %s", runtime.GOOS)
	}
	var stdout, stderr strings.Builder
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	output := strings.TrimSpace(stdout.String())
	if err != nil {
		if cancelled != nil && cancelled(output+stderr.String()) {
			return protocol.HostDirectoryPickResult{Cancelled: true}, nil
		}
		return result, fmt.Errorf("folder picker failed: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	if output == "" || len(output) > 4096 {
		return result, fmt.Errorf("folder picker returned no usable path: %s", strings.TrimSpace(stderr.String()))
	}
	result.Path = strings.TrimRight(output, `/\`)
	return result, nil
}
