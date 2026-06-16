package main

import (
	"archive/tar"
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/wayexperience/quick/internal/storage"
)

// putSite carica un sito nello storage a partire da una mappa path->contenuto.
func putSite(t *testing.T, st storage.Backend, site string, files map[string]string) {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for name, content := range files {
		hdr := &tar.Header{Name: name, Mode: 0o644, Size: int64(len(content)), Typeflag: tar.TypeReg}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	tw.Close()
	if err := st.PutSite(site, tar.NewReader(&buf)); err != nil {
		t.Fatal(err)
	}
}

func newTestServer(t *testing.T) *server {
	t.Helper()
	st, err := storage.New(storage.Config{Kind: "local", SitesDir: t.TempDir(), MetaDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	return &server{store: st}
}

func get(t *testing.T, s *server, sub, path string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	s.serveSite(w, httptest.NewRequest(http.MethodGet, path, nil), sub)
	return w
}

func TestServeNotFoundDoesNotFallBackToIndex(t *testing.T) {
	s := newTestServer(t)
	putSite(t, s.store, "demo", map[string]string{
		"index.html":       "<h1>home</h1>",
		"about/index.html": "<h1>about</h1>",
	})

	// File esistente: 200.
	if w := get(t, s, "demo", "/index.html"); w.Code != http.StatusOK {
		t.Errorf("index.html: code %d, voluto 200", w.Code)
	}
	// URL pulito -> about/index.html.
	if w := get(t, s, "demo", "/about"); w.Code != http.StatusOK || w.Body.String() != "<h1>about</h1>" {
		t.Errorf("/about: code %d body %q", w.Code, w.Body.String())
	}
	// Asset mancante: 404, NON la home con 200.
	w := get(t, s, "demo", "/missing.css")
	if w.Code != http.StatusNotFound {
		t.Errorf("/missing.css: code %d, voluto 404", w.Code)
	}
	if w.Body.String() == "<h1>home</h1>" {
		t.Errorf("/missing.css ha restituito la home invece di un 404")
	}
}

func TestServeCleanURLs(t *testing.T) {
	s := newTestServer(t)
	putSite(t, s.store, "demo", map[string]string{
		"about.html":      "<h1>flat</h1>",
		"blog/index.html": "<h1>folder</h1>",
	})

	// /about -> about.html
	if w := get(t, s, "demo", "/about"); w.Code != http.StatusOK || w.Body.String() != "<h1>flat</h1>" {
		t.Errorf("/about: code %d body %q", w.Code, w.Body.String())
	}
	// /blog -> blog/index.html
	if w := get(t, s, "demo", "/blog"); w.Code != http.StatusOK || w.Body.String() != "<h1>folder</h1>" {
		t.Errorf("/blog: code %d body %q", w.Code, w.Body.String())
	}
}

func TestServeSPAFallback(t *testing.T) {
	s := newTestServer(t)
	putSite(t, s.store, "demo", map[string]string{
		"index.html": "home",
		"200.html":   "<div id=app></div>",
	})

	// Rotta SPA inesistente: 200.html con status 200.
	w := get(t, s, "demo", "/dashboard/settings")
	if w.Code != http.StatusOK || w.Body.String() != "<div id=app></div>" {
		t.Errorf("rotta SPA: code %d body %q, voluto 200 + app shell", w.Code, w.Body.String())
	}
}

func TestServeNotFoundUsesCustom404(t *testing.T) {
	s := newTestServer(t)
	putSite(t, s.store, "demo", map[string]string{
		"index.html": "home",
		"404.html":   "<h1>persa</h1>",
	})

	w := get(t, s, "demo", "/nope")
	if w.Code != http.StatusNotFound {
		t.Errorf("code %d, voluto 404", w.Code)
	}
	if w.Body.String() != "<h1>persa</h1>" {
		t.Errorf("body %q, voluto la 404.html", w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Errorf("content-type %q", ct)
	}
}
