// The apex (== BASE_DOMAIN) is the control plane: control API, auth, and a page
// that shows branding + SSO sign-in for guests and the sites dashboard for
// authenticated users. Subdomains are just sites.
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

// renderSSOPage shows the sign-in page (no bare redirect): branding + SSO
// button. The button goes to sign_in on the apex, which after Google login
// returns to rd. TODO multi-provider: Google only for now.
func (s *server) renderSSOPage(w http.ResponseWriter, r *http.Request, host string) {
	rd := "https://" + host + r.URL.RequestURI()
	signIn := "https://" + s.baseDomain + "/oauth2/sign_in?rd=" + url.QueryEscape(rd)
	l := pickLang(r)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Add("Vary", "Accept-Language")
	_ = ssoPage.Execute(w, map[string]any{
		"Host": host, "SignIn": signIn, "Lang": string(l), "T": textsFor(l),
	})
}

// handleApexRoot serves the public landing (install + usage) at "/". The
// dashboard lives at /dashboard.
func (s *server) handleApexRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r) // the apex serves no sites
		return
	}
	l := pickLang(r)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Add("Vary", "Accept-Language")
	_ = landingPage.Execute(w, map[string]any{
		"Lang": string(l), "T": textsFor(l), "Base": s.baseDomain,
		"Install": "curl -fsSL https://" + s.baseDomain + "/install.sh | sh",
		"Login":   "quick login --server https://" + s.baseDomain,
		"Deploy":  "quick deploy <name> ./folder",
	})
}

func (s *server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	email, ok := s.checkSSO(r)
	if !ok {
		s.renderSSOPage(w, r, s.baseDomain)
		return
	}
	s.renderDashboard(w, pickLang(r), email)
}

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
		p, _ := s.meta.load(n) // listing is best-effort; empty policy on error (display only)
		out = append(out, s.siteInfo(n, p))
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

type dashRow struct {
	Name    string
	URL     string
	Badge   string // localized access label
	Locked  bool
	Updated string // "who · when", preformatted
	at      string // raw RFC3339, for sorting
	Mine    bool
}

func (s *server) renderDashboard(w http.ResponseWriter, l lang, email string) {
	t := textsFor(l)
	names, _ := s.store.ListSites()
	rows := make([]dashRow, 0, len(names))
	for _, n := range names {
		p, _ := s.meta.load(n) // dashboard is best-effort; empty policy on error (display only)
		access := p.Access
		if access == "" {
			access = "sso"
		}
		rows = append(rows, dashRow{
			Name:    n,
			URL:     "https://" + n + "." + s.baseDomain,
			Badge:   badgeLabel(access, t),
			Locked:  p.Locked,
			Updated: updatedLabel(p),
			at:      p.UpdatedAt,
			Mine:    p.CreatedBy != "" && p.CreatedBy == email,
		})
	}
	// Most recent first; rows without a timestamp sink to the bottom, by name.
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
	w.Header().Add("Vary", "Accept-Language")
	_ = dashboardPage.Execute(w, map[string]any{
		"Email": email, "Mine": mine, "All": all, "Base": s.baseDomain,
		"Lang": string(l), "T": t,
	})
}

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

var ssoPage = template.Must(template.New("sso").Parse(`<!doctype html>
<html lang="{{.Lang}}"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.T.SSOTitle}}</title>` + brandHead + `
<style>` + brandCSS + `
body{display:grid;place-items:center;padding:1.5rem}
.card{width:min(360px,100%);background:var(--card);border:1px solid var(--border);border-radius:18px;padding:2.25rem 1.75rem;text-align:center;box-shadow:0 1px 2px rgba(13,24,50,.05),0 18px 48px rgba(13,24,50,.10)}
.card .brand{font-size:1.5rem;margin-bottom:.9rem}
h1{font-size:1.15rem;font-weight:700;margin:0 0 .35rem;color:var(--ink)}
p{margin:0 0 1.6rem;color:var(--muted);font-size:.9rem;overflow-wrap:anywhere}
.btn{width:100%;padding:.8rem;font-size:.95rem}
</style></head><body>
<div class="card">
  ` + brandWordmark + `
  <h1>{{.T.SSOHeading}}</h1>
  <p>{{.T.SSOIntro}} <b>{{.Host}}</b>.</p>
  <a class="btn" href="{{.SignIn}}">{{.T.SSOButton}}</a>
</div></body></html>`))

var dashboardPage = template.Must(template.New("dash").Parse(`<!doctype html>
<html lang="{{.Lang}}"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.T.DashTitle}}</title>` + brandHead + `
<style>` + brandCSS + `
.wrap{max-width:780px;margin:0 auto;padding:2rem 1.25rem}
header{display:flex;justify-content:space-between;align-items:center;margin-bottom:1.5rem}
.who{color:var(--muted);font-size:.85rem}
h2{font-size:.8rem;text-transform:uppercase;letter-spacing:.04em;color:var(--muted);margin:1.8rem 0 .6rem}
.site{display:flex;justify-content:space-between;align-items:center;gap:1rem;padding:.7rem .9rem;background:var(--card);border:1px solid var(--border);border-radius:12px;margin-bottom:.5rem}
.site .name{font-weight:600;font-family:var(--font-head)}
.site .meta{color:var(--muted);font-size:.8rem}
.tag{font-size:.7rem;border:1px solid var(--border);border-radius:999px;padding:.1rem .5rem;color:var(--muted);margin-left:.4rem}
.empty{color:var(--muted);font-size:.9rem}
.help{margin-top:2.5rem;padding-top:1.5rem;border-top:1px solid var(--border);color:var(--muted);font-size:.85rem}
code{background:var(--bg);border:1px solid var(--border);border-radius:6px;padding:.1rem .35rem;font-size:.85em;font-family:ui-monospace,SFMono-Regular,Menlo,monospace}
</style></head><body>
<div class="wrap">
  <header>` + brandLink + `<div class="who">{{.Email}}</div></header>

  <h2>{{.T.DashYourSites}}</h2>
  {{if .Mine}}{{range .Mine}}
  <div class="site">
    <div><span class="name"><a href="{{.URL}}">{{.Name}}</a></span><span class="tag">{{.Badge}}</span>{{if .Locked}}<span class="tag">{{$.T.DashLocked}}</span>{{end}}</div>
    <div class="meta">{{.Updated}}</div>
  </div>{{end}}{{else}}<p class="empty">{{.T.DashEmptyMine}} <code>quick deploy</code>.</p>{{end}}

  <h2>{{.T.DashAllSites}}</h2>
  {{if .All}}{{range .All}}
  <div class="site">
    <div><span class="name"><a href="{{.URL}}">{{.Name}}</a></span><span class="tag">{{.Badge}}</span>{{if .Locked}}<span class="tag">{{$.T.DashLocked}}</span>{{end}}</div>
    <div class="meta">{{.Updated}}</div>
  </div>{{end}}{{else}}<p class="empty">{{.T.DashEmptyAll}}</p>{{end}}

  <div class="help">
    {{.T.DashHelpPublish}} <code>quick deploy &lt;name&gt; ./folder</code> → <code>&lt;name&gt;.{{.Base}}</code>.
    {{.T.DashHelpInstall}} <code>go install github.com/zupolgec/quick/cmd/quick@latest</code>.
  </div>
</div></body></html>`))

var landingPage = template.Must(template.New("landing").Parse(`<!doctype html>
<html lang="{{.Lang}}"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.T.LandingTitle}}</title>` + brandHead + `
<style>` + brandCSS + `
.wrap{max-width:640px;margin:0 auto;padding:2rem 1.25rem}
header{display:flex;justify-content:space-between;align-items:center;margin-bottom:3.5rem}
.nav{font-family:var(--font-head);font-size:.9rem;font-weight:700;border:1px solid var(--border);border-radius:10px;padding:.45rem .9rem;color:var(--ink)}
.nav:hover{text-decoration:none;border-color:var(--brand);color:var(--brand)}
h1{font-size:2.4rem;font-weight:800;letter-spacing:-.035em;line-height:1.08;margin:0 0 .65rem;color:var(--ink);max-width:14ch;text-wrap:balance}
.tagline{color:var(--muted);font-size:1.05rem;margin:0 0 3rem;max-width:46ch;text-wrap:pretty}
h2{font-size:.8rem;text-transform:uppercase;letter-spacing:.04em;color:var(--muted);margin:0 0 1.2rem}
.step{margin-bottom:1.4rem}
.step .label{font-family:var(--font-head);font-size:.92rem;font-weight:700;margin-bottom:.5rem;display:flex;gap:.6rem;align-items:center}
.step .n{flex:none;display:grid;place-items:center;width:1.45rem;height:1.45rem;border-radius:999px;background:var(--btn);color:var(--btn-fg);font-size:.76rem;font-weight:700}
.cmd{display:flex;align-items:stretch;gap:.5rem}
.cmd code{flex:1;background:var(--card);border:1px solid var(--border);border-radius:11px;padding:.72rem .85rem;font-size:.88rem;overflow-x:auto;white-space:nowrap;color:var(--fg);font-family:ui-monospace,SFMono-Regular,Menlo,monospace}
.cmd .copy{flex:none;font-family:var(--font-head);border:1px solid var(--border);border-radius:11px;background:var(--card);color:var(--ink);font-size:.82rem;font-weight:700;padding:0 .95rem;cursor:pointer;transition:border-color .15s,color .15s}
.cmd .copy:hover{border-color:var(--brand);color:var(--brand)}
.result{color:var(--muted);font-size:.82rem;margin:.55rem 0 0}
.result code{background:var(--bg);border:1px solid var(--border);border-radius:6px;padding:.1rem .35rem;font-size:.85em;font-family:ui-monospace,SFMono-Regular,Menlo,monospace}
</style></head><body>
<div class="wrap">
  <header>
    ` + brandLink + `
    <a class="nav" href="/dashboard">{{.T.NavDashboard}}</a>
  </header>

  <h1>{{.T.LandingHeadline}}</h1>
  <p class="tagline">{{.T.LandingTagline}}</p>

  <h2>{{.T.LandingGetStarted}}</h2>

  <div class="step">
    <div class="label"><span class="n">1</span>{{.T.LandingInstall}}</div>
    <div class="cmd"><code>{{.Install}}</code><button class="copy" type="button" data-done="{{.T.Copied}}" onclick="cp(this)">{{.T.Copy}}</button></div>
  </div>

  <div class="step">
    <div class="label"><span class="n">2</span>{{.T.LandingLogin}}</div>
    <div class="cmd"><code>{{.Login}}</code><button class="copy" type="button" data-done="{{.T.Copied}}" onclick="cp(this)">{{.T.Copy}}</button></div>
  </div>

  <div class="step">
    <div class="label"><span class="n">3</span>{{.T.LandingDeploy}}</div>
    <div class="cmd"><code>{{.Deploy}}</code><button class="copy" type="button" data-done="{{.T.Copied}}" onclick="cp(this)">{{.T.Copy}}</button></div>
    <p class="result">→ <code>&lt;name&gt;.{{.Base}}</code></p>
  </div>
</div>
<script>
function cp(b){navigator.clipboard.writeText(b.previousElementSibling.textContent).then(function(){var t=b.textContent;b.textContent=b.dataset.done;b.disabled=true;setTimeout(function(){b.textContent=t;b.disabled=false},1200)})}
</script>
</body></html>`))
