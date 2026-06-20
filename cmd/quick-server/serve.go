package main

import (
	"io"
	"mime"
	"net/http"
	"path"
	"strings"
)

// MIME types Go doesn't know by default: with nosniff on, a wrong type means
// "downloaded" instead of served correctly.
func init() {
	mime.AddExtensionType(".wasm", "application/wasm")
	mime.AddExtensionType(".webmanifest", "application/manifest+json")
}

// handleSite is the catch-all: resolves the site from the host, applies policy
// (public / code / SSO) and, if access is granted, serves the file.
func (s *server) handleSite(w http.ResponseWriter, r *http.Request) {
	host := fwdHost(r)
	sub := subOf(host, s.baseDomain)
	if sub == "" {
		http.NotFound(w, r)
		return
	}
	p, err := s.meta.load(sub)
	if err != nil {
		http.Error(w, "site temporarily unavailable", http.StatusServiceUnavailable)
		return
	}
	switch p.Access {
	case "public":
		s.serveSite(w, r, sub)
	case "code":
		if c, err := r.Cookie("quick_access_" + sub); err == nil && s.meta.validAccessCookie(sub, c.Value) {
			s.serveSite(w, r, sub)
			return
		}
		s.redirect(w, r, host, "/__quick/code")
	default: // SSO: if not authenticated, show the sign-in page (no bare redirect)
		if _, ok := s.checkSSO(r); !ok {
			s.renderSSOPage(w, r, host)
			return
		}
		s.serveSite(w, r, sub)
	}
}

// serveSite resolves a path to a file with static-host conventions:
//
//	/path                -> exact file
//	/about               -> /about.html        (clean URLs, no extension)
//	/about  or  /about/  -> /about/index.html  (directory index)
//
// A path that matches nothing does NOT fall back to the home page: if 200.html
// exists it acts as an SPA app shell (200), otherwise it's a 404 (with the
// site's 404.html if present).
func (s *server) serveSite(w http.ResponseWriter, r *http.Request, sub string) {
	p := r.URL.Path
	cands := []string{p}
	if path.Ext(p) == "" && !strings.HasSuffix(p, "/") {
		cands = append(cands, p+".html")
	}
	cands = append(cands, strings.TrimRight(p, "/")+"/index.html")
	for _, cand := range cands {
		if s.serveFile(w, r, sub, cand, http.StatusOK) {
			return
		}
	}
	// SPA opt-in: 200.html acts as the app shell for non-file routes.
	if s.serveFile(w, r, sub, "/200.html", http.StatusOK) {
		return
	}
	if !s.serveFile(w, r, sub, "/404.html", http.StatusNotFound) {
		w.WriteHeader(http.StatusNotFound)
	}
}

// serveFile serves cand if it exists and returns true. With status 200 it uses
// http.ServeContent (range, etag, content-type). With an error status it writes
// that status with the content-type from the extension (ServeContent would force 200).
func (s *server) serveFile(w http.ResponseWriter, r *http.Request, sub, cand string, status int) bool {
	rc, fi, err := s.store.OpenFile(sub, cand)
	if err != nil {
		return false
	}
	defer rc.Close()
	// No MIME sniffing: a public site must not be able to get a file interpreted
	// as the wrong type (XSS vector). The octet-stream fallback downloads, not renders.
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if status == http.StatusOK {
		if fi.ETag != "" {
			w.Header().Set("ETag", fi.ETag)
		}
		http.ServeContent(w, r, fi.Name, fi.ModTime, rc)
		return true
	}
	if ct := mime.TypeByExtension(path.Ext(cand)); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	w.WriteHeader(status)
	io.Copy(w, rc)
	return true
}
