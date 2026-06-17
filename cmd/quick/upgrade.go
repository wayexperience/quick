// quick upgrade: aggiorna la CLI all'ultima release pubblicata su GitHub,
// rimpiazzando il binario in esecuzione. Nessuna dipendenza esterna: API GitHub
// + estrazione dell'archivio con la stdlib.
//
//	quick upgrade            # scarica e installa l'ultima versione
//	quick upgrade --check    # dice solo se ce n'è una più recente
package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"time"
)

// upgradeRepo è il repo upstream da cui si scaricano le release.
const upgradeRepo = "zupolgec/quick"

func upgradeCmd(args []string) {
	fs := flag.NewFlagSet("upgrade", flag.ExitOnError)
	checkOnly := fs.Bool("check", false, "controlla solo se c'è una versione più recente, senza installarla")
	fs.Parse(args)

	cur := currentVersion()
	tag, assetURL, err := latestRelease()
	fatal(err)

	if normVer(tag) == normVer(cur) {
		fmt.Printf("%s quick è già aggiornato (%s)\n", check(), cur)
		return
	}
	if *checkOnly {
		fmt.Printf("È disponibile %s (hai %s). Aggiorna con %s\n", cBold(tag), cur, cBold("quick upgrade"))
		return
	}
	if assetURL == "" {
		fatal(fmt.Errorf("nessun binario per %s/%s nella release %s", runtime.GOOS, runtime.GOARCH, tag))
	}

	fmt.Printf("Aggiorno quick %s → %s…\n", cur, cBold(tag))
	fatal(doUpgrade(assetURL))
	fmt.Printf("%s quick aggiornato a %s\n", check(), cBold(tag))
}

// currentVersion è la versione del binario (var version, o il module version se
// installato con `go install`).
func currentVersion() string {
	v := version
	if v == "dev" {
		if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
			v = info.Main.Version
		}
	}
	return v
}

func normVer(s string) string { return strings.TrimPrefix(strings.TrimSpace(s), "v") }

func archiveExt() string {
	if runtime.GOOS == "windows" {
		return "zip"
	}
	return "tar.gz"
}

// latestRelease interroga l'API GitHub e restituisce tag e URL dell'archivio per
// la piattaforma corrente (assetURL vuoto se non c'è un binario adatto).
func latestRelease() (tag, assetURL string, err error) {
	req, _ := http.NewRequest(http.MethodGet, "https://api.github.com/repos/"+upgradeRepo+"/releases/latest", nil)
	req.Header.Set("User-Agent", "quick-cli") // l'API GitHub lo richiede
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("API GitHub: %s", resp.Status)
	}
	var rel struct {
		TagName string `json:"tag_name"`
		Assets  []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return "", "", err
	}
	want := fmt.Sprintf("quick_%s_%s.%s", runtime.GOOS, runtime.GOARCH, archiveExt())
	for _, a := range rel.Assets {
		if a.Name == want {
			assetURL = a.URL
			break
		}
	}
	return rel.TagName, assetURL, nil
}

// doUpgrade scarica l'archivio, ne estrae il binario e rimpiazza l'eseguibile in
// corsa (rename atomico nella stessa cartella; su Windows sposta prima il vecchio).
func doUpgrade(assetURL string) error {
	arch, err := download(assetURL)
	if err != nil {
		return err
	}
	binName := "quick"
	if runtime.GOOS == "windows" {
		binName = "quick.exe"
	}
	data, err := extractBinary(arch, binName)
	if err != nil {
		return err
	}

	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	tmp := exe + ".new"
	if err := os.WriteFile(tmp, data, 0o755); err != nil {
		return permHint(filepath.Dir(exe), err)
	}
	if runtime.GOOS == "windows" {
		// Su Windows non si può sovrascrivere un .exe in uso: sposta il vecchio.
		_ = os.Rename(exe, exe+".old")
	}
	if err := os.Rename(tmp, exe); err != nil {
		os.Remove(tmp)
		return permHint(filepath.Dir(exe), err)
	}
	return nil
}

func download(url string) ([]byte, error) {
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("User-Agent", "quick-cli")
	resp, err := (&http.Client{Timeout: 2 * time.Minute}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download fallito: %s", resp.Status)
	}
	return io.ReadAll(resp.Body)
}

// extractBinary estrae il file binName dall'archivio (tar.gz o zip).
func extractBinary(arch []byte, binName string) ([]byte, error) {
	if archiveExt() == "zip" {
		zr, err := zip.NewReader(bytes.NewReader(arch), int64(len(arch)))
		if err != nil {
			return nil, err
		}
		for _, f := range zr.File {
			if filepath.Base(f.Name) == binName {
				rc, err := f.Open()
				if err != nil {
					return nil, err
				}
				defer rc.Close()
				return io.ReadAll(rc)
			}
		}
		return nil, errors.New("binario non trovato nell'archivio")
	}
	gz, err := gzip.NewReader(bytes.NewReader(arch))
	if err != nil {
		return nil, err
	}
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if filepath.Base(hdr.Name) == binName {
			return io.ReadAll(tr)
		}
	}
	return nil, errors.New("binario non trovato nell'archivio")
}

func permHint(dir string, err error) error {
	if os.IsPermission(err) {
		return fmt.Errorf("permessi insufficienti per scrivere in %s: riprova con sudo o reinstalla con l'one-liner", dir)
	}
	return err
}
