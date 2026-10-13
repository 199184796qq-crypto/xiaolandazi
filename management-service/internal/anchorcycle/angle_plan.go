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
// 侧面切入，程序硬保证相邻段落侧面不重复。侧面只决定「从哪个维度切入」，不含
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

// CurrentFissionSide 返回本段话术的切入侧面。按「卖点×角度」任务自身的累计使用
// 次数轮换，同一卖点每次讲换一个侧面、7 个侧面循环——短时间（7 段内）对观众多样；
// 声音缓存 group 纳入侧面后，第 8 次回到同侧面即可复用该侧面的旧音频，省 TTS。
// 侧面池产品无关，展开内容由本场事实决定。
func (s *State) CurrentFissionSide() string {
	if len(fissionAngleSides) == 0 {
		return ""
	}
	return fissionAngleSides[s.Used[s.Task().Key].Count%len(fissionAngleSides)]
}
