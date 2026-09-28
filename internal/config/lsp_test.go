package config

import (
	"errors"
	"reflect"
	"testing"

	"github.com/context-labs/whip/internal/lsp"
	"github.com/context-labs/whip/internal/session"
)

func TestHostLanguageServersRoundTripAndBound(t *testing.T) {
	host := Default()
	host.LSP = map[string]lsp.Config{"custom": {Command: []string{"uninstalled-server", "--stdio"}, Extensions: []string{".example"}, RootMarkers: []string{"project.json"}, Env: map[string]string{"MODE": "fixture"}}, "gopls": {Enabled: new(false)}}
	directory := t.TempDir()
	if err := Save(directory, host); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(directory)
	if err != nil || !reflect.DeepEqual(loaded.LSP, host.LSP) {
		t.Fatal(loaded.LSP, err)
	}
	loaded.LSP["custom"].Command[0] = "mutated"
	again, err := Load(directory)
	if err != nil || !reflect.DeepEqual(again.LSP, host.LSP) {
		t.Fatal("configuration aliased", again.LSP, err)
	}
	host.LSP["bad"] = lsp.Config{Command: []string{"server"}, Extensions: []string{".x"}, RootMarkers: []string{"../outside"}}
	if err := host.Validate(); !errors.Is(err, session.ErrInvalid) {
		t.Fatal("escaping marker accepted", err)
	}
}
