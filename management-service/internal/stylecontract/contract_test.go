package stylecontract

import (
	"livecompanion/management/internal/model"
	"strings"
	"testing"
)

func fixture(source string, habits ...model.LiveAnchorLiteralHabit) model.LiveAgentPlanAnchorStyleProfile {
	return Normalize(model.LiveAgentPlanAnchorStyleProfile{Delivery: &model.LiveAnchorDeliverySpec{Version: Version, Instructions: []string{"按样本组织短句", "事实解释适当展开", "句尾沿用有证据原词", "称呼用于引起注意", "自称保留原词", "转场沿用样本句式", "短答先回答问题", "严肃场景不促销"}, Habits: habits, SampleChars: 999999, Rulebook: "伪造规范"}}, source)
}

func TestLiteralGroundingAndStatistics(t *testing.T) {
	source := "我们家讲清楚哟。我们家再说明哟。我们家慢慢讲哟。"
	p := fixture(source, model.LiveAnchorLiteralHabit{Kind: "self_address", Text: "我们家", Count: 999}, model.LiveAnchorLiteralHabit{Kind: "particle", Text: "哟"}, model.LiveAnchorLiteralHabit{Kind: "catchphrase", Text: "欢迎回家"}, model.LiveAnchorLiteralHabit{Kind: "catchphrase", Text: "1号链接"})
	if len(p.Delivery.Habits) != 2 || p.Delivery.Habits[0].Count != 3 || p.Delivery.SentenceCount != 3 || p.Delivery.SampleChars == 999999 || p.Delivery.Rulebook == "伪造规范" {
		t.Fatalf("ungrounded spec: %+v", p.Delivery)
	}
	if issues := CoverageErrors(p, source); len(issues) > 0 {
		t.Fatal(issues)
	}
	p.Delivery.Habits = p.Delivery.Habits[:1]
	if issues := CoverageErrors(p, source); len(issues) != 1 {
		t.Fatalf("particle omission was not rejected: %v", issues)
	}
}

func TestDifferentSamplesNeverInheritVocabulary(t *testing.T) {
	source := "先看这个现象。接着解释原因。最后总结结论。酒吧里有酒吧文化，哈密瓜就是哈密瓜。酒吧、哈密瓜。"
	p := fixture(source, model.LiveAnchorLiteralHabit{Kind: "connector", Text: "接着"}, model.LiveAnchorLiteralHabit{Kind: "particle", Text: "哟"}, model.LiveAnchorLiteralHabit{Kind: "self_address", Text: "我们家"})
	if len(p.Delivery.Habits) != 1 || len(CoverageErrors(p, source)) != 0 {
		t.Fatalf("merchant style leaked: %+v", p)
	}
	if strings.Contains(Render(p), "我们家") || strings.Contains(Render(p), "哟") {
		t.Fatal("fixed speaker vocabulary")
	}
	if !CheckLongText(p, strings.Repeat("先说明现象，再给大家解释依据。", 15)).Passed {
		t.Fatal("quiet speaker must not need sales vocabulary")
	}
}

func TestLongAndShortContextsAndSelfAddress(t *testing.T) {
	source := strings.Repeat("我们家解释清楚哟，朋友们，接着慢慢讲。", 10)
	p := fixture(source, model.LiveAnchorLiteralHabit{Kind: "self_address", Text: "我们"}, model.LiveAnchorLiteralHabit{Kind: "self_address", Text: "我们家"}, model.LiveAnchorLiteralHabit{Kind: "particle", Text: "哟"}, model.LiveAnchorLiteralHabit{Kind: "audience_address", Text: "朋友们"})
	bad := strings.Repeat("我们解释清楚，朋友们，接着慢慢讲哟。", 10)
	if CheckLongText(p, bad).Passed {
		t.Fatal("generic self address replaced literal phrase")
	}
	if !CheckLongText(p, source).Passed {
		t.Fatal("grounded example rejected")
	}
	if !CheckLongText(p, "好的，接着给您解释。").Passed {
		t.Fatal("short interaction forced to include every habit")
	}
}

func TestInvalidContractVersionRejected(t *testing.T) {
	p := Normalize(model.LiveAgentPlanAnchorStyleProfile{Delivery: &model.LiveAnchorDeliverySpec{Version: "other/v999"}}, "样本")
	if Valid(p) || p.Delivery != nil {
		t.Fatal("unsupported version accepted")
	}
}

func TestPureStyleCompilerRejectsContentStrategy(t *testing.T) {
	source := "哥哥姐姐们啊，我们家先讲清楚。对呀，你看嘛，然后换个说法。哥哥姐姐们，我们家再讲一遍。新粉跟着老粉丝走，一定不会让你失望的。都买好了不？"
	raw := model.LiveAgentPlanAnchorStyleProfile{Delivery: &model.LiveAnchorDeliverySpec{
		Version: Version,
		Instructions: []string{
			"主线用短句说明一个重点，再用补充句换角度解释",
			"称呼放在段首或转场处，不要连续堆叠",
			"主播自称放在解释句中，自然承担陈述主体",
			"句尾语气词只在自然停顿处出现",
			"连接词用于因果、递进和换角度",
			"确认短句独立出现，随后回到当前说明",
			"短互动先回答核心问题，再补一句必要说明",
			"严肃答复改用克制短句并减少语气词",
			"开场先说明紧迫理由，再给行动指令",
			"用新粉跟着老粉丝走建立社会证明",
			"段尾说一定不会让你失望并追问都买好了不",
			"同一重点换词复述，如“很香”换成“香味浓”",
		},
		Habits: []model.LiveAnchorLiteralHabit{
			{Kind: "audience_address", Text: "哥哥姐姐们", Position: "句首", When: "段首提醒注意"},
			{Kind: "self_address", Text: "我们家", Position: "句中", When: "解释当前内容"},
			{Kind: "connector", Text: "然后", Position: "句首", When: "切换说明角度"},
			{Kind: "catchphrase", Text: "对呀", Position: "句首", When: "确认前句"},
			{Kind: "catchphrase", Text: "一定不会让你失望的", Position: "句尾", When: "给新用户信心"},
			{Kind: "catchphrase", Text: "新粉跟着老粉丝走", Position: "句中", When: "降低犹豫"},
			{Kind: "catchphrase", Text: "都买好了不", Position: "句尾", When: "确认购买状态"},
		},
	}}
	report := AssessPurity(raw)
	if report.Passed || len(report.Issues) < 7 {
		t.Fatalf("content strategy was not detected: %+v", report)
	}
	normalized := Normalize(raw, source)
	if !Valid(normalized) {
		t.Fatalf("pure rules should remain executable: %+v", normalized.Delivery)
	}
	rendered := Render(normalized)
	for _, forbidden := range []string{"新粉跟着老粉丝走", "一定不会让你失望", "都买好了不", "紧迫理由", "行动指令", "很香", "香味浓"} {
		if strings.Contains(rendered, forbidden) {
			t.Fatalf("content-bearing phrase leaked into compiler output: %q\n%s", forbidden, rendered)
		}
	}
	for _, required := range []string{"哥哥姐姐们", "我们家", "然后", "对呀"} {
		if !strings.Contains(rendered, required) {
			t.Fatalf("pure style marker was lost: %q\n%s", required, rendered)
		}
	}
	if len(normalized.CandidatePatterns) != 0 || len(normalized.ReusableRules) != len(normalized.Delivery.Instructions) {
		t.Fatalf("free-form display rules survived canonical compilation: %+v", normalized)
	}
	for _, rule := range normalized.ReusableRules {
		if strings.Contains(rule, "行动建议") || strings.Contains(rule, "表妹") {
			t.Fatalf("model-authored display prose survived: %q", rule)
		}
	}
}

func TestNormalizeDerivesRepeatedSentenceFinalParticles(t *testing.T) {
	source := "先把重点说清楚啊。这里换个角度讲哟。你看嘛，确实是这样啊。接着往下说哟。最后再确认一下嘛。就是这个意思啊。再补一句哟。大家听明白了嘛。"
	profile := Normalize(model.LiveAgentPlanAnchorStyleProfile{Delivery: &model.LiveAnchorDeliverySpec{
		Version: Version,
	}}, source)
	if !Valid(profile) {
		t.Fatalf("derived style contract should be valid: %+v", profile.Delivery)
	}
	want := map[string]int{"啊": 3, "哟": 3, "嘛": 3}
	for _, habit := range profile.Delivery.Habits {
		if habit.Kind != "particle" {
			continue
		}
		if count, ok := want[habit.Text]; ok {
			if habit.Count != count || habit.Position != "句尾" {
				t.Fatalf("particle %q=%+v", habit.Text, habit)
			}
			delete(want, habit.Text)
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing derived particles: %v", want)
	}
	if issues := CoverageErrors(profile, source); len(issues) != 0 {
		t.Fatalf("derived particles should satisfy coverage: %v", issues)
	}
}

func TestNormalizeDerivesStableLiveAudienceAndSelfAddresses(t *testing.T) {
	source := strings.Repeat("哥哥姐姐们，我们家先把这一段说明白啊。", 8) +
		"叔叔阿姨们也听一下。新粉先看重点，老粉接着听，钻石老乡也在，我们自家再补一句。"
	profile := Normalize(model.LiveAgentPlanAnchorStyleProfile{Delivery: &model.LiveAnchorDeliverySpec{
		Version: Version,
	}}, source)
	if !Valid(profile) {
		t.Fatalf("derived live style contract should be valid: %+v", profile.Delivery)
	}
	habits := map[string]model.LiveAnchorLiteralHabit{}
	for _, habit := range profile.Delivery.Habits {
		habits[habit.Kind+":"+habit.Text] = habit
	}
	for _, expected := range []string{
		"audience_address:哥哥姐姐们",
		"audience_address:叔叔阿姨们",
		"audience_address:新粉",
		"audience_address:老粉",
		"audience_address:老乡",
		"self_address:我们家",
		"self_address:我们自家",
	} {
		if _, ok := habits[expected]; !ok {
			t.Fatalf("stable live lexical habit was omitted: %s\n%+v", expected, profile.Delivery.Habits)
		}
	}
	if got := habits["audience_address:哥哥姐姐们"].Count; got != 8 {
		t.Fatalf("audience address count=%d, want 8", got)
	}
	if got := habits["self_address:我们家"].Count; got != 8 {
		t.Fatalf("self address count=%d, want 8", got)
	}
	rendered := Render(profile)
	for _, forbidden := range []string{"样本没有稳定观众称呼", "样本没有稳定主播方自指"} {
		if strings.Contains(rendered, forbidden) {
			t.Fatalf("false negative rule survived: %q\n%s", forbidden, rendered)
		}
	}
	for _, expected := range []string{"观众称呼", "主播方自称/自指", "跨商家、跨主播", "按样本密度"} {
		if !strings.Contains(rendered, expected) {
			t.Fatalf("live address rule missing %q:\n%s", expected, rendered)
		}
	}
}

func TestNormalizeDoesNotInventLiveAddressesForQuietSample(t *testing.T) {
	source := "先说明现象，再解释原因，最后总结。"
	profile := Normalize(model.LiveAgentPlanAnchorStyleProfile{Delivery: &model.LiveAnchorDeliverySpec{Version: Version}}, source)
	for _, habit := range profile.Delivery.Habits {
		if habit.Kind == "audience_address" || habit.Kind == "self_address" {
			t.Fatalf("invented live address for quiet sample: %+v", habit)
		}
	}
}

func TestCompilerAdmitsOnlyGroundedPureExpressionDimensions(t *testing.T) {
	source := "先把重点说清楚。为什么这么讲？我再换个顺序说一遍。最后把重点收回来。"
	profile := Normalize(model.LiveAgentPlanAnchorStyleProfile{
		Dimensions: []model.LiveAgentPlanAnchorStyleDimension{
			{Key: "repetition_strategy", Rule: "同一重点先直述，再问后自答，隔开后换序重述", Level: "高", Confidence: "high", EvidenceQuotes: []string{"为什么这么讲？"}},
			{Key: "transition_style", Rule: "每段都假装收到弹幕再转场", Level: "高", Confidence: "high", EvidenceQuotes: []string{"最后把重点收回来"}},
			{Key: "sentence_rhythm", Rule: "连续使用等长短句", Level: "高", Confidence: "high", EvidenceQuotes: []string{"原文没有的证据"}},
			{Key: "interaction_style", Rule: "主动决定何时打断主线", Level: "高", Confidence: "high", EvidenceQuotes: []string{"先把重点说清楚"}},
		},
		Delivery: &model.LiveAnchorDeliverySpec{Version: Version},
	}, source)
	rendered := Render(profile)
	if !strings.Contains(rendered, "同一重点先直述，再问后自答，隔开后换序重述") {
		t.Fatalf("grounded repetition rule missing:\n%s", rendered)
	}
	for _, forbidden := range []string{"假装收到弹幕", "连续使用等长短句", "主动决定何时打断主线"} {
		if strings.Contains(rendered, forbidden) {
			t.Fatalf("untrusted dimension entered rulebook: %q\n%s", forbidden, rendered)
		}
	}
}

func TestCompilerAllowsControlledFactRecurrence(t *testing.T) {
	profile := Normalize(model.LiveAgentPlanAnchorStyleProfile{Delivery: &model.LiveAnchorDeliverySpec{Version: Version}}, "先说明，再换个说法，最后收回来。")
	rendered := Render(profile)
	for _, expected := range []string{"不同口播轮次回环出现", "问后自答", "fact_expansion用户授权决定", "事实有限时允许围绕同一正式事实做多轮口语展开"} {
		if !strings.Contains(rendered, expected) {
			t.Fatalf("controlled expansion rule missing %q:\n%s", expected, rendered)
		}
	}
}
