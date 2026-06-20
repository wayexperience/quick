package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPickLang(t *testing.T) {
	cases := []struct {
		name   string
		url    string
		accept string
		want   lang
	}{
		{"default english", "/", "", langEN},
		{"italian browser", "/", "it-IT,it;q=0.9,en;q=0.8", langIT},
		{"english browser", "/", "en-US,en;q=0.9", langEN},
		{"q-order picks italian", "/", "en;q=0.3, it;q=0.9", langIT},
		{"unsupported falls back", "/", "fr-FR,fr;q=0.9", langEN},
		{"query overrides header", "/?lang=it", "en-US,en;q=0.9", langIT},
		{"query english over italian header", "/?lang=en", "it-IT,it", langEN},
		{"invalid query ignored", "/?lang=fr", "it-IT", langIT},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, c.url, nil)
			if c.accept != "" {
				r.Header.Set("Accept-Language", c.accept)
			}
			if got := pickLang(r); got != c.want {
				t.Errorf("pickLang = %q, want %q", got, c.want)
			}
		})
	}
}

func TestSSOPageLocalized(t *testing.T) {
	s := &server{baseDomain: "example.test"}

	en := httptest.NewRecorder()
	s.renderSSOPage(en, httptest.NewRequest(http.MethodGet, "/", nil), "foo.example.test")
	if b := en.Body.String(); !strings.Contains(b, `<html lang="en"`) || !strings.Contains(b, "Sign-in required") {
		t.Errorf("english SSO page not localized:\n%s", b)
	}
	if v := en.Header().Get("Vary"); !strings.Contains(v, "Accept-Language") {
		t.Errorf("missing Vary: Accept-Language, got %q", v)
	}

	it := httptest.NewRecorder()
	ri := httptest.NewRequest(http.MethodGet, "/", nil)
	ri.Header.Set("Accept-Language", "it-IT,it;q=0.9")
	s.renderSSOPage(it, ri, "foo.example.test")
	if b := it.Body.String(); !strings.Contains(b, `<html lang="it"`) || !strings.Contains(b, "Accesso richiesto") {
		t.Errorf("italian SSO page not localized:\n%s", b)
	}
}

func TestLandingLocalized(t *testing.T) {
	s := &server{baseDomain: "quick.example.test"}

	en := httptest.NewRecorder()
	s.handleApexRoot(en, httptest.NewRequest(http.MethodGet, "/", nil))
	b := en.Body.String()
	if !strings.Contains(b, `<html lang="en"`) || !strings.Contains(b, "Get started") {
		t.Errorf("english landing not localized:\n%s", b)
	}
	if !strings.Contains(b, "curl -fsSL https://quick.example.test/install.sh | sh") {
		t.Errorf("landing missing the install one-liner:\n%s", b)
	}
	if !strings.Contains(b, `href="/dashboard"`) {
		t.Errorf("landing missing the dashboard link:\n%s", b)
	}
	if !strings.Contains(b, `class="brand"`) || !strings.Contains(b, `/img/logo.png`) {
		t.Errorf("landing missing the brand logo:\n%s", b)
	}
	if !strings.Contains(b, "Publish a folder, get a URL.") {
		t.Errorf("landing missing the headline:\n%s", b)
	}

	it := httptest.NewRecorder()
	ri := httptest.NewRequest(http.MethodGet, "/?lang=it", nil)
	s.handleApexRoot(it, ri)
	if b := it.Body.String(); !strings.Contains(b, `<html lang="it"`) || !strings.Contains(b, "Come iniziare") {
		t.Errorf("italian landing not localized:\n%s", b)
	}

	// "/" is the landing; anything else under the apex root is 404.
	nf := httptest.NewRecorder()
	s.handleApexRoot(nf, httptest.NewRequest(http.MethodGet, "/nope", nil))
	if nf.Code != http.StatusNotFound {
		t.Errorf("/nope: code %d, want 404", nf.Code)
	}
}

func TestCodeFormLocalized(t *testing.T) {
	en := httptest.NewRecorder()
	renderCodeForm(en, langEN, "foo.example.test", "https://foo.example.test/", true)
	if b := en.Body.String(); !strings.Contains(b, `<html lang="en"`) || !strings.Contains(b, "Wrong code, try again.") {
		t.Errorf("english code form not localized:\n%s", b)
	}

	it := httptest.NewRecorder()
	renderCodeForm(it, langIT, "foo.example.test", "https://foo.example.test/", true)
	if b := it.Body.String(); !strings.Contains(b, `<html lang="it"`) || !strings.Contains(b, "Codice errato, riprova.") {
		t.Errorf("italian code form not localized:\n%s", b)
	}
}
