// Package brandicon turns the registrable domain of an MCP server into a small
// data: URI for the import screen's tile. It is decoration with one network
// party, DuckDuckGo's icon endpoint, and one on-disk cache per host so a domain
// leaves the machine at most once per month; misses are remembered too.
//
// Everything fetched is untrusted: https only, no redirects, no credentials,
// a byte cap, and the image type sniffed from the bytes against a short
// allowlist. SVG is refused because it can carry script.
package brandicon

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

const (
	maxBytes = 48 << 10
	hitTTL   = 30 * 24 * time.Hour
	missTTL  = 3 * 24 * time.Hour
	inFlight = 4
)

// Endpoint is the icon service, with %s for the registrable domain. It is a
// variable so tests can point it at a local server; nothing else changes it.
var Endpoint = "https://icons.duckduckgo.com/ip3/%s.ico"

// maxFiles bounds the cache directory; the oldest entries go first.
var maxFiles = 512

var allowed = map[string]bool{
	"image/png": true, "image/jpeg": true, "image/gif": true, "image/webp": true,
	"image/x-icon": true, "image/vnd.microsoft.icon": true,
}

type entry struct {
	Key     string `json:"key"`
	Src     string `json:"src,omitempty"` // empty: a remembered miss
	Expires int64  `json:"expires"`
}

// Resolver answers Resolve from a cache directory it owns, fetching what it
// does not have. One Resolver per daemon; it is safe for concurrent use.
type Resolver struct {
	dir     string
	client  *http.Client
	sem     chan struct{} // at most inFlight fetches at once
	mu      sync.Mutex
	pending map[string]chan struct{} // closed when that key's fetch is done
}

func New(dir string) *Resolver {
	return &Resolver{
		dir: dir,
		client: &http.Client{
			Timeout:       4 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
		sem:     make(chan struct{}, inFlight),
		pending: map[string]chan struct{}{},
	}
}

// Resolve returns a data: URI for each key it could resolve. Keys are
// lower-case registrable domains (see mcp.Candidate.BrandKey); anything else
// is ignored. Cached answers come back at once; the rest are fetched, a few
// at a time, until ctx ends. A key whose fetch failed transiently is absent
// and not remembered, so the next call tries again.
func (r *Resolver) Resolve(ctx context.Context, keys []string) map[string]string {
	out := map[string]string{}
	var wg sync.WaitGroup
	var mu sync.Mutex
	for _, key := range slices.Compact(slices.Sorted(slices.Values(keys))) {
		if !validKey(key) {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			if src := r.lookup(ctx, key); src != "" {
				mu.Lock()
				out[key] = src
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	return out
}

// lookup serves one key from the cache or fetches it once, however many
// callers ask at the same time: the first creates the pending channel and the
// rest wait for its close, then read what it wrote.
func (r *Resolver) lookup(ctx context.Context, key string) string {
	if e, ok := r.read(key); ok {
		return e.Src
	}
	r.mu.Lock()
	done, busy := r.pending[key]
	if !busy {
		done = make(chan struct{})
		r.pending[key] = done
	}
	r.mu.Unlock()
	if busy {
		select {
		case <-done:
			e, _ := r.read(key)
			return e.Src
		case <-ctx.Done():
			return ""
		}
	}
	defer func() {
		r.mu.Lock()
		delete(r.pending, key)
		r.mu.Unlock()
		close(done)
	}()
	select {
	case r.sem <- struct{}{}:
		defer func() { <-r.sem }()
	case <-ctx.Done():
		return ""
	}
	src, definitive := r.fetch(ctx, key)
	if definitive {
		ttl := hitTTL
		if src == "" {
			ttl = missTTL
		}
		r.write(entry{Key: key, Src: src, Expires: time.Now().Add(ttl).Unix()})
	}
	return src
}

// fetch asks the endpoint for one key. definitive is false for anything that
// might succeed next time (network error, 5xx, cancellation) and true for an
// answer worth remembering: an image that passes the checks, or a miss.
func (r *Resolver) fetch(ctx context.Context, key string) (src string, definitive bool) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf(Endpoint, key), nil)
	if err != nil {
		return "", true
	}
	req.Header.Set("User-Agent", "whip")
	req.Header.Set("Accept", "image/*")
	resp, err := r.client.Do(req)
	if err != nil {
		return "", false
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusBadRequest:
		return "", true
	case resp.StatusCode != http.StatusOK:
		return "", false
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return "", false
	}
	if len(body) == 0 || len(body) > maxBytes {
		return "", true
	}
	mime := http.DetectContentType(body) // the bytes decide, never the header
	if !allowed[mime] {
		return "", true
	}
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(body), true
}

// validKey admits lower-case registrable domains only: the key becomes part
// of a URL path and a cache file name, so nothing else may pass.
func validKey(key string) bool {
	if len(key) == 0 || len(key) > 253 || !strings.Contains(key, ".") || strings.Contains(key, "..") ||
		strings.HasPrefix(key, ".") || strings.HasSuffix(key, ".") || strings.HasPrefix(key, "-") || net.ParseIP(key) != nil {
		return false
	}
	letter := false
	for _, c := range key[strings.LastIndex(key, ".")+1:] { // the top-level label is alphabetic
		letter = letter || c >= 'a' && c <= 'z'
	}
	if !letter {
		return false
	}
	for _, c := range key {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '.' || c == '-') {
			return false
		}
	}
	return true
}

func (r *Resolver) path(key string) string {
	sum := sha256.Sum256([]byte(key))
	return filepath.Join(r.dir, hex.EncodeToString(sum[:12])+".json")
}

func (r *Resolver) read(key string) (entry, bool) {
	body, err := os.ReadFile(r.path(key))
	if err != nil {
		return entry{}, false
	}
	var e entry
	if json.Unmarshal(body, &e) != nil || e.Key != key || time.Now().Unix() > e.Expires {
		return entry{}, false
	}
	return e, true
}

// write is best effort: a cache that cannot be written only costs a refetch.
func (r *Resolver) write(e entry) {
	if err := os.MkdirAll(r.dir, 0o700); err != nil {
		return
	}
	body, err := json.Marshal(e)
	if err != nil {
		return
	}
	path := r.path(e.Key)
	if err := os.WriteFile(path+".tmp", body, 0o600); err != nil {
		return
	}
	if err := os.Rename(path+".tmp", path); err != nil {
		_ = os.Remove(path + ".tmp")
		return
	}
	r.prune()
}

// prune keeps the directory at maxFiles entries, dropping the oldest.
func (r *Resolver) prune() {
	entries, err := os.ReadDir(r.dir)
	if err != nil || len(entries) <= maxFiles {
		return
	}
	type aged struct {
		name string
		mod  time.Time
	}
	files := make([]aged, 0, len(entries))
	for _, d := range entries {
		if info, err := d.Info(); err == nil && !d.IsDir() && strings.HasSuffix(d.Name(), ".json") {
			files = append(files, aged{d.Name(), info.ModTime()})
		}
	}
	slices.SortFunc(files, func(a, b aged) int { return a.mod.Compare(b.mod) })
	for i := 0; i < len(files)-maxFiles; i++ {
		_ = os.Remove(filepath.Join(r.dir, files[i].name))
	}
}
