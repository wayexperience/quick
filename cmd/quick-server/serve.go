package main

import (
	"io"
	"mime"
	"net/http"
	"path"
	"strings"
)

// Tipi MIME che Go non conosce di default: con nosniff attivo un tipo sbagliato
// significa "scaricato" invece che servito correttamente.
func init() {
	mime.AddExtensionType(".wasm", "application/wasm")
	mime.AddExtensionType(".webmanifest", "application/manifest+json")
}

// handleSite è il catch-all: risolve il sito dall'host, applica la policy
// (pubblico / codice / SSO) e, se l'accesso è concesso, serve il file.
// È il punto d'estensione futuro: oggi l'unico esito è lo static-serve, domani
// il manifest del sito potrà dirottare certi path verso un backend.
func (s *server) handleSite(w http.ResponseWriter, r *http.Request) {
	host := fwdHost(r)
	sub := subOf(host, s.baseDomain)
	if sub == "" {
		http.NotFound(w, r)
		return
	}
	p, err := s.meta.load(sub)
	if err != nil {
		http.Error(w, "sito temporaneamente non disponibile", http.StatusServiceUnavailable)
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
	default: // SSO: se non autenticato mostro la pagina di accesso (niente redirect secco)
		if _, ok := s.checkSSO(r); !ok {
			s.renderSSOPage(w, r, host)
			return
		}
		s.serveSite(w, r, sub)
	}
}

// serveSite risolve un path verso un file con le convenzioni degli static host:
//
//	/percorso            -> file esatto
//	/about               -> /about.html        (URL puliti, niente estensione)
//	/about  o  /about/   -> /about/index.html  (indice di cartella)
//
// Un path che non corrisponde a nulla NON ricasca sulla home: se esiste 200.html
// fa da app shell SPA (200), altrimenti è un 404 (con 404.html del sito se c'è).
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
	// SPA opt-in: 200.html serve da app shell per le rotte che non sono file.
	if s.serveFile(w, r, sub, "/200.html", http.StatusOK) {
		return
	}
	// 404 reale: pagina del sito se c'è, altrimenti corpo vuoto (default del browser).
	if !s.serveFile(w, r, sub, "/404.html", http.StatusNotFound) {
		w.WriteHeader(http.StatusNotFound)
	}
}

// serveFile serve cand se esiste e restituisce true. Con status 200 usa
// http.ServeContent (range, etag, content-type). Con uno status d'errore scrive
// quello status col content-type dedotto dall'estensione (ServeContent forzerebbe 200).
func (s *server) serveFile(w http.ResponseWriter, r *http.Request, sub, cand string, status int) bool {
	rc, fi, err := s.store.OpenFile(sub, cand)
	if err != nil {
		return false
	}
	defer rc.Close()
	// Niente MIME-sniffing: un sito pubblico non deve poter far interpretare un
	// file col tipo sbagliato (vettore XSS). Fallback octet-stream = scarica, non rende.
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
