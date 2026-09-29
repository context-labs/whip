package runtime

import (
	"bytes"
	"encoding/json"
	"io"
	"os/exec"
	"strings"
	"testing"
)

func TestExecutionAndClientImportBoundaries(t *testing.T) {
	command := exec.CommandContext(t.Context(), "go", "list", "-json", "./internal/trace", "./internal/hostview", "./internal/theme", "./internal/computer", "./internal/computerconfig", "./internal/helperprogram", "./internal/browser", "./internal/browserhost", "./internal/terminal", "./internal/mcp", "./internal/mcpconfig", "./internal/jsonc", "./internal/secretref", "./internal/brandicon", "./internal/bashrun", "./internal/shell", "./internal/executor", "./internal/gateway", "./internal/hostmodule", "./internal/engine/process", "./internal/model", "./internal/openaiauth", "./internal/inferenceauth", "./internal/account", "./internal/providerhost", "./internal/capability", "./internal/lsp", "./internal/lspconfig", "./internal/inferenceaccount", "./internal/runner", "./internal/tool", "./internal/instruction", "./internal/skills", "./internal/workspace", "./internal/runtime", "./internal/rpc", "./internal/client")
	command.Dir = "../.."
	raw, err := command.Output()
	if err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	const prefix = "github.com/context-labs/whip/internal/"
	allowed := map[string]map[string]bool{
		"computer":       {"browser": true, "computerconfig": true, "helperprogram": true, "capability": true, "buildinfo": true},
		"computerconfig": {}, "helperprogram": {}, "browser": {"buildinfo": true, "capability": true, "browser/extrelay": true, "helperprogram": true}, "browserhost": {"session": true},
		"hostview": {"capability": true, "session": true}, "theme": {}, "trace": {"session": true},
		"terminal":       {"capability": true},
		"mcp":            {"buildinfo": true, "capability": true, "jsonc": true, "mcpconfig": true, "secretref": true},
		"mcpconfig":      {},
		"jsonc":          {},
		"secretref":      {"capability": true},
		"brandicon":      {},
		"bashrun":        {"capability": true},
		"shell":          {"bashrun": true, "capability": true},
		"gateway":        {"protocol": true},
		"hostmodule":     {},
		"engine/process": {"engine": true, "engine/quickjs": true, "hostmodule": true},
		"model":          {"session": true, "openaiauth": true, "inferenceauth": true}, "runner": {"session": true, "model": true},
		// Credentials remain a host-owned leaf, independent of session execution.
		"openaiauth":       {},
		"inferenceauth":    {},
		"inferenceaccount": {"inferenceauth": true},
		"account":          {"config": true, "openaiauth": true},
		"providerhost":     {"config": true, "model": true, "session": true, "openaiauth": true, "inferenceauth": true},
		"capability":       {"buildinfo": true},
		"lsp":              {"capability": true, "lspconfig": true},
		"lspconfig":        {},
		"tool":             {"session": true, "capability": true},
		"instruction":      {"session": true, "skills": true},
		"skills":           {"buildinfo": true},
		"workspace":        {"session": true, "capability": true},
		"executor":         {"session": true},
		"runtime":          {"browser": true, "browserhost": true, "trace": true, "hostview": true, "theme": true, "computer": true, "computerconfig": true, "mcp": true, "mcpconfig": true, "brandicon": true, "bashrun": true, "shell": true, "executor": true, "lsp": true, "capability": true, "model": true, "session": true, "store": true, "config": true, "content": true, "runner": true, "engine/process": true, "tool": true, "instruction": true, "workspace": true},
		"rpc":              {"browserhost": true, "hostview": true, "theme": true, "computerconfig": true, "terminal": true, "bashrun": true, "mcp": true, "mcpconfig": true, "shell": true, "executor": true, "lsp": true, "providerhost": true, "account": true, "inferenceaccount": true, "config": true, "session": true, "store": true, "protocol": true, "runtime": true},
		"client":           {"protocol": true},
	}
	for {
		var pkg struct {
			ImportPath string
			Imports    []string
		}
		err := decoder.Decode(&pkg)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		name := strings.TrimPrefix(pkg.ImportPath, prefix)
		for _, dependency := range pkg.Imports {
			if dependency == "database/sql" || dependency == "modernc.org/sqlite" {
				t.Errorf("%s owns SQL outside store", name)
			}
			if internal, ok := strings.CutPrefix(dependency, prefix); ok && !allowed[name][internal] {
				t.Errorf("%s crosses boundary into %s", name, internal)
			}
		}
	}
}
