package llm

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGenericRequestsWithoutAuthentication(t *testing.T) {
	for _, key := range []string{"", "test-literal-key"} {
		t.Run(key, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if key == "" {
					if _, present := r.Header["Authorization"]; present {
						t.Error("unauthenticated request emitted Authorization")
					}
				} else if r.Header.Get("Authorization") != "Bearer "+key {
					t.Error("authenticated request omitted its credential")
				}
				if r.URL.Path == "/models" {
					_, _ = w.Write([]byte(`{"data":[{"id":"m"}]}`))
					return
				}
				var request Request
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Error(err)
				}
				if request.Stream {
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\ndata: [DONE]\n\n"))
				} else {
					_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"hello"}}]}`))
				}
			}))
			defer server.Close()
			client := New(server.URL, key)
			if _, err := client.Models(t.Context()); err != nil {
				t.Fatal(err)
			}
			if _, _, err := client.Complete(t.Context(), Request{Model: "m"}); err != nil {
				t.Fatal(err)
			}
			if _, _, err := client.Stream(t.Context(), Request{Model: "m"}, nil, nil, nil); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestModelsResponseBound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[],"padding":"` + strings.Repeat("x", 8<<20) + `"}`))
	}))
	defer server.Close()
	if _, err := New(server.URL, "").Models(t.Context()); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("unbounded catalog: %v", err)
	}
}
