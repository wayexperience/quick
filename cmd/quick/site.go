// .quick: file di progetto scritto nella cartella al primo deploy, con il minimo
// per ripetere i comandi senza parametri (nome del sito, server). NON viene
// caricato nel deploy (escluso dal tarball), quindi non finisce servito sul sito.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const siteFileName = ".quick"

type siteFile struct {
	Name   string `json:"name"`
	Server string `json:"server,omitempty"`
}

func loadSiteFile(dir string) *siteFile {
	b, err := os.ReadFile(filepath.Join(dir, siteFileName))
	if err != nil {
		return nil
	}
	var sf siteFile
	if json.Unmarshal(b, &sf) != nil || sf.Name == "" {
		return nil
	}
	return &sf
}

func saveSiteFile(dir string, sf siteFile) {
	b, _ := json.MarshalIndent(sf, "", "  ")
	_ = os.WriteFile(filepath.Join(dir, siteFileName), append(b, '\n'), 0o644)
}

// confirmSiteMismatch avvisa quando la cartella corrente è collegata (via .quick)
// a un sito diverso da quello su cui il comando sta per agire, e chiede conferma.
// verb è l'azione da mostrare, es. "fare deploy su", "modificare", "eliminare".
// Restituisce false se l'utente annulla.
func confirmSiteMismatch(sf *siteFile, name, verb string) bool {
	if sf == nil || sf.Name == "" || sf.Name == name {
		return true
	}
	fmt.Fprintf(os.Stderr, "⚠️  questa cartella è collegata al sito %q (.quick), ma stai per %s %q.\n", sf.Name, verb, name)
	fmt.Fprint(os.Stderr, "Procedo lo stesso? [s/N]: ")
	if !yesNo(readLine()) {
		fmt.Fprintln(os.Stderr, "annullato")
		return false
	}
	return true
}
