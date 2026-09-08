// Package webassets serves the production browser application embedded at build
// time. A plain Go development build remains valid and reports missing assets.
package webassets

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"regexp"
	"strings"
)

//go:embed csp.txt
var contentSecurityPolicy string

// ContentSecurityPolicy is shared by Go responses, desktop packaging and acceptance tests.
var ContentSecurityPolicy = strings.TrimSpace(contentSecurityPolicy)

//go:embed all:dist
var embedded embed.FS

var hashedAsset = regexp.MustCompile(`-[A-Za-z0-9_-]{8,}\.[a-zA-Z0-9]+$`)

// Available reports whether a production app was packaged into this binary.
func Available() bool {
	info, err := fs.Stat(embedded, "dist/index.html")
	return err == nil && !info.IsDir() && info.Size() > 0
}

// Handler serves assets and client-side routes from the embedded app only.
func Handler() http.Handler {
	files, err := fs.Sub(embedded, "dist")
	if err != nil {
		panic(err)
	} // dist is enforced by go:embed.
	return newHandler(files)
}

func newHandler(files fs.FS) http.Handler {
	server := http.FileServer(http.FS(files))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", ContentSecurityPolicy)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		name := strings.TrimPrefix(r.URL.Path, "/")
		if name == "api" || strings.HasPrefix(name, "api/") || strings.Contains(name, "\\") ||
			strings.HasPrefix(name, ".") || strings.Contains(name, "/.") {
			http.NotFound(w, r)
			return
		}
		if _, err := fs.Stat(files, "index.html"); err != nil {
			http.Error(w, "WHIP web assets are not embedded. From source, run npm ci followed by task build, "+
				"then explicitly restart the daemon.", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Cache-Control", "no-cache")
		if name != "" {
			info, err := fs.Stat(files, name)
			if err == nil && !info.IsDir() {
				if strings.HasPrefix(name, "assets/") && hashedAsset.MatchString(path.Base(name)) {
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				}
				server.ServeHTTP(w, r)
				return
			}
			if name == "assets" || strings.HasPrefix(name, "assets/") || path.Ext(name) != "" {
				http.NotFound(w, r)
				return
			}
		}
		// FileServer serves index.html for /. Rewrite only the cloned request so a
		// deep-link refresh never redirects away from its client-side route.
		request := r.Clone(r.Context())
		request.URL.Path = "/"
		request.URL.RawPath = ""
		server.ServeHTTP(w, request)
	})
}
