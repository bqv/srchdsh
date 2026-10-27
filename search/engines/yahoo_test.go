//go:build live

package engines

import (
	"testing"

	"srchdsh/internal/engtest"
	"srchdsh/search"
)

func TestYahooSearch(t *testing.T) {
	engtest.New("yahoo", search.Config{}).RunTests(t,
		"hello world",
		"wikipedia",
		"big bang theory",
		"exanple", // intentionally misspelled
	)
}
