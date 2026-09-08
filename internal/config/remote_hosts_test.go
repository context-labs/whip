package config

import "testing"

func TestNormalizeRemoteHosts(t *testing.T) {
	t.Parallel()
	valid := RemoteHost{ID: "profile", Name: " Kuzco ", URL: "wss://Kuzco.test/api/v3/ws", RuntimeID: "runtime", ConnectOnLaunch: true}
	hosts, err := NormalizeRemoteHosts([]RemoteHost{valid})
	if err != nil || len(hosts) != 1 {
		t.Fatalf("normalize: %v %v", hosts, err)
	}
	if hosts[0].URL != "https://kuzco.test" || hosts[0].Name != "Kuzco" || valid.Name != " Kuzco " {
		t.Fatalf("normalization changed its source or lost endpoint: %+v", hosts)
	}
	for _, test := range []struct {
		name string
		edit func(*RemoteHost)
	}{
		{"reserved local profile", func(h *RemoteHost) { h.ID = "local" }},
		{"missing identity", func(h *RemoteHost) { h.RuntimeID = "" }},
		{"missing name", func(h *RemoteHost) { h.Name = " " }},
		{"credentials", func(h *RemoteHost) { h.URL = "http://user:secret@host" }},
		{"query", func(h *RemoteHost) { h.URL = "https://host?token=secret" }},
		{"fragment", func(h *RemoteHost) { h.URL = "https://host#secret" }},
		{"path prefix", func(h *RemoteHost) { h.URL = "https://host/whip" }},
		{"scheme", func(h *RemoteHost) { h.URL = "file:///tmp/daemon" }},
		{"missing hostname", func(h *RemoteHost) { h.URL = "http://:8080" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			host := valid
			test.edit(&host)
			if _, err := NormalizeRemoteHosts([]RemoteHost{host}); err == nil {
				t.Fatal("accepted invalid profile")
			}
		})
	}
	for _, test := range []struct {
		name string
		edit func(*RemoteHost)
	}{
		{"profile", func(h *RemoteHost) { h.ID = valid.ID }},
		{"endpoint", func(h *RemoteHost) { h.URL = "https://kuzco.test/" }},
		{"runtime alias", func(h *RemoteHost) { h.RuntimeID = valid.RuntimeID }},
	} {
		t.Run(test.name, func(t *testing.T) {
			other := RemoteHost{ID: "other", Name: "Other", URL: "http://other", RuntimeID: "other"}
			test.edit(&other)
			if _, err := NormalizeRemoteHosts([]RemoteHost{valid, other}); err == nil {
				t.Fatal("accepted duplicate")
			}
		})
	}
}
