package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"livecompanion/management/internal/agentgateway"
	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
)

// Automatic scripts only receive confirmed, unconditional facts. In particular,
// an exception cannot be silently dropped while turning a claim into copy.
func autoScriptClaimUsable(c model.PlanMaterialClaim) bool {
	return (c.Status == "" || c.Status == "active") &&
		(c.Destination == "" || c.Destination == "fact") &&
		strings.TrimSpace(c.Content) != "" &&
		!c.NeedsConfirmation && !c.NeedsSplit && len(c.RiskFlags) == 0 &&
		len(c.Risks) == 0 && len(c.P0Hits) == 0 &&
		strings.TrimSpace(c.ConditionIssue) == "" && strings.TrimSpace(c.PolarityIssue) == ""
}

func autoScriptNonEmptyJSON(raw json.RawMessage) bool {
	text := strings.TrimSpace(string(raw))
	if text == "" || text == "null" || text == "[]" || text == "{}" || text == `""` {
		return false
	}
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return true
	}
	switch v := value.(type) {
	case []any:
		return len(v) > 0
	case map[string]any:
		return len(v) > 0
	case string:
		return strings.TrimSpace(v) != ""
	default:
		return value != nil
	}
}

func autoScriptClaimObjects(c model.PlanMaterialClaim, labels map[int64]string) []string {
	if len(c.ObjectTargets) > 0 {
		out := []string{}
		for _, target := range c.ObjectTargets {
			switch target.Kind {
			case "plan", "global":
				return []string{"全局"}
			case "product", "link", "voucher":
				if label := labels[target.LinkID]; label != "" && !autoScriptContains(out, label) {
					out = append(out, label)
				}
			}
		}
		return out
	}
	if c.ProductLinkID > 0 {
		if label := labels[c.ProductLinkID]; label != "" {
			return []string{label}
		}
		return nil
	}
	if c.ScopeKind == "" || c.ScopeKind == "plan" || c.ScopeKind == "global" {
		return []string{"全局"}
	}
	return nil
}

func autoScriptClaimIsProductSpecific(c model.PlanMaterialClaim, linkID int64) bool {
	if len(c.ObjectTargets) > 0 {
		for _, target := range c.ObjectTargets {
			if (target.Kind == "product" || target.Kind == "link" || target.Kind == "voucher") && target.LinkID == linkID {
				return true
			}
		}
		return false
	}
	return c.ProductLinkID == linkID
}

func autoScriptSelectedClaimLabels(c model.PlanMaterialClaim, selected []string, links []planAutoLink) []string {
	labels := map[int64]string{}
	for _, link := range links {
		labels[link.ID] = link.Label
	}
	out := []string{}
	for _, label := range autoScriptClaimObjects(c, labels) {
		if label == "全局" {
			return []string{"全局"}
		}
		if autoScriptContains(selected, label) {
			out = append(out, label)
		}
	}
	return out
}

func autoScriptContains(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

func autoScriptCatalogRelevant(item planAutoCatalogItem, objects []string) bool {
	return item.Object == "全局" || autoScriptContains(objects, item.Object)
}

func autoScriptRelevantAttributes(attrs []string, catalog []planAutoCatalogItem, objects []string) []string {
	out := make([]string, 0, len(attrs))
	for _, attr := range attrs {
		for _, item := range catalog {
			if item.Attribute == attr && autoScriptCatalogRelevant(item, objects) {
				out = append(out, attr)
				break
			}
		}
	}
	return out
}

func autoScriptValidatePlan(plan planAutoScriptPlan, catalog []planAutoCatalogItem, links []planAutoLink) error {
	if len(plan.Objects) == 0 || len(plan.Attributes) == 0 {
		return fmt.Errorf("没有适用的对象或属性")
	}
	for _, requirement := range plan.WritingRequirements {
		for _, link := range links {
			if strings.Contains(requirement, link.Label) && !autoScriptContains(plan.Objects, link.Label) {
				return fmt.Errorf("写作要求引用了未选商品")
			}
		}
		for _, item := range catalog {
			if !autoScriptCatalogRelevant(item, plan.Objects) {
				continue
			}
			name := strings.TrimSpace(item.Name)
			mentioned := strings.Contains(requirement, item.Attribute) || (utf8.RuneCountInString(name) >= 4 && strings.Contains(requirement, name))
			if mentioned && !autoScriptContains(plan.Attributes, item.Attribute) {
				return fmt.Errorf("写作要求引用了未选属性")
			}
		}
	}
	return nil
}

func autoScriptRecentDirections(texts []string, links []planAutoLink) string {
	if len(texts) == 0 {
		return ""
	}
	if len(texts) > 6 {
		texts = texts[len(texts)-6:]
	}
	lines := []string{}
	for _, raw := range texts {
		opening := autoScriptOpening(raw)
		if opening == "" {
			continue
		}
		objects := autoScriptObjectsInText(raw, links)
		lines = append(lines, fmt.Sprintf("- 开场：%s；对象组合：%s", opening, strings.Join(objects, "、")))
	}
	if len(lines) == 0 {
		return ""
	}
	return "\n近期已用方向，避免重复开场和对象组合：\n" + strings.Join(lines, "\n") + "\n"
}

func autoScriptOpening(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	for _, sep := range []string{"。", "！", "？", "!", "?", "\n"} {
		if i := strings.Index(raw, sep); i >= 0 {
			raw = raw[:i]
		}
	}
	runes := []rune(strings.TrimSpace(raw))
	if len(runes) > 45 {
		runes = runes[:45]
	}
	return string(runes)
}

func autoScriptObjectsInText(raw string, links []planAutoLink) []string {
	out := []string{}
	for _, link := range links {
		if strings.Contains(raw, link.Label) {
			out = append(out, link.Label)
		}
	}
	sort.Strings(out)
	return out
}

func autoScriptRepeatedDirection(plan planAutoScriptPlan, recent []string, links []planAutoLink) bool {
	hook := strings.TrimSpace(plan.Hook)
	currentObjects := append([]string(nil), plan.Objects...)
	sort.Strings(currentObjects)
	for _, raw := range recent {
		opening := autoScriptOpening(raw)
		if hook != "" && opening != "" && (strings.HasPrefix(opening, hook) || strings.HasPrefix(hook, opening)) {
			return true
		}
		if len(links) > 1 && len(currentObjects) > 0 && strings.Join(currentObjects, "|") == strings.Join(autoScriptObjectsInText(raw, links), "|") {
			return true
		}
	}
	return false
}

var autoScriptNumber = regexp.MustCompile(`[0-9]+(?:\.[0-9]+)?`)

func autoScriptCheckLiteralFacts(body, facts string, objects []string, links []planAutoLink) error {
	allowed := map[string]bool{}
	for _, token := range autoScriptNumber.FindAllString(facts, -1) {
		allowed[token] = true
	}
	for _, token := range autoScriptNumber.FindAllString(body, -1) {
		if !allowed[token] {
			return fmt.Errorf("正文出现资料之外的数字")
		}
	}
	for _, link := range links {
		if !autoScriptContains(objects, link.Label) && strings.Contains(body, link.Label) {
			return fmt.Errorf("正文提到了未选商品")
		}
	}
	return nil
}

func (s *Server) autoScriptComplete(ctx context.Context, tenant, planID int64, gateway *agentgateway.Gateway, request agentgateway.Request) (agentgateway.Response, error) {
	profile, _, err := gateway.ResolveStageModel(ctx, request.Stage)
	if err != nil {
		return agentgateway.Response{}, err
	}
	if !strings.HasPrefix(profile, "anchor:") {
		return agentgateway.Response{}, fmt.Errorf("审核模型未配置")
	}
	profile = strings.TrimPrefix(profile, "anchor:")
	callCtx := withExchangeScope(ctx, appdb.ModelExchangeScope{TenantID: tenant, PlanID: planID})
	id, err := s.beginModelExchange(callCtx, request.Stage, profile, request)
	if err != nil {
		return agentgateway.Response{}, err
	}
	started := time.Now()
	resp, callErr := gateway.Complete(callCtx, request)
	if err := s.finishModelExchange(callCtx, id, resp, resp.Text, started, resp.InputTokens, resp.OutputTokens, callErr); err != nil {
		return agentgateway.Response{}, err
	}
	return resp, nil
}

func (s *Server) autoScriptReviewFacts(ctx context.Context, tenant, planID int64, gateway *agentgateway.Gateway, facts, body string) error {
	request := agentgateway.Request{
		Stage:          "script_auto_fact_review",
		Messages:       autoScriptMessages("你是直播文案事实审校员。只返回严格 JSON。", `只用【可用事实】审查【正文】。客观断言（商品特性、工艺、效果、价格、数字、时效、对象、福利、条件）必须有对应事实支持；不得把一个链接的事实移给另一个链接。主观感受和普通衔接语可以保留。没有依据或与事实矛盾时 supported=false，并逐条列出正文原句；不许用常识替正文补证据。输出 {"supported":true,"unsupported":[]}。`+"\n【可用事实】\n"+facts+"\n【正文】\n"+body),
		ResponseFormat: agentgateway.ResponseJSON, MaxTokens: 700, Timeout: 180 * time.Second,
	}
	resp, err := s.autoScriptComplete(ctx, tenant, planID, gateway, request)
	if err != nil {
		return fmt.Errorf("review_call: %w", err)
	}
	var verdict struct {
		Supported   bool     `json:"supported"`
		Unsupported []string `json:"unsupported"`
	}
	if err := autoScriptJSON(resp.Text, &verdict); err != nil {
		return fmt.Errorf("review_parse: %w", err)
	}
	if !verdict.Supported || len(verdict.Unsupported) > 0 {
		return fmt.Errorf("unsupported_claims=%d", len(verdict.Unsupported))
	}
	return nil
}

func autoScriptFailureStage(err error) string {
	if err == nil {
		return ""
	}
	stage := strings.SplitN(err.Error(), ":", 2)[0]
	if strings.HasPrefix(stage, "writer_fact_review") {
		return "writer_fact_review"
	}
	if strings.HasPrefix(stage, "writer_fact_literal") {
		return "writer_fact_literal"
	}
	for _, known := range []string{"planner_call", "planner_parse", "planner_hook_redline", "planner_repeated_direction", "planner_inconsistent", "facts_empty", "writer_call", "writer_parse", "writer_redline"} {
		if stage == known {
			return stage
		}
	}
	return "data_read"
}

func autoScriptPublicError(err error) string {
	switch autoScriptFailureStage(err) {
	case "facts_empty", "planner_inconsistent":
		return "当前方案缺少适用且已确认的事实，请检查语义库后重试"
	case "planner_hook_redline", "writer_redline", "writer_fact_literal", "writer_fact_review":
		return "生成内容未通过事实或红线核验，原话术已保留，请重试"
	case "planner_repeated_direction":
		return "生成方向与近期话术重复，原话术已保留，请重试"
	default:
		return "自动话术生成失败，原话术已保留，请稍后重试"
	}
}
