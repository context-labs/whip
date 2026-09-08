package mcp

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/context-labs/whip/internal/capability"
)

func (s *server) unavailableLocked() error {
	switch s.status {
	case StatusFailed:
		if s.err != "" {
			return fmt.Errorf("mcp server %q unavailable: %s (/mcp %s reconnect)", s.name, s.err, s.name)
		}
		return fmt.Errorf("mcp server %q unavailable (/mcp %s reconnect)", s.name, s.name)
	case StatusDisabled:
		return fmt.Errorf("mcp server %q is disabled (/mcp %s enable)", s.name, s.name)
	default:
		return fmt.Errorf("mcp server %q is %s", s.name, s.status)
	}
}

// retireLocked cancels queued and active calls before retiring the catalog.
// The caller closes the returned session outside the state lock.
func (s *server) retireLocked() *sdkmcp.ClientSession {
	if s.connectionStop != nil {
		s.connectionStop()
	}
	old := s.sess
	s.sess, s.defs, s.instr = nil, nil, ""
	return old
}

func (m *Manager) lookup(name string) (*server, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil, fmt.Errorf("mcp manager is closed (server %q)", name)
	}
	s := m.servers[name]
	if s == nil {
		return nil, fmt.Errorf("MCP server %q not found", name)
	}
	return s, nil
}

// descriptorLocked binds authority to exact raw names and the complete
// configured definition. Secrets/references contribute only to an opaque hash;
// neither endpoint credentials nor env/header values are returned to callers.
func (s *server) descriptorLocked(tool string) (capability.MCPCall, any, error) {
	if s.sess == nil || s.status != StatusReady || s.cfg.Disabled() {
		return capability.MCPCall{}, nil, s.unavailableLocked()
	}
	var found *sdkmcp.Tool
	for _, definition := range s.defs {
		if definition.Name == tool {
			if found != nil {
				return capability.MCPCall{}, nil, fmt.Errorf("MCP server %q advertises duplicate tool %q", s.name, tool)
			}
			found = definition
		}
	}
	if found == nil {
		return capability.MCPCall{}, nil, fmt.Errorf("MCP tool %s.%s not found", s.name, tool)
	}
	cfg := s.cfg
	cfg.Enabled = nil // live enable/disable gates calls; it is not endpoint identity
	identity, err := json.Marshal(struct {
		Server  string
		Config  ServerConfig
		Trusted bool
		Tool    *sdkmcp.Tool
	}{s.name, cfg, s.cfg.Trusted, found})
	if err != nil {
		return capability.MCPCall{}, nil, errors.New("MCP tool definition cannot be encoded")
	}
	return capability.MCPCall{
		Server: s.name, Tool: tool, Definition: fmt.Sprintf("%x", sha256.Sum256(identity)),
		Generation: s.generation, Source: s.cfg.Source, Trusted: s.cfg.Trusted,
	}, found.InputSchema, nil
}

// ResolveTool returns the currently advertised exact tool identity. A stale
// generation must be resolved and admitted again; it is never silently retried.
func (m *Manager) ResolveTool(serverName, toolName string) (capability.MCPCall, error) {
	s, err := m.lookup(serverName)
	if err != nil {
		return capability.MCPCall{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	call, _, err := s.descriptorLocked(toolName)
	return call, err
}

func matchesCall(current, expected capability.MCPCall) bool {
	return current.MCPSelector == expected.MCPSelector && current.Generation == expected.Generation && current.Source == expected.Source && current.Trusted == expected.Trusted
}

// CallContext returns the exact descriptor's lifetime, including time spent
// awaiting admission before CallChecked. A catalog change or retired connection
// cancels it, so callers must link it before creating a permission request.
func (m *Manager) CallContext(call capability.MCPCall) (context.Context, error) {
	s, err := m.lookup(call.Server)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current, _, err := s.descriptorLocked(call.Tool)
	if err != nil {
		return nil, err
	}
	if !matchesCall(current, call) {
		return nil, errors.New("MCP tool definition changed; resolve and admit again")
	}
	if err := s.connectionCtx.Err(); err != nil {
		return nil, err
	}
	return s.connectionCtx, nil
}

func toolArguments(schema any, arguments json.RawMessage) (json.RawMessage, error) {
	var args map[string]any
	if len(arguments) == 0 {
		arguments = json.RawMessage(`{}`)
	}
	if err := json.Unmarshal(arguments, &args); err != nil || args == nil {
		return nil, errors.New("invalid tool arguments: expected one JSON object")
	}
	data, err := json.Marshal(schema)
	if err != nil || len(data) > 1<<20 {
		return nil, errors.New("MCP input schema is invalid or exceeds 1 MiB")
	}
	var definition jsonschema.Schema
	if err := json.Unmarshal(data, &definition); err != nil {
		return nil, fmt.Errorf("invalid MCP input schema: %w", err)
	}
	resolved, err := definition.Resolve(nil) // remote schema references never cause network requests
	if err != nil {
		return nil, fmt.Errorf("invalid MCP input schema: %w", err)
	}
	if err := resolved.Validate(args); err != nil {
		return nil, fmt.Errorf("invalid tool arguments: %w", err)
	}
	// Validation uses the schema library's JSON representation, but forwarding
	// those floats would silently round large integer IDs. Preserve the wire JSON.
	return arguments, nil
}

// ValidateArguments lets admission reject malformed input before asking for
// consent. CallChecked repeats it and validates the same descriptor at dispatch.
func (m *Manager) ValidateArguments(call capability.MCPCall) error {
	s, err := m.lookup(call.Server)
	if err != nil {
		return err
	}
	s.mu.Lock()
	current, schema, err := s.descriptorLocked(call.Tool)
	s.mu.Unlock()
	if err != nil {
		return err
	}
	if !matchesCall(current, call) {
		return errors.New("MCP tool definition changed; resolve and admit again")
	}
	_, err = toolArguments(schema, call.Arguments)
	return err
}

// CallChecked checks the advertised schema before queueing, then rechecks the
// exact definition and authority after acquiring the server's serialized slot.
// Cancellation retires local work; effects already transmitted are never retried.
func (m *Manager) CallChecked(ctx context.Context, call capability.MCPCall, before func(context.Context) error) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	call.Arguments = slices.Clone(call.Arguments)
	s, err := m.lookup(call.Server)
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	current, schema, err := s.descriptorLocked(call.Tool)
	connectionCtx, timeout := s.connectionCtx, s.cfg.ToolTimeoutDuration()
	s.mu.Unlock()
	if err != nil {
		return "", err
	}
	if !matchesCall(current, call) {
		return "", errors.New("MCP tool definition changed; resolve and admit again")
	}
	args, err := toolArguments(schema, call.Arguments)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	stop := context.AfterFunc(connectionCtx, cancel)
	defer stop()
	if err := connectionCtx.Err(); err != nil {
		return "", err
	}
	select {
	case s.calling <- struct{}{}:
		defer func() { <-s.calling }()
	case <-ctx.Done():
		return "", ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := connectionCtx.Err(); err != nil {
		return "", err
	}
	s.mu.Lock()
	current, _, err = s.descriptorLocked(call.Tool)
	s.mu.Unlock()
	if err != nil {
		return "", err
	}
	if !matchesCall(current, call) {
		return "", errors.New("MCP tool definition changed while queued; resolve and admit again")
	}
	if before != nil {
		if err := before(ctx); err != nil {
			return "", err
		}
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	s.mu.Lock()
	current, _, err = s.descriptorLocked(call.Tool)
	sess := s.sess
	s.mu.Unlock()
	if err != nil {
		return "", err
	}
	if !matchesCall(current, call) {
		return "", errors.New("MCP tool definition changed during admission; resolve and admit again")
	}
	if err := connectionCtx.Err(); err != nil {
		return "", err
	}
	result, err := sess.CallTool(ctx, &sdkmcp.CallToolParams{Name: call.Tool, Arguments: args})
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return "", fmt.Errorf("mcp tool %s timed out after %s: %w", call.Tool, timeout, context.DeadlineExceeded)
		}
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", err
	}
	if result == nil {
		return "", errors.New("MCP server returned no tool result")
	}
	output := flattenResult(result)
	if result.IsError {
		return output, errors.New(strings.TrimPrefix(output, "Error: "))
	}
	return output, nil
}

// Instructions returns bounded server usage guidance and its current source.
// Guidance is data; it grants no tool authority and must be fetched again after
// a generation change. Larger guidance fails explicitly rather than clipping.
func (m *Manager) Instructions(name string) (text, generation, source string, err error) {
	s, err := m.lookup(name)
	if err != nil {
		return "", "", "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sess == nil || s.status != StatusReady {
		return "", "", "", s.unavailableLocked()
	}
	if len(s.instr) > 8<<20 {
		return "", "", "", errors.New("MCP instructions exceed 8 MiB")
	}
	return s.instr, s.generation, s.cfg.Source, nil
}

func (s *server) refreshCatalog(m *Manager, sess *sdkmcp.ClientSession) {
	s.mu.Lock()
	s.catalogChanges++
	if s.sess != sess {
		s.mu.Unlock()
		return
	}
	s.defs = nil // invalidate before fetching so an obsolete admission cannot run
	if s.connectionStop != nil {
		s.connectionStop()
	}
	s.connectionCtx, s.connectionStop = context.WithCancel(m.runCtx)
	s.generation = rand.Text()
	generation, ctx, timeout := s.generation, s.connectionCtx, s.cfg.StartupTimeoutDuration()
	s.mu.Unlock()
	m.launch("MCP catalog refresh "+s.name, func() {
		ctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		listed, err := sess.ListTools(ctx, nil)
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.sess != sess || s.generation != generation {
			return
		}
		if err != nil {
			s.err = "tool catalog refresh failed"
			return
		}
		s.defs = listed.Tools
	})
}
