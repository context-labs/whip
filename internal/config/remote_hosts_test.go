package config

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/session"
)

func TestRemoteHostsBoundedExactEndpointsAndAliases(t *testing.T) {
	profile := RemoteHost{ID: "remote", Name: " Remote ", URL: "https://Example.test:443/", RuntimeID: "runtime", ConnectOnLaunch: true}
	values, err := NormalizeRemoteHosts([]RemoteHost{profile})
	if err != nil || values[0].URL != profile.URL || values[0].Name != "Remote" || profile.Name != " Remote " {
		t.Fatal("profile normalization lost exact URL or mutated its source", values, err)
	}
	for _, address := range []string{
		"HTTPS://example.test", "wss://example.test/api/v4/ws", "http://host/path", "http://:80", "http://host:", "http://host:0", "http://host:65536",
		"https://user:private-secret@host", "http://host?private-secret", "http://host#", "http://host?", " http://host",
		"http://2001:db8::1:80", "http://host\n", "http://host/%2F", "http://host/%2e", "http://[::1%25zone]", "file:///private-secret",
	} {
		bad := profile
		bad.URL = address
		if _, err := NormalizeRemoteHosts([]RemoteHost{bad}); !errors.Is(err, session.ErrInvalid) || strings.Contains(err.Error(), "private-secret") {
			t.Fatal("invalid endpoint accepted or exposed", address, err)
		}
	}
	for _, mutate := range []func(*RemoteHost){
		func(p *RemoteHost) { p.ID = "local" }, func(p *RemoteHost) { p.RuntimeID = "" },
		func(p *RemoteHost) { p.Name = "\t" }, func(p *RemoteHost) { p.Name = "bad\x7fname" },
		func(p *RemoteHost) { p.Name = strings.Repeat("界", 257) },
	} {
		bad := profile
		mutate(&bad)
		if _, err := NormalizeRemoteHosts([]RemoteHost{bad}); !errors.Is(err, session.ErrInvalid) {
			t.Fatal("invalid profile accepted", bad, err)
		}
	}
	for _, address := range []string{"https://example.test", "https://EXAMPLE.test/", "https://example.test.:00443"} {
		other := RemoteHost{ID: "other", Name: "Other", URL: address, RuntimeID: "other"}
		if _, err := NormalizeRemoteHosts([]RemoteHost{profile, other}); !errors.Is(err, session.ErrInvalid) {
			t.Fatal("endpoint alias accepted", address, err)
		}
	}
	for _, address := range []string{"http://[::1]:8080/", "https://[2001:db8::1]", "http://127.0.0.1:80"} {
		ip := profile
		ip.URL = address
		if values, err := NormalizeRemoteHosts([]RemoteHost{ip}); err != nil || values[0].URL != address {
			t.Fatal("valid IP endpoint changed or rejected", values, err)
		}
	}
	values = []RemoteHost{}
	for index := range MaxRemoteHosts {
		id := fmt.Sprintf("host-%d", index)
		values = append(values, RemoteHost{ID: id, Name: strings.Repeat("界", 256), URL: "http://" + id, RuntimeID: id})
	}
	if _, err := NormalizeRemoteHosts(values); err != nil {
		t.Fatal(err)
	}
	if _, err := NormalizeRemoteHosts(append(values, profile)); !errors.Is(err, session.ErrInvalid) {
		t.Fatal("unbounded profile count", err)
	}
}

func TestRemoteHostCASPreservesHostDeclarationsAndNoopRevision(t *testing.T) {
	host := Default()
	host.Providers["keep"] = Provider{Kind: "openai-chat", BaseURL: "https://provider.example/v1", CredentialEnv: "PRIVATE_SECRET_ENV"}
	authority, before := configAuthority(t, host)
	empty, err := authority.SetRemoteHosts(t.Context(), before.Revision, []RemoteHost{})
	if err != nil || empty.Revision != before.Revision {
		t.Fatal("empty no-op changed host", empty, err)
	}
	profiles := []RemoteHost{{ID: "remote", Name: "Remote", URL: "https://example.test", RuntimeID: "other"}}
	after, err := authority.SetRemoteHosts(t.Context(), before.Revision, profiles)
	if err != nil || after.Revision == before.Revision {
		t.Fatal("profile publication failed", after, err)
	}
	profiles[0].Name = "caller mutation"
	expected := before.Host
	expected.RemoteHosts = []RemoteHost{{ID: "remote", Name: "Remote", URL: "https://example.test", RuntimeID: "other"}}
	if !reflect.DeepEqual(after.Host, expected) {
		t.Fatal("profile edit altered unrelated host declarations")
	}
	same, err := authority.SetRemoteHosts(t.Context(), after.Revision, after.Host.RemoteHosts)
	if err != nil || same.Revision != after.Revision {
		t.Fatal("same-value edit changed revision", err)
	}
	if _, err := authority.SetRemoteHosts(t.Context(), before.Revision, after.Host.RemoteHosts); !errors.Is(err, ErrRevisionConflict) {
		t.Fatal("stale same-value edit bypassed CAS", err)
	}
	cleared, err := authority.SetRemoteHosts(t.Context(), same.Revision, []RemoteHost{})
	if err != nil || len(cleared.Host.RemoteHosts) != 0 || cleared.Revision == same.Revision {
		t.Fatal("clear failed", err)
	}
	loaded, err := Load(authority.directory)
	if err != nil || !reflect.DeepEqual(loaded.Providers, host.Providers) || len(loaded.RemoteHosts) != 0 {
		t.Fatal("saved profiles or unrelated declarations changed", loaded, err)
	}
}
