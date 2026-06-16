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
	"html/template"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/wayexperience/quick/internal/quick"
	"github.com/wayexperience/quick/internal/storage"
)

// codeAccessTTL: durata del cookie di accesso a un sito con codice.
const codeAccessTTL = 7 * 24 * time.Hour

// policy è lo stato per-sito persistito (JSON) nello storage.
type policy struct {
	Owner    string `json:"owner,omitempty"`     // owner (set da lock)
	Locked   bool   `json:"locked,omitempty"`    // solo l'owner può deploy/policy
	Access   string `json:"access,omitempty"`    // "" = SSO | "public" | "code"
	CodeHash string `json:"code_hash,omitempty"` // HMAC del codice (mai in chiaro)
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

func (m *metaStore) load(site string) policy {
	if !quick.ValidName(site) {
		return policy{}
	}
	m.mu.Lock()
	if c, ok := m.cache[site]; ok && time.Since(c.at) < m.ttl {
		m.mu.Unlock()
		return c.p
	}
	m.mu.Unlock()

	var p policy
	if b, ok, err := m.be.GetMeta(site); ok && err == nil {
		_ = json.Unmarshal(b, &p)
	}
	m.mu.Lock()
	m.cache[site] = cachedPolicy{p, time.Now()}
	m.mu.Unlock()
	return p
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
	req, err := http.NewRequest(http.MethodGet, s.oauth2URL+"/oauth2/auth", nil)
	if err != nil {
		return "", false
	}
	if c := r.Header.Get("Cookie"); c != "" {
		req.Header.Set("Cookie", c)
	}
	resp, err := http.DefaultClient.Do(req)
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
	p := s.meta.load(sub)
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

// handlePolicy: PATCH/POST /api/site/<name>/policy. Auth con ID token Google; se
// il sito è bloccato, solo l'owner.
func (s *server) handlePolicy(w http.ResponseWriter, r *http.Request) {
	name, action, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/api/site/"), "/")
	if action != "policy" || !quick.ValidName(name) {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPost && r.Method != http.MethodPatch {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	email, err := s.authenticate(r)
	if err != nil {
		http.Error(w, "auth: "+err.Error(), http.StatusUnauthorized)
		return
	}
	cur := s.meta.load(name)
	if cur.Locked && cur.Owner != "" && cur.Owner != email {
		http.Error(w, "sito bloccato da "+cur.Owner, http.StatusForbidden)
		return
	}
	var req quick.PolicyRequest
	if json.NewDecoder(r.Body).Decode(&req) != nil {
		http.Error(w, "json non valido", http.StatusBadRequest)
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
	access := cur.Access
	if access == "" {
		access = "sso"
	}
	_ = json.NewEncoder(w).Encode(quick.PolicyResponse{
		Site: name, Access: access, Locked: cur.Locked, Owner: cur.Owner,
	})
}

var codeForm = template.Must(template.New("code").Parse(`<!doctype html>
<html lang="it"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Accesso protetto</title>
<style>
  body{font:16px/1.5 system-ui,sans-serif;background:#f5f5f7;color:#1d1d1f;
    display:grid;place-items:center;min-height:100vh;margin:0}
  form{background:#fff;padding:2rem;border-radius:14px;box-shadow:0 1px 4px rgba(0,0,0,.1);
    width:min(360px,90vw);box-sizing:border-box}
  h1{font-size:1.1rem;margin:0 0 .25rem}
  p{margin:0 0 1.25rem;color:#6e6e73;font-size:.9rem}
  input{width:100%;padding:.7rem;font-size:1rem;border:1px solid #d2d2d7;border-radius:9px;
    box-sizing:border-box}
  button{width:100%;margin-top:.75rem;padding:.7rem;font-size:1rem;border:0;border-radius:9px;
    background:#1d1d1f;color:#fff;cursor:pointer}
  .err{color:#c0392b;font-size:.85rem;margin-top:.5rem}
</style></head><body>
<form method="post" action="/__quick/code">
  <h1>Sito protetto</h1>
  <p>Inserisci il codice di accesso per {{.Host}}.</p>
  <input type="hidden" name="rd" value="{{.RD}}">
  <input type="password" name="code" placeholder="Codice" autofocus required>
  <button type="submit">Entra</button>
  {{if .Error}}<div class="err">Codice errato.</div>{{end}}
</form></body></html>`))

func renderCodeForm(w http.ResponseWriter, host, rd string, isErr bool) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if isErr {
		w.WriteHeader(http.StatusUnauthorized)
	}
	_ = codeForm.Execute(w, map[string]any{"Host": host, "RD": rd, "Error": isErr})
}
