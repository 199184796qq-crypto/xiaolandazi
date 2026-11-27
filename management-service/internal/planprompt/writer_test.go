package planprompt

import (
	"strings"
	"testing"
)

func TestFillWriterOnlyReplacesWhitelist(t *testing.T) {
	master := "名称{{方案名称}} 主播{{主播名}} 未知{{未知占位}}"
	got := FillWriter(master, map[string]string{"方案名称": "YangDuck", "主播名": "XiaoLin"})
	if !strings.Contains(got, "名称YangDuck") || !strings.Contains(got, "主播XiaoLin") {
		t.Fatalf("whitelist placeholder not replaced: %s", got)
	}
	if !strings.Contains(got, "{{未知占位}}") {
		t.Fatalf("unknown placeholder must stay intact: %s", got)
	}
	if bad := UnknownWriterPlaceholders(got); len(bad) != 1 || bad[0] != "{{未知占位}}" {
		t.Fatalf("unknown placeholder detection failed: %v", bad)
	}
}

func TestNormalizeWriterOutputStripsFenceAndPreamble(t *testing.T) {
	cases := []string{
		"```\n【主播人设】body\n```",
		"好的，以下是主播人设：\n【主播人设】body",
	}
	for _, in := range cases {
		if got := NormalizeWriterOutput(in); !strings.HasPrefix(got, "【主播人设】") {
			t.Fatalf("wrapper not stripped: %q", got)
		}
	}
}

func TestAuditWriterOutputRejectsInternalTermsInPersona(t *testing.T) {
	body := strings.Repeat("说话稳，一句一个意思。", 12)
	fatal, notes := AuditWriterOutput("persona", body+"称呼交给系统处理。")
	if !fatal {
		t.Fatalf("internal term in persona must be fatal: %v", notes)
	}
	if fatal, _ := AuditWriterOutput("persona", body); fatal {
		t.Fatal("clean persona must not be fatal")
	}
}

func TestAuditWriterOutputWarnsOnAddressTermsAndNumbers(t *testing.T) {
	body := strings.Repeat("一句一个意思。", 14)
	_, notes := AuditWriterOutput("persona", body+"单句20字以内。")
	joined := strings.Join(notes, "|")
	if !strings.Contains(joined, "数字") {
		t.Fatalf("missing number warning: %v", notes)
	}
	_, notes = AuditWriterOutput("persona", body+"老乡们好。")
	if !strings.Contains(strings.Join(notes, "|"), "老乡们") {
		t.Fatalf("missing address-term warning: %v", notes)
	}
}

func TestIndustryFactFieldsFallsBackWide(t *testing.T) {
	if got := IndustryFactFields("ecommerce", "food"); !strings.Contains(got, "克重") {
		t.Fatalf("food industry should list weight: %s", got)
	}
	if got := IndustryFactFields("ecommerce", "unknown-industry"); !strings.Contains(got, "资质") {
		t.Fatalf("unknown industry should fall back to widest list: %s", got)
	}
}