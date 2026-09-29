package config

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestVersion21ReadPreservesBytesAndChangedUpdatePublishes22(t *testing.T) {
	a, _ := configAuthority(t, Default())
	path := filepath.Join(a.directory, FileName)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	old := bytes.Replace(raw, []byte(`"version": 22`), []byte(`"version": 21`), 1)
	if err := os.WriteFile(path, old, 0o600); err != nil {
		t.Fatal(err)
	}
	value, err := a.Snapshot(t.Context())
	if err != nil || value.Host.Version != Version {
		t.Fatalf("read old configuration: %+v %v", value, err)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(after, old) {
		t.Fatal("read rewrote old configuration")
	}
	_, err = a.Update(t.Context(), value.Revision, func(h *Host) error {
		h.Providers["fixture"] = Provider{Kind: "openai-chat", BaseURL: "https://fixture.test/v1", Disabled: true}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	after, err = os.ReadFile(path)
	if err != nil || !bytes.Contains(after, []byte(`"version": 22`)) {
		t.Fatal("changed configuration did not advance version")
	}
}
