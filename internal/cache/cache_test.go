package cache

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// openTemp opens a store backed by a temporary cache directory.
func openTemp(t *testing.T, name string) *Store {
	t.Helper()

	dir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", dir)

	s := Open(name)
	if want := filepath.Join(dir, "srchdsh", name+".json"); s.Path() != want {
		t.Fatalf("Path() = %q, wanted %q", s.Path(), want)
	}
	return s
}

func TestStoreRoundTrip(t *testing.T) {
	s := openTemp(t, "test")

	if err := s.Put("token", "abc", time.Hour); err != nil {
		t.Fatalf("put: %v", err)
	}

	var got string
	if !s.Get("token", &got) {
		t.Fatal("token missing from a fresh store")
	}
	if got != "abc" {
		t.Fatalf("token = %q, wanted %q", got, "abc")
	}

	// A second store must see the value on disk.
	var got2 string
	if !Open("test").Get("token", &got2) || got2 != "abc" {
		t.Fatalf("token after reopen = %q (found %v)", got2, got2 != "")
	}

	s.Delete("token")
	var got3 string
	if s.Get("token", &got3) {
		t.Fatal("deleted token still present")
	}
	if Open("test").Get("token", &got3) {
		t.Fatal("deleted token still on disk")
	}
}

func TestStoreExpiry(t *testing.T) {
	s := openTemp(t, "test")

	if err := s.Put("token", "abc", time.Hour); err != nil {
		t.Fatalf("put: %v", err)
	}

	// Age the entry rather than waiting for it: rewrite the file with an
	// expiry in the past.
	b, err := os.ReadFile(s.Path())
	if err != nil {
		t.Fatalf("read cache: %v", err)
	}

	var data map[string]entry
	if err := json.Unmarshal(b, &data); err != nil {
		t.Fatalf("decode cache: %v", err)
	}

	e := data["token"]
	e.Expires = time.Now().Add(-time.Minute).Unix()
	data["token"] = e

	b, err = json.Marshal(data)
	if err != nil {
		t.Fatalf("encode cache: %v", err)
	}
	if err := os.WriteFile(s.Path(), b, 0o600); err != nil {
		t.Fatalf("write cache: %v", err)
	}

	var got string
	if Open("test").Get("token", &got) {
		t.Fatalf("expired value returned: %q", got)
	}
}

func TestStorePermissions(t *testing.T) {
	s := openTemp(t, "test")

	// The values include credentials, so the file must not be world
	// readable.
	if err := s.Put("token", "secret", time.Hour); err != nil {
		t.Fatalf("put: %v", err)
	}

	info, err := os.Stat(s.Path())
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Fatalf("mode = %o, wanted 600", mode)
	}
}

func TestStoreCorruptFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", dir)

	if err := os.MkdirAll(filepath.Join(dir, "srchdsh"), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	path := filepath.Join(dir, "srchdsh", "test.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	s := Open("test")

	var got string
	if s.Get("token", &got) {
		t.Fatal("value returned from a corrupt file")
	}

	// The store must still be usable.
	if err := s.Put("token", "abc", time.Hour); err != nil {
		t.Fatalf("put after corruption: %v", err)
	}
	if !s.Get("token", &got) || got != "abc" {
		t.Fatalf("token after corruption = %q", got)
	}
}

func TestStoreTypes(t *testing.T) {
	s := openTemp(t, "test")

	type state struct {
		Count int      `json:"count"`
		Names []string `json:"names"`
	}

	in := state{Count: 2, Names: []string{"a", "b"}}
	if err := s.Put("state", in, 0); err != nil {
		t.Fatalf("put: %v", err)
	}

	var out state
	if !s.Get("state", &out) {
		t.Fatal("state missing")
	}
	if out.Count != in.Count || len(out.Names) != len(in.Names) {
		t.Fatalf("state = %+v, wanted %+v", out, in)
	}

	// A value of the wrong shape is a miss, not a panic.
	var wrong int
	if s.Get("state", &wrong) {
		t.Fatal("decoded a struct into an int")
	}
}
