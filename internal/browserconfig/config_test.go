package browserconfig

import "testing"

func TestExplicitBrowserSelection(t *testing.T) {
	for _, test := range []struct {
		name   string
		config Config
		valid  bool
	}{
		{"default", Config{}, true},
		{"live endpoint", Config{Mode: "live", LiveEndpoint: "http://127.0.0.1:9222"}, true},
		{"live profile", Config{Mode: "live", LiveProfile: "/owned/profile"}, true},
		{"live no fallback", Config{Mode: "live"}, false},
		{"headless", Config{Mode: "headless", Executable: "/owned/chrome"}, true},
		{"no implicit binary", Config{Mode: "dedicated"}, false},
		{"extension", Config{Mode: "extension"}, true},
		{"ambiguous", Config{Mode: "live", LiveEndpoint: "http://127.0.0.1:9222", LiveProfile: "/owned/profile"}, false},
		{"relative", Config{Mode: "headless", Executable: "chrome"}, false},
		{"unclean", Config{LiveProfile: "/owned/../profile"}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := test.config.Normalize()
			if (err == nil) != test.valid {
				t.Fatalf("%+v: %v", got, err)
			}
			if test.name == "default" && got.Mode != "disabled" {
				t.Fatal(got)
			}
		})
	}
}

func TestEndpointAuthority(t *testing.T) {
	for _, raw := range []string{"http://127.0.0.1:9222", "http://[::1]:9222/", "ws://127.0.0.1:9999/devtools/browser/exact-id"} {
		if err := ValidateEndpoint(raw); err != nil {
			t.Error(raw, err)
		}
	}
	for _, raw := range []string{"http://localhost:9222", "https://127.0.0.1:9222", "ws://127.0.0.1:9222/devtools/page/1", "http://169.254.169.254:80", "http://127.0.0.1", "http://127.0.0.1:0", "http://127.0.0.1:65536", "http://x@127.0.0.1:9222", "http://127.0.0.1:9222?token=secret", "http://127.0.0.1:9222/json/version", "ws://127.0.0.1:9222/devtools/browser/%2e%2e"} {
		if err := ValidateEndpoint(raw); err == nil {
			t.Errorf("accepted %q", raw)
		}
	}
}
