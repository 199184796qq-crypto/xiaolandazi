package httpapi

import (
	"strings"
	"testing"
)

func TestNormalizeAnchorStyleReviewJSON(t *testing.T) {
	raw := `{"overall_score":82,"dimensions":[{"name":"称呼体系","score":88,"evidence":["哥哥姐姐们"],"diagnosis":"称呼稳定"}],"strengths":["口语感强"],"deviations":["自指略少"],"feedback_prompt":"下一段自然补一次‘我们家’，其余节奏保持。"}`
	result, err := normalizeAnchorStyleReviewJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	if result.OverallScore != 82 || result.FeedbackPrompt == "" || len(result.Dimensions) != 1 {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestRenderAnchorStyleReviewRequestOnlyScoresStyle(t *testing.T) {
	prompt := renderAnchorStyleReviewRequest("风格规则", `{}`, anchorStyleWheelTask{SellingPoint: "新油", Angle: "年份"}, []string{"第一段"}, "最新段")
	for _, required := range []string{"风格规则", "新油 × 年份", "最近口播滑动窗口", "严格只评腔调风格"} {
		if !strings.Contains(prompt, required) {
			t.Fatalf("missing %q: %s", required, prompt)
		}
	}
}
