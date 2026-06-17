// Gli script di install della CLI, serviti (pubblici, niente SSO) dall'apex così
// che l'one-liner punti al proprio dominio invece che a un raw GitHub:
//
//	curl -fsSL https://<dominio>/install.sh | sh        (macOS/Linux)
//	irm https://<dominio>/install.ps1 | iex             (Windows)
//
// Gli script scaricano il binario dall'ultima GitHub Release (vedi .goreleaser.yaml).
package main

import (
	_ "embed"
	"net/http"
)

//go:embed install/install.sh
var installSh string

//go:embed install/install.ps1
var installPs1 string

func (s *server) handleInstallSh(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
	_, _ = w.Write([]byte(installSh))
}

func (s *server) handleInstallPs1(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte(installPs1))
}
