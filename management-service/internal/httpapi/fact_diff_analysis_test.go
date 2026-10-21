package httpapi

import (
	"strings"
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

// 保存前就要拦住算错的差额：资料 22 与 15，写「多10道」必须报错，并列出 7。
func TestSourceDifferencesExposeUsableGaps(t *testing.T) {
	source := "1号链接：规格=含22道荤素菜；团购价=337\n2号链接：规格=含15道荤素菜；团购价=187"
	gaps := sourceDifferences(source)
	joined := strings.Join(gaps, ",")
	if !strings.Contains(","+joined+",", ",7,") {
		t.Fatalf("22-15=7 必须出现在可用差值里：%v", gaps)
	}
	if !strings.Contains(","+joined+",", ",150,") {
		t.Fatalf("337-187=150 必须出现在可用差值里：%v", gaps)
	}
	if strings.Contains(","+joined+",", ",10,") {
		t.Fatalf("10 不是任何两数之差，不该出现：%v", gaps)
	}
	if len(gaps) > 8 {
		t.Fatalf("最多列 8 个，避免刷屏：%v", gaps)
	}
}

// 「多了10道菜」这种写法，10 在「10人餐」「10道凉菜」里都出现过，
// 单看「数字是否出现」拦不住，必须靠差额语境核对。
func TestDifferenceClaimMustMatchRealGap(t *testing.T) {
	source := "1号链接：规格=含22道荤素菜，10道凉菜小吃，10种冰淇淋；团购价=337；适用人数=10人\n" +
		"2号链接：规格=含15道荤素菜，10道凉菜小吃，10种冰淇淋；团购价=187；适用人数=6人以内"
	// 22-15=7 是真的，放行。
	if claimed, _, bad := checkDifferenceClaims("1号链接比2号链接多7道菜", source); bad {
		t.Fatalf("7 是真实差额，不该拦：%s", claimed)
	}
	// 10 虽然出现过，但差额对不上，必须拦。
	claimed, gaps, bad := checkDifferenceClaims("1号链接比2号链接多了10道菜", source)
	if !bad || claimed != "10" {
		t.Fatalf("差额写成 10 必须被拦：bad=%v claimed=%s", bad, claimed)
	}
	if strings.Join(gaps, ",") == "" {
		t.Fatal("报错时要给出可用差值，方便用户核对")
	}
	// 普通陈述没有差额语境，不参与这项检查。
	if _, _, bad := checkDifferenceClaims("1号链接团购价337元", source); bad {
		t.Fatal("没有差额语境的描述不该被拦")
	}
}
