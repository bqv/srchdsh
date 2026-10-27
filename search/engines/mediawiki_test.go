//go:build live

package engines

import (
	"testing"

	"srchdsh/internal/engtest"
	"srchdsh/search"
)

func TestMediawikiSearch(t *testing.T) {
	engtest.New(
		"mediawiki",
		search.Config{
			Type: "mediawiki",
			Extra: map[string]any{
				"endpoint": "https://en.wikipedia.org/w/api.php",
			},
		},
		engtest.Config{IgnoreEmptyDescription: true},
	).RunTests(t,
		"hello world",
		"wikipedia",
		"big bang theory",
	)
}
