package main

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProviderFixtureCannotReachExternalNetwork(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "provider-control.json"), []byte(`{"login":"expired"}`), 0600); err != nil {
		t.Fatal(err)
	}
	tr := &transport{directory: dir}
	for _, target := range []string{"https://example.com/private", "https://api.inference.net/v1/chat/completions", "https://observability-api.inference.net/unexpected"} {
		r, _ := http.NewRequestWithContext(t.Context(), http.MethodPost, target, nil)
		if _, err := tr.RoundTrip(r); err == nil {
			t.Fatalf("external or unexpected effect allowed: %s", target)
		}
	}
	r, _ := http.NewRequestWithContext(t.Context(), http.MethodPost, "https://observability-api.inference.net/api/auth/device/token", nil)
	response, err := tr.RoundTrip(r)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil || response.StatusCode != 400 || !strings.Contains(string(body), "expired_token") {
		t.Fatalf("response %s: %v", body, err)
	}
}
