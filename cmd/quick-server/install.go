// CLI install scripts, served publicly (no SSO) from the apex so the one-liner
// points at this domain instead of a raw GitHub URL:
//
//	curl -fsSL https://<domain>/install.sh | sh        (macOS/Linux)
//	irm https://<domain>/install.ps1 | iex             (Windows)
//
// The scripts download the binary from the latest GitHub Release (see .goreleaser.yaml).
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
