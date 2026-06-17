// quick delete <sito>: elimina definitivamente un sito (contenuti + metadata).
// L'operazione è irreversibile, quindi chiede conferma; se il sito non è dietro
// l'SSO (pubblico o protetto da codice) richiede di ridigitarne il nome. Se il
// sito è bloccato, il server consente l'eliminazione solo all'owner.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/zupolgec/quick/internal/quick"
)

func deleteCmd(args []string) {
	var name string
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		name, args = args[0], args[1:]
	}

	fs := flag.NewFlagSet("delete", flag.ExitOnError)
	server := fs.String("server", "", "URL del server (o QUICK_SERVER)")
	token := fs.String("token", os.Getenv("QUICK_TOKEN"), "ID token Google (default: login salvato)")
	fs.Parse(args)
	if name == "" && fs.NArg() > 0 {
		name = fs.Arg(0) // posizionale messo dopo i flag
	}

	sf := loadSiteFile(".")
	if name == "" && sf != nil {
		name = sf.Name
	}
	if name == "" {
		fatal(errors.New("manca il nome del sito (o esegui in una cartella con .quick)"))
	}
	if !confirmSiteMismatch(sf, name, "eliminare") {
		return
	}

	srv := *server
	if srv == "" && sf != nil {
		srv = sf.Server
	}
	cfg, err := resolveConfig(srv)
	fatal(err)

	tok := *token
	if tok == "" {
		if tok, err = idToken(cfg); err != nil {
			fatal(err)
		}
	}

	// Stato attuale: serve a sapere se esiste, se è bloccato e con che accesso
	// (per decidere il livello di conferma).
	pol := fetchPolicy(cfg, name, tok)
	if !pol.Exists {
		fatal(fmt.Errorf("sito %q non trovato", name))
	}

	siteURL := "https://" + name + "." + cfg.BaseDomain
	fmt.Fprintf(os.Stderr, "\n  ⚠️  stai per ELIMINARE definitivamente il sito\n      %s  (%s)\n", name, siteURL)
	if pol.Locked {
		fmt.Fprintf(os.Stderr, "      bloccato da %s\n", pol.Owner)
	}
	fmt.Fprintf(os.Stderr, "      i file vengono rimossi e l'operazione non è reversibile.\n\n")

	if pol.Access == quick.AccessPublic || pol.Access == quick.AccessCode {
		label := "pubblico"
		if pol.Access == quick.AccessCode {
			label = "protetto da codice"
		}
		fmt.Fprintf(os.Stderr, "  Il sito è %s. Per confermare, ridigita il suo nome (%s): ", label, name)
		if readLine() != name {
			fatal(errors.New("nome non corrispondente: eliminazione annullata"))
		}
	} else {
		fmt.Fprint(os.Stderr, "  Confermi l'eliminazione? [s/N]: ")
		if !yesNo(readLine()) {
			fmt.Fprintln(os.Stderr, "annullato")
			return
		}
	}

	callDelete(cfg, name, tok)
	fmt.Printf("✓ %s eliminato\n", name)
}

// fetchPolicy legge lo stato corrente del sito via GET /api/site/<name>/policy.
func fetchPolicy(cfg *cliConfig, name, tok string) quick.PolicyResponse {
	endpoint := cfg.Server + "/api/site/" + url.PathEscape(name) + "/policy"
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	fatal(err)
	req.Header.Set("Authorization", "Bearer "+tok)

	resp, err := httpClient.Do(req)
	fatal(err)
	defer resp.Body.Close()
	rb, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "impossibile leggere lo stato (%d): %s\n", resp.StatusCode, strings.TrimSpace(string(rb)))
		os.Exit(1)
	}
	var p quick.PolicyResponse
	json.Unmarshal(rb, &p)
	return p
}

// callDelete esegue DELETE /api/site/<name>.
func callDelete(cfg *cliConfig, name, tok string) {
	endpoint := cfg.Server + "/api/site/" + url.PathEscape(name)
	req, err := http.NewRequest(http.MethodDelete, endpoint, nil)
	fatal(err)
	req.Header.Set("Authorization", "Bearer "+tok)

	resp, err := httpClient.Do(req)
	fatal(err)
	defer resp.Body.Close()
	rb, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "eliminazione fallita (%d): %s\n", resp.StatusCode, strings.TrimSpace(string(rb)))
		os.Exit(1)
	}
}
