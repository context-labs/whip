package extrelay

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNativeStateOwnedPublication(t *testing.T) {
	directory := t.TempDir()
	if err := WriteNativeState(directory, "127.0.0.1:1234", strings.Repeat("a", 48)); err == nil {
		t.Fatal("published before explicit installation")
	}
	if _, err := WriteExtension(directory); err != nil {
		t.Fatal(err)
	}
	first, second := strings.Repeat("a", 48), strings.Repeat("b", 48)
	if err := WriteNativeState(directory, "127.0.0.1:1234", first); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "relay.json")
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatal(info, err)
	}
	if err := WriteNativeState(directory, "127.0.0.1:2345", second); err != nil {
		t.Fatal(err)
	}
	if err := RemoveNativeState(directory, first); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var value struct {
		Token string `json:"token"`
	}
	if json.Unmarshal(raw, &value) != nil || value.Token != second {
		t.Fatal("obsolete cleanup removed current publication")
	}
	if err := RemoveNativeState(directory, second); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal(err)
	}
}
