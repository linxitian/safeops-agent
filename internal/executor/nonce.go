package executor

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type NonceStore struct {
	path string
	mu   sync.Mutex
	now  func() time.Time
	used map[string]time.Time
}

func NewNonceStore(path string) (*NonceStore, error) {
	store := &NonceStore{path: path, now: time.Now, used: map[string]time.Time{}}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, err
	}
	if b, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(b, &store.used); err != nil {
			return nil, err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	return store, nil
}
func (s *NonceStore) Reserve(nonce string, expiresAt time.Time) error {
	if nonce == "" {
		return errors.New("nonce is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	for value, expiry := range s.used {
		if now.After(expiry) {
			delete(s.used, value)
		}
	}
	if _, exists := s.used[nonce]; exists {
		return errors.New("envelope replay detected")
	}
	s.used[nonce] = expiresAt
	if err := s.save(); err != nil {
		delete(s.used, nonce)
		return err
	}
	return nil
}
func (s *NonceStore) save() error {
	b, err := json.MarshalIndent(s.used, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	dir := filepath.Dir(s.path)
	f, err := os.CreateTemp(dir, ".nonces-*.tmp")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(name, s.path)
}
