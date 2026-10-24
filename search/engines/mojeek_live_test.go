//go:build live

package engines

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"git.sr.ht/~cmcevoy/srchd/search"
)

// TestMojeekLive queries the real Mojeek, solving the Altcha challenge if one
// is served. It only runs with -tags live because it needs the network and, on
// a cold cache, a proof of work.
func TestMojeekLive(t *testing.T) {
	if testing.Short() {
		t.Skip("live test")
	}

	cacheDir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cacheDir)

	query := func(eng search.Engine, page int) []search.Result {
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

	// The first run pays for the challenge; the cookie is good for weeks.
	first := query(newLiveEngine(t, "mojeek"), 0)

	raw, err := os.ReadFile(filepath.Join(cacheDir, "srchdsh", "mojeek.json"))
	if err != nil {
		t.Fatalf("read cache: %v", err)
	}
	if !strings.Contains(string(raw), `"chllg"`) {
		t.Errorf("cache does not hold the challenge cookie: %s", raw)
	}

	// A fresh engine has an empty jar and must restore the cookie from disk.
	query(newLiveEngine(t, "mojeek"), 0)

	// Deep pages are refused for non-browser clients (403, "automated
	// queries"), so this is allowed to fail; a regression that returns the
	// first page again would be silent otherwise.
	next, err := searchPage(newLiveEngine(t, "mojeek"), 1)
	switch {
	case err != nil && errors.Is(err, search.ErrCaptcha):
		t.Logf("page 2 refused, as expected for a non-browser client: %v", err)
	case err != nil:
		t.Errorf("page 2 failed unexpectedly: %v", err)
	case sameLinks(first, next):
		t.Errorf("page 2 returned the same links as page 1")
	}
}

// searchPage runs one query on a page and returns its results.
func searchPage(eng search.Engine, page int) ([]search.Result, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	return eng.Search(ctx, "deepseek harness", page)
}
