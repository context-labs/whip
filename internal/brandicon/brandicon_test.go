package brandicon

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func tinyPNG(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// fixture serves a fake icon endpoint keyed by the domain in the path and
// counts requests per domain. Every handler asserts the request carries no
// credentials.
func fixture(t *testing.T, routes map[string]http.HandlerFunc) (*Resolver, *sync.Map) {
	t.Helper()
	var hits sync.Map
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
			t.Errorf("icon fetch carried credentials: %v", r.Header)
		}
		key := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/"), ".ico")
		n, _ := hits.LoadOrStore(key, new(atomic.Int32))
		n.(*atomic.Int32).Add(1)
		if h, ok := routes[key]; ok {
			h(w, r)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	old := Endpoint
	Endpoint = srv.URL + "/%s.ico"
	t.Cleanup(func() { Endpoint = old })
	return New(filepath.Join(t.TempDir(), "icons")), &hits
}

func count(hits *sync.Map, key string) int32 {
	n, ok := hits.Load(key)
	if !ok {
		return 0
	}
	return n.(*atomic.Int32).Load()
}

func TestResolveCachesHitsAndMisses(t *testing.T) {
	img := tinyPNG(t)
	r, hits := fixture(t, map[string]http.HandlerFunc{
		"figma.com": func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(img) },
		"exa.ai":    func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusBadRequest) },
	})
	got := r.Resolve(t.Context(), []string{"figma.com", "exa.ai", "nobody.example", "figma.com"})
	if !strings.HasPrefix(got["figma.com"], "data:image/png;base64,") || len(got) != 1 {
		t.Fatalf("Resolve = %v", got)
	}
	got = r.Resolve(t.Context(), []string{"figma.com", "exa.ai", "nobody.example"})
	if len(got) != 1 || count(hits, "figma.com") != 1 || count(hits, "exa.ai") != 1 || count(hits, "nobody.example") != 1 {
		t.Errorf("hits and misses must be served from the cache on the second call: %v, figma=%d exa=%d nobody=%d", got, count(hits, "figma.com"), count(hits, "exa.ai"), count(hits, "nobody.example"))
	}
	// A fresh resolver over the same directory reads the same cache.
	Endpoint = "http://127.0.0.1:1/%s.ico" // unreachable: only the cache can answer
	if got := New(r.dir).Resolve(t.Context(), []string{"figma.com"}); got["figma.com"] == "" {
		t.Error("the cache must survive a restart")
	}
}

func TestResolveRefusesWhatIsNotASmallRasterImage(t *testing.T) {
	img := tinyPNG(t)
	r, hits := fixture(t, map[string]http.HandlerFunc{
		"html.example": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "image/png") // the header lies; the bytes decide
			_, _ = w.Write([]byte("<!doctype html><html><body>hi</body></html>"))
		},
		"svg.example": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "image/svg+xml")
			_, _ = w.Write([]byte(`<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`))
		},
		"big.example": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write(append(slices.Clone(img), make([]byte, maxBytes)...))
		},
		"redirect.example": func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "http://169.254.169.254/latest/meta-data/", http.StatusFound)
		},
		"flaky.example": func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusBadGateway) },
	})
	keys := []string{"html.example", "svg.example", "big.example", "redirect.example", "flaky.example"}
	if got := r.Resolve(t.Context(), keys); len(got) != 0 {
		t.Fatalf("nothing here is an acceptable icon, got %v", got)
	}
	r.Resolve(t.Context(), keys)
	for _, key := range []string{"html.example", "svg.example", "big.example"} {
		if count(hits, key) != 1 {
			t.Errorf("%s is a definitive miss and must be cached, fetched %d times", key, count(hits, key))
		}
	}
	for _, key := range []string{"redirect.example", "flaky.example"} {
		if count(hits, key) != 2 {
			t.Errorf("%s may succeed later and must not be cached, fetched %d times", key, count(hits, key))
		}
	}
}

func TestResolveIgnoresKeysThatAreNotDomains(t *testing.T) {
	r, hits := fixture(t, nil)
	r.Resolve(t.Context(), []string{"Figma.com", "localhost", "127.0.0.1", "a/b.com", "../x.com", ".x.com", "x.com.", "-x.com", "x..com", ""})
	total := int32(0)
	hits.Range(func(_, n any) bool { total += n.(*atomic.Int32).Load(); return true })
	if total != 0 {
		t.Fatalf("no request may leave for an invalid key, saw %d", total)
	}
	if entries, _ := os.ReadDir(r.dir); len(entries) != 0 {
		t.Fatalf("nothing should be cached for invalid keys, found %d files", len(entries))
	}
}

func TestResolveFetchesAConcurrentKeyOnce(t *testing.T) {
	img := tinyPNG(t)
	release := make(chan struct{})
	r, hits := fixture(t, map[string]http.HandlerFunc{
		"slow.example": func(w http.ResponseWriter, _ *http.Request) { <-release; _, _ = w.Write(img) },
	})
	var wg sync.WaitGroup
	results := make([]map[string]string, 16)
	for i := range results {
		wg.Add(1)
		go func() { defer wg.Done(); results[i] = r.Resolve(t.Context(), []string{"slow.example"}) }()
	}
	time.Sleep(50 * time.Millisecond)
	close(release)
	wg.Wait()
	for i, got := range results {
		if got["slow.example"] == "" {
			t.Errorf("caller %d got nothing", i)
		}
	}
	if n := count(hits, "slow.example"); n != 1 {
		t.Fatalf("sixteen callers must share one fetch, saw %d", n)
	}
}

func TestResolveStopsWithTheContext(t *testing.T) {
	r, _ := fixture(t, map[string]http.HandlerFunc{
		"stuck.example": func(w http.ResponseWriter, req *http.Request) { <-req.Context().Done() },
	})
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	if got := r.Resolve(ctx, []string{"stuck.example"}); len(got) != 0 {
		t.Fatalf("got %v", got)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("Resolve must return when its context ends")
	}
	if entries, _ := os.ReadDir(r.dir); len(entries) != 0 {
		t.Fatal("a cancelled fetch must not be remembered as a miss")
	}
}

func TestCacheIsBoundedAndPrivate(t *testing.T) {
	old := maxFiles
	maxFiles = 3
	t.Cleanup(func() { maxFiles = old })
	img := tinyPNG(t)
	r, _ := fixture(t, map[string]http.HandlerFunc{
		"a.example": func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(img) },
		"b.example": func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(img) },
		"c.example": func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(img) },
		"d.example": func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(img) },
	})
	for _, key := range []string{"a.example", "b.example", "c.example", "d.example"} {
		r.Resolve(t.Context(), []string{key})
		time.Sleep(15 * time.Millisecond) // distinct mtimes
	}
	entries, err := os.ReadDir(r.dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("cache should hold %d files, has %d", 3, len(entries))
	}
	if _, ok := r.read("a.example"); ok {
		t.Error("the oldest entry should have been pruned")
	}
	info, err := os.Stat(r.dir)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Errorf("cache dir mode = %o, want 700", info.Mode().Perm())
	}
	if info, err := os.Stat(r.path("d.example")); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("cache file mode = %v %v, want 600", info, err)
	}
}
