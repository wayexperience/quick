// Policy per-sito: lock, accesso pubblico, accesso con codice. Persistite via
// storage.Backend (file locale o S3) con una piccola cache TTL davanti, perché
// la load è sul hot-path di ogni richiesta servita.
package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/zupolgec/quick/internal/quick"
	"github.com/zupolgec/quick/internal/storage"
)

// codeAccessTTL: durata del cookie di accesso a un sito con codice.
const codeAccessTTL = 7 * 24 * time.Hour

// Azioni di scrittura soggette al controllo di ownership.
const (
	actDeploy = "deploy"
	actDelete = "delete"
	actPolicy = "policy"
)

// nowStamp è il formato dei timestamp salvati nei metadata.
func nowStamp() string { return time.Now().UTC().Format(time.RFC3339) }

// canWrite decide se email può eseguire action sul sito con metadata p, secondo
// la modalità di ownership (QUICK_OWNERSHIP) e l'eventuale lock esplicito. Il
// lock ha sempre la precedenza; poi:
//
//	free   (default) chiunque può tutto
//	shared           chiunque può deploy; solo il creatore elimina/cambia visibilità
//	owned            solo il creatore può tutto
func (s *server) canWrite(p policy, email, action string) (bool, string) {
	if p.Locked && p.Owner != "" && p.Owner != email {
		return false, "sito bloccato da " + p.Owner
	}
	creatorOnly := false
	switch s.ownership {
	case "owned":
		creatorOnly = true
	case "shared":
		creatorOnly = action != actDeploy
	}
	if creatorOnly && p.CreatedBy != "" && p.CreatedBy != email {
		return false, "sito di " + p.CreatedBy + " (modalità " + s.ownership + ")"
	}
	return true, ""
}

// policy è lo stato per-sito persistito (JSON) nello storage: visibilità, lock e
// tracciamento di chi/quando ha creato e aggiornato il sito.
type policy struct {
	CreatedBy string `json:"created_by,omitempty"` // email del creatore (primo deploy)
	CreatedAt string `json:"created_at,omitempty"` // RFC3339
	UpdatedBy string `json:"updated_by,omitempty"` // email dell'ultimo deploy
	UpdatedAt string `json:"updated_at,omitempty"` // RFC3339
	Owner     string `json:"owner,omitempty"`      // owner del lock (set da lock)
	Locked    bool   `json:"locked,omitempty"`     // solo l'owner può deploy/policy
	Access    string `json:"access,omitempty"`     // "" = SSO | "public" | "code"
	CodeHash  string `json:"code_hash,omitempty"`  // HMAC del codice (mai in chiaro)
}

type cachedPolicy struct {
	p  policy
	at time.Time
}

type metaStore struct {
	be     storage.Backend
	secret []byte
	ttl    time.Duration
	mu     sync.Mutex
	cache  map[string]cachedPolicy
}

func newMetaStore(be storage.Backend, secret []byte, ttl time.Duration) *metaStore {
	return &metaStore{be: be, secret: secret, ttl: ttl, cache: map[string]cachedPolicy{}}
}

// load restituisce la policy del sito. Distingue tre casi: metadata assenti
// (policy vuota legittima, si cacha), metadata presenti e validi (si cachano),
// errore di storage o JSON corrotto (errore propagato e NON cachato). I caller
// sul path di scrittura/servizio devono trattare l'errore come fail-closed:
// una policy vuota per errore farebbe sparire lock e proprietà (ownership bypass).
func (m *metaStore) load(site string) (policy, error) {
	if !quick.ValidName(site) {
		return policy{}, nil
	}
	m.mu.Lock()
	if c, ok := m.cache[site]; ok && time.Since(c.at) < m.ttl {
		m.mu.Unlock()
		return c.p, nil
	}
	m.mu.Unlock()

	b, ok, err := m.be.GetMeta(site)
	if err != nil {
		return policy{}, err
	}
	var p policy
	if ok {
		if err := json.Unmarshal(b, &p); err != nil {
			return policy{}, fmt.Errorf("metadata di %q illeggibili: %w", site, err)
		}
	}
	m.mu.Lock()
	m.cache[site] = cachedPolicy{p, time.Now()}
	m.mu.Unlock()
	return p, nil
}

func (m *metaStore) save(site string, p policy) error {
	if !quick.ValidName(site) {
		return errors.New("nome sito non valido")
	}
	b, _ := json.MarshalIndent(p, "", "  ")
	if err := m.be.PutMeta(site, b); err != nil {
		return err
	}
	m.mu.Lock()
	m.cache[site] = cachedPolicy{p, time.Now()}
	m.mu.Unlock()
	return nil
}

// forget invalida la cache per un sito (es. dopo l'eliminazione).
func (m *metaStore) forget(site string) {
	m.mu.Lock()
	delete(m.cache, site)
	m.mu.Unlock()
}

func (m *metaStore) hashCode(code string) string {
	mac := hmac.New(sha256.New, m.secret)
	mac.Write([]byte(code))
	return hex.EncodeToString(mac.Sum(nil))
}

func (m *metaStore) checkCode(p policy, code string) bool {
	if p.CodeHash == "" || code == "" {
		return false
	}
	return hmac.Equal([]byte(p.CodeHash), []byte(m.hashCode(code)))
}

// signAccess produce il valore del cookie di accesso: "<scadenza>.<firma>".
func (m *metaStore) signAccess(sub string, exp int64) string {
	mac := hmac.New(sha256.New, m.secret)
	mac.Write([]byte(sub + "|" + strconv.FormatInt(exp, 10)))
	return strconv.FormatInt(exp, 10) + "." + hex.EncodeToString(mac.Sum(nil))
}

func (m *metaStore) validAccessCookie(sub, val string) bool {
	expStr, _, ok := strings.Cut(val, ".")
	if !ok {
		return false
	}
	exp, err := strconv.ParseInt(expStr, 10, 64)
	if err != nil || time.Now().Unix() > exp {
		return false
	}
	return hmac.Equal([]byte(m.signAccess(sub, exp)), []byte(val))
}

// subOf estrae il sottodominio di primo livello da un host del dominio base.
// "foo.quick.example.com" + "quick.example.com" -> "foo".
func subOf(host, base string) string {
	host, _, _ = strings.Cut(host, ":")
	sub, ok := strings.CutSuffix(host, "."+base)
	if !ok || strings.Contains(sub, ".") {
		return ""
	}
	return sub
}

func fwdHost(r *http.Request) string {
	if h := r.Header.Get("X-Forwarded-Host"); h != "" {
		return h
	}
	return r.Host
}

// checkSSO interroga oauth2-proxy /oauth2/auth (202 = sessione valida) rigirando
// il cookie della richiesta.
func (s *server) checkSSO(r *http.Request) (string, bool) {
	if s.noAuth {
		return "dev@" + def(s.domain, "example.com"), true
	}
	req, err := http.NewRequest(http.MethodGet, s.oauth2URL+"/oauth2/auth", nil)
	if err != nil {
		return "", false
	}
	if c := r.Header.Get("Cookie"); c != "" {
		req.Header.Set("Cookie", c)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", false
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	if resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusOK {
		return "", false
	}
	return resp.Header.Get("X-Auth-Request-Email"), true
}

// redirect manda il browser a path (sign_in o pagina codice) con ?rd= all'URL corrente.
func (s *server) redirect(w http.ResponseWriter, r *http.Request, host, path string) {
	rd := "https://" + host + r.URL.RequestURI()
	http.Redirect(w, r, path+"?rd="+url.QueryEscape(rd), http.StatusFound)
}

// handleCodePage serve e processa la pagina di inserimento codice (/__quick/code).
func (s *server) handleCodePage(w http.ResponseWriter, r *http.Request) {
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
	if p.Access != "code" {
		http.Redirect(w, r, "https://"+host+"/", http.StatusFound)
		return
	}
	rd := r.FormValue("rd")
	if !safeRedirect(rd, host) {
		rd = "https://" + host + "/"
	}
	if r.Method == http.MethodPost {
		if !s.meta.checkCode(p, r.FormValue("code")) {
			renderCodeForm(w, host, rd, true)
			return
		}
		exp := time.Now().Add(codeAccessTTL).Unix()
		http.SetCookie(w, &http.Cookie{
			Name:     "quick_access_" + sub,
			Value:    s.meta.signAccess(sub, exp),
			Path:     "/",
			Secure:   true,
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
			Expires:  time.Unix(exp, 0),
		})
		http.Redirect(w, r, rd, http.StatusFound)
		return
	}
	renderCodeForm(w, host, rd, false)
}

// safeRedirect ammette solo URL https dello stesso host o path relativi.
func safeRedirect(rd, host string) bool {
	if rd == "" {
		return false
	}
	u, err := url.Parse(rd)
	if err != nil {
		return false
	}
	if u.Host == "" {
		return strings.HasPrefix(rd, "/")
	}
	return u.Scheme == "https" && u.Host == host
}

// handleSiteAPI instrada /api/site/<name>[/policy]:
//   - GET|POST|PATCH .../policy  -> handlePolicy (leggi/muta la policy)
//   - DELETE /api/site/<name>    -> handleDelete (elimina il sito)
func (s *server) handleSiteAPI(w http.ResponseWriter, r *http.Request) {
	name, action, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/api/site/"), "/")
	if !quick.ValidName(name) {
		http.NotFound(w, r)
		return
	}
	switch {
	case action == "policy":
		s.handlePolicy(w, r, name)
	case action == "rollback" && r.Method == http.MethodPost:
		s.handleRollback(w, r, name)
	case action == "" && r.Method == http.MethodDelete:
		s.handleDelete(w, r, name)
	default:
		http.NotFound(w, r)
	}
}

// handleRollback riporta il sito alla versione precedente. Auth con ID token
// Google; stessa regola di scrittura del deploy.
func (s *server) handleRollback(w http.ResponseWriter, r *http.Request, name string) {
	email, err := s.authenticate(r)
	if err != nil {
		http.Error(w, "auth: "+err.Error(), http.StatusUnauthorized)
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
	ok, err := s.store.Rollback(name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !ok {
		http.Error(w, "nessuna versione precedente da ripristinare", http.StatusNotFound)
		return
	}
	cur.UpdatedBy, cur.UpdatedAt = email, nowStamp()
	if err := s.meta.save(name, cur); err != nil {
		log.Printf("ATTENZIONE: rollback %q applicato ma salvataggio metadata fallito: %v", name, err)
		http.Error(w, "rollback applicato ma salvataggio stato fallito: "+err.Error(), http.StatusInternalServerError)
		return
	}
	log.Printf("rollback %q da %s", name, email)
	_ = json.NewEncoder(w).Encode(quick.RollbackResponse{
		Site: name, RolledBack: true, URL: "https://" + name + "." + s.baseDomain,
	})
}

// handlePolicy: GET legge la policy corrente, POST/PATCH la muta. Auth con ID
// token Google; per le modifiche, se il sito è bloccato, solo l'owner.
func (s *server) handlePolicy(w http.ResponseWriter, r *http.Request, name string) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost && r.Method != http.MethodPatch {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	email, err := s.authenticate(r)
	if err != nil {
		http.Error(w, "auth: "+err.Error(), http.StatusUnauthorized)
		return
	}
	unlock := s.locks.lock(name)
	defer unlock()
	cur, err := s.meta.load(name)
	if err != nil {
		http.Error(w, "stato sito non leggibile: "+err.Error(), http.StatusServiceUnavailable)
		return
	}
	if r.Method == http.MethodGet {
		s.writePolicy(w, name, cur)
		return
	}
	if ok, reason := s.canWrite(cur, email, actPolicy); !ok {
		http.Error(w, reason, http.StatusForbidden)
		return
	}
	var req quick.PolicyRequest
	if json.NewDecoder(r.Body).Decode(&req) != nil {
		http.Error(w, "json non valido", http.StatusBadRequest)
		return
	}
	// Il lock si può cambiare solo se sei il creatore: impedisce di "rubare" un
	// sito altrui bloccandolo a proprio nome (rilevante in modalità free).
	if req.Locked != nil && cur.CreatedBy != "" && cur.CreatedBy != email {
		http.Error(w, "solo il creatore ("+cur.CreatedBy+") può bloccare il sito", http.StatusForbidden)
		return
	}
	if req.Access != nil {
		switch *req.Access {
		case quick.AccessSSO, "sso":
			cur.Access, cur.CodeHash = "", ""
		case quick.AccessPublic:
			cur.Access, cur.CodeHash = quick.AccessPublic, ""
		case quick.AccessCode:
			if req.Code == nil || *req.Code == "" {
				http.Error(w, "access=code richiede un codice", http.StatusBadRequest)
				return
			}
			cur.Access, cur.CodeHash = quick.AccessCode, s.meta.hashCode(*req.Code)
		default:
			http.Error(w, "access non valido", http.StatusBadRequest)
			return
		}
	}
	if req.Locked != nil {
		if cur.Locked = *req.Locked; cur.Locked {
			cur.Owner = email
		} else {
			cur.Owner = ""
		}
	}
	if err := s.meta.save(name, cur); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.writePolicy(w, name, cur)
}

// writePolicy serializza lo stato corrente del sito (access normalizzato + esistenza).
func (s *server) writePolicy(w http.ResponseWriter, name string, p policy) {
	access := p.Access
	if access == "" {
		access = "sso"
	}
	exists, _ := s.store.SiteExists(name)
	_ = json.NewEncoder(w).Encode(quick.PolicyResponse{
		Site: name, Access: access, Locked: p.Locked, Owner: p.Owner, Exists: exists,
		CreatedBy: p.CreatedBy, CreatedAt: p.CreatedAt, UpdatedBy: p.UpdatedBy, UpdatedAt: p.UpdatedAt,
	})
}

// handleDelete: DELETE /api/site/<name>. Elimina contenuti e metadata del sito.
// Auth con ID token Google; se il sito è bloccato, solo l'owner può eliminarlo.
func (s *server) handleDelete(w http.ResponseWriter, r *http.Request, name string) {
	email, err := s.authenticate(r)
	if err != nil {
		http.Error(w, "auth: "+err.Error(), http.StatusUnauthorized)
		return
	}
	unlock := s.locks.lock(name)
	defer unlock()
	cur, err := s.meta.load(name)
	if err != nil {
		http.Error(w, "stato sito non leggibile: "+err.Error(), http.StatusServiceUnavailable)
		return
	}
	if ok, reason := s.canWrite(cur, email, actDelete); !ok {
		http.Error(w, reason, http.StatusForbidden)
		return
	}
	existed, err := s.store.DeleteSite(name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.meta.forget(name)
	if !existed {
		http.Error(w, "sito non trovato", http.StatusNotFound)
		return
	}
	log.Printf("delete %q da %s", name, email)
	_ = json.NewEncoder(w).Encode(quick.DeleteResponse{Site: name, Deleted: true})
}

var codeForm = template.Must(template.New("code").Parse(`<!doctype html>
<html lang="it"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Accesso protetto</title>
<style>
  :root{
    --bg:#f4f4f5; --card:#fff; --fg:#18181b; --muted:#71717a;
    --border:#e4e4e7; --ring:#18181b; --btn:#18181b; --btn-fg:#fafafa;
    --err:#dc2626; --err-bg:#fef2f2;
  }
  @media (prefers-color-scheme:dark){
    :root{
      --bg:#09090b; --card:#161618; --fg:#fafafa; --muted:#a1a1aa;
      --border:#27272a; --ring:#fafafa; --btn:#fafafa; --btn-fg:#18181b;
      --err:#f87171; --err-bg:#2a1416;
    }
  }
  *{box-sizing:border-box}
  body{margin:0;min-height:100vh;display:grid;place-items:center;padding:1.5rem;
    font:16px/1.5 -apple-system,BlinkMacSystemFont,"Segoe UI",system-ui,sans-serif;
    background:var(--bg);color:var(--fg);-webkit-font-smoothing:antialiased}
  .card{width:min(360px,100%);background:var(--card);border:1px solid var(--border);
    border-radius:18px;padding:2rem 1.75rem;
    box-shadow:0 1px 2px rgba(0,0,0,.04),0 14px 40px rgba(0,0,0,.10)}
  .badge{width:46px;height:46px;border-radius:13px;display:grid;place-items:center;
    background:var(--bg);border:1px solid var(--border);margin-bottom:1.2rem}
  .badge svg{width:22px;height:22px;stroke:var(--fg);fill:none;
    stroke-width:2;stroke-linecap:round;stroke-linejoin:round}
  h1{font-size:1.15rem;font-weight:600;margin:0 0 .35rem;letter-spacing:-.01em}
  p{margin:0 0 1.4rem;color:var(--muted);font-size:.9rem}
  p b{color:var(--fg);font-weight:600;overflow-wrap:anywhere}
  label{display:block;font-size:.8rem;color:var(--muted);margin:0 0 .4rem}
  input[type=password]{width:100%;padding:.72rem .85rem;font-size:1rem;color:var(--fg);
    background:transparent;border:1px solid var(--border);border-radius:11px;outline:none;
    transition:border-color .15s,box-shadow .15s}
  input[type=password]:focus{border-color:var(--ring);
    box-shadow:0 0 0 3px color-mix(in srgb,var(--ring) 16%,transparent)}
  button{width:100%;margin-top:1.05rem;padding:.74rem;font-size:.95rem;font-weight:600;
    border:0;border-radius:11px;background:var(--btn);color:var(--btn-fg);cursor:pointer;
    transition:opacity .15s}
  button:hover{opacity:.88}
  .err{margin-top:.9rem;padding:.55rem .7rem;border-radius:9px;font-size:.83rem;
    color:var(--err);background:var(--err-bg)}
</style></head><body>
<form class="card" method="post" action="/__quick/code">
  <div class="badge" aria-hidden="true">
    <svg viewBox="0 0 24 24"><rect x="3" y="11" width="18" height="11" rx="2"/><path d="M7 11V7a5 5 0 0 1 10 0v4"/></svg>
  </div>
  <h1>Sito protetto</h1>
  <p>Inserisci il codice di accesso per <b>{{.Host}}</b>.</p>
  <input type="hidden" name="rd" value="{{.RD}}">
  <label for="code">Codice</label>
  <input id="code" type="password" name="code" placeholder="••••••••" autofocus required autocomplete="off">
  <button type="submit">Entra</button>
  {{if .Error}}<div class="err">Codice errato, riprova.</div>{{end}}
</form></body></html>`))

func renderCodeForm(w http.ResponseWriter, host, rd string, isErr bool) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if isErr {
		w.WriteHeader(http.StatusUnauthorized)
	}
	_ = codeForm.Execute(w, map[string]any{"Host": host, "RD": rd, "Error": isErr})
}
