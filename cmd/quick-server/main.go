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
	"sync"
	"time"

	"github.com/zupolgec/quick/internal/quick"
	"github.com/zupolgec/quick/internal/storage"
)

const maxUpload = 200 << 20 // 200 MiB per deploy

// httpClient: chiamate verso Google (tokeninfo) e oauth2-proxy (checkSSO) con
// timeout, così una dipendenza appesa non blocca la goroutine della richiesta.
var httpClient = &http.Client{Timeout: 10 * time.Second}

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
	locks        keyedMutex     // serializza le scritture per-sito (single instance)
}

// keyedMutex serializza le operazioni per chiave (qui: nome sito), così il ciclo
// load→modifica→save di deploy/policy/delete/rollback è atomico e due richieste
// sullo stesso sito non si sovrascrivono a vicenda. Zero value pronto all'uso.
type keyedMutex struct {
	mu sync.Mutex
	m  map[string]*sync.Mutex
}

// lock acquisisce il mutex della chiave e restituisce la funzione di rilascio.
func (k *keyedMutex) lock(key string) func() {
	k.mu.Lock()
	if k.m == nil {
		k.m = map[string]*sync.Mutex{}
	}
	mtx := k.m[key]
	if mtx == nil {
		mtx = &sync.Mutex{}
		k.m[key] = mtx
	}
	k.mu.Unlock()
	mtx.Lock()
	return mtx.Unlock
}

func main() {
	store, err := storage.New(storageConfigFromEnv())
	if err != nil {
		log.Fatal(err)
	}
	metaSecret := os.Getenv("QUICK_META_SECRET")
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
	if err := s.validateConfig(metaSecret); err != nil {
		log.Fatal(err)
	}
	s.meta = newMetaStore(store, []byte(metaSecret), 5*time.Second)
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

// validateConfig applica il fail-closed all'avvio: fuori dalla modalità di
// sviluppo (QUICK_DEV_NOAUTH=1) le env critiche per la sicurezza devono essere
// valorizzate, altrimenti l'auth fallirebbe in apertura (cookie dei siti protetti
// forgiabili, qualsiasi account Google ammesso al deploy, audience non verificata).
func (s *server) validateConfig(metaSecret string) error {
	if s.noAuth {
		log.Print("⚠ QUICK_DEV_NOAUTH=1: autenticazione disattivata, solo per sviluppo locale")
		return nil
	}
	var missing []string
	if metaSecret == "" {
		missing = append(missing, "QUICK_META_SECRET (firma cookie e codici dei siti protetti)")
	}
	if strings.TrimSpace(s.domain) == "" {
		missing = append(missing, `QUICK_ALLOWED_DOMAINS (dominio email ammesso; usa "*" per qualsiasi account Google)`)
	}
	if s.clientID == "" {
		missing = append(missing, "QUICK_OAUTH_CLIENT_ID (audience dell'ID token di deploy)")
	}
	if len(missing) > 0 {
		return fmt.Errorf("configurazione insicura, env mancanti:\n  - %s\n(imposta QUICK_DEV_NOAUTH=1 solo per sviluppo locale)", strings.Join(missing, "\n  - "))
	}
	if strings.TrimSpace(s.domain) == "*" {
		log.Print(`⚠ QUICK_ALLOWED_DOMAINS="*": qualsiasi account Google può fare deploy`)
	}
	return nil
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
	unlock := s.locks.lock(name)
	defer unlock()
	cur, err := s.meta.load(name)
	if err != nil {
		http.Error(w, "stato sito non leggibile: "+err.Error(), http.StatusServiceUnavailable)
		return
	}
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
	if err := s.meta.save(name, cur); err != nil {
		log.Printf("ATTENZIONE: deploy %q applicato ma salvataggio metadata fallito: %v", name, err)
		http.Error(w, "deploy applicato ma salvataggio stato fallito: "+err.Error(), http.StatusInternalServerError)
		return
	}
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
	resp, err := httpClient.Get("https://oauth2.googleapis.com/tokeninfo?id_token=" + tok)
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
