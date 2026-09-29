package mcp

import (
	"fmt"
	"strings"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestAggregateBudgetBoundsConnectionsAndReleasesRoots(t *testing.T) {
	budget := NewBudget()
	configs := map[string]ServerConfig{}
	for index := range MaxRootConnections {
		configs[fmt.Sprintf("server%d", index)] = ServerConfig{Command: []string{"never-start"}}
	}
	managers := []*Manager{}
	for range MaxHostConnections / MaxRootConnections {
		m, err := NewBoundedManager(configs, budget)
		if err != nil {
			t.Fatal(err)
		}
		managers = append(managers, m)
	}
	if _, err := NewBoundedManager(configs, budget); err == nil {
		t.Fatal("unbounded host connection reservation")
	}
	if _, err := managers[0].AddServers(t.Context(), map[string]ServerConfig{"extra": {Command: []string{"never-start"}}}); err == nil {
		t.Fatal("unbounded root connections")
	}
	for _, m := range managers {
		m.Close()
	}
	if budget.connections != 0 || len(budget.managers) != 0 {
		t.Fatalf("released roots retained ownership: %+v", budget)
	}
	m, err := NewBoundedManager(configs, budget)
	if err != nil {
		t.Fatal(err)
	}
	m.Close()
}

func TestMCPDecodedResultReservationsAreIndependentAndReleased(t *testing.T) {
	budget := NewBudget()
	for range MaxHostResultBytes / maxWireBytes {
		if !budget.reserveResult() {
			t.Fatal("capacity missing")
		}
	}
	if budget.reserveResult() {
		t.Fatal("decoded result capacity unbounded")
	}
	if !budget.reserveWire(maxWireBytes) {
		t.Fatal("result reservation consumed incoming wire capacity")
	}
	budget.releaseWire(maxWireBytes)
	budget.releaseResult()
	if !budget.reserveResult() {
		t.Fatal("result capacity not released")
	}
}

func TestMCPMetadataBoundsRejectAtomicallyAndRetirementReleasesDeclarations(t *testing.T) {
	budget := NewBudget()
	manager, err := NewBoundedManager(map[string]ServerConfig{"disabled": {Command: []string{"unused"}, Enabled: new(false)}}, budget)
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	if err := manager.AddBlocked(map[string]ServerConfig{"original": {Note: "retained"}}); err != nil {
		t.Fatal(err)
	}
	oversized := map[string]ServerConfig{}
	for index := range 257 {
		oversized[fmt.Sprintf("blocked_%d", index)] = ServerConfig{Note: "blocked"}
	}
	if err := manager.AddBlocked(oversized); err == nil {
		t.Fatal("unbounded attachment metadata")
	}
	if rows := manager.Blocked(); len(rows) != 1 || rows[0].Name != "original" {
		t.Fatal("failed metadata update published", rows)
	}
	manager.RemoveServers("disabled")
	if budget.configBytes != 0 {
		t.Fatal("disabled declaration retained after removal", budget.configBytes)
	}
	manager.Close()
	if budget.metadataBytes != 0 || budget.configBytes != 0 {
		t.Fatal("owner retained metadata budget")
	}
}

func TestAggregateCatalogRejectsAtomicallyAndRetiresBytes(t *testing.T) {
	budget := NewBudget()
	var managers []*Manager
	t.Cleanup(func() {
		for _, m := range managers {
			m.Close()
		}
	})
	entry := &sdkmcp.Tool{Name: "tool", Description: strings.Repeat("x", (MaxHostCatalogBytes/5)+1), InputSchema: map[string]any{"type": "object"}}
	for index := range 5 {
		m, err := NewBoundedManager(map[string]ServerConfig{"source": {Command: []string{"never-start"}}}, budget)
		if err != nil {
			t.Fatal(err)
		}
		managers = append(managers, m)
		s := m.servers["source"]
		s.mu.Lock()
		err = s.setCatalogLocked([]*sdkmcp.Tool{entry}, "")
		s.mu.Unlock()
		if (index == 4) != (err != nil) {
			t.Fatalf("catalog %d admission: %v", index, err)
		}
		if index == 4 && s.defs != nil {
			t.Fatal("failed catalog publication retained partial data")
		}
	}
	managers[0].Close()
	s := managers[4].servers["source"]
	s.mu.Lock()
	err := s.setCatalogLocked([]*sdkmcp.Tool{entry}, "")
	s.mu.Unlock()
	if err != nil {
		t.Fatalf("retired catalog still occupies byte budget: %v", err)
	}
	for _, m := range managers {
		m.Close()
	}
	if budget.bytes != 0 || budget.tools != 0 || len(budget.catalogs) != 0 {
		t.Fatalf("catalog charges leaked: %+v", budget)
	}
}
