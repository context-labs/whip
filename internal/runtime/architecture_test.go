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
	command := exec.CommandContext(t.Context(), "go", "list", "-json", "./internal/model", "./internal/openaiauth", "./internal/inferenceauth", "./internal/account", "./internal/runner", "./internal/tool", "./internal/instruction", "./internal/skills", "./internal/runtime", "./internal/rpc", "./internal/client")
	command.Dir = "../.."
	raw, err := command.Output()
	if err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	const prefix = "github.com/context-labs/whip/internal/"
	allowed := map[string]map[string]bool{
		"model": {"session": true, "openaiauth": true, "inferenceauth": true}, "runner": {"session": true, "model": true},
		// Credentials remain a host-owned leaf, independent of session execution.
		"openaiauth":    {},
		"inferenceauth": {},
		"account":       {"config": true, "openaiauth": true},
		"tool":          {"session": true, "capability": true},
		"instruction":   {"session": true, "skills": true},
		"skills":        {"buildinfo": true},
		"runtime":       {"model": true, "session": true, "store": true, "config": true, "content": true, "runner": true, "engine/process": true, "tool": true, "instruction": true},
		"rpc":           {"account": true, "session": true, "store": true, "protocol": true, "runtime": true},
		"client":        {"protocol": true},
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
