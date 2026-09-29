package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"sync"
	"syscall"
	"time"

	"github.com/context-labs/whip/internal/client"
	"github.com/context-labs/whip/internal/mcp"
	"github.com/context-labs/whip/internal/protocol"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func mcpServe(version string) (err error) {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	streams, err := newProtocolStdio(os.Stdin, os.Stdout, 5*time.Second, 8<<20)
	if err != nil {
		return err
	}
	defer func() { _ = streams.Close() }()
	stopped := context.AfterFunc(ctx, func() { _ = streams.Close() })
	defer stopped()
	c, err := connectNativeRuntime(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()
	provider, err := newNativeMCPTools(ctx, c)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, provider.Close()) }()
	input := &mcpLineReader{input: streams.input, scanner: bufio.NewScanner(streams.input)}
	input.scanner.Buffer(make([]byte, 4096), (1<<20)+1)
	return mcp.ServeTransport(ctx, version, provider, &sdkmcp.IOTransport{Reader: input, Writer: streams})
}

// Reject oversized frames and batches before the SDK's JSON decoder can allocate
// an unbounded value. Read is serial; closing the pollable input releases it.
type mcpLineReader struct {
	input     io.ReadCloser
	scanner   *bufio.Scanner
	remaining []byte
}

func (r *mcpLineReader) Read(out []byte) (int, error) {
	if len(out) == 0 {
		return 0, nil
	}
	for len(r.remaining) == 0 {
		if !r.scanner.Scan() {
			if err := r.scanner.Err(); err != nil {
				return 0, fmt.Errorf("MCP input frame exceeds 1 MiB or cannot be read: %w", err)
			}
			return 0, io.EOF
		}
		line := r.scanner.Bytes()
		trimmed := bytes.TrimSpace(line)
		if len(trimmed) == 0 {
			continue
		}
		if trimmed[0] == '[' {
			var batch []json.RawMessage
			if err := json.Unmarshal(trimmed, &batch); err != nil || len(batch) == 0 || len(batch) > 16 {
				return 0, errors.New("MCP input batch must contain 1–16 messages")
			}
		} else if trimmed[0] != '{' {
			return 0, errors.New("MCP input requires JSON-RPC messages")
		}

		r.remaining = append(bytes.Clone(line), '\n')
	}
	n := copy(out, r.remaining)
	r.remaining = r.remaining[n:]
	return n, nil
}
func (r *mcpLineReader) Close() error { return r.input.Close() }

type nativeMCPTools struct {
	owner             *mcpCLIOwner
	clientID          protocol.ID
	definitions       []mcp.Definition
	mu                sync.Mutex
	closed, uncertain bool
	calls             int
	joined            sync.WaitGroup
	closeOnce         sync.Once
	closeErr          error
}

var mcpAliases = map[string]struct{ module, name string }{
	"read": {"files", "read"}, "write": {"files", "write"}, "edit": {"files", "patch"}, "bash": {"shell", "run"},
	"browser_list_tabs": {"browser", "list_tabs"}, "browser_open": {"browser", "open"}, "browser_attach": {"browser", "attach"}, "browser_run": {"browser", "run"}, "browser_detach": {"browser", "detach"}, "browser_allow_preview_port": {"browser", "allow_preview_port"},
}

func newNativeMCPTools(ctx context.Context, c *client.Client) (*nativeMCPTools, error) {
	owner, err := newMCPOwner(ctx, c, &protocol.MCPSelection{Servers: []string{}}, []protocol.ID{"files", "shell", "browser"})
	if err != nil {
		return nil, err
	}
	fail := func(err error) (*nativeMCPTools, error) { return nil, errors.Join(err, owner.close()) }
	var policy protocol.PermissionPolicy
	if err := c.Call(ctx, "permissions.policy", protocol.SessionParams{SessionID: owner.session.ID}, &policy); err != nil {
		return fail(err)
	}
	var denial protocol.PermissionDenialEdit
	if err := c.Call(ctx, "permissions.set_denial", protocol.SetPermissionDenialParams{EditID: protocol.ID(rand.Text()), SessionID: owner.session.ID, ExpectedRevision: policy.Revision, DenyInteractive: true}, &denial); err != nil {
		return fail(err)
	}
	var grant protocol.Grant
	if err := c.Call(ctx, "grants.create", protocol.CreateGrantParams{ID: protocol.ID(rand.Text()), SessionID: owner.session.ID, Capability: "files.read", Resource: owner.session.WorkingDirectory}, &grant); err != nil {
		return fail(err)
	}
	var schemas protocol.HostToolSchemasResult
	if err := c.Call(ctx, "tool.schemas", protocol.SessionParams{SessionID: owner.session.ID}, &schemas); err != nil {
		return fail(err)
	}
	result := &nativeMCPTools{owner: owner, clientID: protocol.ID(rand.Text())}
	for name, alias := range mcpAliases {
		found := false
		for _, schema := range schemas.Items {
			if schema.Module == alias.module && string(schema.Name) == alias.name {
				found = true
				definition := mcp.Definition{Type: "function"}
				definition.Function.Name = name
				definition.Function.Description = schema.Description
				definition.Function.Parameters = bytes.Clone(schema.InputSchema)
				if name == "edit" {
					definition.Function.Parameters = bytes.ReplaceAll(definition.Function.Parameters, []byte(`"old_text"`), []byte(`"old_string"`))
					definition.Function.Parameters = bytes.ReplaceAll(definition.Function.Parameters, []byte(`"new_text"`), []byte(`"new_string"`))
				}
				result.definitions = append(result.definitions, definition)
			}
		}
		if !found {
			return fail(fmt.Errorf("native tool schema %s.%s unavailable", alias.module, alias.name))
		}
	}
	sort.Slice(result.definitions, func(i, j int) bool { return result.definitions[i].Function.Name < result.definitions[j].Function.Name })
	return result, nil
}

func (p *nativeMCPTools) ToolDefinitions(context.Context) ([]mcp.Definition, error) {
	result := append([]mcp.Definition{}, p.definitions...)
	for i := range result {
		result[i].Function.Parameters = bytes.Clone(result[i].Function.Parameters)
	}
	return result, nil
}

func (p *nativeMCPTools) Close() error {
	p.closeOnce.Do(func() {
		p.mu.Lock()
		p.closed = true
		p.mu.Unlock()
		p.joined.Wait()
		p.mu.Lock()
		uncertain := p.uncertain
		p.mu.Unlock()
		if uncertain {
			p.closeErr = fmt.Errorf("MCP session %s retained because an accepted or possibly accepted call has no observed terminal outcome", p.owner.session.ID)
			return
		}
		p.closeErr = p.owner.close()
	})
	return p.closeErr
}

func (p *nativeMCPTools) CallTool(observing context.Context, name string, arguments json.RawMessage) (string, error) {
	if err := observing.Err(); err != nil {
		return "", err
	}
	alias, ok := mcpAliases[name]
	if !ok {
		return "", errors.New("tool is not exposed by this MCP endpoint")
	}
	if len(arguments) > 512<<10 {
		return "", errors.New("MCP tool arguments exceed 512 KiB")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(arguments, &fields); err != nil || fields == nil {
		return "", errors.New("MCP tool arguments must be an object")
	}
	if name == "edit" {
		if _, ok := fields["old_text"]; ok {
			return "", errors.New("edit expects old_string")
		}
		if _, ok := fields["new_text"]; ok {
			return "", errors.New("edit expects new_string")
		}
		if value, ok := fields["old_string"]; ok {
			fields["old_text"] = value
			delete(fields, "old_string")
		}
		if value, ok := fields["new_string"]; ok {
			fields["new_text"] = value
			delete(fields, "new_string")
		}
	}
	if alias.module == "files" {
		var path string
		if err := json.Unmarshal(fields["path"], &path); err != nil {
			return "", errors.New("file path must be text")
		}
		if filepath.IsAbs(path) {
			relative, err := filepath.Rel(p.owner.session.WorkingDirectory, path)
			if err != nil || !filepath.IsLocal(relative) {
				return "", errors.New("file path is outside the MCP workspace")
			}
			fields["path"], _ = json.Marshal(relative)
		}
	}
	arguments, err := json.Marshal(fields)
	if err != nil {
		return "", err
	}
	p.mu.Lock()
	if p.closed || p.uncertain || p.calls >= 16 {
		p.mu.Unlock()
		return "", errors.New("MCP endpoint closed, has an unknown outcome, or reached its call limit")
	}
	p.calls++
	p.joined.Add(1)
	p.mu.Unlock()
	defer func() { p.mu.Lock(); p.calls--; p.mu.Unlock(); p.joined.Done() }()
	// Protocol cancellation detaches observation, never silently cancels accepted
	// host work. Close joins this bounded owner before deleting its private root.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	identity := protocol.RequestIdentity{ClientID: p.clientID, RequestID: protocol.ID(rand.Text())}
	command, err := p.owner.client.PrepareInput("tool.call", protocol.CallHostToolParams{Identity: identity, SessionID: p.owner.session.ID, Operation: protocol.DirectHostInput{Module: alias.module, Name: protocol.ID(alias.name), ArgumentsBase64: base64.StdEncoding.EncodeToString(arguments)}})
	if err != nil {
		return "", err
	}
	_, err = command.Send(ctx)
	if err != nil {
		if _, ok := errors.AsType[*client.Error](err); ok {
			return "", err
		}
		_, found, checkErr := command.Check(ctx)
		if checkErr != nil || !found {
			p.markUncertain()
			return "", fmt.Errorf("MCP input %s outcome unknown; no resend: %w", identity.RequestID, errors.Join(err, checkErr))
		}
	}
	completed, err := command.Wait(ctx)
	if err != nil {
		p.markUncertain()
		return "", fmt.Errorf("MCP input %s observation failed: %w", identity.RequestID, err)
	}
	if completed.Input == nil || completed.Turn == nil {
		return "", errors.New("MCP host input deleted or settled without a turn")
	}
	var operations protocol.HostOperationsResult
	if err := p.owner.client.Call(ctx, "turns.operations", protocol.HostOperationsParams{TurnID: completed.Turn.ID, Limit: 100}, &operations); err != nil {
		p.markUncertain()
		return "", err
	}
	for _, operation := range operations.Items {
		if operation.SessionID != p.owner.session.ID || operation.TurnID != completed.Turn.ID || operation.Origin != "host_operation" || operation.CellID != nil || operation.RequestID != completed.Input.ID {
			continue
		}
		if operation.Result == nil {
			p.markUncertain()
			return "", errors.New("MCP direct operation has no terminal result")
		}
		raw, err := json.Marshal(struct {
			SessionID   protocol.ID                   `json:"session_id"`
			TurnID      protocol.ID                   `json:"turn_id"`
			OperationID protocol.ID                   `json:"operation_id"`
			Result      *protocol.HostOperationResult `json:"result"`
		}{p.owner.session.ID, completed.Turn.ID, operation.ID, operation.Result})
		if err != nil {
			return "", err
		}
		if operation.Result.State != "succeeded" {
			failure := operation.Result.State
			if operation.Result.Failure != nil {
				failure += ": " + *operation.Result.Failure
			}
			return string(raw), errors.New(failure)
		}
		return string(raw), nil
	}
	if completed.Turn.State == "succeeded" {
		p.markUncertain()
	}
	failure := "host tool did not dispatch"
	if completed.Turn.Failure != nil {
		failure = *completed.Turn.Failure
	}
	return "", errors.New(failure)
}
func (p *nativeMCPTools) markUncertain() { p.mu.Lock(); p.uncertain = true; p.mu.Unlock() }
