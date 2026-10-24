// Package cache stores small pieces of engine state that are cheaper to
// reuse than to recreate, such as a captcha exemption cookie or a form token.
//
// Nothing kept here is authoritative. Deleting the directory only costs
// latency: every value can be fetched again from the engine.
package cache

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// entry is a single stored value, optionally with an expiry.
type entry struct {
	// Expires is a Unix timestamp after which the entry is invalid.
	// A zero value means the entry never expires.
	Expires int64 `json:"expires,omitempty"`

	// Value holds the JSON encoding of the stored value.
	Value json.RawMessage `json:"value"`
}

// Store is a namespaced key/value store persisted as one JSON file.
//
// The zero value is not usable; use [Open]. A Store is safe for concurrent
// use within a process. Separate processes may race on the same file, which is
// acceptable here: the values are cheap to recreate and the write is atomic.
type Store struct {
	path string

	mu   sync.Mutex
	data map[string]entry
}

// Open returns the store for name, backed by
// $XDG_CACHE_HOME/srchdsh/<name>.json.
//
// If no cache directory can be determined, the returned store works but never
// persists anything.
func Open(name string) *Store {
	s := &Store{data: map[string]entry{}}

	base, err := os.UserCacheDir()
	if err != nil || base == "" {
		return s
	}

	s.path = filepath.Join(base, "srchdsh", name+".json")
	s.load()
	return s
}

// Path returns the file backing the store, or an empty string if the store is
// memory-only.
func (s *Store) Path() string {
	return s.path
}

// load reads the backing file, silently discarding anything unreadable or
// expired. A corrupt cache is not an error.
func (s *Store) load() {
	b, err := os.ReadFile(s.path)
	if err != nil {
		return
	}

	var data map[string]entry
	if err := json.Unmarshal(b, &data); err != nil {
		return
	}

	now := time.Now().Unix()
	for k, v := range data {
		if v.Expires == 0 || v.Expires > now {
			s.data[k] = v
		}
	}
}

// Get decodes the value stored under key into dst.
//
// It reports whether a live value was found. Expired values are treated as
// missing.
func (s *Store) Get(key string, dst any) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	e, ok := s.data[key]
	if !ok {
		return false
	}
	if e.Expires != 0 && e.Expires <= time.Now().Unix() {
		delete(s.data, key)
		return false
	}
	if err := json.Unmarshal(e.Value, dst); err != nil {
		return false
	}
	return true
}

// Put stores v under key. If ttl is greater than zero the value expires after
// that duration; otherwise it lasts until overwritten.
//
// Errors are returned for the caller to log, but a failed write never
// invalidates the in-memory copy.
func (s *Store) Put(key string, v any, ttl time.Duration) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("cache: encode %q: %w", key, err)
	}

	e := entry{Value: raw}
	if ttl > 0 {
		e.Expires = time.Now().Add(ttl).Unix()
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[key] = e
	return s.save()
}

// Delete removes key.
func (s *Store) Delete(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.data, key)
	_ = s.save()
}

// save writes the whole store to disk atomically: a temporary file in the same
// directory, restricted to the owner, renamed over the destination.
//
// The caller must hold s.mu.
func (s *Store) save() error {
	if s.path == "" {
		return nil
	}

	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("cache: create directory: %w", err)
	}

	b, err := json.Marshal(s.data)
	if err != nil {
		return fmt.Errorf("cache: encode store: %w", err)
	}

	f, err := os.CreateTemp(dir, filepath.Base(s.path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("cache: create temporary file: %w", err)
	}
	tmp := f.Name()
	defer os.Remove(tmp)

	if err := f.Chmod(0o600); err != nil {
		f.Close()
		return fmt.Errorf("cache: chmod temporary file: %w", err)
	}
	if _, err := f.Write(b); err != nil {
		f.Close()
		return fmt.Errorf("cache: write temporary file: %w", err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return fmt.Errorf("cache: sync temporary file: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("cache: close temporary file: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return fmt.Errorf("cache: replace %s: %w", s.path, err)
	}
	return nil
}
