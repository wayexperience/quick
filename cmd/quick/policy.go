// Comandi di policy della CLI: apertura al pubblico, accesso con codice, lock.
//
//	quick publish   <sito>            # visibile a chiunque (niente SSO)
//	quick unpublish <sito>            # torna dietro SSO aziendale
//	quick private   <sito> [--code X] # accesso con codice (generato se assente)
//	quick lock      <sito>            # solo tu puoi sovrascriverlo
//	quick unlock    <sito>
package main

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/wayexperience/quick/internal/quick"
)

func policyCmd(action string, args []string) {
	var name string
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		name, args = args[0], args[1:]
	}

	fs := flag.NewFlagSet(action, flag.ExitOnError)
	server := fs.String("server", "", "URL del server (o QUICK_SERVER)")
	token := fs.String("token", os.Getenv("QUICK_TOKEN"), "ID token Google (default: login salvato)")
	var code string
	if action == "private" {
		fs.StringVar(&code, "code", "", "codice di accesso (se vuoto, generato)")
	}
	fs.Parse(args)

	if name == "" {
		fatal(errors.New("manca il nome del sito"))
	}

	payload := quick.PolicyRequest{}
	switch action {
	case "publish":
		payload.Access = new(quick.AccessPublic)
	case "unpublish":
		payload.Access = new("sso")
	case "private":
		if code == "" {
			code = genCode()
		}
		payload.Access, payload.Code = new(quick.AccessCode), new(code)
	case "lock":
		payload.Locked = new(true)
	case "unlock":
		payload.Locked = new(false)
	}

	cfg, err := resolveConfig(*server)
	fatal(err)

	tok := *token
	if tok == "" {
		if tok, err = idToken(cfg); err != nil {
			fatal(err)
		}
	}

	res := callPolicy(cfg, name, tok, payload)
	url := "https://" + res.Site + "." + cfg.BaseDomain
	switch action {
	case "private":
		fmt.Printf("✓ %s protetto da codice → %s\n  codice: %s\n", name, url, code)
	case "publish":
		fmt.Printf("✓ %s pubblico → %s\n", name, url)
	case "unpublish":
		fmt.Printf("✓ %s di nuovo dietro SSO → %s\n", name, url)
	case "lock":
		fmt.Printf("✓ %s bloccato (solo %s può sovrascriverlo)\n", name, res.Owner)
	case "unlock":
		fmt.Printf("✓ %s sbloccato\n", name)
	}
}

func callPolicy(cfg *cliConfig, name, tok string, payload quick.PolicyRequest) quick.PolicyResponse {
	body, _ := json.Marshal(payload)
	endpoint := cfg.Server + "/api/site/" + url.PathEscape(name) + "/policy"
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	fatal(err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+tok)

	resp, err := http.DefaultClient.Do(req)
	fatal(err)
	defer resp.Body.Close()
	rb, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "fallito (%d): %s\n", resp.StatusCode, strings.TrimSpace(string(rb)))
		os.Exit(1)
	}
	var res quick.PolicyResponse
	json.Unmarshal(rb, &res)
	return res
}

// genCode crea un codice di accesso breve e leggibile (niente caratteri ambigui).
func genCode() string {
	const alphabet = "abcdefghijkmnpqrstuvwxyz23456789"
	b := make([]byte, 8)
	rand.Read(b)
	for i := range b {
		b[i] = alphabet[int(b[i])%len(alphabet)]
	}
	return string(b)
}
