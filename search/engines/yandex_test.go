//go:build live

package engines

import (
	"os"
	"testing"

	"srchdsh/internal/engtest"
	"srchdsh/search"
)

func TestYandexSearch(t *testing.T) {
	if os.Getenv("SRCHD_TEST_BROKEN") == "" {
		t.Skip("Set SRCHD_TEST_BROKEN=1 to test Yandex search.")
		return
	}

	engtest.New("yandex", search.Config{}).RunTests(t,
		"hello world",
		"wikipedia",
		"big bang theory",
		"exanple", // intentionally misspelled
	)
}
