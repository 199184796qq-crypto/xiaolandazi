package danmakutest

import (
	"encoding/json"
	"livecompanion/management/internal/model"
	"testing"
)

func fixture(id int64, target model.MaterialObjectTarget, topic, content string) Record {
	return Record{Claim: model.PlanMaterialClaim{ID: id, Topic: topic, Content: content, EmbeddingModel: "qwen", MaterialClaimDetails: model.MaterialClaimDetails{Destination: "fact", ObjectTargets: []model.MaterialObjectTarget{target}}}, Vector: []float32{1, 0}}
}
func TestRecallScopeAndMultiAsk(t *testing.T) {
	links := []Link{{ID: 11, Name: "1号链接"}, {ID: 22, Name: "2号链接"}}
	records := []Record{fixture(1, model.MaterialObjectTarget{Kind: "product", LinkID: 11}, "价格", "87.6元"), fixture(2, model.MaterialObjectTarget{Kind: "product", LinkID: 22}, "价格", "98元"), fixture(3, model.MaterialObjectTarget{Kind: "plan"}, "配送", "新疆空运需补运费"), fixture(4, model.MaterialObjectTarget{Kind: "store", StoreName: "王家店"}, "切块", "支持切块")}
	stores := []model.MaterialStoreObject{{Name: "王家店", LinkIDs: []int64{11}}}
	asks := []Ask{{Text: "1号多少钱", Subjects: []string{"1号"}, Attribute: "价格"}, {Text: "新疆能寄吗", Subjects: []string{"1号"}, Conditions: map[string]string{"region": "新疆"}}}
	out := Retrieve(records, links, stores, asks, Queries("1号多少钱，新疆能寄吗", asks), [][]float32{{1, 0}, {1, 0}, {1, 0}}, "qwen", false)
	if len(out.Groups) != 2 {
		t.Fatal(out)
	}
	for _, g := range out.Groups {
		for _, h := range g.Hits {
			if h.ID == 2 {
				t.Fatal("cross-card evidence", h)
			}
		}
	}
	if len(out.Hits) != 3 {
		t.Fatal("global/store inherited once", out)
	}
	unresolved := Retrieve(records, links, stores, []Ask{{Text: "这个多少钱"}}, []string{"这个多少钱"}, [][]float32{{1, 0}}, "qwen", false)
	if len(unresolved.Hits) != 1 || unresolved.Hits[0].ID != 3 {
		t.Fatal("unresolved scope leaked", unresolved)
	}
}
func TestRecallUnknownTypoDoesNotBecomeRegionPromise(t *testing.T) {
	c := fixture(1, model.MaterialObjectTarget{Kind: "plan"}, "运费", "包邮").Claim
	c.Exceptions = json.RawMessage(`[{"field":"region","op":"in","values":["新疆"],"universe":"china_provinces","raw":"新疆除外"}]`)
	for _, tc := range []struct{ region, want string }{{"新疆", "不适用"}, {"新建", "待核对"}, {"", "待核对"}, {"上海", "适用"}} {
		if got := conditionStatus(c, map[string]string{"region": tc.region}); got != tc.want {
			t.Fatal(tc, got)
		}
	}
	c.Conditions = json.RawMessage(`[{"op":"and","values":[{"field":"amount","op":"gte","values":[99]},{"field":"channel","op":"eq","values":["线上"]}]}]`)
	if got := conditionStatus(c, map[string]string{"region": "上海", "amount": "88", "channel": "线上"}); got != "不适用" {
		t.Fatal(got)
	}
	if got := conditionStatus(c, map[string]string{"region": "上海", "amount": "100", "channel": "线上"}); got != "适用" {
		t.Fatal(got)
	}
}
func TestRecallCompatibilityExclusionAndCompletePredicates(t *testing.T) {
	r := fixture(1, model.MaterialObjectTarget{Kind: "plan"}, "运费", "满99元包邮")
	r.Claim.Conditions = json.RawMessage(`[{"field":"amount","op":"gte","values":[99],"raw":"满99元"}]`)
	r.Claim.Exceptions = json.RawMessage(`[{"field":"region","op":"in","values":["新疆"],"raw":"新疆除外"}]`)
	word := fixture(2, model.MaterialObjectTarget{Kind: "plan"}, "词义", "砍=还价")
	word.Claim.Destination = "word"
	bad := fixture(3, model.MaterialObjectTarget{Kind: "plan"}, "运费", "旧向量")
	bad.Claim.EmbeddingModel = "old"
	unknown := fixture(4, model.MaterialObjectTarget{}, "运费", "待归属")
	unknown.Claim.ObjectTargets = []model.MaterialObjectTarget{}
	out := Retrieve([]Record{r, word, bad, unknown}, nil, nil, []Ask{{Text: "运费", Conditions: map[string]string{"amount": "10", "region": "新疆"}}}, []string{"运费"}, [][]float32{{1, 0}}, "qwen", true)
	if len(out.Hits) != 1 || len(out.Notices) != 1 || out.Hits[0].Conditions != "满99元" || out.Hits[0].Exceptions != "新疆除外" || out.Hits[0].ConditionStatus != "不适用" {
		t.Fatal(out)
	}
}

func TestRecallDisplayedSimilarityIsNotAttributePriority(t *testing.T) {
	r := fixture(1, model.MaterialObjectTarget{Kind: "plan"}, "价格", "87.6元")
	r.Vector = []float32{.8, .6}
	out := Retrieve([]Record{r}, nil, nil, []Ask{{Text: "多少钱", Attribute: "价格"}}, []string{"多少钱"}, [][]float32{{1, 0}}, "qwen", false)
	if len(out.Hits) != 1 || out.Hits[0].Score < .799 || out.Hits[0].Score > .801 {
		t.Fatal("similarity inflated by ranking", out)
	}
}
