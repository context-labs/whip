package store

import (
	"bytes"
	"encoding/json"
	"io"
	"os/exec"
	"strings"
	"testing"
)

func TestCoreImportBoundaries(t *testing.T) {
	command := exec.CommandContext(t.Context(), "go", "list", "-deps", "-json", "./internal/session", "./internal/config", "./internal/store", "./internal/protocol")
	command.Dir = "../.."
	output, err := command.Output()
	if err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(bytes.NewReader(output))
	const prefix = "github.com/context-labs/whip/internal/"
	// The schedule parser is a pure leaf shared with retained consumers; it may
	// import no internal packages, persistence, filesystem or runtime authority.
	allowed := map[string]map[string]bool{
		"session": {"schedule": true, "hostmodule": true}, "config": {"session": true, "lspconfig": true, "mcpconfig": true},
		"store": {"session": true, "schedule": true}, "protocol": {"session": true, "hostmodule": true}, "schedule": {}, "lspconfig": {}, "mcpconfig": {}, "hostmodule": {},
	}
	for {
		var pkg struct {
			ImportPath string
			Imports    []string
		}
		if err := decoder.Decode(&pkg); err == io.EOF {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		name := strings.TrimPrefix(pkg.ImportPath, prefix)
		if _, ok := allowed[name]; !ok {
			continue
		}
		for _, dependency := range pkg.Imports {
			if internal, ok := strings.CutPrefix(dependency, prefix); ok && !allowed[name][internal] {
				t.Errorf("%s crosses the core boundary into %s", pkg.ImportPath, dependency)
			}
			if (name == "schedule" || name == "lspconfig" || name == "mcpconfig" || name == "hostmodule") && (dependency == "os" || dependency == "os/exec" || dependency == "net" || dependency == "net/http") {
				t.Errorf("pure %s leaf imports side-effect capability %s", name, dependency)
			}
			if pkg.ImportPath != prefix+"store" && (dependency == "database/sql" || dependency == "modernc.org/sqlite") {
				t.Errorf("%s owns database access", pkg.ImportPath)
			}
		}
	}
}
