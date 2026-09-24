package browser

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestManagerDriver(t *testing.T) {
	t.Setenv("WHIP_BROWSER_DRIVER", "")
	m := NewManager(ModeHeadless)
	m.SwitchDriver(DriverChromedp)
	if m.Driver() != DriverChromedp {
		t.Fatalf("got %q", m.Driver())
	}
	m.SwitchDriver(DriverRod)
	if m.Driver() != DriverRod {
		t.Fatalf("got %q", m.Driver())
	}
	m.SwitchDriver("bogus")
	if m.Driver() != DriverRod {
		t.Fatalf("bogus driver must be ignored, got %q", m.Driver())
	}
}

func TestSetDriverEnvPinWins(t *testing.T) {
	t.Setenv("WHIP_BROWSER_DRIVER", "chromedp")
	m := NewManager(ModeHeadless)
	m.SwitchDriver(DriverRod)
	if m.Driver() != DriverChromedp {
		t.Fatalf("pin must make SwitchDriver a no-op, got %q", m.Driver())
	}
}

func TestChromedpRejectsScopedLaunchEnvironment(t *testing.T) {
	_, err := openChromedp(context.Background(), ModeHeadless, "test", []string{"PATH=/bin"})
	if err == nil || !strings.Contains(err.Error(), "isolated environment") {
		t.Fatalf("scoped chromedp launch error = %v", err)
	}
}

type blockedCloseBackend struct {
	fakeBackend
	entered chan struct{}
	release chan struct{}
}

func (b *blockedCloseBackend) Close() error {
	close(b.entered)
	<-b.release
	return nil
}

func TestManagerStatusDoesNotWaitForBackendClose(t *testing.T) {
	for _, operation := range []string{"switch", "close"} {
		t.Run(operation, func(t *testing.T) {
			t.Setenv("WHIP_BROWSER_DRIVER", "")
			manager := NewManager(ModeHeadless)
			session, err := manager.Session("old")
			if err != nil {
				t.Fatal(err)
			}
			backend := &blockedCloseBackend{entered: make(chan struct{}), release: make(chan struct{})}
			session.backend = backend
			finished := make(chan struct{})
			go func() {
				defer close(finished)
				if operation == "switch" {
					manager.SwitchDriver(DriverChromedp)
				} else {
					manager.CloseAll()
				}
			}()
			defer func() { close(backend.release); <-finished }()
			<-backend.entered
			queried := make(chan error, 1)
			go func() { _ = manager.Driver(); _, err := manager.Session("new"); queried <- err }()
			select {
			case err := <-queried:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(time.Second):
				t.Fatal("backend close blocked manager status or session lookup")
			}
		})
	}
}

func TestConcurrentDriverSwitchAndSessionLookup(t *testing.T) {
	t.Setenv("WHIP_BROWSER_DRIVER", "")
	manager := NewManager(ModeHeadless)
	var group sync.WaitGroup
	group.Go(func() {
		for range 100 {
			manager.SwitchDriver(DriverChromedp)
			manager.SwitchDriver(DriverRod)
		}
	})
	group.Go(func() {
		for range 100 {
			if _, err := manager.Session("test"); err != nil {
				t.Error(err)
			}
		}
	})
	group.Wait()
}
