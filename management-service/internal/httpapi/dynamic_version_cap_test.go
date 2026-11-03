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
		name           string
		pieces, chars  int
		want           int
	}{
		{"1条4000字", 1, 4000, 8},
		{"1条800字撞上限", 1, 800, 20},
		{"3条900字", 3, 900, 12},
		{"5条900字", 5, 900, 7},
		{"12条900字", 12, 900, 3},
		{"20条4000字撞下限", 20, 4000, 3},
	}
	for _, c := range cases {
		if got := scriptVersionCapFromUnits(unitsWithChars(c.pieces, c.chars)); got != c.want {
			t.Fatalf("%s: 期望 %d，实际 %d", c.name, c.want, got)
		}
	}
}