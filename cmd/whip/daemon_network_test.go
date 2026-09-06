package main

import "testing"

func TestDaemonNetworkEnvironment(t *testing.T) {
	for _, test := range []struct {
		name, enabled, listen string
		want                  bool
		bad                   bool
	}{
		{name: "disabled"},
		{name: "loopback", enabled: "1", want: true},
		{name: "trusted bind", listen: "192.168.1.10:8080", want: true},
		{name: "explicit disable", enabled: "false", listen: "127.0.0.1:8080"},
		{name: "invalid boolean", enabled: "maybe", bad: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("WHIP_NETWORK", test.enabled)
			t.Setenv("WHIP_LISTEN", test.listen)
			t.Setenv("WHIP_ALLOWED_ORIGINS", "http://localhost:3000, https://whip.example")
			t.Setenv("WHIP_ALLOWED_HOSTS", "localhost:8080, 127.0.0.1:8080")
			options, err := daemonNetworkEnvironment()
			if test.bad {
				if err == nil {
					t.Fatal("invalid boolean accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if options.Enabled != test.want || options.Address != test.listen {
				t.Fatalf("options: %+v", options)
			}
			if len(options.AllowedOrigins) != 2 || options.AllowedOrigins[1] != "https://whip.example" || len(options.AllowedHosts) != 2 {
				t.Fatalf("allowlists: %+v", options)
			}
		})
	}
}
