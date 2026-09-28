// whip-contract generates the initial v4 schemas and interchange fixtures.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/context-labs/whip/internal/protocol"
)

func main() {
	out := flag.String("out", "packages/protocol/schema", "generated schema directory")
	check := flag.Bool("check", false, "check deterministic generation")
	validate := flag.Bool("validate", false, "validate a fixture array from stdin")
	flag.Parse()
	if err := run(*out, *check, *validate); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(out string, check, validate bool) error {
	if validate {
		raw, err := io.ReadAll(io.LimitReader(os.Stdin, (8<<20)+1))
		if err != nil {
			return err
		}
		if len(raw) > 8<<20 {
			return errors.New("fixtures exceed limit")
		}
		var fixtures []protocol.Fixture
		if err := json.Unmarshal(raw, &fixtures); err != nil {
			return err
		}
		for _, fixture := range fixtures {
			if err := protocol.Validate(fixture.Type, fixture.Value); (err == nil) != fixture.Valid {
				return fmt.Errorf("%s validation disagrees: %w", fixture.Type, err)
			}
		}
		return nil
	}
	files := map[string]any{}
	for name, typ := range protocol.Types() {
		schema, err := protocol.SchemaFor(typ)
		if err != nil {
			return err
		}
		files[name+".json"] = schema
	}
	type operation struct {
		Name   string `json:"name"`
		Params string `json:"params"`
		Result string `json:"result"`
	}
	operations := []operation{}
	for _, op := range protocol.Operations() {
		operations = append(operations, operation{op.Name, op.Params.Name(), op.Result.Name()})
	}
	files["manifest.json"] = struct {
		Major      int         `json:"major"`
		Minor      int         `json:"minor"`
		Operations []operation `json:"operations"`
	}{protocol.Major, protocol.Minor, operations}
	fixtures, err := protocol.Fixtures()
	if err != nil {
		return err
	}
	files["fixtures.json"] = fixtures
	if !check {
		//nolint:gosec // Generated public contract artifacts must be readable by repository tooling and package consumers.
		if err := os.MkdirAll(out, 0o755); err != nil {
			return err
		}
	}
	for name, value := range files {
		raw, err := json.MarshalIndent(value, "", "  ")
		if err != nil {
			return err
		}
		raw = append(raw, '\n')
		path := filepath.Join(out, name)
		if check {
			//nolint:gosec // The CLI caller explicitly selects the generated-artifact directory; filenames come from the contract registry.
			actual, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if !bytes.Equal(actual, raw) {
				return fmt.Errorf("generated contract drift: %s", path)
			}
		} else if err := os.WriteFile(path, raw, 0o644); err != nil { //nolint:gosec // Public generated schemas and fixtures contain no secrets.
			return err
		}
	}
	entries, err := os.ReadDir(out)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if _, ok := files[entry.Name()]; !ok {
			return fmt.Errorf("stale generated schema: %s", entry.Name())
		}
	}
	return nil
}
