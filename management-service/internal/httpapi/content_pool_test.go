package httpapi

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/contentpools"
	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
)

func TestPoolTasksAcceptAliasesPreserveIDAndRejectInventedFacts(t *testing.T) {
	p := contentpools.Pool{Key: "product:1", Owner: 1, Products: []int64{1}, Facts: []contentpools.Fact{{ID: "f", Text: "- [商品事实] 日常价=19元", Products: []int64{1}}}}
	products := []contentpools.Product{{ID: 1, Link: "1"}, {ID: 2, Link: "2"}}
	raw := `{"任务":[{"编号":"old","卖点":"改名","角度":"日常价19元","事实编号":["f"],"额外中文字段":"兼容"}]}`
	got, err := decodePoolTaskResponse(raw, p, []contentpools.Task{{ID: "old", Point: "旧名字"}}, products)
	if err != nil || got[0].ID != "old" {
		t.Fatal(got, err)
	}
	for _, bad := range []string{strings.Replace(raw, "19元", "9元", 1), strings.Replace(raw, `["f"]`, `["unknown"]`, 1), strings.Replace(raw, "日常价19元", "看2号链接", 1)} {
		if _, err = decodePoolTaskResponse(bad, p, nil, products); err == nil {
			t.Fatal("invalid task accepted", bad)
		}
	}
	p.Owner = 0
	p.Products = []int64{1, 2}
	p.Facts[0].Products = p.Products
	if _, err = decodePoolTaskResponse(strings.Replace(raw, "日常价19元", "看1号链接", 1), p, nil, products); err == nil {
		t.Fatal("common pool named a single product")
	}
}
func TestCardFactsKeepConditionsAndStableAttributeID(t *testing.T) {
	source := contentpools.Sources{Products: []contentpools.Product{{ID: 7, Link: "1"}}, Rows: []contentpools.SourceRow{{ID: "products:7", Products: []int64{7}}, {ID: "attributes:9", Area: "products", Products: []int64{7}, Fields: map[string]string{"属性": "加热", "属性编号": "heating"}}}}
	facts := contentFactsFromSafety(source, "- [商品卡] 链接=1；商品=鸭子；加热=180度；仅限拆开包装后；规格=半只")
	found := false
	for _, f := range facts {
		if f.ID == "attributes:9" {
			found = true
			if !strings.Contains(f.Text, "仅限拆开包装后") {
				t.Fatal("condition split away", f)
			}
		}
	}
	if !found {
		t.Fatal(facts)
	}
	source.Rows[1].Fields["属性"] = "食用建议"
	facts = contentFactsFromSafety(source, "- [商品卡] 链接=1；食用建议=180度；仅限拆开包装后；商品=鸭子")
	found = false
	for _, f := range facts {
		if f.ID == "attributes:9" {
			found = true
		}
	}
	if !found {
		t.Fatal("display rename changed origin ID")
	}
}
func TestBenefitScopesSupportManyAndFilterInactiveProducts(t *testing.T) {
	links := []model.LiveAgentPlanProductLink{}
	ids := []int64{}
	for i := int64(1); i <= 25; i++ {
		links = append(links, model.LiveAgentPlanProductLink{ID: i, LinkKey: fmt.Sprint(i), Status: "active"})
		ids = append(ids, i)
	}
	b := model.LiveAgentPlanBenefit{ID: 1, LinkKey: "1"}
	scopes := map[int64]appdb.BenefitScope{1: {BenefitID: 1, Products: ids}}
	if got := benefitScopeProducts(b, links, scopes); len(got) != 25 {
		t.Fatal(got)
	}
	links[0].Status = "disabled"
	got := benefitScopeProducts(b, links, scopes)
	if len(got) != 24 || contentpools.Contains(got, 1) {
		t.Fatal("inactive product still eligible", got)
	}
	scopes[1] = appdb.BenefitScope{BenefitID: 1, All: true}
	if len(benefitScopeProducts(b, links, scopes)) != 24 {
		t.Fatal("all-products scope")
	}
}
func TestRuntimeManyProductsStableIDsAndCommonSubsetFacts(t *testing.T) {
	source := contentpools.Sources{Products: []contentpools.Product{}, Rows: []contentpools.SourceRow{{ID: "shared"}}}
	pools := []contentpools.Pool{}
	for i := int64(1); i <= 20; i++ {
		roles := []string{"auxiliary"}
		if i == 1 {
			roles = []string{"main"}
		}
		source.Products = append(source.Products, contentpools.Product{ID: i, Link: fmt.Sprint(i), Name: fmt.Sprint(i), Roles: roles})
		origin := fmt.Sprintf("products:%d", i)
		source.Rows = append(source.Rows, contentpools.SourceRow{ID: origin})
		p := contentpools.Pool{Key: origin, Owner: i, Products: []int64{i}, Facts: []contentpools.Fact{{ID: origin, Text: origin, Sources: []string{origin}, Products: []int64{i}}}}
		for j := 0; j < 30; j++ {
			p.Tasks = append(p.Tasks, contentpools.Task{ID: fmt.Sprintf("t-%d-%d", i, j), Point: fmt.Sprintf("卖点%d", j), Angle: "名称可以相同", Sources: []string{origin}})
		}
		pools = append(pools, p)
	}
	pools = append(pools, contentpools.Pool{Key: "common", Products: []int64{1, 2}, Facts: []contentpools.Fact{{ID: "common-fact", Text: "AB共有事实", Sources: []string{"shared"}, Products: []int64{1, 2}}}, Tasks: []contentpools.Task{{ID: "common-task", Point: "公共卖点", Angle: "角度", Sources: []string{"common-fact"}}}})
	set := contentpools.Set{Sources: source, SourceSHA: source.SHA(), SafeFacts: "保存事实", Pools: pools}
	tasks, focus, err := contentPoolRuntime(set, anchorcycle.ProductPolicy{Version: "v1", MainSharePercent: 70, MinMainRounds: 1, AuxiliaryMaxChars: 200})
	if err != nil {
		t.Fatal(err)
	}
	state, err := anchorcycle.New(tasks, anchorcycle.Config{MaxChars: 300, PointGap: 2, CombinationGap: 8})
	if err != nil {
		t.Fatal(err)
	}
	state.ProductFocus = focus
	focus.ResetRun()
	state.SelectProductTask()
	if state.Status != "ready" || len(state.Queue) != 601 || state.Queue[0].Key != tasks[0].ID {
		t.Fatalf("first selection %+v", state.Status)
	}
	for _, link := range []string{"1", "2"} {
		focus.CurrentLink = link
		focus.AuxiliaryActive = link != "1"
		state.SelectProductTask()
		facts := state.ProductFacts(state.Task())
		if !strings.Contains(facts, "AB共有事实") {
			t.Fatal("common subset unavailable", link, facts)
		}
	}
	focus.CurrentLink = "3"
	focus.AuxiliaryActive = true
	state.SelectProductTask()
	facts := state.ProductFacts(state.Task())
	if strings.Contains(facts, "AB共有事实") || strings.Contains(facts, "products:1\n") {
		t.Fatal("C got A/B facts", facts)
	}
	raw, _ := json.Marshal(set)
	var frozen contentpools.Set
	_ = json.Unmarshal(raw, &frozen)
	source.Products[0].Name = "编辑了新名称"
	if frozen.Sources.Products[0].Name == source.Products[0].Name {
		t.Fatal("snapshot mutated")
	}
}
