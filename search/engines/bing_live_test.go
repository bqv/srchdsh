//go:build live

package engines

import (
	"context"
	"testing"
	"time"
)

// TestBingLive queries Bing's RSS feed. Bing answers a share of requests with
// an empty channel, which is indistinguishable from "no results", so an empty
// answer skips rather than fails.
func TestBingLive(t *testing.T) {
	if testing.Short() {
		t.Skip("live test")
	}

	eng := newLiveEngine(t, "bing")

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	started := time.Now()
	results, err := eng.Search(ctx, "deepseek harness", 0)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) == 0 {
		t.Skip("Bing returned an empty channel (it blocks some requests)")
	}

	t.Logf("%d results in %s", len(results), time.Since(started))
	for i, r := range results {
		if i == 3 {
			break
		}
		t.Logf("  %s | %s | %.50s", r.Title, r.Link, r.Description)
	}

	// The feed has no pages; a later page must come back empty, not error.
	next, err := eng.Search(ctx, "deepseek harness", 1)
	if err != nil {
		t.Fatalf("page 1: %v", err)
	}
	if len(next) != 0 {
		t.Errorf("page 1 returned %d results; the feed has no pages", len(next))
	}
}
