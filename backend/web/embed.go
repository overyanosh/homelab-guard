package web

import (
	"embed"
	"io/fs"
	"net/http"
	"regexp"
	"strings"
)

//go:embed all:dist
var distFS embed.FS

var assetRe = regexp.MustCompile(`(?:src|href)="(/assets/[^"]+\.js)"`)

// CurrentAsset returns the JS bundle referenced by the embedded index.html
// (e.g. "index-BC7HJvnI.js"). It lets the UI detect that it is running an
// outdated bundle after a deployment.
func CurrentAsset() string {
	b, err := fs.ReadFile(distFS, "dist/index.html")
	if err != nil {
		return ""
	}
	m := assetRe.FindSubmatch(b)
	if len(m) != 2 {
		return ""
	}
	full := string(m[1])
	if i := strings.LastIndexByte(full, '/'); i >= 0 {
		return full[i+1:]
	}
	return full
}

// SPAHandler serves the embedded frontend. Hashed assets are cached forever
// (their name changes on every build) while index.html must always be
// revalidated, otherwise browsers keep loading an outdated bundle.
func SPAHandler() http.Handler {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		panic(err)
	}
	fileServer := http.FileServer(http.FS(sub))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if path != "/" {
			if _, err := fs.Stat(sub, strings.TrimPrefix(path, "/")); err != nil {
				path = "/"
				r.URL.Path = "/"
			}
		}

		if strings.HasPrefix(path, "/assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			// index.html (and SPA fallback): never serve a stale shell.
			w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
			w.Header().Set("Pragma", "no-cache")
			w.Header().Set("Expires", "0")
		}
		fileServer.ServeHTTP(w, r)
	})
}
