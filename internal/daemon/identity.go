package daemon

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/context-labs/whip/internal/session"
)

type EnrollIdentityParams struct {
	PublicKey    []byte `json:"public_key"`
	TTYConfirmed bool   `json:"tty_confirmed,omitempty"`
	AuthorizedBy string `json:"authorized_by,omitempty"`
	Signature    []byte `json:"signature,omitempty"`
}

type IdentityResult struct {
	ClientID string `json:"client_id"`
	Kind     string `json:"kind"`
	Nonce    []byte `json:"nonce"`
}

func randomNonce() ([]byte, error) {
	nonce := make([]byte, 32)
	_, err := rand.Read(nonce)
	return nonce, err
}

func enrollmentMessage(generation int64, nonce []byte, clientID, kind string, publicKey []byte) []byte {
	hash := sha256.New()
	_, _ = hash.Write([]byte("whip identity enrollment v1\x00"))
	_, _ = hash.Write([]byte(strconv.FormatInt(generation, 10)))
	_, _ = hash.Write([]byte{0})
	_, _ = hash.Write(nonce)
	_, _ = hash.Write([]byte(clientID))
	_, _ = hash.Write([]byte{0})
	_, _ = hash.Write([]byte(kind))
	_, _ = hash.Write(publicKey)
	return hash.Sum(nil)
}

func authorizationMessage(method string, generation int64, nonce []byte, params any) ([]byte, error) {
	raw, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	hash := sha256.New()
	_, _ = hash.Write([]byte("whip privileged request v1\x00"))
	_, _ = hash.Write([]byte(method))
	_, _ = hash.Write([]byte(strconv.FormatInt(generation, 10)))
	_, _ = hash.Write([]byte{0})
	_, _ = hash.Write(nonce)
	_, _ = hash.Write(raw)
	return hash.Sum(nil), nil
}

func (s *Server) enrollIdentity(connection *serverConn, params EnrollIdentityParams) (IdentityResult, error) {
	if connection.client.ClientKind == "automation" {
		return IdentityResult{}, errors.New("automation cannot enroll a human identity")
	}
	if len(params.PublicKey) != ed25519.PublicKeySize {
		return IdentityResult{}, errors.New("identity requires an Ed25519 public key")
	}
	identity := session.ClientIdentity{
		ClientID: connection.client.ClientID, Kind: connection.client.ClientKind,
		PublicKey: append([]byte(nil), params.PublicKey...), PairedBy: params.AuthorizedBy,
	}
	count, err := s.daemon.store.HumanIdentityCount(s.ctx)
	if err != nil {
		return IdentityResult{}, err
	}
	if count == 0 {
		if !params.TTYConfirmed {
			return IdentityResult{}, errors.New("first human enrollment requires explicit TTY confirmation")
		}
		err = s.daemon.control.route(s.ctx, func(ctx context.Context) error {
			return s.daemon.store.PairFirstHuman(ctx, identity)
		})
	} else {
		if params.AuthorizedBy == "" || len(params.Signature) != ed25519.SignatureSize {
			return IdentityResult{}, errors.New("later enrollment requires an authenticated human")
		}
		authorizer, loadErr := s.daemon.store.LoadClientIdentity(s.ctx, params.AuthorizedBy)
		if loadErr != nil || authorizer.Kind == "automation" {
			return IdentityResult{}, errors.New("enrollment authorizer is not a paired human")
		}
		message := enrollmentMessage(s.options.Generation, connection.nonceValue(), identity.ClientID, identity.Kind, identity.PublicKey)
		if !ed25519.Verify(authorizer.PublicKey, message, params.Signature) {
			return IdentityResult{}, errors.New("invalid enrollment signature")
		}
		err = s.daemon.control.route(s.ctx, func(ctx context.Context) error {
			return s.daemon.store.PairClient(ctx, identity)
		})
	}
	if err != nil {
		return IdentityResult{}, err
	}
	nonce, err := connection.rotateNonce()
	return IdentityResult{ClientID: identity.ClientID, Kind: identity.Kind, Nonce: nonce}, err
}

func (s *Server) identityStatus(connection *serverConn) (IdentityStatusResult, error) {
	result := IdentityStatusResult{ClientID: connection.client.ClientID, Kind: connection.client.ClientKind}
	identity, err := s.daemon.store.LoadClientIdentity(s.ctx, connection.client.ClientID)
	if err == nil {
		result.Paired = identity.Kind == connection.client.ClientKind
	} else if !errors.Is(err, sql.ErrNoRows) {
		return IdentityStatusResult{}, err
	}
	count, err := s.daemon.store.HumanIdentityCount(s.ctx)
	if err != nil {
		return IdentityStatusResult{}, err
	}
	result.EnrollmentOpen = count == 0 && connection.client.ClientKind != "automation"
	return result, nil
}

func (s *Server) verifyPrivileged(connection *serverConn, method string, unsigned any, signature []byte) error {
	identity, err := s.daemon.store.LoadClientIdentity(s.ctx, connection.client.ClientID)
	if err != nil || identity.Kind == "automation" || identity.Kind != connection.client.ClientKind {
		return errors.New("privileged request requires a paired human identity")
	}
	message, err := authorizationMessage(method, s.options.Generation, connection.nonceValue(), unsigned)
	if err != nil {
		return err
	}
	if len(signature) != ed25519.SignatureSize || !ed25519.Verify(identity.PublicKey, message, signature) {
		return fmt.Errorf("invalid %s signature", method)
	}
	return nil
}
