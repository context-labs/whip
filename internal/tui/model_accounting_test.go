package tui

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/daemon"
	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/session"
)

func TestModelAccountingUsesDurableTreeChargesAndShowsUncertainty(t *testing.T) {
	m := model{catalogs: map[string]config.Catalog{"p": {Models: []config.ModelInfoLite{{ID: "m", Pricing: llm.Pricing{Prompt: "999", Completion: "999"}}}}}}
	a := session.ModelAccounting{RootID: "root", Scope: "subtree", ReportedCostMicros: 12345, EstimatedCostMicros: 5000, EstimatedCostCalls: 1, UnknownCostCalls: 2, PendingCalls: 1}
	m.clientView.accounting = a
	rows := fmt.Sprint(m.accountingRows())
	for _, want := range []string{"tree reported $0.0123", "tree estimated $0.0050", "cost unknown 2 calls", "accounting pending 1 calls"} {
		if !strings.Contains(rows, want) {
			t.Fatalf("rows=%s missing=%s", rows, want)
		}
	}
	// Catalog reloads cannot reprice completed calls.
	m.catalogs = nil
	if got := fmt.Sprint(m.accountingRows()); got != rows {
		t.Fatalf("reprice: %s -> %s", rows, got)
	}
	// Tree totals update even while a descendant is visible.
	m.agentOpen = "child"
	a.ReportedCostMicros = 0
	a.Revision = 10
	payload, err := json.Marshal(daemon.StreamEvent{Accounting: &a})
	if err != nil {
		t.Fatal(err)
	}
	handled, _ := m.applyClientStream("stream.accounting", payload)
	if !handled || m.clientView.accounting.ReportedCostMicros != 0 {
		t.Fatalf("free reported charge not applied: %+v", m.clientView.accounting)
	}
}

func TestModelAccountingIgnoresOlderQueuedSummary(t *testing.T) {
	m := model{}
	m.clientView.accounting = session.ModelAccounting{RootID: "root", Revision: 20, ReportedCostMicros: 300}
	payload, err := json.Marshal(daemon.StreamEvent{Accounting: &session.ModelAccounting{RootID: "root", Revision: 19, ReportedCostMicros: 100}})
	if err != nil {
		t.Fatal(err)
	}
	m.applyClientStream("stream.accounting", payload)
	if m.clientView.accounting.ReportedCostMicros != 300 {
		t.Fatal("older queued summary repriced the snapshot")
	}
	m.clientView.accounting.EstimatedCostCalls = 1
	if !strings.Contains(fmt.Sprint(m.accountingRows()), "tree estimated $0.0000") {
		t.Fatal("free estimated call lost provenance")
	}
}

func TestModelAccountingUnreportedUsageDoesNotInventEstimatedCost(t *testing.T) {
	m := model{}
	m.clientView.accounting = session.ModelAccounting{RootID: "root", EstimatedCalls: 1, UnknownCostCalls: 1}
	rows := fmt.Sprint(m.accountingRows())
	if !strings.Contains(rows, "usage unreported 1 calls") || !strings.Contains(rows, "cost unknown 1 calls") || strings.Contains(rows, "tree estimated") {
		t.Fatalf("uncertain usage became a zero cost estimate: %s", rows)
	}
}
