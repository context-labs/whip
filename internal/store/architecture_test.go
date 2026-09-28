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
	core := map[string]bool{prefix + "session": true, prefix + "config": true, prefix + "store": true, prefix + "protocol": true}
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
		if !core[pkg.ImportPath] {
			continue
		}
		for _, dependency := range pkg.Imports {
			if strings.HasPrefix(dependency, prefix) && dependency != prefix+"session" {
				t.Errorf("%s crosses the core boundary into %s", pkg.ImportPath, dependency)
			}
			if pkg.ImportPath != prefix+"store" && (dependency == "database/sql" || dependency == "modernc.org/sqlite") {
				t.Errorf("%s owns database access", pkg.ImportPath)
			}
		}
	}
}
