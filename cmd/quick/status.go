// quick status (anche `quick` da solo): riassume server, autenticazione, sito
// della cartella corrente, visibilità sul server e cosa salirebbe col deploy.
// quick ignore: materializza un .quickignore modificabile nella cartella.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"time"

	"github.com/wayexperience/quick/internal/quick"
)

func statusCmd(args []string) {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	server := fs.String("server", "", "URL del server (o QUICK_SERVER)")
	fs.Parse(args)

	sf := loadSiteFile(".")
	// Stessa risoluzione del deploy: la cartella pubblicata è quella ricordata
	// nel .quick (o la corrente).
	dir := "."
	if sf != nil && sf.Dir != "" {
		dir = sf.Dir
	}
	name := ""
	if sf != nil {
		name = sf.Name
	}
	if name == "" {
		abs, _ := filepath.Abs(dir)
		name = filepath.Base(abs)
	}

	srv := *server
	if srv == "" && sf != nil {
		srv = sf.Server
	}
	cfg, err := resolveConfig(srv)
	fatal(err)

	fmt.Printf("Server:  %s\n", cfg.Server)
	tok, logged := silentToken(cfg)
	if logged {
		fmt.Println("Accesso: autenticato")
	} else {
		fmt.Println("Accesso: non autenticato (esegui `quick login`)")
	}

	fmt.Printf("Sito:    %s  → https://%s.%s\n", name, name, cfg.BaseDomain)

	// Visibilità reale dal server (serve un token; con sito inesistente lo dice).
	if logged && quick.ValidName(name) {
		if pol, ok := getPolicy(cfg, name, tok); ok {
			if !pol.Exists {
				fmt.Println("Stato:   non ancora pubblicato")
			} else {
				fmt.Printf("Stato:   %s\n", describeAccess(pol.Access))
				if pol.CreatedBy != "" {
					fmt.Printf("Creato:  %s%s\n", pol.CreatedBy, fmtWhen(pol.CreatedAt))
				}
				if pol.UpdatedBy != "" {
					fmt.Printf("Ultimo:  %s%s\n", pol.UpdatedBy, fmtWhen(pol.UpdatedAt))
				}
				if pol.Locked {
					fmt.Printf("Lock:    bloccato da %s\n", pol.Owner)
				}
			}
		}
	}

	// Cosa salirebbe col deploy (dalla cartella ricordata o dalla corrente).
	if pl, err := buildPlan(dir); err == nil {
		from := ""
		if dir != "." {
			from = " da ./" + filepath.ToSlash(dir)
		}
		fmt.Printf("Deploy:  %d file, %s%s (esclusioni: %s", len(pl.files), humanSize(pl.totalSize), from, pl.ignoreSource())
		if pl.excluded > 0 {
			fmt.Printf(", %d esclusi", pl.excluded)
		}
		fmt.Println(")")
	}
}

// fmtWhen rende un timestamp RFC3339 come " (2006-01-02 15:04)", o "" se vuoto.
func fmtWhen(ts string) string {
	if ts == "" {
		return ""
	}
	if t, err := time.Parse(time.RFC3339, ts); err == nil {
		return " (" + t.Local().Format("2006-01-02 15:04") + ")"
	}
	return " (" + ts + ")"
}

// describeAccess traduce il valore di policy in una frase per l'utente.
func describeAccess(access string) string {
	switch access {
	case quick.AccessPublic:
		return "pubblico (niente SSO)"
	case quick.AccessCode:
		return "privato, accesso con codice"
	default:
		return "dietro SSO aziendale"
	}
}

// getPolicy legge la policy corrente del sito (GET). ok=false se la richiesta
// fallisce (es. token scaduto): in quel caso lo status omette la visibilità.
func getPolicy(cfg *cliConfig, name, tok string) (quick.PolicyResponse, bool) {
	endpoint := cfg.Server + "/api/site/" + url.PathEscape(name) + "/policy"
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return quick.PolicyResponse{}, false
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return quick.PolicyResponse{}, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return quick.PolicyResponse{}, false
	}
	body, _ := io.ReadAll(resp.Body)
	var pol quick.PolicyResponse
	if json.Unmarshal(body, &pol) != nil {
		return quick.PolicyResponse{}, false
	}
	return pol, true
}

func ignoreCmd(args []string) {
	dir := "."
	if len(args) > 0 && args[0] != "" {
		dir = args[0]
	}
	path, err := writeQuickignore(dir)
	fatal(err)
	if path == "" {
		fmt.Printf("%s esiste già: lo lascio com'è.\n", filepath.Join(dir, quickignoreName))
		return
	}
	fmt.Printf("✓ scritto %s\n", path)
	fmt.Println("  Modificalo per decidere cosa NON pubblicare. Da ora fa fede lui.")
}
