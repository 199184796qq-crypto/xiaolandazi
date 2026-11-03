package httpapi

import (
	"strings"
	"testing"
)

func unitsWithChars(pieces, chars int) []volcengineScriptUnit {
	units := make([]volcengineScriptUnit, pieces)
	for i := range units {
		units[i] = volcengineScriptUnit{Text: strings.Repeat("字", chars)}
	}
	return units
}

func TestScriptVersionCapScalesWithRoundLength(t *testing.T) {
	cases := []struct {
		name          string
		pieces, chars int
		want          int
	}{
		{"1条400字", 1, 400, 64},
		{"1条4000字", 1, 4000, 7},
		{"1条800字", 1, 800, 32},
		{"3条900字", 3, 900, 10},
		{"5条900字", 5, 900, 6},
		{"12条900字", 12, 900, 3},
		{"20条4000字", 20, 4000, 1},
	}
	for _, c := range cases {
		if got := scriptVersionCapFromUnits(unitsWithChars(c.pieces, c.chars)); got != c.want {
			t.Fatalf("%s: 期望 %d，实际 %d", c.name, c.want, got)
		}
	}
}
