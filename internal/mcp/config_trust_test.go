package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/context-labs/whip/internal/config"
)

func TestConfigTrustPreservesOriginThroughImportPersistence(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("WHIP_HOME", filepath.Join(dir, "whip"))
	source := filepath.Join(dir, ".mcp.json")
	imported := ServerConfig{Command: []string{"imported"}, Origin: "claude", Source: source}
	data, err := json.Marshal(imported)
	if err != nil {
		t.Fatal(err)
	}
	var saved config.MCPServer
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	restored := FromConfigMap(map[string]config.MCPServer{"imported": saved})["imported"]
	if restored.Trusted || restored.Origin != "claude" || restored.Source != source {
		t.Fatalf("import acquired native trust after save: %+v", restored)
	}
	native := FromConfigMap(map[string]config.MCPServer{"native": {Command: []string{"native"}}})["native"]
	if !native.Trusted || native.Origin != "whip" || native.Source != whipConfigPath() {
		t.Fatalf("native discovery did not establish provenance: %+v", native)
	}
	data, err = json.Marshal(native)
	if err != nil {
		t.Fatal(err)
	}
	decoded := native // reused values must lose non-serializable trust too
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Trusted {
		t.Fatal("JSON transported native trust")
	}
	if err := json.Unmarshal([]byte(`{"command":["spoofed"],"source":"~/.whip/config.json","origin":"whip","Trusted":true,"trusted":true}`), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Trusted {
		t.Fatal("client JSON claimed native trust")
	}
	attached := AttachedConfigs(map[string]ServerConfig{"native": native})["native"]
	if attached.Trusted || attached.Origin != "attachment" || attached.Source != "client attachment" {
		t.Fatalf("attachment acquired trust: %+v", attached)
	}
	merged := Merge(map[string]ServerConfig{"native": native}, map[string]ServerConfig{"native": attached}, nil, nil)
	if !merged["native"].Trusted || merged["native"].Origin != "whip" {
		t.Fatal("attachment shadowed rediscovered native config")
	}
}

func TestConfigDiscoveryReportsWinningProvenance(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("WHIP_HOME", filepath.Join(dir, "whip"))
	project := filepath.Join(dir, ".mcp.json")
	if err := os.WriteFile(project, []byte(`{"mcpServers":{"shared":{"command":"project"},"project":{"command":"project"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	codex := filepath.Join(dir, "codex.toml")
	if err := os.WriteFile(codex, []byte("[mcp_servers.shared]\ncommand = \"codex\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	oldCodex, oldClaude := CodexPath, ClaudeGlobalPath
	CodexPath = func() string { return codex }
	ClaudeGlobalPath = func() string { return filepath.Join(dir, "missing-global.json") }
	t.Cleanup(func() { CodexPath, ClaudeGlobalPath = oldCodex, oldClaude })
	native := FromConfigMap(map[string]config.MCPServer{"shared": {Command: []string{"native"}}})
	merged := LoadMergedFiltered(dir, native, ImportPolicyFrom(nil))
	if merged.Sources["shared"] != "whip" || !merged.Merged["shared"].Trusted || merged.Merged["shared"].Source != whipConfigPath() || merged.Merged["shared"].Command[0] != "native" {
		t.Fatalf("winning native provenance overwritten: %+v", merged)
	}
	if merged.Merged["project"].Trusted || merged.Merged["project"].Origin != "claude" || merged.Merged["project"].Source != project {
		t.Fatalf("import source lost: %+v", merged.Merged["project"])
	}
	// A filtered higher-precedence import must not leave a ghost blocked row
	// or falsely label the lower-precedence server that actually remains.
	filtered := LoadMergedFiltered(dir, nil, ImportPolicyFrom(&config.MCPImport{Codex: &config.MCPImportSource{Exclude: []string{"shared"}}}))
	if _, ghost := filtered.Blocked["shared"]; ghost || filtered.Sources["shared"] != ".mcp.json" || filtered.Merged["shared"].Source != project {
		t.Fatalf("blocked import shadowed live source: %+v", filtered)
	}
}

func TestManagerOwnsConfigMapsAndNeverTrustsSourceLabels(t *testing.T) {
	cfg := ServerConfig{Command: []string{"fixture"}, Env: map[string]string{"TOKEN": "original"}, Headers: map[string]string{"Authorization": "original"}, Source: "~/.whip/config.json", Origin: "claude", Trusted: true}
	m := NewManager(map[string]ServerConfig{"fixture": cfg})
	defer m.Close()
	cfg.Command[0], cfg.Env["TOKEN"], cfg.Headers["Authorization"] = "changed", "changed", "changed"
	stored, ok := m.Config("fixture")
	if !ok || stored.Command[0] != "fixture" || stored.Env["TOKEN"] != "original" || stored.Headers["Authorization"] != "original" || stored.Trusted {
		t.Fatalf("manager borrowed config data or trusted imported source: %+v", stored)
	}
	stored.Env["TOKEN"] = "changed through Config"
	again, _ := m.Config("fixture")
	if again.Env["TOKEN"] != "original" {
		t.Fatal("Config exposed manager-owned mutable map")
	}
}
