package browser

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/browserconfig"
)

func realNativeHost(t *testing.T, driver string) *NativeHost {
	t.Helper()
	options := nativeOptions(t, browserconfig.Config{Mode: "headless", Executable: chromiumPath(t)}, driver)
	host := NewNativeHost(options.Directory, options.Processes)
	t.Cleanup(func() { _ = host.Close() })
	if err := host.Update(options.Config, driver); err != nil {
		t.Fatal(err)
	}
	return host
}

func nativeTestBatch(ctx context.Context, host *NativeHost, name string, fn func(context.Context, Backend) error) error {
	capture, err := host.Capture("root", "root", name)
	if err != nil {
		return err
	}
	lease, err := capture.Acquire(ctx)
	if err != nil {
		return err
	}
	defer lease.Close()
	return lease.Run(ctx, allowedNative, fn)
}

func TestConcurrentSessionsDriveIsolatedBrowsers(t *testing.T) {
	host := realNativeHost(t, DriverRod)
	page := testPage(t)
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	const sessions = 3
	var group sync.WaitGroup
	failures := make(chan error, sessions)
	for i := range sessions {
		group.Go(func() {
			marker := fmt.Sprintf("session-%d", i)
			failures <- nativeTestBatch(ctx, host, marker, func(ctx context.Context, b Backend) error {
				if err := b.Navigate(ctx, page+"/marker/"+marker); err != nil {
					return err
				}
				title, err := b.Eval(ctx, "document.title")
				if err == nil && !strings.Contains(title, marker) {
					return fmt.Errorf("cross-session title %s", title)
				}
				return err
			})
		})
	}
	group.Wait()
	close(failures)
	for err := range failures {
		if err != nil {
			t.Error(err)
		}
	}
}

// Ten real launch/close cycles retain the original churn workload, with joined
// native process ownership and one explicit private profile instead of fallback.
func TestChurnOpenClose(t *testing.T) {
	options := nativeOptions(t, browserconfig.Config{Mode: "headless", Executable: chromiumPath(t)}, DriverRod)
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	page := testPage(t)
	for i := range 10 {
		browser, err := OpenNative(ctx, t.Context(), options)
		if err != nil {
			t.Fatal(i, err)
		}
		if err := browser.Navigate(ctx, page); err != nil {
			_ = browser.Close()
			t.Fatal(i, err)
		}
		if _, err := browser.Eval(ctx, "document.title"); err != nil {
			_ = browser.Close()
			t.Fatal(i, err)
		}
		if err := browser.Close(); err != nil {
			t.Fatal(i, err)
		}
	}
}

func TestRecoverFromClosedBrowserRequiresExplicitGeneration(t *testing.T) {
	host := realNativeHost(t, DriverRod)
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	page := testPage(t)
	if err := nativeTestBatch(ctx, host, "crashy", func(ctx context.Context, b Backend) error { return b.Navigate(ctx, page) }); err != nil {
		t.Fatal(err)
	}
	old := host.List("root")[0]
	calls := 0
	dropped := errors.New("delivered connection lost")
	err := nativeTestBatch(ctx, host, "crashy", func(_ context.Context, b Backend) error { calls++; _ = b.Close(); return dropped })
	if !errors.Is(err, dropped) || calls != 1 {
		t.Fatal("failed effect replayed", calls, err)
	}
	if _, err := host.Capture("root", "root", "crashy"); !errors.Is(err, ErrNativeStale) {
		t.Fatal("capture silently reopened", err)
	}
	fresh, err := host.Reconnect("root", "crashy", old.Generation)
	if err != nil {
		t.Fatal(err)
	}
	if fresh.Generation == old.Generation || fresh.Resource == old.Resource || fresh.State != "prepared" {
		t.Fatal(fresh)
	}
	if err := nativeTestBatch(ctx, host, "crashy", func(ctx context.Context, b Backend) error {
		if err := b.Navigate(ctx, page); err != nil {
			return err
		}
		title, err := b.Eval(ctx, "document.title")
		if err == nil && !strings.Contains(title, "whipcode e2e") {
			return fmt.Errorf("post-reconnect title %s", title)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatal("failed callback was replayed", calls)
	}
}

func TestManySequentialCalls(t *testing.T) {
	host := realNativeHost(t, DriverChromedp)
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	page := testPage(t)
	if err := nativeTestBatch(ctx, host, "longhaul", func(ctx context.Context, b Backend) error { return b.Navigate(ctx, page) }); err != nil {
		t.Fatal(err)
	}
	for i := range 50 {
		if err := nativeTestBatch(ctx, host, "longhaul", func(ctx context.Context, b Backend) error {
			value, err := b.Eval(ctx, fmt.Sprintf("%d+1", i))
			if err == nil && value != strconv.Itoa(i+1) {
				return fmt.Errorf("iteration %d: %s", i, value)
			}
			return err
		}); err != nil {
			t.Fatal(i, err)
		}
	}
}
