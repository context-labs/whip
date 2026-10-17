package client

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/protocol"
)

func TestRootClientWaitingRequestsUnblockOnCancellation(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"caller", "client"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			connecting := make(chan struct{})
			client, err := NewRootClient(RootClientOptions{
				ClientID: "waiting", RootID: "root",
				Connector: func(ctx context.Context, _ map[string]int64) (RootConnection, error) {
					close(connecting)
					<-ctx.Done()
					return nil, ctx.Err()
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = client.Close() })
			client.Start()
			<-connecting
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			action := RootAction{CommandID: "command", RootID: "root", Operation: "submit"}
			permission := RootAction{CommandID: "permission", RootID: "root", Operation: "permission.decide"}
			requests := []func() error{
				func() error { return client.WaitLive(ctx) },
				func() error { _, err := client.Command(ctx, action); return err },
				func() error {
					_, err := client.ValidateProvider(ctx, protocol.ProviderValidateParams{})
					return err
				},
				func() error {
					_, err := client.DecidePermission(
						ctx,
						permission,
						"permission-id",
						true,
						"approved",
						"",
					)
					return err
				},
			}
			results := make(chan error, len(requests))
			for _, request := range requests {
				go func() { results <- request() }()
			}
			if name == "caller" {
				cancel()
			} else if err := client.Close(); err != nil {
				t.Fatal(err)
			}
			deadline := time.NewTimer(5 * time.Second)
			defer deadline.Stop()
			for range requests {
				select {
				case err := <-results:
					if !errors.Is(err, context.Canceled) {
						t.Fatalf("waiting request lost cancellation: %v", err)
					}
				case <-deadline.C:
					t.Fatal("request remained blocked after cancellation")
				}
			}
		})
	}
}

type providerValidationConnection struct {
	*staticRootConnection
	validate func() error
}

func (c *providerValidationConnection) ValidateProvider(
	context.Context,
	protocol.ProviderValidateParams,
) (protocol.ProviderValidateResult, error) {
	return protocol.ProviderValidateResult{}, c.validate()
}

func TestRootClientProviderValidationPreservesFailureAndCancellation(t *testing.T) {
	t.Parallel()
	for _, cancelled := range []bool{false, true} {
		name := "provider failure"
		if cancelled {
			name = "caller cancellation"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			failure := errors.New("provider validation unavailable")
			calls := 0
			connection := &providerValidationConnection{
				staticRootConnection: newStaticRootConnection(),
				validate: func() error {
					calls++
					if cancelled {
						cancel()
					}
					return failure
				},
			}
			client, err := NewRootClient(RootClientOptions{
				ClientID: "validate", RootID: "root",
				Connector: func(context.Context, map[string]int64) (RootConnection, error) { return connection, nil },
			})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = client.Close() })
			client.Start()
			ready, stop := context.WithTimeout(ctx, 5*time.Second)
			defer stop()
			if err := client.WaitLive(ready); err != nil {
				t.Fatal(err)
			}
			_, err = client.ValidateProvider(ctx, protocol.ProviderValidateParams{})
			want := failure
			if cancelled {
				want = context.Canceled
			}
			if !errors.Is(err, want) || calls != 1 {
				t.Fatalf("validation failure was retried or replaced: calls=%d err=%v", calls, err)
			}
		})
	}
}
