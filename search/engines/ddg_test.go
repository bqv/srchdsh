package engines

import (
	"fmt"
	"testing"
)

func TestDDGPageParams(t *testing.T) {
	// This setup is completely unnecessary but it does verify my math.

	tests := []struct{ page, s, dc int }{
		{0, 0, 0},
		{1, 24, 25},
		{2, 74, 75},
		{3, 124, 125},
		{4, 174, 175},
	}

	for _, test := range tests {
		t.Run(fmt.Sprint(test.page), func(t *testing.T) {
			s, dc := determinePageParams(test.page)
			if s != test.s || dc != test.dc {
				t.Fatalf("s = %d, wanted %d; dc = %d, wanted %d", s, test.s, dc, test.dc)
			}
		})
	}
}
