// quick-server è l'unico front di *.<BASE_DOMAIN>: serve i siti (dallo storage),
// applica la policy per-sito (SSO / pubblico / codice), riceve i deploy e proxa
// l'SSO verso oauth2-proxy. Tutta la config arriva da variabili d'ambiente:
// nessun valore legato a un dominio specifico è hardcoded.
package main

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"os"
	"strings"
	"time"

	"github.com/zupolgec/quick/internal/quick"
	"github.com/zupolgec/quick/internal/storage"
)

const maxUpload = 200 << 20 // 200 MiB per deploy

type server struct {
	store        storage.Backend
	meta         *metaStore
	baseDomain   string // dominio dei siti, es. quick.example.com
	domain       string // hosted domain Google ammesso (email) + esposto in /api/config
	clientID     string // audience dell'ID token + esposto in /api/config
	clientSecret string // opzionale: secret del client CLI (solo se è un client Web), servito via /api/config
	oauth2URL    string // base URL interno di oauth2-proxy
	ownership    string // free | shared | owned (QUICK_OWNERSHIP)
	oauthProxy   *httputil.ReverseProxy
	apexMux      *http.ServeMux // control plane sull'apex
	noAuth       bool           // solo sviluppo locale
}

func main() {
	store, err := storage.New(storageConfigFromEnv())
	if err != nil {
		log.Fatal(err)
	}
	s := &server{
		store:        store,
		baseDomain:   os.Getenv("QUICK_BASE_DOMAIN"),
		domain:       os.Getenv("QUICK_ALLOWED_DOMAINS"),
		clientID:     os.Getenv("QUICK_OAUTH_CLIENT_ID"),
		clientSecret: os.Getenv("QUICK_OAUTH_CLIENT_SECRET"),
		oauth2URL:    quick.Env("QUICK_OAUTH2_URL", "http://oauth2-proxy:4180"),
		ownership:    parseOwnership(os.Getenv("QUICK_OWNERSHIP")),
		noAuth:       os.Getenv("QUICK_DEV_NOAUTH") == "1",
	}
	s.meta = newMetaStore(store, []byte(quick.Env("QUICK_META_SECRET", "dev-insecure-secret")), 5*time.Second)
	if err := s.setupOAuthProxy(); err != nil {
		log.Fatal(err)
	}
	s.apexMux = s.buildApexMux()

	addr := quick.Env("QUICK_ADDR", ":8080")
	log.Printf("quick-server su %s (base=%s, storage=%s, ownership=%s, noauth=%v)", addr, s.baseDomain, quick.Env("QUICK_STORAGE", "local"), s.ownership, s.noAuth)
	log.Fatal(http.ListenAndServe(addr, http.HandlerFunc(s.route)))
}

// route smista per host: l'apex (== baseDomain) è il control plane (API, auth,
// dashboard); ogni sottodominio è solo un sito.
func (s *server) route(w http.ResponseWriter, r *http.Request) {
	host := hostNoPort(fwdHost(r))
	if host == s.baseDomain {
		s.apexMux.ServeHTTP(w, r)
		return
	}
	sub := subOf(host, s.baseDomain)
	if sub == "" {
		http.NotFound(w, r)
		return
	}
	if r.URL.Path == "/__quick/code" {
		s.handleCodePage(w, r)
		return
	}
	s.handleSite(w, r) // gate per-sito + serve
}

// buildApexMux costruisce le rotte servite SOLO sull'apex.
func (s *server) buildApexMux() *http.ServeMux {
	m := http.NewServeMux()
	m.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) { fmt.Fprintln(w, "ok") })
	m.HandleFunc("/api/config", s.handleConfig)
	m.HandleFunc("/api/deploy", s.handleDeploy)
	m.HandleFunc("/api/sites", s.handleSites)
	m.HandleFunc("/api/site/", s.handleSiteAPI)
	m.HandleFunc("/oauth2/", s.handleOAuth2) // sign_in + callback (cookie su .baseDomain)
	m.HandleFunc("/install.sh", s.handleInstallSh)
	m.HandleFunc("/install.ps1", s.handleInstallPs1)
	m.HandleFunc("/", s.handleApexRoot) // dashboard (loggato) o pagina SSO (guest)
	return m
}

// parseOwnership normalizza QUICK_OWNERSHIP; default "free".
func parseOwnership(v string) string {
	switch v {
	case "shared", "owned":
		return v
	default:
		return "free"
	}
}

// hostNoPort toglie l'eventuale ":porta" da un host.
func hostNoPort(h string) string {
	host, _, _ := strings.Cut(h, ":")
	return host
}

func (s *server) handleDeploy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	email, err := s.authenticate(r)
	if err != nil {
		http.Error(w, "auth: "+err.Error(), http.StatusUnauthorized)
		return
	}
	name := r.URL.Query().Get("name")
	if !quick.ValidName(name) {
		http.Error(w, "nome sito non valido (usa a-z, 0-9, trattino)", http.StatusBadRequest)
		return
	}
	cur := s.meta.load(name)
	if ok, reason := s.canWrite(cur, email, actDeploy); !ok {
		http.Error(w, reason, http.StatusForbidden)
		return
	}
	body := http.MaxBytesReader(w, r.Body, maxUpload)
	gz, err := gzip.NewReader(body)
	if err != nil {
		http.Error(w, "gzip: "+err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.store.PutSite(name, tar.NewReader(gz)); err != nil {
		http.Error(w, "deploy: "+err.Error(), http.StatusBadRequest)
		return
	}
	// Timbra creatore (al primo deploy) e ultimo aggiornamento.
	now := nowStamp()
	if cur.CreatedBy == "" {
		cur.CreatedBy, cur.CreatedAt = email, now
	}
	cur.UpdatedBy, cur.UpdatedAt = email, now
	_ = s.meta.save(name, cur)
	log.Printf("deploy %q da %s", name, email)
	_ = json.NewEncoder(w).Encode(quick.DeployResponse{
		Site: name,
		URL:  "https://" + name + "." + s.baseDomain,
		By:   email,
	})
}

// authenticate valida l'ID token Google (Authorization: Bearer) e restituisce
// l'email, verificando hosted domain e (se impostata) audience.
func (s *server) authenticate(r *http.Request) (string, error) {
	if s.noAuth {
		return "dev@" + def(s.domain, "example.com"), nil
	}
	tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if tok == "" {
		return "", errors.New("token mancante")
	}
	resp, err := http.Get("https://oauth2.googleapis.com/tokeninfo?id_token=" + tok)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", errors.New("token non valido")
	}
	var info struct {
		Email string `json:"email"`
		Hd    string `json:"hd"`
		Aud   string `json:"aud"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return "", err
	}
	if !s.domainAllowed(info.Hd) {
		return "", fmt.Errorf("dominio %q non ammesso", info.Hd)
	}
	if s.clientID != "" && info.Aud != s.clientID {
		return "", errors.New("audience del token non corrisponde")
	}
	return info.Email, nil
}

// domainAllowed verifica l'hosted domain Google contro QUICK_ALLOWED_DOMAINS, che
// può essere vuoto o "*" (qualsiasi account), un singolo dominio, o una lista
// comma-separated. Coerente con OAUTH2_PROXY_EMAIL_DOMAINS.
func (s *server) domainAllowed(hd string) bool {
	d := strings.TrimSpace(s.domain)
	if d == "" || d == "*" {
		return true
	}
	for part := range strings.SplitSeq(d, ",") {
		if strings.TrimSpace(part) == hd {
			return true
		}
	}
	return false
}

func def(v, d string) string {
	if v != "" {
		return v
	}
	return d
}
