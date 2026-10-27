//go:build live

package engines

import (
	"testing"

	"srchdsh/internal/engtest"
	"srchdsh/search"
)

func TestGoogleSearch(t *testing.T) {
	engtest.New("google", search.Config{}).RunTests(t,
		"hello world",
		"wikipedia",
		"big bang theory",
		"exanple", // intentionally misspelled
	)
}
