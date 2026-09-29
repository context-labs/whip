// Package tool implements host operations with explicit resource lifetimes.
package tool

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/context-labs/whip/internal/capability"
	"github.com/context-labs/whip/internal/session"
)

const (
	fileBytes   = 256 << 10
	outputBytes = 32 << 10 // Leaves room for JSON escaping and result metadata.
)

// Prepared contains an immutable request and a one-use execution lifetime.
// Acquire runs after consent and before durable dispatch. Its release must run
// even when dispatch is refused. Run requires that acquired lifetime and receives
// the committed operation identity for any separately recorded model attempts.
type Prepared struct {
	Capability string
	Resource   string
	Arguments  json.RawMessage
	Mutating   bool
	Acquire    func(context.Context) (func(), error)
	Run        func(context.Context, session.OperationID) (any, error)
	// ModelTimeouts leaves request deadlines to the model attempt runner. It is
	// valid only for models.call/batch; parent cancellation still applies.
	ModelTimeouts bool
	// FileSnapshot returns bounded captured content after a successful file run.
	FileSnapshot func() FileSnapshot
	// Timeout overrides the ordinary thirty-second effect deadline. Only trusted
	// preparation chooses it, up to five minutes; model and database paths reject it.
	Timeout time.Duration
	// Apply is used instead of Acquire/Run for database-only coordination. It
	// rechecks dispatch authority, performs the mutation and records its outcome
	// in one transaction. It never performs an external effect.
	Apply func(context.Context, session.OperationID) (any, error)
}

// Files shares canonical mutation locks across the runtime's sessions.
type Files struct{ workspaces *capability.Workspaces }

func NewFiles() *Files { return &Files{workspaces: capability.NewWorkspaces()} }

type fileRequest struct {
	path, content, oldText, newText string
	query                           string
	offset, limit                   int
	replaceAll                      bool
}

type fileExecution struct {
	mu        sync.Mutex
	workspace *capability.Workspace
	identity  os.FileInfo
	target    string
	relative  string
	request   fileRequest
	operation string
	root      *os.Root
	attempted bool
	ran       bool
	snapshot  FileSnapshot
}

func (f *Files) Prepare(cwd, operation string, args map[string]any) (Prepared, error) {
	request, normalized, err := prepareFileRequest(operation, args)
	if err != nil {
		return Prepared{}, err
	}
	if !filepath.IsAbs(cwd) {
		return Prepared{}, errors.New("workspace must be an absolute directory")
	}
	workspace, err := f.workspaces.Open(cwd)
	if err != nil {
		return Prepared{}, err
	}
	identity, err := os.Stat(workspace.Root())
	if err != nil {
		return Prepared{}, err
	}
	target, err := workspace.Resolve(request.path)
	if err != nil {
		return Prepared{}, err
	}
	relative, err := filepath.Rel(workspace.Root(), target)
	if err != nil {
		return Prepared{}, err
	}
	execution := &fileExecution{
		workspace: workspace, identity: identity, target: target, relative: relative,
		request: request, operation: operation,
	}
	return Prepared{
		Capability: operation, Resource: cwd, Arguments: normalized, Mutating: fileMutation(operation),
		Acquire: execution.acquire, Run: execution.run, FileSnapshot: func() FileSnapshot { execution.mu.Lock(); defer execution.mu.Unlock(); return execution.snapshot },
	}, nil
}

func prepareFileRequest(operation string, args map[string]any) (fileRequest, json.RawMessage, error) {
	for _, value := range args {
		if text, ok := value.(string); ok && !utf8.ValidString(text) {
			return fileRequest{}, nil, errors.New("file arguments must be UTF-8 text")
		}
	}
	raw, err := json.Marshal(args)
	if err != nil || len(raw) > fileBytes {
		return fileRequest{}, nil, errors.New("file arguments exceed 256 KiB or cannot be encoded")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	request := fileRequest{}
	normalized := map[string]any{}
	switch operation {
	case "files.list", "files.search":
		var scan struct {
			Path  string `json:"path"`
			Query string `json:"query,omitempty"`
			Limit *int   `json:"limit,omitempty"`
		}
		if err := decoder.Decode(&scan); err != nil {
			return request, nil, err
		}
		request.path, request.query, request.limit = scan.Path, scan.Query, 2000
		if request.path == "" {
			request.path = "."
		}
		maximum := 2000
		if operation == "files.search" {
			maximum, request.limit = 100, 100
			if request.query == "" || len(request.query) > 4096 || strings.ContainsAny(request.query, "\x00\r\n") {
				return request, nil, errors.New("query must be a nonempty literal single-line string of at most 4096 bytes")
			}
			normalized["query"] = request.query
		} else if _, supplied := args["query"]; supplied {
			return request, nil, errors.New("query is supported only by files.search")
		}
		if scan.Limit != nil {
			request.limit = *scan.Limit
		}
		if request.limit < 1 || request.limit > maximum {
			return request, nil, fmt.Errorf("limit must be from 1 to %d", maximum)
		}
		normalized["limit"] = request.limit
	case "files.diagnostics":
		var args struct {
			Path string `json:"path"`
		}
		if err := decoder.Decode(&args); err != nil {
			return request, nil, err
		}
		request.path = args.Path
	case "files.read":
		var args struct {
			Path   string `json:"path"`
			Offset *int   `json:"offset"`
			Limit  *int   `json:"limit"`
		}
		if err := decoder.Decode(&args); err != nil {
			return request, nil, err
		}
		request.path, request.offset, request.limit = args.Path, 1, 2000
		if args.Offset != nil {
			request.offset = *args.Offset
		}
		if args.Limit != nil {
			request.limit = *args.Limit
		}
		if request.offset < 1 || request.offset > fileBytes || request.limit < 1 || request.limit > fileBytes {
			return request, nil, errors.New("offset and limit must be integers from 1 to 262144")
		}
		normalized["offset"], normalized["limit"] = request.offset, request.limit
	case "files.write":
		var args struct {
			Path    string  `json:"path"`
			Content *string `json:"content"`
		}
		if err := decoder.Decode(&args); err != nil {
			return request, nil, err
		}
		if args.Content == nil {
			return request, nil, errors.New("content is required")
		}
		request.path, request.content = args.Path, *args.Content
		normalized["content"] = request.content
	case "files.patch":
		var args struct {
			Path       string  `json:"path"`
			OldText    string  `json:"old_text"`
			NewText    *string `json:"new_text"`
			ReplaceAll bool    `json:"replace_all"`
		}
		if err := decoder.Decode(&args); err != nil {
			return request, nil, err
		}
		if args.OldText == "" || args.NewText == nil {
			return request, nil, errors.New("nonempty old_text and new_text are required")
		}
		request.path, request.oldText, request.newText = args.Path, args.OldText, *args.NewText
		request.replaceAll = args.ReplaceAll
		normalized["old_text"], normalized["new_text"] = request.oldText, request.newText
		normalized["replace_all"] = request.replaceAll
	default:
		return request, nil, errors.New("unsupported file operation")
	}
	if !filepath.IsLocal(request.path) || len(request.path) > 4096 || strings.ContainsRune(request.path, '\x00') {
		return request, nil, errors.New("path must be a workspace-relative file path of at most 4096 bytes")
	}
	if slices.Contains(strings.Split(request.path, string(filepath.Separator)), "..") {
		return request, nil, errors.New("path traversal is not allowed")
	}
	request.path = filepath.Clean(request.path)
	if request.path == "." && operation != "files.list" && operation != "files.search" {
		return request, nil, errors.New("path must name a file")
	}
	normalized["path"] = request.path
	raw, err = json.Marshal(normalized)
	if err != nil || len(raw) > fileBytes {
		return request, nil, errors.New("normalized file arguments exceed 256 KiB")
	}
	return request, raw, nil
}

func fileMutation(operation string) bool {
	return operation == "files.write" || operation == "files.patch"
}

func (e *fileExecution) acquire(ctx context.Context) (func(), error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.attempted {
		return nil, errors.New("prepared operation was already acquired")
	}
	e.attempted = true
	release := func() {}
	if fileMutation(e.operation) || e.operation == "files.diagnostics" {
		path, unlock, err := e.workspace.LockPath(ctx, e.request.path)
		if err != nil {
			return nil, err
		}
		release = unlock
		if path != e.target {
			release()
			return nil, capability.ErrStaleAdmission
		}
	}
	root, err := os.OpenRoot(e.workspace.Root())
	if err == nil {
		var identity os.FileInfo
		identity, err = root.Stat(".")
		if err == nil && !os.SameFile(identity, e.identity) {
			err = capability.ErrStaleAdmission
		}
	}
	if err == nil {
		var path string
		path, err = e.workspace.Resolve(e.request.path)
		if err == nil && path != e.target {
			err = capability.ErrStaleAdmission
		}
	}
	if err == nil {
		err = ctx.Err()
	}
	if err != nil {
		if root != nil {
			_ = root.Close()
		}
		release()
		return nil, err
	}
	e.root = root
	return sync.OnceFunc(func() {
		e.mu.Lock()
		defer e.mu.Unlock()
		_ = e.root.Close()
		e.root = nil
		release()
	}), nil
}

func (e *fileExecution) run(ctx context.Context, _ session.OperationID) (any, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.root == nil || e.ran {
		return nil, errors.New("prepared operation requires one acquired execution")
	}
	e.ran = true
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	switch e.operation {
	case "files.list":
		return e.list(ctx)
	case "files.search":
		return e.search(ctx)
	case "files.read":
		return e.read()
	case "files.diagnostics":
		data, info, err := readRegular(e.root, e.relative)
		if err != nil {
			return nil, err
		}
		if info.Size() > fileBytes || !utf8.Valid(data) {
			return nil, errors.New("diagnostics require a UTF-8 file of at most 256 KiB")
		}
		e.snapshot = FileSnapshot{Path: e.target, Text: string(data), WorkspaceIdentity: e.identity}
		return map[string]any{"path": e.request.path}, nil
	}
	parentPath := filepath.Dir(e.relative)
	if e.operation == "files.write" {
		if err := e.root.MkdirAll(parentPath, 0o755); err != nil {
			return nil, err
		}
	}
	parent, err := e.root.OpenRoot(parentPath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = parent.Close() }()
	name := filepath.Base(e.relative)
	var mode *os.FileMode
	info, err := parent.Lstat(name)
	if err == nil {
		if !info.Mode().IsRegular() {
			return nil, errors.New("target must be a regular file")
		}
		mode = new(info.Mode().Perm())
	} else if !errors.Is(err, os.ErrNotExist) || e.operation == "files.patch" {
		return nil, err
	}
	data := []byte(e.request.content)
	result := map[string]any{"path": e.request.path}
	if e.operation == "files.patch" {
		previous, opened, err := readRegular(parent, name)
		if err != nil {
			return nil, err
		}
		if opened.Size() > fileBytes {
			return nil, errors.New("patch target exceeds 256 KiB")
		}
		mode = new(opened.Mode().Perm())
		count := strings.Count(string(previous), e.request.oldText)
		if count == 0 || (count > 1 && !e.request.replaceAll) {
			return nil, fmt.Errorf("old_text matched %d times; require one match or replace_all", count)
		}
		// Bound the result before ReplaceAll allocates expanded content.
		length := int64(len(previous)) + int64(count)*(int64(len(e.request.newText))-int64(len(e.request.oldText)))
		if length > fileBytes {
			return nil, errors.New("patched content exceeds 256 KiB")
		}
		data = []byte(strings.ReplaceAll(string(previous), e.request.oldText, e.request.newText))
		result["replacements"] = count
	}
	if err := publishFile(ctx, parent, name, data, mode, nil); err != nil {
		return nil, err
	}
	// Persist newly created ancestor directory entries as well as the file's parent.
	for path := filepath.Dir(parentPath); ; path = filepath.Dir(path) {
		if err := syncRootDirectory(e.root, path); err != nil {
			return nil, err
		}
		if path == "." {
			break
		}
	}
	e.snapshot = FileSnapshot{Path: e.target, Text: string(data), WorkspaceIdentity: e.identity}
	result["bytes_written"] = len(data)
	return result, nil
}

func readRegular(root *os.Root, path string) ([]byte, os.FileInfo, error) {
	file, err := root.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return nil, nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, nil, errors.New("target must be a regular file")
	}
	data, err := io.ReadAll(io.LimitReader(file, fileBytes))
	if err == nil {
		var current os.FileInfo
		current, err = file.Stat()
		if err == nil && (current.Size() != info.Size() || int64(len(data)) != min(info.Size(), fileBytes)) {
			err = errors.New("file size changed while reading")
		}
	}
	return data, info, err
}

func (e *fileExecution) read() (any, error) {
	data, info, err := readRegular(e.root, e.relative)
	if err != nil {
		return nil, err
	}
	limited := info.Size() > int64(len(data))
	if limited && !utf8.Valid(data) {
		for trimmed := 1; trimmed < utf8.UTFMax && trimmed <= len(data); trimmed++ {
			boundary := len(data) - trimmed
			if !utf8.FullRune(data[boundary:]) && utf8.Valid(data[:boundary]) {
				data = data[:boundary]
				break
			}
		}
	}
	if !utf8.Valid(data) {
		return nil, errors.New("file is not UTF-8 text")
	}
	lines := strings.Split(string(data), "\n")
	start := e.request.offset - 1
	if start >= len(lines) {
		return nil, fmt.Errorf("offset %d is past %d available lines (read byte limit reached: %t)", e.request.offset, len(lines), limited)
	}
	end := min(start+e.request.limit, len(lines))
	var output strings.Builder
	next := start
	partial := false
	for ; next < end; next++ {
		line := fmt.Sprintf("%d\t%s\n", next+1, lines[next])
		remaining := outputBytes - output.Len()
		if remaining == 0 {
			break
		}
		if len(line) > remaining {
			line = line[:remaining]
			for !utf8.ValidString(line) {
				line = line[:len(line)-1]
			}
			output.WriteString(line)
			partial = true
			break
		}
		output.WriteString(line)
	}
	truncated := limited || next < len(lines)
	result := map[string]any{
		"path": e.request.path, "output": output.String(), "offset": e.request.offset,
		"next_offset": next + 1, "truncated": truncated,
	}
	if truncated {
		result["reason"] = fmt.Sprintf("limits: %d lines, %d output bytes, first %d file bytes; partial line: %t; file byte limit reached: %t", e.request.limit, outputBytes, fileBytes, partial, limited)
		if partial || (limited && next >= len(lines)) {
			result["next_offset"] = nil // A line offset cannot resume a partial line or cross the byte ceiling.
		}
	}
	return result, nil
}

// Publication is a same-directory rename: failures before rename preserve the
// destination. Failures afterward may have changed it and must not be retried.
func publishFile(ctx context.Context, root *os.Root, name string, data []byte, mode *os.FileMode, beforeRename func() error) error {
	temporary := ".whip-" + rand.Text()
	createMode := os.FileMode(0o644) // New files honor the process umask.
	if mode != nil {
		createMode = 0o600
	}
	file, err := root.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, createMode)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close(); _ = root.Remove(temporary) }()
	if _, err := file.Write(data); err != nil {
		return err
	}
	if mode != nil {
		if err := file.Chmod(*mode); err != nil {
			return err
		}
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if beforeRename != nil {
		if err := beforeRename(); err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if info, err := root.Lstat(name); err == nil && !info.Mode().IsRegular() {
		return errors.New("publication target is no longer a regular file")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := root.Rename(temporary, name); err != nil {
		return err
	}
	return syncRootDirectory(root, ".")
}

func syncRootDirectory(root *os.Root, path string) error {
	directory, err := root.OpenFile(path, os.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return err
	}
	return errors.Join(directory.Sync(), directory.Close())
}
