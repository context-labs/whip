package openaiauth

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func capturedManager(t *testing.T, handler http.HandlerFunc) (*Manager, CapturedCredentials) {
	t.Helper()
	m := testManager(t, handler)
	credentials := testCredentials()
	credentials.ExpiresAt = time.Now().Add(time.Hour)
	if err := m.Install(t.Context(), m.Generation(), credentials); err != nil {
		t.Fatal(err)
	}
	captured, err := m.Capture(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	return m, captured
}

func TestCaptureOrdersWithInstall(t *testing.T) {
	m, old := capturedManager(t, func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("fresh capture refreshed credentials")
	})
	credentials := old.Credentials
	credentials.AccessToken, credentials.AccountID = "replacement", "replacement-account"
	started, release := make(chan struct{}), make(chan struct{})
	save := m.save
	m.save = func(path string, credentials Credentials) (bool, error) {
		close(started)
		<-release
		return save(path, credentials)
	}
	installed := make(chan error, 1)
	go func() { installed <- m.Install(t.Context(), old.Generation, credentials) }()
	<-started
	result := make(chan CapturedCredentials, 1)
	captureErr := make(chan error, 1)
	go func() {
		captured, err := m.Capture(t.Context())
		result <- captured
		captureErr <- err
	}()
	close(release)
	if err := <-installed; err != nil {
		t.Fatal(err)
	}
	captured := <-result
	if err := <-captureErr; err != nil {
		t.Fatal(err)
	}
	if captured.Generation != old.Generation+1 || captured.Credentials != credentials {
		t.Fatal("capture mixed credentials and login generations")
	}
	if err := m.Check(t.Context(), captured); err != nil {
		t.Fatal(err)
	}
}

func TestCapturedLoginChanges(t *testing.T) {
	for _, change := range []string{"logout", "same account", "different account"} {
		t.Run(change, func(t *testing.T) {
			m, captured := capturedManager(t, func(_ http.ResponseWriter, _ *http.Request) {
				t.Error("stale login caused a token exchange")
			})
			if err := m.Check(t.Context(), captured); err != nil {
				t.Fatal(err)
			}
			if change == "logout" {
				if err := m.Logout(); err != nil {
					t.Fatal(err)
				}
			} else {
				credentials := captured.Credentials
				credentials.AccessToken = "replacement"
				if change == "different account" {
					credentials.AccountID = "replacement-account"
				}
				if err := m.Install(t.Context(), captured.Generation, credentials); err != nil {
					t.Fatal(err)
				}
				current, err := m.Capture(t.Context())
				if err != nil || current.Generation == captured.Generation || current.Credentials != credentials {
					t.Fatalf("replacement was not captured: %v", err)
				}
			}
			if err := m.Check(t.Context(), captured); !errors.Is(err, ErrLoginChanged) {
				t.Fatalf("stale check: %v", err)
			}
			if result, err := m.RefreshCaptured(t.Context(), captured); !errors.Is(err, ErrLoginChanged) || result != (CapturedCredentials{}) {
				t.Fatalf("stale refresh: %v", err)
			}
		})
	}
}

func TestCapturedAccountAndCancellation(t *testing.T) {
	m, captured := capturedManager(t, func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("invalid capture caused a token exchange")
	})
	for _, account := range []string{"", "different-account"} {
		t.Run("account="+account, func(t *testing.T) {
			invalid := captured
			invalid.Credentials.AccountID = account
			if err := m.Check(t.Context(), invalid); !errors.Is(err, ErrLoginChanged) {
				t.Fatalf("wrong account check: %v", err)
			}
			if result, err := m.RefreshCaptured(t.Context(), invalid); !errors.Is(err, ErrLoginChanged) || result != (CapturedCredentials{}) {
				t.Fatalf("wrong account refresh: %v", err)
			}
		})
	}
	if err := m.Check(t.Context(), CapturedCredentials{}); !errors.Is(err, ErrLoginChanged) {
		t.Fatalf("empty capture check: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if result, err := m.Capture(ctx); !errors.Is(err, context.Canceled) || result != (CapturedCredentials{}) {
		t.Fatalf("cancelled capture: %v", err)
	}
	if err := m.Check(ctx, captured); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled check: %v", err)
	}
	if result, err := m.RefreshCaptured(ctx, captured); !errors.Is(err, context.Canceled) || result != (CapturedCredentials{}) {
		t.Fatalf("cancelled refresh: %v", err)
	}
}

func TestRefreshCapturedCoalesces(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var requests atomic.Int32
	m, captured := capturedManager(t, func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			close(started)
		}
		if err := r.ParseForm(); err != nil || r.Form.Get("refresh_token") != "old-refresh" {
			t.Error("incorrect refresh token")
		}
		<-release
		refreshResponse(w)
	})
	ctx, cancel := context.WithCancel(t.Context())
	cancelled := make(chan error, 1)
	go func() { _, err := m.RefreshCaptured(ctx, captured); cancelled <- err }()
	<-started
	var wg sync.WaitGroup
	for range 24 {
		wg.Go(func() {
			result, err := m.RefreshCaptured(t.Context(), captured)
			if err != nil || result.Generation != captured.Generation || result.Credentials.AccessToken != "new-access" {
				t.Errorf("captured refresh waiter: %v", err)
			}
		})
	}
	cancel()
	if err := <-cancelled; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled waiter: %v", err)
	}
	close(release)
	wg.Wait()
	if err := m.Check(t.Context(), captured); err != nil {
		t.Fatalf("rotation invalidated the login: %v", err)
	}
	result, err := m.RefreshCaptured(t.Context(), captured)
	if err != nil || result.Generation != captured.Generation || result.Credentials.AccessToken != "new-access" || requests.Load() != 1 {
		t.Fatalf("did not reuse the already rotated token: requests=%d err=%v", requests.Load(), err)
	}
	reopened := New(t.Context(), filepath.Dir(m.path))
	t.Cleanup(reopened.Close)
	durable, err := reopened.Capture(t.Context())
	if err != nil || durable.Credentials.AccessToken != result.Credentials.AccessToken ||
		durable.Credentials.RefreshToken != result.Credentials.RefreshToken ||
		durable.Credentials.AccountID != result.Credentials.AccountID ||
		!durable.Credentials.ExpiresAt.Equal(result.Credentials.ExpiresAt) {
		t.Fatalf("rotation did not persist: %v", err)
	}
}

func TestRefreshCapturedCannotUndoLoginChange(t *testing.T) {
	for _, change := range []string{"logout", "same account", "different account"} {
		t.Run(change, func(t *testing.T) {
			started, release := make(chan struct{}), make(chan struct{})
			m, captured := capturedManager(t, func(w http.ResponseWriter, _ *http.Request) {
				close(started)
				<-release
				refreshResponse(w)
			})
			result := make(chan error, 1)
			go func() { _, err := m.RefreshCaptured(t.Context(), captured); result <- err }()
			<-started
			if change == "logout" {
				if err := m.Logout(); err != nil {
					t.Fatal(err)
				}
			} else {
				credentials := captured.Credentials
				credentials.AccessToken = "replacement"
				if change == "different account" {
					credentials.AccountID = "replacement-account"
				}
				if err := m.Install(t.Context(), captured.Generation, credentials); err != nil {
					t.Fatal(err)
				}
			}
			close(release)
			if err := <-result; !errors.Is(err, ErrLoginChanged) {
				t.Fatalf("late refresh returned success: %v", err)
			}
			current, err := m.Capture(t.Context())
			if change == "logout" {
				if !errors.Is(err, ErrSignInRequired) {
					t.Fatalf("refresh undid logout: %v", err)
				}
			} else if err != nil || current.Credentials.AccessToken != "replacement" {
				t.Fatalf("refresh overwrote replacement: %v", err)
			}
		})
	}
}

// waitContext pauses a refresh caller at its wait boundary. The manager's owned
// network work uses its own context, so it can finish before the caller resumes.
type waitContext struct {
	context.Context
	waiting chan struct{}
	release chan struct{}
	once    sync.Once
}

func (c *waitContext) Done() <-chan struct{} {
	c.once.Do(func() {
		close(c.waiting)
		<-c.release
	})
	return c.Context.Done()
}

func TestCaptureRevalidatesAfterRefreshPublication(t *testing.T) {
	for _, operation := range []string{"capture", "rejected token"} {
		t.Run(operation, func(t *testing.T) {
			m, captured := capturedManager(t, func(w http.ResponseWriter, _ *http.Request) { refreshResponse(w) })
			if operation == "capture" {
				m.mu.Lock()
				m.credential.ExpiresAt = time.Now().Add(-time.Minute)
				m.mu.Unlock()
			}
			ctx := &waitContext{Context: t.Context(), waiting: make(chan struct{}), release: make(chan struct{})}
			result := make(chan error, 1)
			go func() {
				var err error
				if operation == "capture" {
					_, err = m.Capture(ctx)
				} else {
					_, err = m.RefreshCaptured(ctx, captured)
				}
				result <- err
			}()
			<-ctx.waiting
			// This independent waiter observes a fully published refresh first.
			if _, err := m.RefreshCaptured(t.Context(), captured); err != nil {
				t.Fatal(err)
			}
			credentials := captured.Credentials
			credentials.AccessToken = "replacement"
			if err := m.Install(t.Context(), captured.Generation, credentials); err != nil {
				t.Fatal(err)
			}
			close(ctx.release)
			if err := <-result; !errors.Is(err, ErrLoginChanged) {
				t.Fatalf("waiter returned a login replaced after refresh publication: %v", err)
			}
		})
	}
}

func TestRefreshCapturedRetriesRotatedPersistence(t *testing.T) {
	var requests atomic.Int32
	m, captured := capturedManager(t, func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		refreshResponse(w)
	})
	diskErr := errors.New("disk unavailable")
	save := m.save
	m.save = func(string, Credentials) (bool, error) { return false, diskErr }
	if result, err := m.RefreshCaptured(t.Context(), captured); !errors.Is(err, diskErr) || result != (CapturedCredentials{}) {
		t.Fatalf("unpersisted rotation returned success: %v", err)
	}
	if err := m.Check(t.Context(), captured); err == nil {
		t.Fatal("unpersisted rotated credentials passed dispatch check")
	}
	m.mu.Lock()
	m.save = save
	m.mu.Unlock()
	result, err := m.RefreshCaptured(t.Context(), captured)
	if err != nil || result.Generation != captured.Generation || result.Credentials.RefreshToken != "new-refresh" || requests.Load() != 1 {
		t.Fatalf("did not persist the rotated token without another exchange: requests=%d err=%v", requests.Load(), err)
	}
	if err := m.Check(t.Context(), result); err != nil {
		t.Fatal(err)
	}
	reopened := New(t.Context(), filepath.Dir(m.path))
	t.Cleanup(reopened.Close)
	durable, err := reopened.Capture(t.Context())
	if err != nil || durable.Credentials.AccessToken != result.Credentials.AccessToken ||
		durable.Credentials.RefreshToken != result.Credentials.RefreshToken ||
		durable.Credentials.AccountID != result.Credentials.AccountID ||
		!durable.Credentials.ExpiresAt.Equal(result.Credentials.ExpiresAt) {
		t.Fatalf("rotation did not persist: %v", err)
	}
}

func TestCapturedRejectionAndShutdown(t *testing.T) {
	t.Run("terminal rejection", func(t *testing.T) {
		var requests atomic.Int32
		m, captured := capturedManager(t, func(w http.ResponseWriter, _ *http.Request) {
			requests.Add(1)
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"code":"refresh_token_reused","message":"private-token"}}`))
		})
		for range 2 {
			result, err := m.RefreshCaptured(t.Context(), captured)
			if err == nil || result != (CapturedCredentials{}) || strings.Contains(err.Error(), "private-token") {
				t.Fatalf("terminal refresh error: %v", err)
			}
			if err := m.Check(t.Context(), captured); err == nil || strings.Contains(err.Error(), "private-token") {
				t.Fatalf("terminal check error: %v", err)
			}
		}
		if requests.Load() != 1 {
			t.Fatal("retried a terminal refresh rejection")
		}
	})
	t.Run("close joins owned refresh", func(t *testing.T) {
		started := make(chan struct{})
		m, captured := capturedManager(t, func(_ http.ResponseWriter, r *http.Request) {
			_ = r.ParseForm()
			close(started)
			<-r.Context().Done()
		})
		result := make(chan error, 1)
		go func() { _, err := m.RefreshCaptured(t.Context(), captured); result <- err }()
		<-started
		m.Close()
		if err := <-result; err == nil {
			t.Fatal("refresh succeeded after shutdown")
		}
		if _, err := m.Capture(t.Context()); !errors.Is(err, context.Canceled) {
			t.Fatalf("capture after shutdown: %v", err)
		}
		if err := m.Check(t.Context(), captured); !errors.Is(err, context.Canceled) {
			t.Fatalf("check after shutdown: %v", err)
		}
	})
}
