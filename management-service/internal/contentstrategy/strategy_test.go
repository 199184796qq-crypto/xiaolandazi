package contentstrategy

import (
	"encoding/json"
	"strings"
	"testing"
)

func testSnapshot(text string) *Snapshot {
	facts := Facts(text)
	return &Snapshot{Version: 1, Enabled: true, FactsSHA: SHA(text), Facts: facts,
		Purposes: []Purpose{{"P01", "理解价格", "担心价格偏高", []string{facts[0].ID}}},
		Rows:     []Row{{"P01", "按只理解预算", "讲清一只的花费，联系选购判断", []string{facts[0].ID}, "价格讲解或贵不贵的问题"}}}
}
func TestFullRecordsPreserveControlsAndCounterexample(t *testing.T) {
	text := "# 正式事实包\n- [other/算账] 一只85.5；安全改写=按只或斤\n不要算一克多少钱，900克只是反例\n- [商品卡] 商品=油炸鸭；工艺=现卤现炸\n边界：不要编造"
	facts := Facts(text)
	if len(facts) != 2 || !strings.Contains(facts[0].Text, "900克只是反例") || !strings.Contains(facts[0].Text, "安全改写") || strings.Contains(facts[1].Text, "边界：") {
		t.Fatal(facts)
	}
}
func TestStrictProtocolAndRealFactReferences(t *testing.T) {
	var p Purposes
	for _, raw := range []string{`{"目的列表":[],"extra":1}`, `{"目的列表":[]} trailing`, `{"目的列表":[{"目的编号":"P01","目的":"理解价格","观众顾虑":"贵不贵","关联事实":["missing"]}]}`} {
		err := Decode(raw, &p)
		if err == nil {
			err = Validate([]Fact{{"F001", "真实事实"}}, p.Items, nil, false)
		}
		if err == nil {
			t.Fatal("invalid purpose accepted", raw)
		}
	}
	s := testSnapshot("- [商品卡] 一只85.5元")
	s.Rows[0].PurposeID = "unknown"
	if Validate(s.Facts, s.Purposes, s.Rows, true) == nil {
		t.Fatal("unknown purpose accepted")
	}
}
func TestSnapshotRejectsChangedFactOrForgedRecord(t *testing.T) {
	text := "- [商品卡] 一只85.5元"
	s := testSnapshot(text)
	raw, _ := json.Marshal(s)
	if _, err := Parse(string(raw), text); err != nil {
		t.Fatal(err)
	}
	if _, err := Parse(string(raw), "- [商品卡] 一只99元"); err == nil {
		t.Fatal("stale facts accepted")
	}
	s.Facts[0].Text = "- [商品卡] 伪造事实"
	raw, _ = json.Marshal(s)
	if _, err := Parse(string(raw), text); err == nil {
		t.Fatal("forged record accepted")
	}
}
func TestProductScopeCannotBorrowOtherLinks(t *testing.T) {
	text := "- [商品卡] 1号油129.9元\n- [商品卡] 2号鸭85.5元"
	s := testSnapshot(text)
	s.Rows = append(s.Rows, Row{"P01", "另一商品预算", "2号鸭讲解", []string{"F002"}, "鸭子价格问题"})
	scope := "- [商品卡] 1号油129.9元"
	prompt, err := s.Render("主线", scope)
	if err != nil || !strings.Contains(prompt, "1号油129.9元") || strings.Contains(prompt, "2号鸭85.5元") || strings.Contains(prompt, "2号鸭讲解") {
		t.Fatal(prompt, err)
	}
	s.Enabled = false
	if prompt, err = s.Render("主线", text); err != nil || prompt != "" {
		t.Fatal("disabled strategy changed original generation", prompt, err)
	}
}
