//go:build live

package engines

import (
	"testing"

	"srchdsh/internal/engtest"
	"srchdsh/search"
)

func TestBraveSearch(t *testing.T) {
	engtest.New("brave", search.Config{}).RunTests(t,
		"hello world",
		"wikipedia",
		"big bang theory",
		"exanple", // intentionally misspelled
	)
}
