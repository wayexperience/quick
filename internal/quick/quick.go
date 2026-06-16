// Package quick è il contratto condiviso tra la CLI (cmd/quick) e il server
// (cmd/quick-server): validazione dei nomi, modi di accesso, DTO delle API e
// qualche helper. Tenere qui ciò che le due parti DEVONO concordare, così non
// può divergere.
package quick

import (
	"os"
	"regexp"
	"strings"
)

// NameRe valida il nome di un sito (= sottodominio): minuscole, cifre, trattino.
var NameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

// ValidName indica se name è un nome di sito accettabile.
func ValidName(name string) bool { return NameRe.MatchString(name) }

// Modi di accesso a un sito. Stringa vuota = SSO (default aziendale).
const (
	AccessSSO    = ""
	AccessPublic = "public"
	AccessCode   = "code"
)

// DefaultReservedSubs sono i sottodomini che non sono siti servibili.
var DefaultReservedSubs = []string{"deploy", "auth", "api"}

// PolicyRequest è il body di PATCH/POST /api/site/<name>/policy.
type PolicyRequest struct {
	Access *string `json:"access,omitempty"` // "sso" | "public" | "code"
	Code   *string `json:"code,omitempty"`   // richiesto per access=code
	Locked *bool   `json:"locked,omitempty"`
}

// PolicyResponse è la risposta degli endpoint di policy (POST muta, GET legge).
type PolicyResponse struct {
	Site   string `json:"site"`
	Access string `json:"access"`
	Locked bool   `json:"locked"`
	Owner  string `json:"owner"`
	Exists bool   `json:"exists"` // il sito ha contenuti o metadata
}

// DeleteResponse è la risposta di DELETE /api/site/<name>.
type DeleteResponse struct {
	Site    string `json:"site"`
	Deleted bool   `json:"deleted"`
}

// DeployResponse è la risposta di /api/deploy.
type DeployResponse struct {
	Site string `json:"site"`
	URL  string `json:"url"`
	By   string `json:"by"`
}

// ConfigResponse è la risposta pubblica di /api/config: tutto ciò che serve
// alla CLI per auto-configurarsi senza valori hardcoded.
type ConfigResponse struct {
	OAuthClientID string `json:"oauth_client_id"`
	// OAuthClientSecret è valorizzato solo se il server riusa un client OAuth di
	// tipo "Web" per la CLI (che richiede il secret nello scambio token). Per un
	// client "Desktop" resta vuoto e la CLI usa solo PKCE.
	OAuthClientSecret string `json:"oauth_client_secret,omitempty"`
	HostedDomain      string `json:"hosted_domain"`
	BaseDomain        string `json:"base_domain"`
}

// Env restituisce la variabile d'ambiente k, o def se vuota/assente.
func Env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// SplitList spezza una lista separata da virgole, ignorando spazi e vuoti.
func SplitList(s string) []string {
	var out []string
	for p := range strings.SplitSeq(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
