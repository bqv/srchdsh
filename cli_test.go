package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"srchdsh/search"
)

// stubEngine is a search.Engine that returns fixed results.
type stubEngine struct {
	results []search.Result
	err     error
	queries []string
}

func (s *stubEngine) Ping(context.Context) error { return nil }

func (s *stubEngine) Search(_ context.Context, query string, _ int) ([]search.Result, error) {
	s.queries = append(s.queries, query)
	return s.results, s.err
}

// withEngines swaps the global engine set for one test.
func withEngines(t *testing.T, set map[string]search.Engine) {
	t.Helper()

	old := engines
	engines = set
	t.Cleanup(func() { engines = old })
}

func result(link string, sources ...string) search.Result {
	return search.Result{Title: link, Link: link, Sources: sources}
}

func TestDoSearchMerges(t *testing.T) {
	alpha := &stubEngine{results: []search.Result{result("https://a.example/", "alpha")}}
	beta := &stubEngine{results: []search.Result{result("https://b.example/", "beta")}}
	withEngines(t, map[string]search.Engine{"alpha": alpha, "beta": beta})

	out := doSearch(context.Background(), "deepseek harness", 0)
	if out.Err != nil {
		t.Fatalf("search: %v", out.Err)
	}
	if len(out.Results) != 2 {
		t.Fatalf("got %d results, wanted 2", len(out.Results))
	}
	if len(out.Runs) != 2 || out.Runs[0].ID != "alpha" || out.Runs[1].ID != "beta" {
		t.Fatalf("runs = %+v, wanted alpha then beta", out.Runs)
	}
	if alpha.queries[0] != "deepseek harness" {
		t.Fatalf("engine saw query %q", alpha.queries[0])
	}
}

func TestDoSearchPartialFailure(t *testing.T) {
	alpha := &stubEngine{results: []search.Result{result("https://a.example/", "alpha")}}
	beta := &stubEngine{err: errors.New("boom")}
	withEngines(t, map[string]search.Engine{"alpha": alpha, "beta": beta})

	out := doSearch(context.Background(), "q", 0)
	if out.Err != nil {
		t.Fatalf("one engine failing must not fail the search: %v", out.Err)
	}
	if len(out.Results) != 1 {
		t.Fatalf("got %d results, wanted 1", len(out.Results))
	}
	if out.Runs[1].Err == nil {
		t.Fatal("beta's failure was not reported")
	}
}

func TestDoSearchAllFailed(t *testing.T) {
	withEngines(t, map[string]search.Engine{
		"alpha": &stubEngine{err: errors.New("boom")},
		"beta":  &stubEngine{err: errors.New("bang")},
	})

	out := doSearch(context.Background(), "q", 0)
	if !errors.Is(out.Err, errAllFailed) {
		t.Fatalf("got %v, wanted errAllFailed", out.Err)
	}
	if len(out.Runs) != 2 {
		t.Fatalf("got %d runs, wanted 2", len(out.Runs))
	}
}

func TestDoSearchOperator(t *testing.T) {
	alpha := &stubEngine{results: []search.Result{result("https://a.example/", "alpha")}}
	beta := &stubEngine{results: []search.Result{result("https://b.example/", "beta")}}
	withEngines(t, map[string]search.Engine{"alpha": alpha, "beta": beta})

	out := doSearch(context.Background(), ":alpha deepseek harness", 0)
	if out.Err != nil {
		t.Fatalf("search: %v", out.Err)
	}
	if len(out.Runs) != 1 || out.Runs[0].ID != "alpha" {
		t.Fatalf("runs = %+v, wanted only alpha", out.Runs)
	}
	// The operator must be stripped from the query, both for the engines and
	// for the document the caller builds.
	if out.Query != "deepseek harness" {
		t.Fatalf("outcome query = %q", out.Query)
	}
	if alpha.queries[0] != "deepseek harness" {
		t.Fatalf("engine saw query %q", alpha.queries[0])
	}
	if len(beta.queries) != 0 {
		t.Fatal("beta was queried despite the operator")
	}
}

func TestDoSearchUnknownEngine(t *testing.T) {
	withEngines(t, map[string]search.Engine{"alpha": &stubEngine{}})

	if out := doSearch(context.Background(), ":nope q", 0); out.Err == nil {
		t.Fatal("expected an error for an unknown engine")
	}
}

func TestDoSearchEmptyQuery(t *testing.T) {
	withEngines(t, map[string]search.Engine{"alpha": &stubEngine{}})

	if out := doSearch(context.Background(), "   ", 0); out.Err == nil {
		t.Fatal("expected an error for an empty query")
	}
}

func TestProcessOperatorsEscape(t *testing.T) {
	engines, query := processOperators(`\:ddg deepseek`)
	if len(engines) != 0 {
		t.Fatalf("escaped colon produced engines %v", engines)
	}
	if query != "\\:ddg deepseek"[1:] {
		t.Fatalf("query = %q", query)
	}
}

func TestRunVersion(t *testing.T) {
	var stdout, stderr bytes.Buffer

	if code := run([]string{"version"}, &stdout, &stderr); code != exitOK {
		t.Fatalf("exit code = %d, wanted %d", code, exitOK)
	}
	if !strings.HasPrefix(stdout.String(), "srchdsh v") {
		t.Fatalf("output = %q", stdout.String())
	}
}

func TestRunUsageErrors(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"no command", nil},
		{"unknown command", []string{"frobnicate"}},
		{"search without a query", []string{"search"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := run(test.args, &stdout, &stderr); code != exitFailure {
				t.Fatalf("exit code = %d, wanted %d", code, exitFailure)
			}
		})
	}
}
