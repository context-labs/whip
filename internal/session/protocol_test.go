package session

import (
	"context"
	"crypto/ed25519"
	"errors"
	"path/filepath"
	"sync"
	"testing"
)

func TestDaemonGenerationAndFirstHumanPairingAreDurable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.db")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if generation, err := store.BeginDaemonGeneration(ctx, "build-a"); err != nil || generation != 1 {
		t.Fatalf("first generation = %d, %v", generation, err)
	}
	if generation, err := store.BeginDaemonGeneration(ctx, "build-b"); err != nil || generation != 2 {
		t.Fatalf("second generation = %d, %v", generation, err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	if generation, buildID, status, err := store.DaemonGeneration(ctx); err != nil || generation != 2 || buildID != "build-b" || status != "running" {
		t.Fatalf("durable daemon state = %d %q %q, %v", generation, buildID, status, err)
	}
	if err := store.SetDaemonStatus(ctx, 1, "stopping"); err == nil {
		t.Fatal("stale generation changed daemon state")
	}

	publicA, _, _ := ed25519.GenerateKey(nil)
	publicB, _, _ := ed25519.GenerateKey(nil)
	identities := []ClientIdentity{
		{ClientID: "human-a", Kind: "tui", PublicKey: publicA},
		{ClientID: "human-b", Kind: "tui", PublicKey: publicB},
	}
	var wait sync.WaitGroup
	errs := make(chan error, len(identities))
	for _, identity := range identities {
		wait.Go(func() { errs <- store.PairFirstHuman(ctx, identity) })
	}
	wait.Wait()
	close(errs)
	var paired, refused int
	for err := range errs {
		switch {
		case err == nil:
			paired++
		case errors.Is(err, ErrHumanAlreadyPaired):
			refused++
		default:
			t.Fatalf("pairing error = %v", err)
		}
	}
	if paired != 1 || refused != 1 {
		t.Fatalf("paired=%d refused=%d", paired, refused)
	}
	if count, err := store.HumanIdentityCount(ctx); err != nil || count != 1 {
		t.Fatalf("human count = %d, %v", count, err)
	}
	if err := store.PairFirstHuman(ctx, ClientIdentity{ClientID: "bot", Kind: "automation", PublicKey: publicB}); err == nil {
		t.Fatal("automation consumed enrollment")
	}
}
