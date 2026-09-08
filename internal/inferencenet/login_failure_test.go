package inferencenet

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestLoginFailureNeverReturnsPartiallyAuthorizedAccount(t *testing.T) {
	for _, test := range []struct {
		name, path, body, message string
		status                    int
	}{
		{"device request", "/api/auth/device/code", `{}`, "500", http.StatusInternalServerError},
		{"denied", "/api/auth/device/token", `{"error":"access_denied"}`, "denied", http.StatusBadRequest},
		{"expired", "/api/auth/device/token", `{"error":"expired_token"}`, "expired", http.StatusBadRequest},
		{"provider error", "/api/auth/device/token", `{"error":"unknown","message":"service unavailable"}`, "service unavailable", http.StatusBadGateway},
		{"error code", "/api/auth/device/token", `{"error":"unknown"}`, "unknown", http.StatusBadGateway},
		{"status only", "/api/auth/device/token", `{}`, "HTTP 503", http.StatusServiceUnavailable},
		{"malformed token", "/api/auth/device/token", `{`, "unexpected", http.StatusOK},
		{"identity", "/api/auth/get-session", `{}`, "500", http.StatusInternalServerError},
		{"workspaces", "/api/auth/organization/list", `[]`, "500", http.StatusInternalServerError},
		{"no workspace", "/api/auth/organization/list", `[]`, "no workspace", http.StatusOK},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == test.path {
					w.WriteHeader(test.status)
					_, _ = w.Write([]byte(test.body))
					return
				}
				switch r.URL.Path {
				case "/api/auth/device/code":
					fmt.Fprint(w, `{"device_code":"fixture","user_code":"display","expires_in":60}`)
				case "/api/auth/device/token":
					fmt.Fprint(w, `{"access_token":"fixture-session"}`)
				case "/api/auth/get-session":
					fmt.Fprint(w, `{"user":{"id":"user","email":"fixture@example.test"}}`)
				default:
					t.Errorf("failed authorization continued to %s", r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer server.Close()
			useStub(t, server)
			previous := pollSleepOverride
			pollSleepOverride = func(context.Context, time.Duration) error { return nil }
			t.Cleanup(func() { pollSleepOverride = previous })
			auth, err := CompleteLogin(t.Context(), nil, nil)
			if err == nil || !strings.Contains(err.Error(), test.message) || auth != (Auth{}) {
				t.Fatalf("partial authorization escaped: %+v, %v", auth, err)
			}
		})
	}
}

func TestDevicePollingBackoffExpiryAndCancellation(t *testing.T) {
	polls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		polls++
		if polls == 1 {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, `{"error":"slow_down"}`)
			return
		}
		fmt.Fprint(w, `{"access_token":"fixture-token"}`)
	}))
	defer server.Close()
	useStub(t, server)
	previous := pollSleepOverride
	t.Cleanup(func() { pollSleepOverride = previous })
	var waits []time.Duration
	pollSleepOverride = func(_ context.Context, delay time.Duration) error { waits = append(waits, delay); return nil }
	token, err := pollForDeviceToken(t.Context(), deviceCodeResponse{ExpiresIn: 60, Interval: 1})
	if err != nil || token != "fixture-token" || len(waits) != 2 || waits[1] != 5*time.Second {
		t.Fatalf("provider backoff was ignored: %q %v %v", token, waits, err)
	}
	before := polls
	if _, err := pollForDeviceToken(t.Context(), deviceCodeResponse{ExpiresIn: -1}); !errors.Is(err, errDeviceCodeExpired) || polls != before {
		t.Fatalf("expired code made another request: %d %v", polls, err)
	}
	pollSleepOverride = func(context.Context, time.Duration) error { return context.Canceled }
	if _, err := pollForDeviceToken(t.Context(), deviceCodeResponse{ExpiresIn: 60}); !errors.Is(err, context.Canceled) || polls != before {
		t.Fatalf("cancelled polling made another request: %d %v", polls, err)
	}
}
