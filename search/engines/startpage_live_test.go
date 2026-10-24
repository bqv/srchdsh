//go:build live

package engines

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"git.sr.ht/~cmcevoy/srchd/search"
)

// TestStartpageLive queries the real Startpage, solving the Anubis challenge
// if one is served. It only runs with -tags live because it needs the network
// and, on a cold cache, a proof of work.
func TestStartpageLive(t *testing.T) {
	if testing.Short() {
		t.Skip("live test")
	}

	// Keep the cookie in a throwaway directory so the test does not depend
	// on, or disturb, the user's cache.
	cacheDir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cacheDir)

	search0 := func(eng search.Engine, page int) []search.Result {
		t.Helper()

		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		started := time.Now()
		results, err := eng.Search(ctx, "deepseek harness", page)
		if err != nil {
			t.Fatalf("page %d: search: %v", page, err)
		}
		if len(results) == 0 {
			t.Fatalf("page %d: no results", page)
		}
		t.Logf("page %d: %d results in %s", page, len(results), time.Since(started))

		for i, r := range results {
			if i == 2 {
				break
			}
			t.Logf("  %s | %s | %.50s", r.Title, r.Link, r.Description)
		}
		return results
	}

	// First run pays for the challenge and the sc token.
	first := search0(newLiveEngine(t, "startpage"), 0)

	// The cache must hold both, or the next fresh process pays again.
	raw, err := os.ReadFile(filepath.Join(cacheDir, "srchdsh", "startpage.json"))
	if err != nil {
		t.Fatalf("read cache: %v", err)
	}
	for _, key := range []string{"spchal-auth", "sc"} {
		if !strings.Contains(string(raw), `"`+key+`"`) {
			t.Errorf("cache does not hold %q: %s", key, raw)
		}
	}

	// A new engine is a new process as far as the cache is concerned: it has
	// an empty cookie jar and must restore the cookie from disk.
	search0(newLiveEngine(t, "startpage"), 0)

	// Paging is easy to get wrong by one, so make sure the parameter is
	// accepted and moves the result set.
	next := search0(newLiveEngine(t, "startpage"), 1)
	if sameLinks(first, next) {
		t.Errorf("page 2 returned the same links as page 1; the page parameter is probably wrong")
	}
}

// sameLinks reports whether two result sets contain the same URLs.
func sameLinks(a, b []search.Result) bool {
	if len(a) != len(b) || len(a) == 0 {
		return false
	}

	seen := make(map[string]int, len(a))
	for _, r := range a {
		seen[r.Link]++
	}
	for _, r := range b {
		if seen[r.Link] == 0 {
			return false
		}
		seen[r.Link]--
	}
	return true
}

func newLiveEngine(t *testing.T, engineType string) search.Engine {
	t.Helper()

	eng, err := (search.Config{Type: engineType}).New()
	if err != nil {
		t.Fatalf("initialize %s: %v", engineType, err)
	}
	return eng
}
