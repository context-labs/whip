package daemon

import (
	"errors"
	"testing"

	keyring "github.com/zalando/go-keyring"
)

type memorySecrets struct {
	values map[string]string
	err    error
	setErr error
}

func (m *memorySecrets) Get(service, user string) (string, error) {
	if m.err != nil {
		return "", m.err
	}
	value, ok := m.values[service+"/"+user]
	if !ok {
		return "", keyring.ErrNotFound
	}
	return value, nil
}

func (m *memorySecrets) Set(service, user, value string) error {
	if m.setErr != nil {
		return m.setErr
	}
	if m.err != nil {
		return m.err
	}
	m.values[service+"/"+user] = value
	return nil
}

func (m *memorySecrets) Delete(service, user string) error {
	delete(m.values, service+"/"+user)
	return nil
}

func TestClientKeysReloadAndCredentialFailuresFailClosed(t *testing.T) {
	secrets := &memorySecrets{values: make(map[string]string)}
	first, err := LoadOrCreateClientKey(secrets, "client")
	if err != nil {
		t.Fatal(err)
	}
	second, err := LoadOrCreateClientKey(secrets, "client")
	if err != nil || !first.Equal(second) {
		t.Fatalf("reloaded key changed: %v", err)
	}
	secrets.values[keyringService+"/client"] = "corrupt"
	if _, err := LoadOrCreateClientKey(secrets, "client"); err == nil || err.Error() != "stored client key is corrupt" {
		t.Fatalf("corrupt key error = %v", err)
	}
	unavailable := errors.New("credential service unavailable")
	if _, err := LoadOrCreateClientKey(&memorySecrets{values: make(map[string]string), err: unavailable}, "client"); !errors.Is(err, unavailable) {
		t.Fatalf("unavailable keyring error = %v", err)
	}
}

func TestSystemKeyStoreAdapter(t *testing.T) {
	keyring.MockInit()
	store := SystemKeyStore()
	if err := store.Set("service", "user", "secret"); err != nil {
		t.Fatal(err)
	}
	if value, err := store.Get("service", "user"); err != nil || value != "secret" {
		t.Fatalf("system keyring get = %q, %v", value, err)
	}
	if err := store.Delete("service", "user"); err != nil {
		t.Fatal(err)
	}
}

func TestClientCredentialsKeepStableIdentityInKeyring(t *testing.T) {
	if _, err := LoadOrCreateClientCredentials(nil, ""); err == nil {
		t.Fatal("empty credential request was accepted")
	}
	secrets := &memorySecrets{values: make(map[string]string)}
	first, err := LoadOrCreateClientCredentials(secrets, "tui")
	if err != nil {
		t.Fatal(err)
	}
	second, err := LoadOrCreateClientCredentials(secrets, "tui")
	if err != nil || first.ClientID != second.ClientID || !first.PrivateKey.Equal(second.PrivateKey) {
		t.Fatalf("credentials were not stable: first=%q second=%q err=%v", first.ClientID, second.ClientID, err)
	}
	secrets.values[keyringService+"/identity-tui"] = "corrupt"
	if _, err := LoadOrCreateClientCredentials(secrets, "tui"); err == nil {
		t.Fatal("corrupt credentials did not fail closed")
	}
	unavailable := errors.New("unavailable")
	if _, err := LoadOrCreateClientCredentials(&memorySecrets{values: make(map[string]string), err: unavailable}, "acp"); !errors.Is(err, unavailable) {
		t.Fatalf("unavailable credential store = %v", err)
	}
	if _, err := LoadOrCreateClientKey(nil, ""); err == nil {
		t.Fatal("empty key request was accepted")
	}
	writeErr := errors.New("write failed")
	if _, err := LoadOrCreateClientCredentials(&memorySecrets{values: make(map[string]string), setErr: writeErr}, "headless"); !errors.Is(err, writeErr) {
		t.Fatalf("credential write failure = %v", err)
	}
	if _, err := LoadOrCreateClientKey(&memorySecrets{values: make(map[string]string), setErr: writeErr}, "headless"); !errors.Is(err, writeErr) {
		t.Fatalf("key write failure = %v", err)
	}
}
