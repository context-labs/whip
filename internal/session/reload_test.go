package session

import (
	"reflect"
	"testing"
)

func TestReloadRefreshesInheritedFieldsWithoutInferringExplicitValues(t *testing.T) {
	current := Configuration{Modules: []string{}, MCPServers: &MCPSelection{Servers: []string{}}, Model: ModelSelection{Provider: "saved", Name: "model", Effort: "low"}, Compaction: CompactionPolicy{ThresholdPercent: 50}, Instructions: Instructions{Text: "old"}, Run: &RunConfiguration{System: "run"}}
	host := current.Clone()
	host.Modules = []string{"files"}
	host.MCPServers = &MCPSelection{All: true}
	host.AutomaticTitle = true
	host.GoalsEnabled = true
	host.ReportMode = ReportInline
	host.Compaction.ThresholdPercent = 75
	host.Instructions.Text = "fresh"
	host.Model.Name = "changed"
	for _, patch := range []ConfigPatch{{Modules: []string{}, MCPServers: &MCPSelection{}, AutomaticTitle: new(false), GoalsEnabled: new(false), ReportMode: new(ReportMode("")), Compaction: &current.Compaction, Instructions: &current.Instructions}} {
		result, err := RefreshConfiguration(current, host, DefinitionDocument{}, ExplicitReloadOverrides(patch))
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(result, current) {
			t.Fatal("explicit equal/empty/false lost", result)
		}
	}
	result, err := RefreshConfiguration(current, host, DefinitionDocument{}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if result.Compaction.ThresholdPercent != 75 || result.Instructions.Text != "fresh" || len(result.Modules) != 1 || !result.Model.Equal(current.Model) || !reflect.DeepEqual(result.Run, current.Run) {
		t.Fatal(result)
	}
	def := DefinitionDocument{ID: "pinned", Name: "Pinned", Defaults: ConfigPatch{Instructions: &Instructions{Text: "immutable"}, Compaction: &CompactionPolicy{ThresholdPercent: 60}}}
	result, err = RefreshConfiguration(current, host, def, 0)
	if err != nil || result.Instructions.Text != "immutable" || result.Compaction.ThresholdPercent != 60 {
		t.Fatal(result, err)
	}
}
