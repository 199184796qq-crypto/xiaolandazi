package httpapi

import (
	"strings"
	"testing"
)

func TestProductSupplementPayloadPreservesFullP0AndConditionalFacts(t *testing.T) {
	p0 := "最高规则完整正文\n仅在满足条件时适用，允许的例外不可省略。"
	for _, text := range []string{"通常下单后第二天上午发货", "配件只适配A型号，B型号不适配", "服务须提前一天预约"} {
		input := speechMinerGuideInput{Message: text, Mode: "fact_gaps", Action: "answer", History: []speechMinerMessage{{Role: "user", Text: "补充资料"}}}
		payload := speechMinerGuidePayload(input, factCategoryCatalog{Categories: []factCategoryDefinition{{Value: "product"}}}, "当前方案："+text, nil, p0, "")
		if payload["effective_p0"] != p0 || payload["latest_message"] != text || payload["fact_package"] != "当前方案："+text || payload["current_wheel"] != nil {
			t.Fatal(payload)
		}
	}
}

func TestProductSupplementPromptDoesNotAuthorizeMiningOrPersistence(t *testing.T) {
	prompt, _, err := loadReplaceablePromptAsset("PRODUCT_SUPPLEMENT_CHAT_PROMPT_PATH", "configs/prompts/anchor-style/product-supplement-chat.md", "商品资料补充对话母版")
	if err != nil {
		t.Fatal(err)
	}
	for _, boundary := range []string{"effective_p0", "程序负责关联", "本接口只对话并整理候选，没有保存权限", "只从 latest_message 提取", "商品范围、条件及否定"} {
		if !strings.Contains(prompt, boundary) {
			t.Fatal(boundary)
		}
	}
}
