package factguard

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMain(m *testing.M) {
	path, err := filepath.Abs(filepath.Join("..", "..", "..", "configs", "prompts", "anchor-style", "fact-bound.json"))
	if err == nil {
		os.Setenv("FACT_BOUND_CONFIG", path)
	}
	os.Exit(m.Run())
}

const yangDuckFacts = "F001 店铺有全场包邮活动，活动长期有效\nF006 下单后第二天上午发货，统一走京东物流"

func TestAssertionRejectsUnsupportedSameDayClaim(t *testing.T) {
	// Facts only promise next-day shipping; "当天" is a contradiction, and the
	// model may correct it, so it must not be forced into a boundary.
	result := Assertions("是当天发货吗", yangDuckFacts)
	if len(result.Missing) != 0 {
		t.Fatalf("contradicted time should not be missing: %+v", result)
	}
	// Nothing in the facts mentions cold-chain; the assumption is unsupported.
	result = Assertions("是冷链发货吗", "F002 下单后第二天上午发货")
	if len(result.Missing) == 0 {
		t.Fatalf("unsupported claim should be missing: %+v", result)
	}
}

func TestAuditDropsInventedNumbersAndPromises(t *testing.T) {
	clean, dropped := Audit("朋友们，这个包邮的，三天就能到。", yangDuckFacts)
	if strings.Contains(clean, "三天") {
		t.Fatalf("invented timing kept: %q", clean)
	}
	if len(dropped) == 0 {
		t.Fatal("expected a dropped sentence")
	}
	clean, _ = Audit("朋友们，下单后第二天上午发货，统一走京东物流。", yangDuckFacts)
	if clean == "" {
		t.Fatal("supported sentence dropped")
	}
}

func TestAuditDropsUnsupportedPromiseWord(t *testing.T) {
	clean, _ := Audit("我们保证当天送到。", "F002 下单后第二天上午发货")
	if strings.Contains(clean, "保证") {
		t.Fatalf("promise word kept: %q", clean)
	}
}
