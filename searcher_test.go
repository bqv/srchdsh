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
