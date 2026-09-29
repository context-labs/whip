package browser

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"
)

func TestCapturedChromeDPNavigationNeedsNoAllocator(t *testing.T) {
	for _, outcome := range []string{"loaded", "rejected", "transport"} {
		t.Run(outcome, func(t *testing.T) {
			var methods []string
			failed := errors.New("test connection ended")
			client := &desktopTestClient{call: func(_ context.Context, session, method string, params any) ([]byte, error) {
				if session != "captured" {
					t.Fatalf("session %q", session)
				}
				methods = append(methods, method)
				switch method {
				case "Page.navigate":
					raw, err := json.Marshal(params)
					if err != nil {
						t.Fatal(err)
					}
					var request struct{ URL string }
					if err = json.Unmarshal(raw, &request); err != nil {
						t.Fatal(err)
					}
					if request.URL != "https://example.test/selected" {
						t.Fatal(request.URL)
					}
					if outcome == "transport" {
						return nil, failed
					}
					if outcome == "rejected" {
						return []byte(`{"frameId":"frame","errorText":"private upstream failure"}`), nil
					}
					return []byte(`{"frameId":"frame","loaderId":"loader"}`), nil
				case "Runtime.evaluate":
					return []byte(`{"result":{"type":"string","value":"complete"}}`), nil
				default:
					t.Fatalf("unexpected acquisition or effect %s", method)
					return nil, failed
				}
			}}
			backend := &chromedpBackend{executor: &desktopExecutor{ctx: t.Context(), client: client, sessionID: "captured"}}
			err := backend.Navigate(t.Context(), "https://example.test/selected")
			want := []string{"Page.navigate"}
			switch outcome {
			case "loaded":
				want = append(want, "Runtime.evaluate")
				if err != nil {
					t.Fatal(err)
				}
			case "transport":
				if !errors.Is(err, failed) {
					t.Fatal(err)
				}
			case "rejected":
				if err == nil || err.Error() != "browser navigation failed" {
					t.Fatal(err)
				}
			}
			if !slices.Equal(methods, want) {
				t.Fatalf("navigation replayed or acquired a second target: %v", methods)
			}
		})
	}
}
