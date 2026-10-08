package contentguard

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func mother(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile("../../../configs/prompts/anchor-style/compliance/L1/p0-hard-block.md")
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestMotherNormalizationAndOriginalOffsets(t *testing.T) {
	md := mother(t)
	p, err := Compile(md)
	if err != nil {
		t.Fatal(err)
	}
	again, _ := Compile(md)
	if again != p {
		t.Fatal("compiled policy not shared")
	}
	for _, text := range []string{"哥哥姐姐们，蕞好。", "这是第①。", "排行ＮＯ．１啊。", "这是tOp1。", "零\u200b 风 险啊。", "百分百有效。", "孕妇可用。", "加微信购买。", "销量80万单。"} {
		r := p.Scan(text, "ordinary_food")
		if r.Passed || len(r.Hits) == 0 {
			t.Fatalf("miss: %q %+v", text, r)
		}
		for _, h := range r.Hits {
			if string([]rune(text)[h.Start:h.End]) != h.Text {
				t.Fatal("lost original offsets", h)
			}
		}
	}
	if !p.Scan("我们家在四川绵阳，5L大桶9斤2两啊。", "ordinary_food").Passed {
		t.Fatal("safe fixture blocked")
	}
	for _, text := range []string{"使用京东物流", "由京东快递配送", "订单由京东配送"} {
		if !p.Scan(text, "ordinary_food").Passed {
			t.Fatalf("delivery context incorrectly blocked: %q", text)
		}
	}
	for _, text := range []string{"去京东下单", "到京东购买"} {
		if p.Scan(text, "ordinary_food").Passed {
			t.Fatalf("off-platform trade escaped P0: %q", text)
		}
	}
	if !p.Scan("孕妇可用。", "all").Passed {
		t.Fatal("food rule leaked into global scope")
	}
	if p.Scan("普通文本", "unknown").Passed || p.Scan(" ", "all").Passed {
		t.Fatal("invalid input passed")
	}
}

func TestMotherValidationFailsClosed(t *testing.T) {
	md := mother(t)
	for _, invalid := range []string{
		strings.Replace(md, `"allow_user_override": false`, `"allow_user_override": true`, 1),
		strings.Replace(md, `"max_rewrites": 3`, `"max_rewrites": 99`, 1),
		strings.Replace(md, `"schema_version": 1`, `"unknown_field": 1, "schema_version": 1`, 1),
		strings.Replace(md, `"scope": "all"`, `"scope": "mystery"`, 1),
		md + "\n```json\n{}\n```",
	} {
		if _, err := Compile(invalid); err == nil {
			t.Fatal("invalid mother compiled")
		}
	}
}

func TestPartialRepairOnlyTouchesMatchedUnitsAndRescans(t *testing.T) {
	p, err := Compile(mother(t))
	if err != nil {
		t.Fatal(err)
	}
	text := "老乡啊。零风险！\n5L装哟。加微信购买。继续看规格啊。"
	repair := PlanRepair(text, p.Scan(text, "ordinary_food"), 1)
	if len(repair.Units) != 2 {
		t.Fatal(repair)
	}
	raw := `{"replacements":[{"id":"part_2","text":"点1号链接看规格。"},{"id":"part_1","text":"先看试吃条件！"}]}`
	result, err := repair.Apply(raw, 0)
	if err != nil || result != "老乡啊。先看试吃条件！\n5L装哟。点1号链接看规格。继续看规格啊。" {
		t.Fatal(result, err)
	}
	if !p.Scan(result, "ordinary_food").Passed {
		t.Fatal("valid patch blocked")
	}
	var roundtrip Repair
	encoded, _ := json.Marshal(repair)
	if json.Unmarshal(encoded, &roundtrip) != nil {
		t.Fatal("roundtrip")
	}
	if again, err := roundtrip.Apply(raw, 300); err != nil || again != result {
		t.Fatal("redis snapshot changed patch", err)
	}
	for _, bad := range []string{
		`{"replacements":[]}`, `{"replacements":[{"id":"part_1","text":"a"},{"id":"part_1","text":"b"}]}`,
		`{"replacements":[{"id":"part_1","text":"a"},{"id":"other","text":"b"}]}`,
		`{"replacements":[{"id":"part_1","text":" "},{"id":"part_2","text":"b"}]}`,
		`{"replacements":[{"id":"part_1","text":"a"},{"id":"part_2","text":"b"}],"full_text":"change all"}`,
		`全文改写`, raw + `{}`,
	} {
		if _, err := repair.Apply(bad, 300); err == nil {
			t.Fatal("bad patch accepted", bad)
		}
	}
	if _, err := repair.Apply(raw, 5); err == nil {
		t.Fatal("assembled limit ignored")
	}
	newlyBad := strings.Replace(raw, "先看试吃条件", "最好", 1)
	result, err = repair.Apply(newlyBad, 300)
	if err != nil || p.Scan(result, "ordinary_food").Passed {
		t.Fatal("new illegal claim escaped recheck", err)
	}
}

func TestDenseMatchesKeepOffsets(t *testing.T) {
	p, err := Compile(mother(t))
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Repeat("最", 2000)
	r := p.Scan(text, "ordinary_food")
	if len(r.Hits) != 2000 || r.Hits[1999].Start != 1999 {
		t.Fatal("dense matches lost positions")
	}
}
