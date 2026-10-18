package anchorcycle

import (
	"encoding/json"
	"errors"
	"io"
	"strings"
)

// A plan is writing guidance, never a spoken paragraph or content approval.
// Directions are permanent plan assets; selection and usage are per-run memory.
type AnglePlan struct {
	GenerationID        string                   `json:"generation_id,omitempty"`
	GenerationCount     int                      `json:"generation_count,omitempty"`
	LifetimePlays       map[string]int           `json:"lifetime_plays,omitempty"`
	Cards               []AngleHook              `json:"hooks,omitempty"`
	GeneratedCards      []AngleHook              `json:"generated_hooks,omitempty"`
	DirectionSelections []DirectionSelection     `json:"direction_selections,omitempty"`
	DirectionReviews    []DirectionReviewReceipt `json:"direction_reviews,omitempty"`
	ReviewContextSHA    string                   `json:"review_context_sha256,omitempty"`
	ReviewCandidates    []AngleHook              `json:"review_candidates,omitempty"`
	Calculations        []HookCalculation        `json:"calculations,omitempty"`
	PlanError           string                   `json:"planning_error,omitempty"`
	AudienceConcern     string                   `json:"audience_concern"`
	Goal                string                   `json:"communication_goal"`
	Action              string                   `json:"explanation_action"`
	Hooks               []string                 `json:"hook_intents"`
	Raw                 string                   `json:"raw_response,omitempty"`
	SourceSHA           string                   `json:"source_sha256,omitempty"`
	PromptSHA           string                   `json:"prompt_sha256,omitempty"`
	Model               string                   `json:"model,omitempty"`
	InputTokens         int64                    `json:"input_tokens,omitempty"`
	OutputTokens        int64                    `json:"output_tokens,omitempty"`
}

// All relation keys are assigned by the program, never returned by the model.
type DirectionSelection struct {
	HookID     string `json:"hook_id"`
	Kept       bool   `json:"kept"`
	Reason     string `json:"reason"`
	ExchangeID int64  `json:"exchange_id,omitempty"`
}

func ParseAnglePlan(raw string) (*AnglePlan, error) {
	body := strings.TrimSpace(raw)
	if strings.HasPrefix(body, "```") {
		if i := strings.IndexByte(body, '\n'); i >= 0 && strings.HasSuffix(body, "```") {
			body = strings.TrimSpace(body[i+1 : len(body)-3])
		}
	}
	var p AnglePlan
	d := json.NewDecoder(strings.NewReader(body))
	if d.Decode(&p) != nil || d.Decode(new(any)) != io.EOF {
		return nil, errors.New("角度规划JSON无效")
	}
	p.AudienceConcern, p.Goal, p.Action = strings.TrimSpace(p.AudienceConcern), strings.TrimSpace(p.Goal), strings.TrimSpace(p.Action)
	if p.AudienceConcern == "" || p.Goal == "" || p.Action == "" || len(p.Hooks) == 0 || len(p.Hooks) > 32 {
		return nil, errors.New("角度规划缺少必要结构")
	}
	for i, h := range p.Hooks {
		p.Hooks[i] = strings.TrimSpace(h)
		if p.Hooks[i] == "" {
			return nil, errors.New("角度钩子意图为空")
		}
	}
	p.Raw = raw
	return &p, nil
}

func (s *State) AnglePlanKey() string {
	identity := ""
	if p := s.TextModel; p != nil {
		identity = p.ID + ":" + p.Model + ":" + p.BaseURL + ":" + p.SystemPrompt
	}
	task := s.Task()
	// Scheduling IDs, paragraph length and session mode do not change the content
	// question. Keep one pool for the same angle in all rooms using this plan.
	base := ProductDigest(struct {
		Point, Angle, Concern, Audience string
		Products                        []string
		Facts, Model, Mother, P0, Scope string
	}{task.SellingPoint, task.Angle, task.PainPoint, task.TargetAudience,
		s.HookProducts(), ProductDigest(s.HookFacts()), identity, s.AnglePlanMotherSHA,
		s.P0PolicyMD, s.Config.ComplianceScope})
	if len(s.EffectiveCorrections()) > 0 || task.FactScope != nil && len(task.FactScope.ProductBoundary) > 0 {
		var boundary any
		if task.FactScope != nil {
			boundary = task.FactScope.ProductBoundary
		}
		return ProductDigest(struct {
			Base            string
			Corrections     any
			ProductBoundary any
		}{base, s.EffectiveCorrections(), boundary})
	}
	return base
}
func (s *State) CurrentAnglePlan() *AnglePlan {
	if s.AnglePlanMotherSHA == "" {
		return nil
	}
	return s.AnglePlans[s.AnglePlanKey()]
}

// 通用切入侧面池（产品无关）。直播时按已采纳段数轮换，让同一事实每次从不同
// 侧面切入。侧面只决定「从哪个维度切入」，不含
// 任何商品词、数字或承诺；每个侧面的具体内容由下游结合本场事实展开（产品相关，
// 方案生成时注入）。换任何商品侧面池不变。
var fissionAngleSides = []string{
	"使用场景：什么场合、谁在什么情况下用得上",
	"过程细节：怎么一步步做出来、用了什么原料和步骤",
	"利益算账：划不划算、省了多少、性价比体现在哪",
	"对比参照：和常见做法或其他选择的差别在哪",
	"人群痛点：谁更需要、解决了什么具体顾虑",
	"品质来源：原料、产地、工艺的来源（只讲有事实的部分）",
	"使用方式：怎么吃、怎么保存、怎么复热",
}

// Normalize the existing industry pool without changing its content or mapping.
func schedulingSides(sides []string) []string {
	if len(sides) == 0 {
		sides = fissionAngleSides
	}
	out, seen := []string{}, map[string]bool{}
	for _, side := range sides {
		side = strings.TrimSpace(side)
		if side != "" && !seen[side] {
			out = append(out, side)
			seen[side] = true
		}
	}
	return out
}

// CurrentFissionSide balances each angle's existing pool, preferring a different
// side when equally used choices exist, and stays stable through retries. New tasks have different
// seeded starts instead of all starting at the first industry side.
func (s *State) CurrentFissionSide() string {
	task := s.Task()
	pool := schedulingSides(s.FissionSides)
	if len(pool) == 0 {
		return ""
	}
	// 只从「当前任务事实有专属支撑」的侧面里轮换。侧面选择此前与事实完全解耦，
	// 会轮换到事实撑不起的侧面（如特色菜角度轮换到「利益算账」），把主体事实
	// 全滤掉、只剩价格。先按专属事实筛出支撑集，保证侧面与事实匹配。
	supported := make([]string, 0, len(pool))
	for _, side := range pool {
		if s.SideHasDedicatedFact(side) {
			supported = append(supported, side)
		}
	}
	if len(supported) == 0 {
		// 没有任何侧面有专属事实支撑：返回空，HookFactsForSide 会回退全量事实，
		// 让模型拿到完整素材自己组织，而不是硬套一个侧面把事实筛没。
		return ""
	}
	pool = supported
	start := s.Used[task.Key].Count % len(pool)
	if s.RunID != "" {
		start = (start + int(schedulerSeed(s.RunID, task.Key)*float64(len(pool)))) % len(pool)
	}
	previous := ""
	if len(s.Accepted) > 0 {
		previous = FissionSideName(s.Accepted[len(s.Accepted)-1].Selected.FissionSide)
	}
	chosen, best := "", int(^uint(0)>>1)
	for offset := 0; offset < len(pool); offset++ {
		side := pool[(start+offset)%len(pool)]
		count := s.SideUses[task.Key][FissionSideName(side)]
		if count < best || count == best && FissionSideName(chosen) == previous && FissionSideName(side) != previous {
			best, chosen = count, side
		}
	}
	if chosen != "" {
		return chosen
	}
	return pool[start]
}

func (s *State) recordFissionSide(key, side string) {
	if s.SideUses == nil {
		s.SideUses = map[string]map[string]int{}
	}
	if s.SideUses[key] == nil {
		s.SideUses[key] = map[string]int{}
	}
	s.SideUses[key][FissionSideName(side)]++
}
