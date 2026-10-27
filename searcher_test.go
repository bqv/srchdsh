package main

import (
	"testing"

	"srchdsh/search"
)

func TestMergeResults(t *testing.T) {
	results := []search.Result{
		{Title: "1", Link: "1"},
		{Title: "3", Link: "3"},
		{Title: "2", Link: "2"},
		{Title: "3", Link: "3"},
		{Title: "1", Link: "1"},
		{Title: "2", Link: "2"},
	}

	results = processResults(results)

	for i, link := range []string{"1", "3", "2"} {
		res := results[i].Link
		if res != link {
			t.Errorf(`results[%d] = %q, not %q`, i, res, link)
		}
	}
}

func TestLowTrustEngineDoesNotOutrank(t *testing.T) {
	// Wiby's results are real pages but rarely relevant; at equal weight
	// they tie with Startpage's and can sort first. The default weights must
	// put the trusted engine's result ahead.
	junk := search.Result{Title: "Wang LOCI-2", Link: "https://oldcalculatormuseum.com/", Sources: []string{"wiby"}}
	good := search.Result{Title: "The 5 Best Mechanical Keyboards", Link: "https://www.rtings.com/", Sources: []string{"startpage"}}

	cfg.Engines = nil
	results := processResults([]search.Result{junk, good})

	if results[0].Link != good.Link {
		t.Fatalf("first result = %q, wanted the Startpage one", results[0].Link)
	}
}

func TestConfiguredWeightWins(t *testing.T) {
	cfg.Engines = map[string]search.Config{"wiby": {Weight: 2}}
	defer func() { cfg.Engines = nil }()

	if got := engineWeight("wiby"); got != 2 {
		t.Fatalf("configured weight ignored: %v", got)
	}
	if got := engineWeight("startpage"); got != 1 {
		t.Fatalf("unconfigured engine weight = %v", got)
	}
}
