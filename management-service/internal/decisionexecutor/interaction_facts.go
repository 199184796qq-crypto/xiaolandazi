package decisionexecutor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"livecompanion/management/internal/contentpools"
	"livecompanion/management/internal/contentstrategy"
	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/domaincard"
	"livecompanion/management/internal/interactionpolicy"
	"livecompanion/management/internal/model"
	"log"
	"regexp"
	"strings"
	"time"
)

type interactionFactSnapshotStore interface {
	GetLiveDerivedSafety(context.Context, int64, int64, string) (model.LiveDerivedSafetyAsset, error)
}

type contentStrategyStore interface {
	GetLatestPlanContentStrategy(context.Context, int64, int64, string, time.Time) (string, error)
}

type currentInteractionFactsStore interface {
	CurrentInteractionFacts(context.Context, int64, int64, int64) (string, error)
}

type currentInteractionScopeStore interface {
	CurrentInteractionScopeVersion(context.Context, int64, int64, int64, string) (model.LiveAgentPlanVersion, error)
}
type expressionConstraintStore interface {
	CurrentSpeechExpressionConstraints(context.Context, int64, int64) (contentpools.Sources, error)
}

func (w *Worker) interactionFactSnapshot(ctx context.Context, session model.LiveRuntimeSession, planID int64, options ...any) (string, error) {
	var item *decisionItem
	skipStrategy := false
	for _, option := range options {
		switch value := option.(type) {
		case *decisionItem:
			item = value
		case bool:
			skipStrategy = value
		}
	}
	if item != nil && item.BusinessCoreOnly {
		if source, ok := w.store.(currentInteractionFactsStore); ok {
			facts, err := source.CurrentInteractionFacts(ctx, session.TenantID, session.RoomID, planID)
			if item != nil {
				item.FullFacts = facts
			}
			if err != nil {
				return "", err
			}
			scope := interactionFactScope{FullFacts: facts}
			if source, ok := w.store.(currentInteractionScopeStore); ok && isViewerQuestionAnswer(*item) && !skipStrategy {
				version, e := source.CurrentInteractionScopeVersion(ctx, session.TenantID, session.RoomID, planID, facts)
				if e != nil {
					return "", e
				}
				if version.PlanID != planID {
					return "", fmt.Errorf("互动测试不能混用另一方案的商品范围")
				}
				facts, scope = narrowInteractionFacts(version.GenerationContext, facts, strings.Join(item.SampleQuestions, "\n"))
				scope.Card = domaincard.FromGeneration(version.GenerationContext)
			}
			return w.interactionFactsGuidance(ctx, session, planID, facts, item, skipStrategy, scope)
		}
	}
	published, facts, err := w.interactionApprovedFacts(ctx, session, planID)
	if item != nil {
		item.FullFacts = facts
	}
	if err != nil || facts == "" {
		return "", err
	}
	scope := interactionFactScope{FullFacts: facts, Card: domaincard.FromGeneration(published.GenerationContext)}
	if item != nil && isViewerQuestionAnswer(*item) && !skipStrategy {
		facts, scope = narrowInteractionFacts(published.GenerationContext, facts, strings.Join(item.SampleQuestions, "\n"))
		scope.Card = domaincard.FromGeneration(published.GenerationContext)
	}
	return w.interactionFactsGuidance(ctx, session, planID, facts, item, skipStrategy, scope)
}

// Use the immutable approved package for both answer generation and fact checks.
// An absent reference is legacy data; an invalid reference must not fall back.
func (w *Worker) interactionApprovedFacts(ctx context.Context, session model.LiveRuntimeSession, planID int64) (model.LiveAgentPlanVersion, string, error) {
	source, ok := w.store.(interactionFactSnapshotStore)
	if !ok {
		return model.LiveAgentPlanVersion{}, "", nil
	}
	published, err := w.store.GetPublishedLiveAgentPlanVersionForRoom(ctx, session.TenantID, session.RoomID)
	if err != nil {
		return published, "", fmt.Errorf("读取本场正式事实版本失败: %w", err)
	}
	if published.PlanID != planID {
		return published, "", fmt.Errorf("本场方案已改变，不能混用商品事实")
	}
	value, hasSnapshot := published.GenerationContext["derived_safety"]
	if !hasSnapshot {
		return published, "", nil
	}
	ref, ok := value.(map[string]any)
	if !ok {
		return published, "", fmt.Errorf("本场正式事实引用无效")
	}
	hash, _ := ref["source_sha256"].(string)
	if strings.TrimSpace(hash) == "" {
		return published, "", fmt.Errorf("本场正式事实指纹缺失")
	}
	asset, err := source.GetLiveDerivedSafety(ctx, session.TenantID, planID, hash)
	if err != nil || strings.TrimSpace(asset.SafeFacts) == "" {
		return published, "", fmt.Errorf("本场审核事实不可用，停止生成商品回答")
	}
	if atomic, ok := published.GenerationContext["atomic_facts"].(string); ok && strings.TrimSpace(atomic) != "" {
		return published, atomic, nil
	}
	return published, asset.SafeFacts, nil
}

func (w *Worker) interactionFactsGuidance(ctx context.Context, session model.LiveRuntimeSession, planID int64, facts string, item *decisionItem, skipStrategy bool, scopes ...interactionFactScope) (string, error) {
	fullFacts := facts
	var scopeLinks []string
	if len(scopes) > 0 {
		fullFacts, scopeLinks = scopes[0].FullFacts, scopes[0].Links
		if item != nil {
			item.DomainCard = scopes[0].Card
			applyDomainAnswerScope(item)
		}
	}
	if source, ok := w.store.(expressionConstraintStore); ok {
		constraints, err := source.CurrentSpeechExpressionConstraints(ctx, session.TenantID, planID)
		if err != nil {
			return "", fmt.Errorf("读取用户采用的表达约束失败：%w", err)
		}
		fullFacts += contentpools.RenderCorrections(constraints)
		if len(scopeLinks) > 0 {
			selected := []contentpools.Product{}
			for _, product := range constraints.Products {
				for _, link := range scopeLinks {
					if product.Link == link || (interactionProductNumber(link) != "" && interactionProductNumber(product.Link) == interactionProductNumber(link)) {
						selected = append(selected, product)
						break
					}
				}
			}
			constraints.Products = selected
		}
		facts += contentpools.RenderCorrections(constraints)
	}
	raw, err := interactionpolicy.Read("interaction-facts-boundary.md")
	if err != nil {
		return "", err
	}
	result := "\n" + strings.ReplaceAll(string(raw), "{{FACTS}}", facts)
	if skipStrategy {
		return result, nil
	}
	if strategyStore, ok := w.store.(contentStrategyStore); ok {
		if control, err := strategyStore.GetLatestPlanContentStrategy(ctx, session.TenantID, planID, "", session.StartedAt); err == nil && contentstrategy.Disabled(control) {
			return result, nil
		}
		strategyRaw, strategyErr := strategyStore.GetLatestPlanContentStrategy(ctx, session.TenantID, planID, contentstrategy.SHA(fullFacts), session.StartedAt)
		if errors.Is(strategyErr, appdb.ErrPlanAnalysisAssetNotFound) {
			return result, nil
		}
		if strategyErr == nil {
			var snapshot *contentstrategy.Snapshot
			snapshot, strategyErr = contentstrategy.Parse(strategyRaw, fullFacts)
			if strategyErr == nil {
				var guidance string
				if item != nil && item.PurposeID != "" {
					recent := []string{}
					for _, played := range w.playedInteractionContent(ctx, session) {
						recent = append(recent, played.ContentAngle)
					}
					active := contentstrategy.ActiveBenefitFacts(snapshot.Facts, time.Now().UTC())
					var selected contentstrategy.Selection
					guidance, selected, strategyErr = snapshot.RenderSelection(contentstrategy.SelectionRequest{Mode: "互动", AllowedFacts: facts, Question: primaryQuestion(*item), PurposeID: item.PurposeID, Recent: recent, ActiveBenefit: len(active) > 0, ActiveFactIDs: active, IntentAngles: item.IntentAngles})
					if guidance != "" {
						item.SelectedContentPlan = &selected
					}
				} else {
					guidance, strategyErr = snapshot.Render("互动", facts)
				}
				if strategyErr == nil {
					return result + "\n\n" + guidance, nil
				}
			}
		}
		log.Printf("interaction content strategy fallback tenant=%d room=%d session=%d: %v", session.TenantID, session.RoomID, session.ID, strategyErr)
	}
	return result, nil
}

// applyDomainAnswerScope turns the matched card dimensions into a compact
// answer boundary: only the matched dimensions are allowed, the other
// dimensions must not be volunteered. No per-question text is written by hand.
func applyDomainAnswerScope(item *decisionItem) {
	if item == nil || item.DomainCard == nil || strings.TrimSpace(item.FullFacts) == "" {
		return
	}
	parsed := contentstrategy.Facts(item.FullFacts)
	all := make([]domaincard.Fact, 0, len(parsed))
	for _, fact := range parsed {
		all = append(all, domaincard.Fact{ID: fact.ID, Text: fact.Text})
	}
	context := strings.Join(item.SampleQuestions, " ")
	if item.PurposeID != "" {
		context += " " + item.PurposeID
	}
	if len(item.PurposeKnowledge) > 0 {
		context += " " + string(item.PurposeKnowledge)
	}
	context += " " + item.Title + " " + item.Summary + " " + item.ReplyHint + " " + item.ContentGoal
	for _, angle := range item.IntentAngles {
		context += " " + angle.Angle + " " + angle.Task
	}
	scope := domaincard.ResolveScope(context, item.DomainCard, all)
	if len(scope.Facts) == 0 {
		applyDerivedShippingScope(item)
		applyDerivedLinkScope(item)
		return
	}
	var b strings.Builder
	for _, fact := range scope.Facts {
		b.WriteString(fact.ID)
		b.WriteString(" ")
		b.WriteString(strings.TrimSpace(fact.Text))
		b.WriteString("\n")
	}
	item.ScopedFacts = strings.TrimSpace(b.String())
	item.AnswerScope = "只回答：" + strings.Join(scope.Allowed, "、")
	if len(scope.Forbidden) > 0 {
		item.AnswerScope += "；不要主动提：" + strings.Join(scope.Forbidden, "、")
	}
	applyDerivedShippingScope(item)
	applyDerivedLinkScope(item)
}

var answerLinkPattern = regexp.MustCompile(`([0-9]+)\s*号链接`)

// applyDerivedLinkScope resolves "N号链接还上吗" from the approved link record:
// if the link exists in facts, it is already listed; otherwise the promised
// fallback is to ask the control room to restock it. This is a program rule, not
// a per-question answer.
func applyDerivedLinkScope(item *decisionItem) {
	if item == nil || len(item.PurposeKnowledge) == 0 {
		return
	}
	var intent struct {
		Scope   string `json:"scope"`
		Purpose string `json:"purpose"`
	}
	if json.Unmarshal(item.PurposeKnowledge, &intent) != nil {
		return
	}
	text := strings.Join(item.SampleQuestions, " ") + " " + intent.Scope + " " + intent.Purpose
	match := answerLinkPattern.FindStringSubmatch(text)
	if len(match) < 2 {
		return
	}
	link := match[1]
	if !strings.Contains(text, "上") && !strings.Contains(text, "补货") && !strings.Contains(text, "在售") {
		return
	}
	facts := item.FullFacts
	if strings.TrimSpace(facts) == "" {
		facts = item.ScopedFacts
	}
	listed := false
	for _, line := range strings.Split(facts, "\n") {
		if strings.Contains(line, "链接="+link) {
			listed = true
			break
		}
	}
	conclusion := fmt.Sprintf("SYS-LINK %s号链接当前未上架，需要中控补货。", link)
	if listed {
		conclusion = fmt.Sprintf("SYS-LINK %s号链接已经上架，可以拍。", link)
	}
	if !strings.Contains(item.ScopedFacts, conclusion) {
		if strings.TrimSpace(item.ScopedFacts) != "" {
			item.ScopedFacts += "\n"
		}
		item.ScopedFacts += conclusion
	}
	item.AnswerScope += "；链接状态：" + conclusion
}

// applyDerivedShippingScope converts the explicit remote-region exception and
// the general free-shipping fact into a program conclusion. This is a rule over
// existing facts (remote set vs complement), not a per-city fact.
func applyDerivedShippingScope(item *decisionItem) {
	if item == nil || len(item.PurposeKnowledge) == 0 {
		return
	}
	var intent struct {
		Scope       string `json:"scope"`
		Constraints *struct {
			Region  string   `json:"region"`
			Include []string `json:"include"`
			Exclude []string `json:"exclude"`
		} `json:"constraints"`
	}
	if json.Unmarshal(item.PurposeKnowledge, &intent) != nil {
		return
	}
	region := strings.TrimSpace(intent.Scope)
	include, exclude := "", ""
	if intent.Constraints != nil {
		if strings.TrimSpace(intent.Constraints.Region) != "" {
			region = strings.TrimSpace(intent.Constraints.Region)
		}
		include = strings.Join(intent.Constraints.Include, " ")
		exclude = strings.Join(intent.Constraints.Exclude, " ")
	}
	question := strings.Join(item.SampleQuestions, " ")
	if region == "" || (!strings.Contains(question+" "+include, "包邮") && !strings.Contains(question, "运费")) {
		return
	}
	facts := item.FullFacts
	if strings.TrimSpace(facts) == "" {
		facts = item.ScopedFacts
	}
	if strings.TrimSpace(facts) == "" {
		return
	}
	general, remoteLine := false, ""
	for _, line := range strings.Split(facts, "\n") {
		if strings.Contains(line, "包邮") {
			general = true
		}
		if strings.Contains(line, "只能发空运") && strings.Contains(line, "海南") {
			remoteLine = line
		}
	}
	if !general || remoteLine == "" {
		return
	}
	for _, part := range strings.FieldsFunc(remoteLine, func(r rune) bool {
		return r == '、' || r == '，' || r == ',' || r == ' ' || r == '；' || r == ';'
	}) {
		if strings.TrimSpace(part) == region || strings.Contains(part, region) {
			return
		}
	}
	conclusion := fmt.Sprintf("SYS-REGION %s不属于事实里的偏远地区名单；当前活动包邮，因此%s包邮，不需要补空运费。", region, region)
	if !strings.Contains(item.ScopedFacts, conclusion) {
		if strings.TrimSpace(item.ScopedFacts) != "" {
			item.ScopedFacts += "\n"
		}
		item.ScopedFacts += conclusion
	}
	item.AnswerScope += "；地区结论：" + conclusion
	if strings.Contains(exclude, "空运") || strings.Contains(exclude, "运费") {
		item.AnswerScope += "；不要主动提：空运、补运费"
	}
}
