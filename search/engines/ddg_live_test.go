//go:build live

package engines

import (
	"testing"

	"srchdsh/internal/engtest"
	"srchdsh/search"
)

func TestDDGSearch(t *testing.T) {
	engtest.New("ddg", search.Config{}).RunTests(t,
		"hello world",
		"wikipedia",
		"big bang theory",
		"exanple", // intentionally misspelled
	)
}
