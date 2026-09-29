package executor

import (
	"encoding/json"
	"testing"
)

func TestDirectHostExecutorRequiresExplicitAbsentCellProvenance(t *testing.T) {
	request := toolRequest()
	request.HostOperation = true
	if _, err := request.validate(Tool, "lookup"); err == nil {
		t.Fatal("direct request with cell accepted")
	}
	request.CellID = ""
	if _, err := request.validate(Tool, "lookup"); err != nil {
		t.Fatal(err)
	}
	request.OperationID = ""
	request.Operation = "files.read"
	if _, err := request.validate(Hook, "before_tool"); err != nil {
		t.Fatal(err)
	}
	if _, err := request.validate(Hook, "before_spawn"); err == nil {
		t.Fatal("direct spawn invented")
	}
	if _, err := request.validate(Hook, "turn_start"); err == nil {
		t.Fatal("direct context hook invented")
	}
	request.Operation = "agents.spawn"
	request.Arguments = nil
	request.Spawn = json.RawMessage(`{"request":{},"resolved":{}}`)
	if _, err := request.validate(Hook, "before_spawn"); err != nil {
		t.Fatal("direct spawn provenance rejected", err)
	}
	request.CellID = "invented-cell"
	if _, err := request.validate(Hook, "before_spawn"); err == nil {
		t.Fatal("direct spawn accepted invented cell")
	}
	request.CellID = ""
	request.HostOperation = false
	if _, err := request.validate(Hook, "before_tool"); err == nil {
		t.Fatal("cell provenance omitted")
	}
}
