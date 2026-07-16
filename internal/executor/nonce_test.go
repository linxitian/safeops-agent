package executor

import (
	"path/filepath"
	"testing"
	"time"
)

func TestNonceStorePersistsReplayState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nonces.json")
	store, err := NewNonceStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Reserve("nonce", time.Now().Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewNonceStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := reopened.Reserve("nonce", time.Now().Add(time.Minute)); err == nil {
		t.Fatal("persisted replay was accepted")
	}
}
