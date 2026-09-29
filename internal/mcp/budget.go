package mcp

import (
	"encoding/json"
	"errors"
	"sync"

	"github.com/context-labs/whip/internal/mcpconfig"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	MaxServers           = 64
	MaxRootConnections   = 16
	MaxHostConnections   = 128
	MaxRootManagers      = 128
	MaxHostCatalogBytes  = 8 << 20
	MaxHostWireBytes     = 64 << 20
	MaxHostResultBytes   = 64 << 20
	MaxHostConfigBytes   = 8 << 20
	MaxHostMetadataBytes = 8 << 20
	MaxHostCatalogTools  = 8192
)

type catalogSize struct{ bytes, tools int }

// Budget bounds aggregate retained catalogs and configured connection slots.
// One runtime shares it across every root manager; children borrow that manager.
// A disabled server consumes no connection slot until explicitly enabled.
type Budget struct {
	mu                         sync.Mutex
	managers                   map[*Manager]int
	configSizes                map[*Manager]int
	metadataSizes              map[*Manager]int
	configBytes, metadataBytes int
	catalogs                   map[*server]catalogSize
	connections, bytes, tools  int
	wireBytes                  int
	resultBytes                int
}

func NewBudget() *Budget {
	return &Budget{managers: map[*Manager]int{}, configSizes: map[*Manager]int{}, metadataSizes: map[*Manager]int{}, catalogs: map[*server]catalogSize{}}
}

// NewBoundedManager reserves ownership before any server can be started.
func NewBoundedManager(configs map[string]ServerConfig, budget *Budget) (*Manager, error) {
	if budget == nil {
		return nil, errors.New("MCP aggregate budget is required")
	}
	if len(configs) > MaxServers {
		return nil, errors.New("MCP server count exceeds root limit")
	}
	for name := range configs {
		if !mcpconfig.ValidName(name) {
			return nil, errors.New("invalid MCP server name")
		}
	}
	m := NewManager(configs)
	m.budget = budget
	if err := budget.reserve(m, lenActive(configs), true, configsSize(configs)); err != nil {
		m.Close()
		return nil, err
	}
	for _, s := range m.servers {
		s.configBytes = configSize(s.name, s.cfg)
		s.reserved = !s.cfg.Disabled() && s.cfg.Valid() == ""
	}
	return m, nil
}

func lenActive(configs map[string]ServerConfig) int {
	count := 0
	for _, cfg := range configs {
		if !cfg.Disabled() && cfg.Valid() == "" {
			count++
		}
	}
	return count
}

func (b *Budget) reserve(m *Manager, count int, create bool, bytes int) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if create && len(b.managers) >= MaxRootManagers {
		return errors.New("MCP root manager limit reached")
	}
	if _, exists := b.managers[m]; !create && !exists {
		return errors.New("MCP resource owner is retired")
	}
	if b.configBytes+bytes > MaxHostConfigBytes {
		return errors.New("MCP aggregate declaration byte limit reached")
	}
	if b.connections+count > MaxHostConnections || b.managers[m]+count > MaxRootConnections {
		return errors.New("MCP connection slot limit reached")
	}
	b.managers[m] += count
	b.connections += count
	b.configBytes += bytes
	b.configSizes[m] += bytes
	return nil
}

func (b *Budget) releaseServers(m *Manager, count, bytes int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, exists := b.managers[m]; !exists {
		return
	}
	b.managers[m] -= count
	b.connections -= count
	b.configSizes[m] -= bytes
	b.configBytes -= bytes
}

func (b *Budget) releaseManager(m *Manager) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.connections -= b.managers[m]
	b.configBytes -= b.configSizes[m]
	delete(b.configSizes, m)
	b.metadataBytes -= b.metadataSizes[m]
	delete(b.metadataSizes, m)
	delete(b.managers, m)
	for s, size := range b.catalogs {
		if s.owner == m {
			b.bytes -= size.bytes
			b.tools -= size.tools
			delete(b.catalogs, s)
		}
	}
}

func configSize(name string, cfg ServerConfig) int {
	raw, _ := json.Marshal(cfg)
	return len(name) + len(raw) + 32
}

func configsSize(configs map[string]ServerConfig) int {
	size := 0
	for name, cfg := range configs {
		size += configSize(name, cfg)
	}
	return size
}

func (m *Manager) reserveMetadataLocked(blocked, sourceErrors []Server) error {
	raw, err := json.Marshal(struct{ Blocked, Errors []Server }{blocked, sourceErrors})
	if err != nil || len(blocked) > 256 || len(sourceErrors) > 16 || len(raw) > 256<<10 {
		return errors.New("MCP metadata exceeds root limit")
	}
	if m.budget == nil {
		return nil
	}
	b := m.budget
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, exists := b.managers[m]; !exists {
		return errors.New("MCP resource owner is retired")
	}
	previous := b.metadataSizes[m]
	if b.metadataBytes-previous+len(raw) > MaxHostMetadataBytes {
		return errors.New("MCP aggregate metadata byte limit reached")
	}
	b.metadataBytes += len(raw) - previous
	b.metadataSizes[m] = len(raw)
	return nil
}

func (s *server) setCatalogLocked(defs []*sdkmcp.Tool, instructions string) error {
	raw, err := json.Marshal(defs)
	if err != nil {
		return err
	}
	next := catalogSize{bytes: len(raw) + len(instructions), tools: len(defs)}
	if next.bytes > maxCatalogBytes || next.tools > maxCatalogTools {
		return errors.New("MCP catalog exceeds root limit")
	}
	if s.owner != nil && s.owner.budget != nil {
		b := s.owner.budget
		b.mu.Lock()
		defer b.mu.Unlock()
		previous := b.catalogs[s]
		rootBytes, rootTools := next.bytes, next.tools
		for other, size := range b.catalogs {
			if other != s && other.owner == s.owner {
				rootBytes += size.bytes
				rootTools += size.tools
			}
		}
		if rootBytes > maxCatalogBytes || rootTools > maxCatalogTools || b.bytes-previous.bytes+next.bytes > MaxHostCatalogBytes || b.tools-previous.tools+next.tools > MaxHostCatalogTools {
			return errors.New("MCP aggregate catalog limit reached")
		}
		b.bytes += next.bytes - previous.bytes
		b.tools += next.tools - previous.tools
		b.catalogs[s] = next
	}
	s.defs, s.instr = defs, instructions
	return nil
}

func (s *server) clearCatalogLocked() {
	if s.owner != nil && s.owner.budget != nil {
		b := s.owner.budget
		b.mu.Lock()
		previous := b.catalogs[s]
		b.bytes -= previous.bytes
		b.tools -= previous.tools
		delete(b.catalogs, s)
		b.mu.Unlock()
	}
	s.defs, s.instr = nil, ""
}

func (b *Budget) reserveWire(bytes int) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if bytes > MaxHostWireBytes-b.wireBytes {
		return false
	}
	b.wireBytes += bytes
	return true
}
func (b *Budget) releaseWire(bytes int) { b.mu.Lock(); b.wireBytes -= bytes; b.mu.Unlock() }

// Each dispatched call reserves its maximum possible response until the caller
// has projected and released it. Wire-buffer accounting alone cannot bound
// decoded results retained after the transport reader has drained a frame.
func (b *Budget) reserveResult() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.resultBytes > MaxHostResultBytes-maxWireBytes {
		return false
	}
	b.resultBytes += maxWireBytes
	return true
}
func (b *Budget) releaseResult() { b.mu.Lock(); b.resultBytes -= maxWireBytes; b.mu.Unlock() }
