// .quick: file di progetto scritto nella cartella al primo deploy, con il minimo
// per ripetere i comandi senza parametri (nome del sito, server). NON viene
// caricato nel deploy (escluso dal tarball), quindi non finisce servito sul sito.
package main

import (
	"encoding/json"
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
