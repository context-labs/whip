package store

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestMCPSelectionSchemaRejectsUncapturedPreviousState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.db")
	db := openTest(t, path)
	execTest(t, db, "PRAGMA user_version=39")
	if _, err := Open(t.Context(), path); !errors.Is(err, ErrSchema) {
		t.Fatal("pre-MCP-selection state accepted", err)
	}
}
