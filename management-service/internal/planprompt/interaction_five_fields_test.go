package planprompt

import (
	"strings"
	"testing"
)

func TestInteractionFiveFieldsAndOptionalIdentityFraming(t *testing.T) {
	for _, tc := range []struct {
		contract string
		multi    bool
	}{{InteractionOutputContract, false}, {BatchInteractionOutputContract, true}} {
		for _, key := range []string{"意图：", "类型：", "歧义：", "澄清句：", "话术："} {
			if strings.Count(tc.contract, "\n"+key) != 1 {
				t.Fatal("content keys must appear exactly once", key, tc.contract)
			}
		}
		for _, key := range []string{"置信度：", "优先程度：", "优先原因：", "候选：", "话术要求：", "对话关系：", "继续对话："} {
			if strings.Contains(tc.contract, key) {
				t.Fatal("retired output field", key)
			}
		}
		if strings.Contains(tc.contract, "\n编号：U01") != tc.multi || strings.Contains(tc.contract, "\n结束：U01") != tc.multi {
			t.Fatal("wrong identity framing", tc.contract)
		}
		if !strings.Contains(tc.contract, "空字段先写") || !strings.Contains(tc.contract, "有值的可播字段放在本项最后") {
			t.Fatal("ambiguous content boundary", tc.contract)
		}
	}
}
