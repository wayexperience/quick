// quick rollback <sito>: ripristina la versione precedente del sito (annulla
// l'ultimo deploy). Un secondo rollback la rifà. Disponibile sullo storage
// locale; sull'object storage va gestito col versioning del bucket.
package main

import (
	"bytes"
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

func rollbackCmd(args []string) {
	var name string
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		name, args = args[0], args[1:]
	}

	fs := flag.NewFlagSet("rollback", flag.ExitOnError)
	server := fs.String("server", "", "URL del server (o QUICK_SERVER)")
	token := fs.String("token", os.Getenv("QUICK_TOKEN"), "ID token Google (default: login salvato)")
	fs.Parse(args)

	sf := loadSiteFile(".")
	if name == "" && sf != nil {
		name = sf.Name
	}
	if name == "" {
		fatal(errors.New("manca il nome del sito (o esegui in una cartella con .quick)"))
	}
	if !confirmSiteMismatch(sf, name, "ripristinare") {
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

	endpoint := cfg.Server + "/api/site/" + url.PathEscape(name) + "/rollback"
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(nil))
	fatal(err)
	req.Header.Set("Authorization", "Bearer "+tok)

	resp, err := http.DefaultClient.Do(req)
	fatal(err)
	defer resp.Body.Close()
	rb, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "rollback fallito (%d): %s\n", resp.StatusCode, strings.TrimSpace(string(rb)))
		os.Exit(1)
	}
	var res quick.RollbackResponse
	json.Unmarshal(rb, &res)
	fmt.Printf("%s %s ripristinato alla versione precedente → %s\n", check(), cBold(name), cCyan(res.URL))
}
