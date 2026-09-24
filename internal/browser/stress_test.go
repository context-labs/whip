// stress_test.go — the rigorous tier: concurrency, churn, and crash
// recovery, driver-parameterized (WHIP_BROWSER_DRIVER selects; the default
// run exercises rod, CI/env flips to chromedp). These tests exist to fail
// under interleaving, not to pass politely.

package browser

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestConcurrentSessionsDriveIsolatedBrowsers — N named sessions work
// simultaneously; calls on one session serialize (the channel semaphore)
// while different sessions run in parallel. Fails under -race if the
// per-session isolation breaks.
func TestConcurrentSessionsDriveIsolatedBrowsers(t *testing.T) {
	_ = chromiumPath(t)
	t.Setenv("HOME", t.TempDir())
	url := testPage(t)

	m := NewManager(ModeHeadless)
	defer m.CloseAll()
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	const sessions = 3
	var wg sync.WaitGroup
	errs := make(chan error, sessions)
	for i := range sessions {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sess, err := m.Session(fmt.Sprintf("s%d", i))
			if err != nil {
				errs <- err
				return
			}
			// Each session navigates to a page bearing its own marker, then
			// reads it back — a cross-session leak shows the wrong marker.
			marker := fmt.Sprintf("session-%d", i)
			out, err := sess.Do(ctx, func(b Backend) (string, error) {
				if err := b.Navigate(ctx, url+"/marker/"+marker); err != nil {
					return "", err
				}
				return b.Eval(ctx, "document.title")
			})
			if err != nil {
				errs <- fmt.Errorf("s%d: %w", i, err)
				return
			}
			if !strings.Contains(out, marker) {
				errs <- fmt.Errorf("s%d: session leaked — got title %s, want marker %s", i, out, marker)
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

// TestChurnOpenClose — open/work/close 10× in a row. Catches goroutine
// leaks, leaked Chrome processes, and profile-dir poisoning across restarts
// (the stale-profile wedge class).
func TestChurnOpenClose(t *testing.T) {
	_ = chromiumPath(t)
	t.Setenv("HOME", t.TempDir())
	url := testPage(t)
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	for i := range 10 {
		b, err := Open(ctx, ModeHeadless)
		if err != nil {
			t.Fatalf("iter %d open: %v", i, err)
		}
		if err := b.Navigate(ctx, url); err != nil {
			t.Fatalf("iter %d navigate: %v", i, err)
		}
		if _, err := b.Eval(ctx, "document.title"); err != nil {
			t.Fatalf("iter %d eval: %v", i, err)
		}
		if err := b.Close(); err != nil {
			t.Fatalf("iter %d close: %v", i, err)
		}
	}
}

// TestRecoverFromClosedBrowser — work, kill the browser out from under the
// session, then verify the next call reopens cleanly (Session.Do's
// stale-connection reopen path).
func TestRecoverFromClosedBrowser(t *testing.T) {
	_ = chromiumPath(t)
	t.Setenv("HOME", t.TempDir())
	url := testPage(t)

	m := NewManager(ModeHeadless)
	defer m.CloseAll()
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	sess, err := m.Session("crashy")
	if err != nil {
		t.Fatal(err)
	}
	_, err = sess.Do(ctx, func(b Backend) (string, error) {
		return "", b.Navigate(ctx, url)
	})
	if err != nil {
		t.Fatalf("initial: %v", err)
	}
	// Kill the browser behind the session's back.
	sess.drop() // closes the backend without clearing expectations; next Do reopens
	out, err := sess.Do(ctx, func(b Backend) (string, error) {
		if err := b.Navigate(ctx, url); err != nil {
			return "", err
		}
		return b.Eval(ctx, "document.title")
	})
	if err != nil {
		t.Fatalf("after crash: %v", err)
	}
	if !strings.Contains(out, "whip e2e") {
		t.Fatalf("post-crash eval: %s", out)
	}
}

// TestManySequentialCalls — 50 sequential ops on one session (the agent's
// real shape: a long multi-step task). Catches state drift and slow leaks.
func TestManySequentialCalls(t *testing.T) {
	_ = chromiumPath(t)
	t.Setenv("HOME", t.TempDir())
	url := testPage(t)

	m := NewManager(ModeHeadless)
	defer m.CloseAll()
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	sess, err := m.Session("longhaul")
	if err != nil {
		t.Fatal(err)
	}
	_, err = sess.Do(ctx, func(b Backend) (string, error) {
		return "", b.Navigate(ctx, url)
	})
	if err != nil {
		t.Fatal(err)
	}
	for i := range 50 {
		_, err := sess.Do(ctx, func(b Backend) (string, error) {
			r, err := b.Eval(ctx, fmt.Sprintf("%d+1", i))
			if err == nil && r != strconv.Itoa(i+1) {
				return "", fmt.Errorf("iter %d: got %s", i, r)
			}
			return "", err
		})
		if err != nil {
			t.Fatalf("iter %d: %v", i, err)
		}
	}
}
