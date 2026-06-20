package main

import (
	"testing"
	"time"

	"github.com/zupolgec/quick/internal/storage"
)

// load must distinguish "absent" (empty policy, no error) from "corrupt" (error
// propagated, not a silent empty policy that would allow an ownership bypass).
func TestMetaLoadDistinguishesAbsentFromCorrupt(t *testing.T) {
	st, err := storage.New(storage.Config{Kind: "local", SitesDir: t.TempDir(), MetaDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	m := newMetaStore(st, []byte("s"), 0) // ttl 0: no caching between calls

	if p, err := m.load("nuovo"); err != nil || p.Access != "" {
		t.Fatalf("absent: p=%+v err=%v, want empty/nil", p, err)
	}

	if err := m.save("ok", policy{Access: "public"}); err != nil {
		t.Fatal(err)
	}
	if p, err := m.load("ok"); err != nil || p.Access != "public" {
		t.Fatalf("valid: p=%+v err=%v", p, err)
	}

	if err := st.PutMeta("rotto", []byte("{non-json")); err != nil {
		t.Fatal(err)
	}
	if _, err := m.load("rotto"); err == nil {
		t.Fatal("corrupt metadata: want error, got nil")
	}
}

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
		t.Fatal("concurrent lock('a') did not wait for release")
	case <-time.After(50 * time.Millisecond):
	}

	unlock()
	select {
	case <-reached:
	case <-time.After(time.Second):
		t.Fatal("lock('a') did not proceed after unlock")
	}

	// A different key must not block.
	k.lock("b")()
}
