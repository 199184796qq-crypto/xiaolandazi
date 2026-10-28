package addressing

import (
	"math/rand"
	"testing"
)

func TestDiceMinRatioScalesWithCrowd(t *testing.T) {
	rules := DefaultRules()
	rules.Dice.Max = 5
	rules.Dice.MinRatio = 0.3
	viewers := make([]Viewer, 12)
	for i := range viewers {
		viewers[i] = Viewer{UserID: string(rune('a' + i)), RawName: "观众" + string(rune('甲'+i))}
	}
	for seed := int64(1); seed <= 20; seed++ {
		got := Pick(viewers, &rules, rand.New(rand.NewSource(seed)))
		if len(got) < 4 || len(got) > 5 {
			t.Fatalf("seed=%d picked=%d want 4..5", seed, len(got))
		}
	}
	two := Pick(viewers[:2], &rules, rand.New(rand.NewSource(1)))
	if len(two) != 2 {
		t.Fatalf("small crowd picked=%d want 2", len(two))
	}
}
