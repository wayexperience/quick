package main

import (
	"archive/tar"
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/zupolgec/quick/internal/storage"
)

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

	if w := get(t, s, "demo", "/index.html"); w.Code != http.StatusOK {
		t.Errorf("index.html: code %d, want 200", w.Code)
	}
	if w := get(t, s, "demo", "/about"); w.Code != http.StatusOK || w.Body.String() != "<h1>about</h1>" {
		t.Errorf("/about: code %d body %q", w.Code, w.Body.String())
	}
	// Missing asset must 404, NOT fall back to the home page with 200.
	w := get(t, s, "demo", "/missing.css")
	if w.Code != http.StatusNotFound {
		t.Errorf("/missing.css: code %d, want 404", w.Code)
	}
	if w.Body.String() == "<h1>home</h1>" {
		t.Errorf("/missing.css returned the home page instead of a 404")
	}
}

// After a rollback the restored files are older than the version the browser
// cached, so a plain If-Modified-Since revalidation would 304 and keep showing
// the rolled-back-from version. The identity ETag must make the conditional
// request return the restored content instead.
func TestServeConditionalCacheReflectsRollback(t *testing.T) {
	s := newTestServer(t)
	// Different lengths so the identity ETag changes regardless of mtime resolution.
	putSite(t, s.store, "demo", map[string]string{"index.html": "VERSION ONE"})
	putSite(t, s.store, "demo", map[string]string{"index.html": "VERSION TWO is longer"})

	// Capture the v2 validators, as a browser would cache them.
	w2 := get(t, s, "demo", "/index.html")
	etag2, lastMod2 := w2.Header().Get("ETag"), w2.Header().Get("Last-Modified")
	if etag2 == "" {
		t.Fatal("missing ETag: conditional caching would fall back to the date alone")
	}

	if ok, err := s.store.Rollback("demo"); err != nil || !ok {
		t.Fatalf("rollback: ok=%v err=%v", ok, err)
	}

	// A browser that cached v2 revalidates with v2's validators. After the
	// rollback it must get 200 + the restored v1, not a 304.
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/index.html", nil)
	r.Header.Set("If-None-Match", etag2)
	r.Header.Set("If-Modified-Since", lastMod2)
	s.serveSite(w, r, "demo")
	if w.Code != http.StatusOK {
		t.Fatalf("after rollback: code %d, want 200 (304 = browser shows the wrong version)", w.Code)
	}
	if w.Body.String() != "VERSION ONE" {
		t.Errorf("body %q, want the restored v1", w.Body.String())
	}

	// Caching still works for unchanged content: the current ETag yields 304.
	cur := get(t, s, "demo", "/index.html").Header().Get("ETag")
	w4 := httptest.NewRecorder()
	r4 := httptest.NewRequest(http.MethodGet, "/index.html", nil)
	r4.Header.Set("If-None-Match", cur)
	s.serveSite(w4, r4, "demo")
	if w4.Code != http.StatusNotModified {
		t.Errorf("unchanged content: code %d, want 304", w4.Code)
	}
}

func TestServeCleanURLs(t *testing.T) {
	s := newTestServer(t)
	putSite(t, s.store, "demo", map[string]string{
		"about.html":      "<h1>flat</h1>",
		"blog/index.html": "<h1>folder</h1>",
	})

	if w := get(t, s, "demo", "/about"); w.Code != http.StatusOK || w.Body.String() != "<h1>flat</h1>" {
		t.Errorf("/about: code %d body %q", w.Code, w.Body.String())
	}
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

	w := get(t, s, "demo", "/dashboard/settings")
	if w.Code != http.StatusOK || w.Body.String() != "<div id=app></div>" {
		t.Errorf("SPA route: code %d body %q, want 200 + app shell", w.Code, w.Body.String())
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
		t.Errorf("code %d, want 404", w.Code)
	}
	if w.Body.String() != "<h1>persa</h1>" {
		t.Errorf("body %q, want the 404.html", w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Errorf("content-type %q", ct)
	}
}
