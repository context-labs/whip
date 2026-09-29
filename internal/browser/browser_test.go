package browser

import (
	"context"
	"testing"
)

func TestParseDevToolsActivePort(t *testing.T) {
	port, path, err := parseDevToolsActivePort([]byte("9222\n/devtools/browser/abc-123\n"))
	if err != nil || port != 9222 || path != "/devtools/browser/abc-123" {
		t.Fatalf("got %d %q %v", port, path, err)
	}
	if _, _, err := parseDevToolsActivePort([]byte("9222\n")); err == nil {
		t.Fatal("one line must fail")
	}
	if _, _, err := parseDevToolsActivePort([]byte("notaport\n/x")); err == nil {
		t.Fatal("non-numeric port must fail")
	}
}

func TestCheckURLFloor(t *testing.T) {
	ctx := context.Background()
	for _, u := range []string{
		"http://169.254.169.254/latest/meta-data",
		"http://metadata.google.internal/",
		"http://169.254.170.2/v2/metadata",
	} {
		if err := CheckURL(ctx, u); err == nil {
			t.Errorf("%s must be blocked", u)
		}
	}
	if err := CheckURL(ctx, "https://example.com/"); err != nil {
		t.Errorf("example.com must pass: %v", err)
	}
	if err := CheckURL(ctx, "chrome://newtab"); err != nil {
		t.Errorf("non-http schemes pass: %v", err)
	}
}

func TestCheckPrivateURL(t *testing.T) {
	ctx := context.Background()
	for _, u := range []string{
		"http://127.0.0.1:8080/",
		"http://10.0.0.5/",
		"http://192.168.1.1/",
		"http://100.64.1.2/", // CGNAT
		"http://[::1]/",
	} {
		if err := CheckPrivateURL(ctx, u, false); err == nil {
			t.Errorf("%s must be blocked", u)
		}
	}
	if err := CheckPrivateURL(ctx, "https://example.com/", false); err != nil {
		t.Errorf("example.com must pass: %v", err)
	}
}

func TestNativeSessionNameCannotOverrideCapturedMode(t *testing.T) {
	for _, name := range []string{"", "../escape", "dedicated:name", "live:name", "name:extra", "name/path"} {
		if _, err := nativeName(name, "headless"); err == nil {
			t.Fatal("invalid captured name accepted", name)
		}
	}
	for _, name := range []string{"headless:named", "named"} {
		if value, err := nativeName(name, "headless"); err != nil || value != "named" {
			t.Fatal(value, err)
		}
	}
}
