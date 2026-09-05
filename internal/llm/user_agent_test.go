package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientSendsWhipUserAgent(t *testing.T) {
	tests := []struct {
		name string
		run  func(*Client) error
	}{
		{
			name: "models",
			run: func(c *Client) error {
				_, err := c.Models(context.Background())
				return err
			},
		},
		{
			name: "stream",
			run: func(c *Client) error {
				_, _, err := c.Stream(context.Background(), Request{Model: "m", Messages: []Message{{Role: "user", Content: "hi"}}}, nil, nil, nil)
				return err
			},
		},
		{
			name: "complete",
			run: func(c *Client) error {
				_, _, err := c.Complete(context.Background(), Request{Model: "m", Messages: []Message{{Role: "user", Content: "hi"}}})
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotUserAgent string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotUserAgent = r.Header.Get("User-Agent")
				switch r.URL.Path {
				case "/models":
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(`{"data":[]}`))
				case "/chat/completions":
					if r.Header.Get("Content-Type") != "application/json" {
						t.Fatalf("Content-Type = %q, want application/json", r.Header.Get("Content-Type"))
					}
					w.Header().Set("Content-Type", "text/event-stream")
					if tt.name == "complete" {
						_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
						return
					}
					_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n"))
				default:
					t.Fatalf("unexpected path %s", r.URL.Path)
				}
			}))
			defer srv.Close()

			c := New(srv.URL, "k")
			if err := tt.run(c); err != nil {
				t.Fatal(err)
			}
			if gotUserAgent != "whip" {
				t.Fatalf("User-Agent = %q, want whip", gotUserAgent)
			}
		})
	}
}
