package main

import (
	"slices"
	"testing"
)

func TestDaemonNetworkEnvironment(t *testing.T) {
	for _, test := range []struct {
		name, enabled, listen string
		want                  bool
		bad                   bool
	}{
		{name: "disabled by default"},
		{name: "loopback", enabled: "1", want: true},
		{name: "listen does not opt in", listen: "192.168.1.10:8080"},
		{name: "zero disables", enabled: "0"},
		{name: "zero overrides listen", enabled: "0", listen: "127.0.0.1:8080"},
		{name: "explicit disable", enabled: "false", listen: "127.0.0.1:8080"},
		{name: "invalid boolean", enabled: "maybe", bad: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("WHIPCODE_NETWORK", test.enabled)
			t.Setenv("WHIPCODE_LISTEN", test.listen)
			t.Setenv("WHIPCODE_NETWORK_TERMINALS", "")
			t.Setenv("WHIPCODE_ALLOWED_ORIGINS", "http://localhost:3000, https://whip.example")
			t.Setenv("WHIPCODE_ALLOWED_HOSTS", "localhost:8080, 127.0.0.1:8080")
			launch, err := nativeRuntimeLaunch()
			if test.bad {
				if err == nil {
					t.Fatal("invalid boolean accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if launch.WaitForWeb != test.want || slices.Contains(launch.Arguments, "-web") != test.want {
				t.Fatalf("launch: %+v", launch)
			}
			if test.want {
				for _, value := range []string{"http://localhost:3000, https://whip.example", "localhost:8080, 127.0.0.1:8080"} {
					if !slices.Contains(launch.Arguments, value) {
						t.Fatalf("missing allowlist %q: %+v", value, launch)
					}
				}
			} else if len(launch.Arguments) != 1 {
				t.Fatalf("disabled network retained gateway flags: %+v", launch)
			}
		})
	}
}
