package main

import (
	"testing"
	"time"

	"github.com/zupolgec/quick/internal/storage"
)

// #3: load distingue "assente" (policy vuota, nessun errore) da "corrotto"
// (errore propagato, non una policy vuota silenziosa che farebbe ownership bypass).
func TestMetaLoadDistinguishesAbsentFromCorrupt(t *testing.T) {
	st, err := storage.New(storage.Config{Kind: "local", SitesDir: t.TempDir(), MetaDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	m := newMetaStore(st, []byte("s"), 0) // ttl 0: niente cache tra le chiamate

	// Assente -> policy vuota, nessun errore.
	if p, err := m.load("nuovo"); err != nil || p.Access != "" {
		t.Fatalf("assente: p=%+v err=%v, voluto vuota/nil", p, err)
	}

	// Presente e valido.
	if err := m.save("ok", policy{Access: "public"}); err != nil {
		t.Fatal(err)
	}
	if p, err := m.load("ok"); err != nil || p.Access != "public" {
		t.Fatalf("valido: p=%+v err=%v", p, err)
	}

	// Corrotto -> errore, non policy vuota.
	if err := st.PutMeta("rotto", []byte("{non-json")); err != nil {
		t.Fatal(err)
	}
	if _, err := m.load("rotto"); err == nil {
		t.Fatal("metadata corrotti: voluto errore, ottenuto nil")
	}
}

// #4: il lock per-sito serializza le operazioni sulla stessa chiave e non blocca
// chiavi diverse.
func TestKeyedMutexSerializes(t *testing.T) {
	var k keyedMutex
	unlock := k.lock("a")

	reached := make(chan struct{})
	go func() {
		u := k.lock("a")
		close(reached)
		u()
	}()

	select {
	case <-reached:
		t.Fatal("lock('a') concorrente non ha aspettato il rilascio")
	case <-time.After(50 * time.Millisecond):
	}

	unlock()
	select {
	case <-reached:
	case <-time.After(time.Second):
		t.Fatal("lock('a') non procede dopo unlock")
	}

	// Chiave diversa non deve bloccare.
	k.lock("b")()
}
