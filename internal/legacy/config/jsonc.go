package config

import (
	"encoding/json"
	"github.com/context-labs/whip/internal/jsonc"
	"os"
	"path/filepath"
)

func stripJSONC(src []byte) ([]byte, error) { return jsonc.Strip(src) }
func ParseJSONC(data []byte, v any) error   { return jsonc.Parse(data, v) }

// ReadJSON reads a small JSON file from the loopy dir into v. Missing file
// is an error the caller treats as "empty" — these are optional state files.
func ReadJSON(name string, v any) error {
	dir, err := Dir()
	if err != nil {
		return err
	}
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

// WriteJSON writes v as JSON to a small file in the loopy dir.
func WriteJSON(name string, v any) error {
	dir, err := Dir()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, name), data, 0o600)
}
