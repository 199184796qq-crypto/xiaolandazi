package httpapi

import (
	"context"
	"log"
	"strings"
	"time"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/contentstrategy"
	"livecompanion/management/internal/domaincard"
	"livecompanion/management/internal/model"
)

// buildPlanDomainCard turns the approved facts into an invisible, versioned
// map from audience question dimensions to fact IDs. It is best-effort: a
// model/parse failure returns nil and leaves the old retrieval path untouched.
func (s *Server) buildPlanDomainCard(ctx context.Context, safety model.LiveDerivedSafetyAsset, factsText string) *domaincard.Card {
	if s == nil || s.store == nil || strings.TrimSpace(factsText) == "" {
		factsText = safety.SafeFacts
	}
	if strings.TrimSpace(factsText) == "" {
		return nil
	}
	parsed := contentstrategy.Facts(factsText)
	if len(parsed) < 2 {
		return nil
	}
	facts := make([]domaincard.Fact, 0, len(parsed))
	var factLines strings.Builder
	for _, fact := range parsed {
		text := strings.TrimSpace(strings.ReplaceAll(fact.Text, "\n", "；"))
		if text == "" {
			continue
		}
		facts = append(facts, domaincard.Fact{ID: fact.ID, Text: text})
		factLines.WriteString(fact.ID)
		factLines.WriteString(" ")
		factLines.WriteString(text)
		factLines.WriteString("\n")
	}
	if len(facts) < 2 {
		return nil
	}
	roleID, err := s.store.ResolveTextRole(ctx, "interaction_purpose_resolution")
	if err != nil {
		return nil
	}
	profile, err := s.store.ResolveOperatorSpeechModel(ctx, roleID)
	if err != nil || profile == nil || !profile.Enabled || profile.APIKey == "" {
		return nil
	}
	callCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	request := agentgateway.Request{
		Stage: "interaction_domain_card",
		Model: profile.Model,
		Messages: []agentgateway.Message{
			{Role: "system", Content: domainCardPrompt},
			{Role: "user", Content: "以下是本方案的审核事实，请生成领域卡：\n" + factLines.String()},
		},
		MaxTokens:      4000,
		ResponseFormat: agentgateway.ResponseJSON,
	}
	response, err := agentgateway.CompleteSpeechEndpoint(callCtx, *profile, request)
	if err != nil {
		log.Printf("domain card build failed plan=%d: %v", safety.PlanID, err)
		return nil
	}
	card, err := domaincard.Parse(response.Text)
	if err != nil {
		log.Printf("domain card parse failed plan=%d: %v", safety.PlanID, err)
		return nil
	}
	if err := domaincard.NormalizeAndValidate(card, facts); err != nil {
		log.Printf("domain card validate failed plan=%d: %v", safety.PlanID, err)
		return nil
	}
	domaincard.ExpandSharedFacts(card, facts)
	card.SourceSHA256 = strings.TrimSpace(safety.SourceSHA256)
	card.BuiltAt = time.Now().UTC().Format(time.RFC3339)
	return card
}

const domainCardPrompt = `你是直播问答的领域卡构建器。根据给出的审核事实，生成一张“问题维度 → 事实编号”的索引卡。
只输出 JSON，不要解释，不要输出 Markdown。

JSON 结构：
{"domain":"行业或品类","product":"主要商品","fulfillment":"发货方式，可空","dimensions":[{"name":"维度名","phrases":["观众可能怎么问"],"conditions":["这个维度需要判断的条件，如本地/外地、川渝/非川渝、偏远地区"],"fact_ids":["F001"]}]}

规则：
1. 每条事实必须至少出现在一个维度的 fact_ids 中，不能遗漏。
2. 含有“本地/外地、仅限、除…外、偏远、地区”等条件或例外的事实，conditions 必须保留，不能只写一个代表词。
3. phrases 是观众自然问法，每个维度 6–12 条；至少 3 条是 2–6 字短问法（如“抽真空”“包邮”“切块”“多久到”“什么时候到”“发什么快递”），不要只写长句；不要编造事实里没有的内容。
4. 维度名用中文自由命名，不要套固定分类；不同行业允许不同维度。
5. fact_ids 只能使用输入中真实存在的编号。
6. 相近事实可以合并到一个维度，但条件和例外不能合并掉。
7. 没有把握的事实放进“其他”维度，不要丢。`
