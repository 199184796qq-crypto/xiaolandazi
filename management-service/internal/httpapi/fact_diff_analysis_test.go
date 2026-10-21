package httpapi

import (
	"testing"

	"livecompanion/management/internal/model"
)

// 模型给的说法里，每个数字都必须能在本方案事实里查到，否则整条作废——
// 对比事实最容易出的错就是把一边的数字算到另一边。
func TestFactDiffAnalysisDigitGuard(t *testing.T) {
	source := "1号链接：22道荤素菜，团购价337元，原价547元\n2号链接：15道荤素菜，团购价187元"
	// 差额是两数相减得到的，属于可核算结论，不能误杀。
	if got := unknownDigits("1号链接22道荤素菜，2号链接15道，相差7道", source); len(got) != 0 {
		t.Fatalf("7 正好是 22-15，应该放行：%v", got)
	}
	if got := unknownDigits("1号链接团购337元，2号链接团购187元，相差150元", source); len(got) != 0 {
		t.Fatalf("150 正好是 337-187，应该放行：%v", got)
	}
	// 凭空冒出来的数字必须拦下。
	// 「多10道」也是错的：22-15=7，10 在来源里推不出来，必须和 3 一起被标出来。
	got := unknownDigits("1号链接比2号链接多了10道菜，另送3份小料", source)
	if len(got) != 2 || got[0] != "10" || got[1] != "3" {
		t.Fatalf("10 和 3 都推不出来，必须被标出来：%v", got)
	}
	if got := unknownDigits("1号链接团购337元，2号链接团购187元", source); len(got) != 0 {
		t.Fatalf("数字都在事实里，不该报错：%v", got)
	}
}

func TestParseFactDiffAnalysis(t *testing.T) {
	found, err := parseFactDiffAnalysis("```json\n{\"found\":true,\"value\":\"1号链接22道，2号链接15道\"}\n```")
	if err != nil || !found.Found || found.Value == "" {
		t.Fatalf("found 结果解析失败: %#v %v", found, err)
	}
	miss, err := parseFactDiffAnalysis(`{"found":false,"reason":"两边只有总价","example":"1号链接含X道，2号链接含Y道"}`)
	if err != nil || miss.Found || miss.Example == "" {
		t.Fatalf("not found 结果解析失败: %#v %v", miss, err)
	}
	if _, err = parseFactDiffAnalysis(`{"found":true}`); err == nil {
		t.Fatal("found 但没有 value 必须判失败")
	}
}

// 公共事实两边一样，不能当差异依据；待改的那条自己也要跳过。
func TestGroupFactsByLinkSkipsCommonAndSelf(t *testing.T) {
	s := &Server{}
	items := []model.LiveAgentPlanFact{
		{ID: 1, Status: "active", Key: "价格", Value: "团购337元", Scope: &model.LiveAgentPlanFactScope{Kind: "product", ProductLinkIDs: []int64{7}}},
		{ID: 2, Status: "active", Key: "价格", Value: "团购187元", Scope: &model.LiveAgentPlanFactScope{Kind: "product", ProductLinkIDs: []int64{8}}},
		{ID: 3, Status: "active", Key: "地址", Value: "顺庆区", Scope: &model.LiveAgentPlanFactScope{Kind: "common"}},
		{ID: 4, Status: "active", Key: "差异", Value: "两个链接不同", Scope: &model.LiveAgentPlanFactScope{Kind: "product", ProductLinkIDs: []int64{7, 8}}},
		{ID: 5, Status: "active", Key: "第三品", Value: "不该出现", Scope: &model.LiveAgentPlanFactScope{Kind: "product", ProductLinkIDs: []int64{9}}},
	}
	names := map[int64]string{7: "1号链接", 8: "2号链接", 9: "3号链接"}
	perLink, all := s.groupFactsByLink(items, []int64{7, 8}, names, 4)
	if len(perLink["1号链接"]) != 1 || len(perLink["2号链接"]) != 1 {
		t.Fatalf("每个链接只该有自己那条：%#v", perLink)
	}
	for _, line := range perLink["1号链接"] {
		if line == "" || contains(line, "不该出现") {
			t.Fatalf("混进了别的链接：%s", line)
		}
	}
	if contains(all, "顺庆区") {
		t.Fatal("公共事实不应进入差异依据")
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
