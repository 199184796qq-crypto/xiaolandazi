package atomicfacts

import (
	"strings"
	"testing"
)

func TestBuildSplitsShippingAndPackaging(t *testing.T) {
	input := `- [用户指定公共；配送时效与包装建议] 川渝地区隔天送达；本地发货不抽真空，外地发货采用真空包装。`
	rendered, facts := Build(input)
	if len(facts) != 2 {
		t.Fatalf("facts=%d rendered=%s", len(facts), rendered)
	}
	if !strings.Contains(facts[0].Text, "川渝地区隔天送达") || strings.Contains(facts[0].Text, "真空") {
		t.Fatalf("first fact not shipping-only: %s", facts[0].Text)
	}
	if !strings.Contains(facts[1].Text, "本地发货不抽真空，外地发货采用真空包装") {
		t.Fatalf("packaging condition pair lost: %s", facts[1].Text)
	}
	if facts[0].ID != "F001" || facts[1].ID != "F002" {
		t.Fatalf("unexpected IDs: %s %s", facts[0].ID, facts[1].ID)
	}
}

func TestBuildKeepsAtomicFactUnchanged(t *testing.T) {
	input := `- [发货] 快递选择只有京东物流。`
	rendered, facts := Build(input)
	if len(facts) != 1 || rendered != input {
		t.Fatalf("atomic fact changed: %q %+v", rendered, facts)
	}
}
