// L'apex (== BASE_DOMAIN) è il "cuore": API di controllo, auth e una pagina che
// per i guest mostra il branding + accesso SSO, e per chi è autenticato la
// dashboard dei siti. I sottodomini sono solo siti.
package main

import (
	"encoding/json"
	"html/template"
	"net/http"
	"net/url"
	"sort"
	"time"

	"github.com/zupolgec/quick/internal/quick"
)

// renderSSOPage mostra la pagina di accesso (niente redirect secco): branding +
// bottone per il login SSO. Il bottone porta al sign_in sull'apex, che dopo il
// login Google riporta a rd. TODO multi-provider: oggi solo Google.
func (s *server) renderSSOPage(w http.ResponseWriter, r *http.Request, host string) {
	rd := "https://" + host + r.URL.RequestURI()
	signIn := "https://" + s.baseDomain + "/oauth2/sign_in?rd=" + url.QueryEscape(rd)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = ssoPage.Execute(w, map[string]any{"Host": host, "SignIn": signIn})
}

// handleApexRoot serve la dashboard se autenticato, altrimenti la pagina SSO.
func (s *server) handleApexRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r) // l'apex non serve siti
		return
	}
	email, ok := s.checkSSO(r)
	if !ok {
		s.renderSSOPage(w, r, s.baseDomain)
		return
	}
	s.renderDashboard(w, email)
}

// handleSites elenca i siti (auth con ID token). Usato dalla CLI; la dashboard
// usa lo stesso elenco ma renderizzato lato server.
func (s *server) handleSites(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if _, err := s.authenticate(r); err != nil {
		http.Error(w, "auth: "+err.Error(), http.StatusUnauthorized)
		return
	}
	names, err := s.store.ListSites()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	out := make([]quick.SiteInfo, 0, len(names))
	for _, n := range names {
		out = append(out, s.siteInfo(n, s.meta.load(n)))
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(quick.SitesResponse{Sites: out})
}

func (s *server) siteInfo(name string, p policy) quick.SiteInfo {
	access := p.Access
	if access == "" {
		access = "sso"
	}
	return quick.SiteInfo{
		Site: name, URL: "https://" + name + "." + s.baseDomain,
		Access: access, Locked: p.Locked, Owner: p.Owner,
		CreatedBy: p.CreatedBy, CreatedAt: p.CreatedAt,
		UpdatedBy: p.UpdatedBy, UpdatedAt: p.UpdatedAt,
	}
}

// dashRow è una riga della dashboard.
type dashRow struct {
	Name    string
	URL     string
	Access  string // sso | public | code
	Locked  bool
	Updated string // "chi · quando", già formattato
	at      string // RFC3339 grezzo, per l'ordinamento
	Mine    bool
}

func (s *server) renderDashboard(w http.ResponseWriter, email string) {
	names, _ := s.store.ListSites()
	rows := make([]dashRow, 0, len(names))
	for _, n := range names {
		p := s.meta.load(n)
		access := p.Access
		if access == "" {
			access = "sso"
		}
		rows = append(rows, dashRow{
			Name:    n,
			URL:     "https://" + n + "." + s.baseDomain,
			Access:  access,
			Locked:  p.Locked,
			Updated: updatedLabel(p),
			at:      p.UpdatedAt,
			Mine:    p.CreatedBy != "" && p.CreatedBy == email,
		})
	}
	// Più recenti in cima (chi non ha timestamp finisce in fondo, per nome).
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].at != rows[j].at {
			return rows[i].at > rows[j].at
		}
		return rows[i].Name < rows[j].Name
	})

	var mine, all []dashRow
	for _, r := range rows {
		if r.Mine {
			mine = append(mine, r)
		}
		all = append(all, r)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = dashboardPage.Execute(w, map[string]any{
		"Email": email, "Mine": mine, "All": all, "Base": s.baseDomain,
	})
}

// updatedLabel formatta "aggiornato da X il Y" in modo compatto (— se ignoto).
func updatedLabel(p policy) string {
	if p.UpdatedBy == "" && p.UpdatedAt == "" {
		return "—"
	}
	when := p.UpdatedAt
	if t, err := time.Parse(time.RFC3339, p.UpdatedAt); err == nil {
		when = t.Format("2006-01-02 15:04")
	}
	if p.UpdatedBy == "" {
		return when
	}
	return p.UpdatedBy + " · " + when
}

var basePageCSS = `:root{--bg:#f4f4f5;--card:#fff;--fg:#18181b;--muted:#71717a;--border:#e4e4e7;--ring:#18181b;--btn:#18181b;--btn-fg:#fafafa;--accent:#2563eb}
@media (prefers-color-scheme:dark){:root{--bg:#09090b;--card:#161618;--fg:#fafafa;--muted:#a1a1aa;--border:#27272a;--ring:#fafafa;--btn:#fafafa;--btn-fg:#18181b;--accent:#60a5fa}}
*{box-sizing:border-box}
body{margin:0;min-height:100vh;font:16px/1.5 -apple-system,BlinkMacSystemFont,"Segoe UI",system-ui,sans-serif;background:var(--bg);color:var(--fg);-webkit-font-smoothing:antialiased}
a{color:var(--accent);text-decoration:none}a:hover{text-decoration:underline}`

var ssoPage = template.Must(template.New("sso").Parse(`<!doctype html>
<html lang="it"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>quick — accesso</title>
<style>` + basePageCSS + `
body{display:grid;place-items:center;padding:1.5rem}
.card{width:min(360px,100%);background:var(--card);border:1px solid var(--border);border-radius:18px;padding:2rem 1.75rem;text-align:center;box-shadow:0 1px 2px rgba(0,0,0,.04),0 14px 40px rgba(0,0,0,.10)}
.logo{font-weight:700;font-size:1.4rem;letter-spacing:-.02em;margin-bottom:.4rem}
h1{font-size:1.1rem;font-weight:600;margin:0 0 .3rem}
p{margin:0 0 1.5rem;color:var(--muted);font-size:.9rem;overflow-wrap:anywhere}
.btn{display:block;width:100%;padding:.74rem;font-size:.95rem;font-weight:600;border-radius:11px;background:var(--btn);color:var(--btn-fg)}
.btn:hover{opacity:.88;text-decoration:none}
</style></head><body>
<div class="card">
  <div class="logo">quick</div>
  <h1>Accesso richiesto</h1>
  <p>Accedi con l'account aziendale per continuare su <b>{{.Host}}</b>.</p>
  <a class="btn" href="{{.SignIn}}">Accedi con Google</a>
</div></body></html>`))

var dashboardPage = template.Must(template.New("dash").Funcs(template.FuncMap{
	"badge": func(access string) string {
		switch access {
		case "public":
			return "pubblico"
		case "code":
			return "codice"
		default:
			return "SSO"
		}
	},
}).Parse(`<!doctype html>
<html lang="it"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>quick — i tuoi siti</title>
<style>` + basePageCSS + `
.wrap{max-width:780px;margin:0 auto;padding:2rem 1.25rem}
header{display:flex;justify-content:space-between;align-items:baseline;margin-bottom:1.5rem}
.logo{font-weight:700;font-size:1.3rem;letter-spacing:-.02em}
.who{color:var(--muted);font-size:.85rem}
h2{font-size:.8rem;text-transform:uppercase;letter-spacing:.04em;color:var(--muted);margin:1.8rem 0 .6rem}
.site{display:flex;justify-content:space-between;align-items:center;gap:1rem;padding:.7rem .9rem;background:var(--card);border:1px solid var(--border);border-radius:12px;margin-bottom:.5rem}
.site .name{font-weight:600}
.site .meta{color:var(--muted);font-size:.8rem}
.tag{font-size:.7rem;border:1px solid var(--border);border-radius:999px;padding:.1rem .5rem;color:var(--muted);margin-left:.4rem}
.empty{color:var(--muted);font-size:.9rem}
.help{margin-top:2.5rem;padding-top:1.5rem;border-top:1px solid var(--border);color:var(--muted);font-size:.85rem}
code{background:var(--bg);border:1px solid var(--border);border-radius:6px;padding:.1rem .35rem;font-size:.85em}
</style></head><body>
<div class="wrap">
  <header><div class="logo">quick</div><div class="who">{{.Email}}</div></header>

  <h2>I tuoi siti</h2>
  {{if .Mine}}{{range .Mine}}
  <div class="site">
    <div><span class="name"><a href="{{.URL}}">{{.Name}}</a></span><span class="tag">{{badge .Access}}</span>{{if .Locked}}<span class="tag">bloccato</span>{{end}}</div>
    <div class="meta">{{.Updated}}</div>
  </div>{{end}}{{else}}<p class="empty">Non hai ancora pubblicato siti. Usa <code>quick deploy</code>.</p>{{end}}

  <h2>Tutti i siti</h2>
  {{if .All}}{{range .All}}
  <div class="site">
    <div><span class="name"><a href="{{.URL}}">{{.Name}}</a></span><span class="tag">{{badge .Access}}</span>{{if .Locked}}<span class="tag">bloccato</span>{{end}}</div>
    <div class="meta">{{.Updated}}</div>
  </div>{{end}}{{else}}<p class="empty">Nessun sito pubblicato.</p>{{end}}

  <div class="help">
    Pubblica una cartella: <code>quick deploy &lt;nome&gt; ./cartella</code> → <code>&lt;nome&gt;.{{.Base}}</code>.
    Installa la CLI con <code>go install github.com/zupolgec/quick/cmd/quick@latest</code>.
  </div>
</div></body></html>`))
