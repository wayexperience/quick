// Package storage astrae dove vivono i file dei siti e i metadata di policy:
// su filesystem locale (bind mount) o su object storage S3-compatibile. Il
// resto di quick-server lavora solo su Backend, ignaro di quale impl c'è sotto.
package storage

import (
	"archive/tar"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

// ErrNotFound: file/oggetto inesistente (per il fallback try_files in serve).
var ErrNotFound = errors.New("storage: not found")

// FileInfo accompagna un file aperto. Il content-type lo determina chi serve
// (http.ServeContent) via estensione/sniff, qui basta nome+mtime+etag.
type FileInfo struct {
	Name    string
	ModTime time.Time
	ETag    string
}

// Backend è l'astrazione di storage condivisa da contenuti siti e metadata.
type Backend interface {
	// PutSite rimpiazza l'intero albero del sito col contenuto del tar.
	PutSite(site string, tr *tar.Reader) error
	// OpenFile apre un singolo file del sito; ErrNotFound se non esiste/è dir.
	OpenFile(site, p string) (io.ReadSeekCloser, FileInfo, error)
	// GetMeta restituisce il JSON di policy del sito (ok=false se assente).
	GetMeta(site string) (data []byte, ok bool, err error)
	// PutMeta salva il JSON di policy del sito.
	PutMeta(site string, data []byte) error
}

// Config seleziona e configura il backend.
type Config struct {
	Kind     string // "local" | "s3"
	SitesDir string // local
	MetaDir  string // local
	S3       S3Config
}

// New costruisce il backend secondo Config.Kind.
func New(c Config) (Backend, error) {
	switch c.Kind {
	case "", "local":
		return newLocal(c.SitesDir, c.MetaDir)
	case "s3":
		return newS3(c.S3)
	default:
		return nil, fmt.Errorf("storage: kind %q sconosciuto (usa local|s3)", c.Kind)
	}
}

// cleanRel normalizza un path relativo e blocca il traversal.
func cleanRel(p string) (string, error) {
	rel := strings.TrimPrefix(path.Clean("/"+p), "/")
	if rel == ".." || strings.HasPrefix(rel, "../") {
		return "", fmt.Errorf("storage: percorso non sicuro: %q", p)
	}
	return rel, nil
}

// ---- backend locale (filesystem) ----

type local struct {
	sitesDir string
	metaDir  string
}

func newLocal(sitesDir, metaDir string) (*local, error) {
	for _, d := range []string{sitesDir, metaDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return nil, err
		}
	}
	return &local{sitesDir: sitesDir, metaDir: metaDir}, nil
}

func (l *local) PutSite(site string, tr *tar.Reader) error {
	tmp := filepath.Join(l.sitesDir, "."+site+".tmp")
	os.RemoveAll(tmp)
	if err := os.MkdirAll(tmp, 0o755); err != nil {
		return err
	}
	defer os.RemoveAll(tmp)

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		rel, err := cleanRel(hdr.Name)
		if err != nil {
			return err
		}
		if rel == "" {
			continue
		}
		dst := filepath.Join(tmp, rel)
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(dst, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
				return err
			}
			f, err := os.Create(dst)
			if err != nil {
				return err
			}
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				return err
			}
			f.Close()
		}
	}

	final := filepath.Join(l.sitesDir, site)
	old := filepath.Join(l.sitesDir, "."+site+".old")
	os.RemoveAll(old)
	if _, err := os.Stat(final); err == nil {
		if err := os.Rename(final, old); err != nil {
			return err
		}
	}
	if err := os.Rename(tmp, final); err != nil {
		return err
	}
	os.RemoveAll(old)
	return nil
}

func (l *local) OpenFile(site, p string) (io.ReadSeekCloser, FileInfo, error) {
	rel, err := cleanRel(p)
	if err != nil {
		return nil, FileInfo{}, err
	}
	full := filepath.Join(l.sitesDir, site, rel)
	f, err := os.Open(full)
	if err != nil {
		return nil, FileInfo{}, ErrNotFound
	}
	st, err := f.Stat()
	if err != nil || st.IsDir() {
		f.Close()
		return nil, FileInfo{}, ErrNotFound
	}
	return f, FileInfo{Name: filepath.Base(full), ModTime: st.ModTime()}, nil
}

func (l *local) metaPath(site string) string { return filepath.Join(l.metaDir, site+".json") }

func (l *local) GetMeta(site string) ([]byte, bool, error) {
	b, err := os.ReadFile(l.metaPath(site))
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return b, true, nil
}

func (l *local) PutMeta(site string, data []byte) error {
	tmp := l.metaPath(site) + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, l.metaPath(site))
}
