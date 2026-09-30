package client

import (
	"context"
	"errors"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/protocol"
)

type lostProviderReply struct {
	*staticRootConnection
	calls *atomic.Int32
}

func (c *lostProviderReply) Call(context.Context, string, any, any) error {
	c.calls.Add(1)
	_ = c.Close()
	return net.ErrClosed
}

func TestRootClientProviderMutationsAreNotReplayedAfterLostReply(t *testing.T) {
	var calls atomic.Int32
	client, err := NewRootClient(RootClientOptions{ClientID: "ephemeral", RootID: "root", RetryMin: time.Millisecond, RetryMax: time.Millisecond, Connector: func(context.Context, map[string]int64) (RootConnection, error) {
		return &lostProviderReply{staticRootConnection: newStaticRootConnection(), calls: &calls}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	client.Start()
	defer client.Close()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if err := client.WaitLive(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := client.SetProviderKey(ctx, ProviderKeySetup{Revision: "expected", Provider: "openrouter", Key: "ephemeral-secret"}); !errors.Is(err, net.ErrClosed) {
		t.Fatalf("lost reply=%v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("credential mutation replayed %d times", calls.Load())
	}
}

func TestRootClientRejectsUnsupportedConnectionServices(t *testing.T) {
	client, err := NewRootClient(RootClientOptions{ClientID: "limited", RootID: "root", Connector: func(context.Context, map[string]int64) (RootConnection, error) { return newStaticRootConnection(), nil }})
	if err != nil {
		t.Fatal(err)
	}
	client.Start()
	defer client.Close()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if err := client.WaitLive(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := client.ReadConfiguration(ctx); err == nil || !strings.Contains(err.Error(), "does not support") {
		t.Fatalf("unsupported provider service=%v", err)
	}
	if _, err := client.ValidateProvider(ctx, protocol.ProviderValidateParams{}); err == nil || !strings.Contains(err.Error(), "does not support") {
		t.Fatalf("unsupported validation=%v", err)
	}
	if _, err := client.HistoryPage(ctx, protocol.HistoryPageParams{}); err == nil || !strings.Contains(err.Error(), "does not support") {
		t.Fatalf("unsupported history=%v", err)
	}
}
