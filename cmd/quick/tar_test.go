package main

import (
	"archive/tar"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

func TestTarGzExcludesDotfiles(t *testing.T) {
	dir := t.TempDir()
	write := func(p, c string) {
		full := filepath.Join(dir, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("index.html", "hi")
	write("sub/index.html", "x")
	write(".DS_Store", "junk")
	write(".env", "SECRET=1")
	write(".git/config", "g")
	write(".quick", `{"name":"n"}`)
	write(".well-known/acme.txt", "a")

	buf, err := tarGz(dir)
	if err != nil {
		t.Fatal(err)
	}
	gz, _ := gzip.NewReader(buf)
	tr := tar.NewReader(gz)
	got := map[string]bool{}
	for {
		h, e := tr.Next()
		if e != nil {
			break
		}
		got[h.Name] = true
	}

	for _, w := range []string{"index.html", "sub/index.html", ".well-known/acme.txt"} {
		if !got[w] {
			t.Errorf("manca %q nel tarball", w)
		}
	}
	for _, b := range []string{".DS_Store", ".env", ".git/config", ".git", ".quick"} {
		if got[b] {
			t.Errorf("%q NON dovrebbe essere nel tarball", b)
		}
	}
}
