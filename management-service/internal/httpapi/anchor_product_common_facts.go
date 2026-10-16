package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/anchorcycle"
	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
)

const productCommonMaterialVersion = "product-common-v1"

func (s *Server) productCommonIndustrySignature(ctx context.Context, tenant, plan int64) (string, error) {
	settings, err := s.store.GetPlanBusinessSettings(ctx, tenant, plan)
	if err != nil {
		return "", err
	}
	industry := strings.TrimSpace(settings.Industries[settings.BizDomain])
	var dimensions string
	if industry != "" && industry != "general" {
		pack, e := loadIndustryPack(settings.BizDomain, industry)
		if e != nil {
			return "", e
		}
		dimensions = pack.SellingDimensions
	}
	return anchorcycle.ProductDigest([]string{settings.BizDomain, industry, dimensions}), nil
}

func productCommonIndustryMarker(signature string) string {
	return "<!-- product-common-industry:" + signature + " -->"
}

// Content judgments contain no program identifiers. The caller owns the source
// record and target product of each request, and preserves the complete original.
type productCommonJudgment func(context.Context, map[string]any) (string, error)

func productDescriptions(links []model.LiveAgentPlanProductLink) []string {
	var result []string
	for _, link := range links {
		if link.Status == "active" {
			result = append(result, link.LinkKey+"："+link.ProductName+"；规格="+link.Spec+"；销售价="+link.DailyPrice)
		}
	}
	return result
}

func classifyProductCommonFact(ctx context.Context, block string, links []model.LiveAgentPlanProductLink, judge productCommonJudgment) (anchorcycle.ScopedFact, error) {
	fact := scopedPlanProductFact(block)
	if len(fact.Links) > 0 { // Explicit program bindings remain authoritative.
		return fact, nil
	}
	raw, err := judge(ctx, map[string]any{"operation": "classify", "fact": block, "products": productDescriptions(links)})
	if err != nil {
		return fact, err
	}
	var output struct {
		Scope string `json:"归属"`
	}
	if err = decodeProductJSON(raw, &output); err != nil || (output.Scope != "公共" && output.Scope != "商品") {
		return fact, errors.New("事实归属判断未完成，请重试初始化")
	}
	fact.Shared = output.Scope == "公共"
	if fact.Shared {
		return fact, nil
	}
	for _, link := range links {
		if link.Status != "active" {
			continue
		}
		raw, err = judge(ctx, map[string]any{"operation": "applies", "fact": block, "focus_product": link.LinkKey + "：" + link.ProductName + "；规格=" + link.Spec, "products": productDescriptions(links)})
		if err != nil {
			return fact, err
		}
		var applies struct {
			Value *bool `json:"适用"`
		}
		if decodeProductJSON(raw, &applies) != nil || applies.Value == nil {
			return fact, errors.New("商品事实适用判断未完成，请重试初始化")
		}
		if *applies.Value {
			fact.Links = append(fact.Links, link.LinkKey)
		}
	}
	if len(fact.Links) == 0 {
		return fact, errors.New("商品事实未能对应当前商品，未丢弃原文，请核对后重新初始化")
	}
	return fact, nil
}

func classifyProductCommonFacts(ctx context.Context, facts string, links []model.LiveAgentPlanProductLink, judge productCommonJudgment) ([]anchorcycle.ScopedFact, error) {
	blocks := productFactBlocks(facts)
	result := make([]anchorcycle.ScopedFact, len(blocks))
	jobs := make(chan int)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var firstErr error
	for worker := 0; worker < 6; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				fact, err := classifyProductCommonFact(ctx, blocks[i], links, judge)
				if err != nil {
					mu.Lock()
					if firstErr == nil {
						firstErr = err
					}
					mu.Unlock()
					continue
				}
				fact.ID = "fact:" + derivedHash(blocks[i])
				result[i] = fact
			}
		}()
	}
	for i := range blocks {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if firstErr != nil {
		return nil, firstErr
	}
	return result, nil
}

func (s *Server) resolveProductCommonFacts(ctx context.Context, tenant, plan int64, facts string, links []model.LiveAgentPlanProductLink, profile string) ([]anchorcycle.ScopedFact, error) {
	mother, _, err := loadReplaceablePromptAsset("ANCHOR_PRODUCT_COMMON_FACT_PROMPT_PATH", "configs/prompts/anchor-style/product-common-fact-classification.md", "商品公共事实归属母版")
	if err != nil {
		return nil, err
	}
	p0, _, err := loadP0Mother()
	if err != nil {
		return nil, err
	}
	pack, err := s.loadIndustryPackForPlan(ctx, tenant, plan)
	if err != nil {
		return nil, err
	}
	if pack != nil {
		mother += "\n\n# 当前行业额外边界\n" + pack.P0Extra
	}
	key := anchorcycle.ProductDigest(map[string]any{"version": productCommonMaterialVersion, "facts": facts, "products": productDescriptions(links), "mother": mother, "p0": p0})
	raw, err := s.store.GetPlanAnalysisAsset(ctx, tenant, plan, "product_common_facts", key)
	var result []anchorcycle.ScopedFact
	if err == nil {
		if json.Unmarshal([]byte(raw), &result) != nil {
			return nil, errors.New("已保存事实归属不可读取，请重新初始化")
		}
	} else {
		if !errors.Is(err, appdb.ErrPlanAnalysisAssetNotFound) {
			return nil, err
		}
		if profile == "" {
			settings, e := s.store.AnchorGenerationSettings(ctx)
			if e != nil {
				return nil, e
			}
			profile = settings.SellingPointProfileID
		}
		ctx = withExchangeScope(ctx, appdb.ModelExchangeScope{TenantID: tenant, PlanID: plan})
		result, err = classifyProductCommonFacts(ctx, facts, links, func(call context.Context, packet map[string]any) (string, error) {
			packet["effective_p0"] = generationP0View(p0, "*")
			data, _ := json.Marshal(packet)
			var last error
			for attempt := 0; attempt < 2; attempt++ {
				response, _, e := s.completeAnchorStyleWorkbench(call, profile, "product_common_fact_classification", agentgateway.ResponseJSON, []agentgateway.Message{{Role: "system", Content: mother}, {Role: "user", Content: string(data)}}, 300, false)
				if e == nil {
					return response.Text, nil
				}
				last = e
			}
			return "", last
		})
		if err != nil {
			return nil, err
		}
	}
	products := []anchorcycle.FocusProduct{}
	for _, link := range links {
		if link.Status == "active" {
			products = append(products, anchorcycle.FocusProduct{LinkKey: link.LinkKey})
		}
	}
	if err = validateProductFactSnapshot(facts, &anchorcycle.ProductFocus{Products: products, Facts: result}); err != nil {
		return nil, err
	}
	encoded, _ := json.Marshal(result)
	if err = s.store.SavePlanAnalysisAsset(ctx, tenant, plan, "product_common_facts", key, string(encoded)); err != nil {
		return nil, err
	}
	return result, nil
}

func classifiedProductFactPackage(link string, facts []anchorcycle.ScopedFact) string {
	var blocks []string
	for _, fact := range facts {
		if fact.Shared || containsLink(fact.Links, link) {
			blocks = append(blocks, fact.Text)
		}
	}
	return strings.Join(blocks, "\n")
}

func commonWheelPoints(wheel anchorStyleSellingPointWheel) ([]anchorcycle.FocusSellingPoint, error) {
	var points []anchorcycle.FocusSellingPoint
	seenAngles := map[string]bool{}
	for _, item := range wheel.Items {
		if item.Scope == "" {
			return nil, fmt.Errorf("卖点“%s”缺少商品/公共归属，请重新初始化直播智能体", item.Name)
		}
		if item.Scope != "公共" {
			continue
		}
		if len(item.Angles) == 0 {
			continue
		}
		point := anchorcycle.FocusSellingPoint{Name: item.Name, Tier: item.Tier, Weight: 1, TargetAudience: derefString(item.TargetAudience), PainPoint: derefString(item.PainPoint)}
		for _, angle := range item.Angles {
			key := strings.Join(strings.Fields(angle), " ")
			if seenAngles[key] {
				continue
			}
			seenAngles[key] = true
			point.Angles = append(point.Angles, anchorcycle.FocusSellingAngle{Angle: angle, Weight: 1})
		}
		if len(point.Angles) > 0 {
			points = append(points, point)
		}
	}
	return points, nil
}

func productCommonReadyKey(wheel model.LiveSellingPointWheelAsset, links []model.LiveAgentPlanProductLink) string {
	var identities []string
	for _, link := range links {
		if link.Status == "active" {
			identities = append(identities, fmt.Sprintf("%d:%s", link.ID, link.LinkKey))
		}
	}
	return anchorcycle.ProductDigest(map[string]any{"version": productCommonMaterialVersion, "wheel": wheel.ID, "facts": wheel.FactPackageSHA256, "products": identities})
}
