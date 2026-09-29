package mcp

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"
	"time"

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
func (s *server) retireLocked() *retiredConnection {
	if s.connectionStop != nil {
		s.connectionStop()
	}
	if s.transportStop != nil {
		s.transportStop()
	}
	old := &retiredConnection{session: s.sess, join: s.transportJoin}
	s.transportJoin = nil
	s.sess = nil
	s.clearCatalogLocked()
	if old.session == nil && old.join == nil {
		return nil
	}
	return old
}

type retiredConnection struct {
	session *sdkmcp.ClientSession
	join    func()
}

func (c *retiredConnection) Close() error {
	var err error
	if c.session != nil {
		err = c.session.Close()
	}
	if c.join != nil {
		c.join()
	}
	return err
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
	if len(arguments) > 1<<20 {
		return nil, errors.New("MCP tool arguments exceed byte limit")
	}
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

// AcquiredCall owns one serialized server slot and the exact captured
// descriptor lifetime. Execute is one-use; Close must run on every path.
type AcquiredCall struct {
	server  *server
	call    capability.MCPCall
	ctx     context.Context
	cancel  context.CancelFunc
	stop    func() bool
	timeout time.Duration
	close   sync.Once
	used    atomic.Bool
}

func (m *Manager) AcquireCall(ctx context.Context, call capability.MCPCall) (*AcquiredCall, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	call.Arguments = slices.Clone(call.Arguments)
	s, err := m.lookup(call.Server)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	current, schema, err := s.descriptorLocked(call.Tool)
	lifetime, timeout := s.connectionCtx, s.cfg.ToolTimeoutDuration()
	s.mu.Unlock()
	if err != nil {
		return nil, err
	}
	if !matchesCall(current, call) {
		return nil, errors.New("MCP tool definition changed; resolve and admit again")
	}
	arguments, err := toolArguments(schema, call.Arguments)
	if err != nil {
		return nil, err
	}
	call.Arguments = arguments
	ctx, cancel := context.WithTimeout(ctx, timeout)
	stop := context.AfterFunc(lifetime, cancel)
	if err := lifetime.Err(); err != nil {
		stop()
		cancel()
		return nil, err
	}
	select {
	case s.calling <- struct{}{}:
	case <-ctx.Done():
		stop()
		cancel()
		return nil, ctx.Err()
	}
	acquired := &AcquiredCall{server: s, call: call, ctx: ctx, cancel: cancel, stop: stop, timeout: timeout}
	if err := acquired.validate(); err != nil {
		acquired.Close()
		return nil, err
	}
	return acquired, nil
}

func (c *AcquiredCall) boundSession() (*sdkmcp.ClientSession, error) {
	if err := c.ctx.Err(); err != nil {
		return nil, err
	}
	c.server.mu.Lock()
	defer c.server.mu.Unlock()
	current, _, err := c.server.descriptorLocked(c.call.Tool)
	if err != nil {
		return nil, err
	}
	if !matchesCall(current, c.call) {
		return nil, errors.New("MCP tool definition changed during admission; resolve and admit again")
	}
	if err := c.server.connectionCtx.Err(); err != nil {
		return nil, err
	}
	return c.server.sess, nil
}
func (c *AcquiredCall) validate() error { _, err := c.boundSession(); return err }

func (c *AcquiredCall) Close() { c.close.Do(func() { c.stop(); c.cancel(); <-c.server.calling }) }

func (c *AcquiredCall) Execute(parent context.Context) (capability.MCPResult, error) {
	var none capability.MCPResult
	if !c.used.CompareAndSwap(false, true) {
		return none, errors.New("MCP call already executed; automatic replay prohibited")
	}
	stop := context.AfterFunc(parent, c.cancel)
	defer stop()
	if err := parent.Err(); err != nil {
		return none, err
	}
	sess, err := c.boundSession()
	if err != nil {
		return none, err
	}
	result, err := sess.CallTool(c.ctx, &sdkmcp.CallToolParams{Name: c.call.Tool, Arguments: c.call.Arguments})
	if err != nil {
		if errors.Is(c.ctx.Err(), context.DeadlineExceeded) {
			return none, fmt.Errorf("mcp tool %s timed out after %s: %w", c.call.Tool, c.timeout, context.DeadlineExceeded)
		}
		if c.ctx.Err() != nil {
			return none, c.ctx.Err()
		}
		return none, err
	}
	if result == nil {
		return none, errors.New("MCP server returned no tool result")
	}
	return checkedResult(result)
}

// CallChecked keeps the retained callback contract: validate and serialize
// before durable authorization, then revalidate immediately before transmission.
func (m *Manager) CallChecked(ctx context.Context, call capability.MCPCall, before func(context.Context) error) (capability.MCPResult, error) {
	acquired, err := m.AcquireCall(ctx, call)
	if err != nil {
		return capability.MCPResult{}, err
	}
	defer acquired.Close()
	if before != nil {
		if err := before(acquired.ctx); err != nil {
			return capability.MCPResult{}, err
		}
	}
	return acquired.Execute(ctx)
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
	_ = s.setCatalogLocked(nil, s.instr)
	if s.connectionStop != nil {
		s.connectionStop()
	}
	s.connectionCtx, s.connectionStop = context.WithCancel(m.runCtx)
	s.generation = rand.Text()
	if s.refreshing == sess {
		s.mu.Unlock()
		return
	}
	s.refreshing = sess
	s.mu.Unlock()
	if !m.launch("MCP catalog refresh "+s.name, func() {
		for {
			s.mu.Lock()
			if s.sess != sess {
				if s.refreshing == sess {
					s.refreshing = nil
				}
				s.mu.Unlock()
				return
			}
			generation, ctx, timeout := s.generation, s.connectionCtx, s.cfg.StartupTimeoutDuration()
			s.mu.Unlock()
			listed, err := listToolsWithin(ctx, timeout, sess)
			s.mu.Lock()
			if s.sess != sess {
				if s.refreshing == sess {
					s.refreshing = nil
				}
				s.mu.Unlock()
				return
			}
			if s.generation != generation {
				s.mu.Unlock()
				continue
			}
			if err != nil {
				s.err = "tool catalog refresh failed"
			} else if err = s.setCatalogLocked(listed, s.instr); err != nil {
				s.err = "tool catalog exceeds aggregate limit"
			}
			s.refreshing = nil
			s.mu.Unlock()
			return
		}
	}) {
		s.mu.Lock()
		if s.refreshing == sess {
			s.refreshing = nil
			s.err = "tool catalog refresh unavailable"
		}
		s.mu.Unlock()
	}
}

func listToolsWithin(parent context.Context, timeout time.Duration, sess *sdkmcp.ClientSession) ([]*sdkmcp.Tool, error) {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	return listAllTools(ctx, sess)
}
