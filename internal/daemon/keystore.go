package daemon

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	keyring "github.com/zalando/go-keyring"
)

const keyringService = "whip-daemon-clients"

type SecretStore interface {
	Get(service, user string) (string, error)
	Set(service, user, secret string) error
	Delete(service, user string) error
}

type systemKeyring struct{}

func (systemKeyring) Get(service, user string) (string, error) { return keyring.Get(service, user) }
func (systemKeyring) Set(service, user, secret string) error {
	return keyring.Set(service, user, secret)
}
func (systemKeyring) Delete(service, user string) error { return keyring.Delete(service, user) }

func SystemKeyStore() SecretStore { return systemKeyring{} }

type ClientCredentials struct {
	ClientID   string
	PrivateKey ed25519.PrivateKey
}

type storedCredentials struct {
	ClientID string `json:"client_id"`
	Material string `json:"material"`
}

// LoadOrCreateClientCredentials gives each client kind a stable ID and key
// across process restarts without placing either private material or a
// fallback credential in a plaintext file.
func LoadOrCreateClientCredentials(store SecretStore, kind string) (ClientCredentials, error) {
	if store == nil || kind == "" {
		return ClientCredentials{}, errors.New("client credentials require a credential store and kind")
	}
	user := "identity-" + kind
	encoded, err := store.Get(keyringService, user)
	if err == nil {
		var stored storedCredentials
		if json.Unmarshal([]byte(encoded), &stored) != nil || stored.ClientID == "" {
			return ClientCredentials{}, errors.New("stored client credentials are corrupt")
		}
		private, decodeErr := base64.StdEncoding.DecodeString(stored.Material)
		if decodeErr != nil || len(private) != ed25519.PrivateKeySize {
			return ClientCredentials{}, errors.New("stored client credentials are corrupt")
		}
		return ClientCredentials{ClientID: stored.ClientID, PrivateKey: ed25519.PrivateKey(private)}, nil
	}
	if !errors.Is(err, keyring.ErrNotFound) {
		return ClientCredentials{}, fmt.Errorf("read client credentials: %w", err)
	}
	id := make([]byte, 16)
	if _, err := rand.Read(id); err != nil {
		return ClientCredentials{}, err
	}
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return ClientCredentials{}, err
	}
	stored := storedCredentials{ClientID: hex.EncodeToString(id), Material: base64.StdEncoding.EncodeToString(private)}
	raw, err := json.Marshal(stored)
	if err != nil {
		return ClientCredentials{}, err
	}
	if err := store.Set(keyringService, user, string(raw)); err != nil {
		return ClientCredentials{}, fmt.Errorf("store client credentials: %w", err)
	}
	return ClientCredentials{ClientID: stored.ClientID, PrivateKey: private}, nil
}

// LoadOrCreateClientKey stores private identity only in the OS credential
// service. Credential failures are returned; there is no file fallback.
func LoadOrCreateClientKey(store SecretStore, clientID string) (ed25519.PrivateKey, error) {
	if store == nil || clientID == "" {
		return nil, errors.New("client key requires a credential store and client ID")
	}
	encoded, err := store.Get(keyringService, clientID)
	if err == nil {
		decoded, decodeErr := base64.StdEncoding.DecodeString(encoded)
		if decodeErr != nil || len(decoded) != ed25519.PrivateKeySize {
			return nil, errors.New("stored client key is corrupt")
		}
		return ed25519.PrivateKey(decoded), nil
	}
	if !errors.Is(err, keyring.ErrNotFound) {
		return nil, fmt.Errorf("read client key: %w", err)
	}
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	if err := store.Set(keyringService, clientID, base64.StdEncoding.EncodeToString(private)); err != nil {
		return nil, fmt.Errorf("store client key: %w", err)
	}
	return private, nil
}
