package daemon

import (
	"context"
	"encoding/json"
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

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/theme"
)

func (s *Server) handleHost(ctx context.Context, request rpcMessage) (any, *RPCError, bool) {
	var result any
	var err error
	switch request.Method {
	case "host.directories.list":
		var p protocol.HostDirectoryParams
		if err := decodeProviderParams(request.Params, &p); err != nil {
			return nil, rpcFailure(-32602, err.Error()), true
		}
		result, err = hostDirectories(ctx, p)
	case "host.directory.pick":
		var p protocol.HostDirectoryPickParams
		if err := decodeProviderParams(request.Params, &p); err != nil {
			return nil, rpcFailure(-32602, err.Error()), true
		}
		result, err = hostDirectoryPick(ctx, p)
	case "host.attention":
		var p protocol.HostAttentionParams
		if err := decodeProviderParams(request.Params, &p); err != nil {
			return nil, rpcFailure(-32602, err.Error()), true
		}
		result, err = s.hostAttention(ctx, p)
	case "host.themes.list":
		var dir string
		dir, err = config.Dir()
		if err == nil {
			result, err = theme.Catalog(filepath.Join(dir, "themes"))
		}
	case "host.themes.resolve":
		var p protocol.HostThemeResolveParams
		if err := decodeProviderParams(request.Params, &p); err != nil {
			return nil, rpcFailure(-32602, err.Error()), true
		}
		if (p.Name == "") == (p.JSON == "") || len(p.JSON) > theme.MaxJSONBytes || len(p.Name) > 256 {
			return nil, rpcFailure(-32602, "theme resolution requires exactly one name or JSON document of at most 65536 bytes"), true
		}
		if p.JSON != "" {
			result, err = theme.ResolveJSON([]byte(p.JSON))
			if err != nil {
				return nil, rpcFailure(-32602, err.Error()), true
			}
		} else {
			var dir string
			dir, err = config.Dir()
			if err == nil {
				result, err = theme.Resolve(p.Name, filepath.Join(dir, "themes"))
			}
		}
	case "mailbox.list":
		var p protocol.MailboxPageParams
		if err := decodeProviderParams(request.Params, &p); err != nil {
			return nil, rpcFailure(-32602, err.Error()), true
		}
		if p.RootID == "" || p.AgentID == "" || p.Limit < 1 || p.Limit > 128 || p.MaxBytes < 4096 || p.MaxBytes > 512<<10 || !slices.Contains([]string{"", "all", "pending", "delivered", "done"}, p.Status) {
			return nil, rpcFailure(-32602, "mailbox requires root, agent, valid status, limit 1..128 and max_bytes 4096..524288"), true
		}
		result, err = s.daemon.store.InspectMailboxPage(ctx, p.RootID, p.AgentID, p.Status, p.Cursor, p.Limit, p.MaxBytes)
	case "mailbox.read":
		var p protocol.MailboxReadParams
		if err := decodeProviderParams(request.Params, &p); err != nil {
			return nil, rpcFailure(-32602, err.Error()), true
		}
		if p.RootID == "" || p.AgentID == "" || p.ID == "" {
			return nil, rpcFailure(-32602, "mailbox read requires root, agent and message IDs"), true
		}
		result, err = s.daemon.store.InspectMailboxMessage(ctx, p.RootID, p.AgentID, p.ID)
	default:
		return nil, nil, false
	}
	if errors.Is(err, session.ErrAgentAccess) {
		return nil, rpcFailure(-32003, "mailbox message or agent does not belong to this root and recipient"), true
	}
	return result, rpcFromError(err), true
}

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

func (s *Server) hostAttention(ctx context.Context, p protocol.HostAttentionParams) (protocol.HostAttentionResult, error) {
	result := protocol.HostAttentionResult{Items: []protocol.HostAttentionItem{}}
	if p.Limit < 1 || p.Limit > 128 || p.MaxBytes < 4096 || p.MaxBytes > 512<<10 || len(p.AfterID) > 256 {
		return result, rpcFailure(-32602, "attention requires limit 1..128 and max_bytes 4096..524288")
	}
	// Copy only ready pointers under the registry lock; never open a root or
	// hold the registry lock while inspecting questions or reading SQLite.
	roots := map[string]*Session{}
	s.daemon.mu.Lock()
	for id, entry := range s.daemon.roots {
		if len(roots) == 10000 {
			result.Truncated = true
			break
		}
		select {
		case <-entry.ready:
			if entry.root != nil {
				roots[id] = entry.root
			}
		default:
		}
	}
	s.daemon.mu.Unlock()
	questionRoots := []string{}
	for id, root := range roots {
		root.questions.mu.Lock()
		if len(root.questions.pending) > 0 {
			questionRoots = append(questionRoots, id)
		}
		root.questions.mu.Unlock()
	}
	items, err := s.daemon.store.AttentionRoots(ctx, p.AfterID, questionRoots, p.Limit+1)
	if err != nil {
		return result, err
	}
	for _, row := range items {
		if len(result.Items) == p.Limit {
			result.HasMore = true
			break
		}
		item := protocol.HostAttentionItem{RootID: row.RootID, Title: row.Title, ActiveAgents: row.ActiveAgents, PendingPermissions: row.PendingPermissions, Questions: []session.LifecycleEvent{}}
		if root := roots[row.RootID]; root != nil {
			for _, question := range root.questions.open() {
				// Full question choices are available in the opened root snapshot.
				question.Options = nil
				if len(question.Question) > 1024 {
					question.Question = string([]rune(question.Question)[:min(256, len([]rune(question.Question)))])
					item.Truncated = true
				}
				item.Questions = append(item.Questions, question)
			}
		}
		result.Items = append(result.Items, item)
		result.NextAfterID = item.RootID
		raw, err := json.Marshal(result)
		if err != nil {
			return result, err
		}
		if len(raw) > p.MaxBytes {
			result.Items = result.Items[:len(result.Items)-1]
			result.HasMore = true
			break
		}
	}
	if result.HasMore && len(result.Items) > 0 {
		result.NextAfterID = result.Items[len(result.Items)-1].RootID
	} else {
		result.NextAfterID = ""
	}
	if result.HasMore && len(result.Items) == 0 {
		return result, errors.New("attention item exceeds presentation budget")
	}
	return result, nil
}
