package stylecontract

import (
	"context"
	"strings"
	"testing"

	"livecompanion/management/internal/model"
)

type vectorFixture struct{}

func (vectorFixture) Enabled() bool { return true }
func (vectorFixture) Model() string { return "shadow-fixture" }
func (vectorFixture) Embed(_ context.Context, texts []string) ([][]float32, error) {
	result := make([][]float32, len(texts))
	for index, text := range texts {
		if strings.Contains(text, "平均分句=11") {
			result[index] = []float32{1, 0}
		} else {
			result[index] = []float32{0, 1}
		}
	}
	return result, nil
}

func TestStyleVectorSignatureContainsNoBusinessText(t *testing.T) {
	source := "哥哥姐姐们，1号链接69块9，今天菜籽油直接拍。我们家再讲一句哟！"
	profile := Normalize(model.LiveAgentPlanAnchorStyleProfile{Delivery: &model.LiveAnchorDeliverySpec{
		Version: Version,
		Instructions: []string{
			"短句说明一个重点", "补充句换角度", "称呼用于转场", "自称放在解释句中",
			"语气词放句尾", "连接词表达递进", "短答先回答", "严肃场景减少语气词",
		},
		Habits: []model.LiveAnchorLiteralHabit{
			{Kind: "audience_address", Text: "哥哥姐姐们"},
			{Kind: "self_address", Text: "我们家"},
			{Kind: "particle", Text: "哟"},
		},
	}}, source)
	signature := BuildStyleVectorSignature(profile, source)
	for _, leaked := range []string{"菜籽油", "69块9", "1号链接", "直接拍", "哥哥姐姐们", "我们家"} {
		if strings.Contains(signature, leaked) {
			t.Fatalf("business or literal text leaked into vector signature: %q in %s", leaked, signature)
		}
	}
	for _, wanted := range []string{"平均分句", "观众称呼密度", "主播自称密度", "语气词密度"} {
		if !strings.Contains(signature, wanted) {
			t.Fatalf("style feature missing: %q in %s", wanted, signature)
		}
	}
}

func TestStyleVectorEvaluationIsShadowOnly(t *testing.T) {
	profile := model.LiveAgentPlanAnchorStyleProfile{}
	result := EvaluateStyleVectorShadow(context.Background(), vectorFixture{}, profile, "短句。再一句。", "这是明显更长的一段解释内容，它会形成不同的节奏结构。")
	if !result.ShadowOnly || !result.Available || result.Model != "shadow-fixture" {
		t.Fatalf("unexpected shadow result: %+v", result)
	}
	if result.Score < 0 || result.Score > 100 {
		t.Fatalf("invalid score: %+v", result)
	}
}
