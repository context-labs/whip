package webassets

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestWebAssetsAndClientRoutes(t *testing.T) {
	files := fstest.MapFS{
		"index.html":              {Data: []byte(`<!doctype html><html><script type="module" src="/assets/app-12345678.js"></script></html>`)},
		"assets/app-12345678.js":  {Data: []byte(`export const app=true;`)},
		"assets/app-12345678.css": {Data: []byte(`body{color:black}`)},
		"assets/plain.js":         {Data: []byte("plain script")},
		".gitkeep":                {Data: []byte("hidden")},
	}
	handler := newHandler(files)
	tests := []struct {
		name, path, method string
		status             int
		body               string
		cached             bool
	}{
		{name: "root", path: "/", method: "GET", status: 200, body: "<html>"},
		{name: "route refresh", path: "/sessions/root-123", method: "GET", status: 200, body: "<html>"},
		{name: "script", path: "/assets/app-12345678.js", method: "GET", status: 200, body: "export const", cached: true},
		{name: "unhashed script", path: "/assets/plain.js", method: "GET", status: 200, body: "plain script"},
		{name: "style", path: "/assets/app-12345678.css", method: "GET", status: 200, body: "body{", cached: true},
		{name: "head", path: "/sessions/root", method: "HEAD", status: 200},
		{name: "missing asset", path: "/assets/missing.js", method: "GET", status: 404},
		{name: "missing icon", path: "/favicon.ico", method: "GET", status: 404},
		{name: "unknown api", path: "/api/v1/foo", method: "GET", status: 404},
		{name: "hidden", path: "/.gitkeep", method: "GET", status: 404},
		{name: "traversal", path: "/assets/../.gitkeep", method: "GET", status: 404},
		{name: "writes", path: "/sessions/root", method: "POST", status: 405},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequestWithContext(t.Context(), test.method, "http://localhost"+test.path, nil))
			if response.Code != test.status {
				t.Fatalf("status %d: %s", response.Code, response.Body.String())
			}
			if test.body != "" && !strings.Contains(response.Body.String(), test.body) {
				t.Fatalf("body %q", response.Body.String())
			}
			if test.method == "HEAD" && response.Body.Len() != 0 {
				t.Fatal("HEAD returned a body")
			}
			if response.Header().Get("Location") != "" {
				t.Fatal("SPA route redirected")
			}
			cache := response.Header().Get("Cache-Control")
			if test.status == 200 && (strings.Contains(cache, "immutable") != test.cached) {
				t.Fatalf("cache policy %q", cache)
			}
			if response.Header().Get("Content-Security-Policy") != ContentSecurityPolicy {
				t.Fatal("missing production CSP")
			}
			if response.Header().Get("X-Content-Type-Options") != "nosniff" {
				t.Fatal("missing MIME protection")
			}
		})
	}
}

func TestMissingWebAssetsAreActionable(t *testing.T) {
	handler := newHandler(fstest.MapFS{
		"assets/plain.js": {Data: []byte("plain script")},
		".gitkeep":        {},
	})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "http://localhost/", nil))
	if response.Code != 503 || !strings.Contains(response.Body.String(), "npm ci") || !strings.Contains(response.Body.String(), "task build") {
		t.Fatalf("missing build response: %d %s", response.Code, response.Body.String())
	}
}
