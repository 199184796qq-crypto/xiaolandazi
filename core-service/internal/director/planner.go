package director

import (
	"errors"
	"sort"
	"strings"

	"livecompanion/core/internal/humanization"
	"livecompanion/core/internal/resume"
)

type Dimension string

const (
	DimensionProgress   Dimension = "PROGRESS"
	DimensionAtmosphere Dimension = "ATMOSPHERE"
	DimensionStyle      Dimension = "STYLE"
)

type ProgressAction string

const (
	ProgressContinueStory ProgressAction = "CONTINUE_STORY"
	ProgressHoldForQNA    ProgressAction = "HOLD_FOR_QNA"
	ProgressConvert       ProgressAction = "CONVERSION_PUSH"
	ProgressRecover       ProgressAction = "RECOVER_MAINLINE"
)

type AtmosphereAction string

const (
	AtmosphereNeutral      AtmosphereAction = "NEUTRAL"
	AtmosphereWarmUp       AtmosphereAction = "WARM_UP"
	AtmosphereCelebrate    AtmosphereAction = "CELEBRATE"
	AtmosphereLightHumor   AtmosphereAction = "LIGHT_HUMOR"
	AtmosphereCalmAndFocus AtmosphereAction = "CALM_AND_FOCUS"
)

type StyleProfile struct {
	Name              string
	Tone              string
	SentenceRhythm    string
	CTAStyle          string
	PreferredFillers  []string
	HumorCeiling      int
	DefaultTargetSecs int
}

type RoomSignals struct {
	OnlineCount         int
	Events30s           int
	Chat30s             int
	Members30s          int
	Follows30s          int
	Likes30s            int
	Orders30s           int
	OrderSignals30s     int
	NegativeFeedback30s int
	ActionableQuestions int
	SecondsSinceHumor   int
	PaceLevel           string
	QuestionPressure    float64
	AudienceTurnover5m  float64
	PreferAggregateQNA  bool
	PreferOneToOneQNA   bool
	InteractionDebt     float64
	LikeDebt            float64
	FollowDebt          float64
	ConversionDebt      float64
}

type Snapshot struct {
	RoomID             int64
	ProgressHint       string
	MainlineGoal       string
	ProductStoryRemain bool
	Style              StyleProfile
	Signals            RoomSignals
	Humanization       humanization.Context
	Resume             resume.Context
}

type Plan struct {
	Progress         ProgressAction
	Atmosphere       AtmosphereAction
	Style            StyleProfile
	TargetSeconds    int
	HumorLevel       int
	HumorInstruction string
	PromptDirectives []string
	StrategyTrace    []string
	Humanization     humanization.Plan
	Resume           resume.Plan
}

type Contribution struct {
	Progress         ProgressAction
	Atmosphere       AtmosphereAction
	TargetSeconds    int
	HumorSet         bool
	HumorLevel       int
	HumorInstruction string
	PromptDirectives []string
}

type Strategy interface {
	Name() string
	Dimension() Dimension
	Priority() int
	Match(Snapshot) bool
	Apply(Snapshot, Plan) Contribution
}

type Registry struct {
	strategies map[Dimension][]Strategy
}

func NewRegistry(strategies ...Strategy) *Registry {
	r := &Registry{strategies: map[Dimension][]Strategy{}}
	for _, strategy := range strategies {
		r.Register(strategy)
	}
	return r
}

func NewDefaultRegistry() *Registry {
	return NewRegistry(
		ComplaintCalmStrategy{},
		OrderMomentumStrategy{},
		LowTrafficInteractionStrategy{},
		ColdRoomWarmUpStrategy{},
		NeutralAtmosphereStrategy{},
		AggregateQuestionStrategy{},
		QuestionHoldStrategy{},
		ConversionProgressStrategy{},
		ContinueStoryStrategy{},
		AnchorStyleStrategy{},
	)
}

func (r *Registry) Register(strategy Strategy) {
	if strategy == nil || strings.TrimSpace(strategy.Name()) == "" {
		return
	}
	dim := strategy.Dimension()
	r.strategies[dim] = append(r.strategies[dim], strategy)
	sort.SliceStable(r.strategies[dim], func(i, j int) bool {
		return r.strategies[dim][i].Priority() > r.strategies[dim][j].Priority()
	})
}

func (r *Registry) selectOne(dim Dimension, snapshot Snapshot, current Plan) (Contribution, string) {
	for _, strategy := range r.strategies[dim] {
		if strategy.Match(snapshot) {
			return strategy.Apply(snapshot, current), strategy.Name()
		}
	}
	return Contribution{}, ""
}

type Planner struct {
	strategies   *Registry
	humanization *humanization.Registry
	resume       *resume.Registry
}

func NewPlanner(strategies *Registry, resumeRegistry *resume.Registry) *Planner {
	if strategies == nil {
		strategies = NewDefaultRegistry()
	}
	if resumeRegistry == nil {
		resumeRegistry = resume.NewDefaultRegistry()
	}
	return &Planner{
		strategies:   strategies,
		humanization: humanization.NewDefaultRegistry(),
		resume:       resumeRegistry,
	}
}

func NewPlannerWithHumanization(strategies *Registry, humanizationRegistry *humanization.Registry, resumeRegistry *resume.Registry) *Planner {
	planner := NewPlanner(strategies, resumeRegistry)
	if humanizationRegistry != nil {
		planner.humanization = humanizationRegistry
	}
	return planner
}

func NewDefaultPlanner() *Planner {
	return NewPlanner(nil, nil)
}

func (p *Planner) Plan(snapshot Snapshot) (Plan, error) {
	if p == nil || p.strategies == nil || p.humanization == nil || p.resume == nil {
		return Plan{}, errors.New("director planner is not configured")
	}
	plan := Plan{
		Style:         snapshot.Style,
		TargetSeconds: snapshot.Style.DefaultTargetSecs,
	}
	if plan.TargetSeconds <= 0 {
		plan.TargetSeconds = 10
	}

	for _, dim := range []Dimension{DimensionProgress, DimensionAtmosphere, DimensionStyle} {
		contribution, name := p.strategies.selectOne(dim, snapshot, plan)
		if name == "" {
			continue
		}
		plan.StrategyTrace = append(plan.StrategyTrace, name)
		mergeContribution(&plan, contribution)
	}

	resumePlan, err := p.resume.Plan(snapshot.Resume)
	if err != nil {
		return Plan{}, err
	}
	plan.Resume = resumePlan
	plan.StrategyTrace = append(plan.StrategyTrace, resumePlan.Strategy)

	// Runtime atmosphere is allowed to lower the learned style's humor ceiling,
	// but never exceed it.
	if plan.HumorLevel > plan.Style.HumorCeiling && plan.Style.HumorCeiling >= 0 {
		plan.HumorLevel = plan.Style.HumorCeiling
	}
	if plan.HumorLevel <= 0 {
		plan.HumorInstruction = ""
	}

	humanizationPlan, err := p.humanization.Plan(snapshot.Humanization)
	if err != nil {
		return Plan{}, err
	}
	plan.Humanization = humanizationPlan
	plan.StrategyTrace = append(plan.StrategyTrace, humanizationPlan.Strategy)
	plan.PromptDirectives = append(plan.PromptDirectives, humanizationPlan.PromptDirectives...)
	return plan, nil
}

func mergeContribution(plan *Plan, value Contribution) {
	if value.Progress != "" {
		plan.Progress = value.Progress
	}
	if value.Atmosphere != "" {
		plan.Atmosphere = value.Atmosphere
	}
	if value.TargetSeconds > 0 {
		plan.TargetSeconds = value.TargetSeconds
	}
	if value.HumorSet {
		plan.HumorLevel = value.HumorLevel
	}
	if strings.TrimSpace(value.HumorInstruction) != "" {
		plan.HumorInstruction = strings.TrimSpace(value.HumorInstruction)
	}
	for _, directive := range value.PromptDirectives {
		directive = strings.TrimSpace(directive)
		if directive != "" {
			plan.PromptDirectives = append(plan.PromptDirectives, directive)
		}
	}
}

type AggregateQuestionStrategy struct{}

func (AggregateQuestionStrategy) Name() string         { return "progress.aggregate_questions" }
func (AggregateQuestionStrategy) Dimension() Dimension { return DimensionProgress }
func (AggregateQuestionStrategy) Priority() int        { return 950 }
func (AggregateQuestionStrategy) Match(s Snapshot) bool {
	return s.Signals.PreferAggregateQNA && s.Signals.ActionableQuestions > 0
}
func (AggregateQuestionStrategy) Apply(_ Snapshot, _ Plan) Contribution {
	return Contribution{
		Progress:      ProgressHoldForQNA,
		TargetSeconds: 12,
		PromptDirectives: []string{
			"当前问题压力较高，不逐个点名回答；把同类问题聚合成1到3个主题统一回答，然后尽快回到主线。",
		},
	}
}

type LowTrafficInteractionStrategy struct{}

func (LowTrafficInteractionStrategy) Name() string         { return "atmosphere.low_traffic_interaction" }
func (LowTrafficInteractionStrategy) Dimension() Dimension { return DimensionAtmosphere }
func (LowTrafficInteractionStrategy) Priority() int        { return 650 }
func (LowTrafficInteractionStrategy) Match(s Snapshot) bool {
	return s.Signals.PreferOneToOneQNA && s.Signals.InteractionDebt >= 0.6
}
func (LowTrafficInteractionStrategy) Apply(_ Snapshot, _ Plan) Contribution {
	return Contribution{
		Atmosphere: AtmosphereWarmUp,
		PromptDirectives: []string{
			"当前房间流量较低，可以增加一对一交流、自然点名或轻互动问题，重点提高留人和回应感。",
		},
	}
}

type QuestionHoldStrategy struct{}

func (QuestionHoldStrategy) Name() string          { return "progress.question_hold" }
func (QuestionHoldStrategy) Dimension() Dimension  { return DimensionProgress }
func (QuestionHoldStrategy) Priority() int         { return 800 }
func (QuestionHoldStrategy) Match(s Snapshot) bool { return s.Signals.ActionableQuestions > 0 }
func (QuestionHoldStrategy) Apply(_ Snapshot, _ Plan) Contribution {
	return Contribution{
		Progress:      ProgressHoldForQNA,
		TargetSeconds: 10,
		PromptDirectives: []string{
			"先把观众当前问题自然回答清楚，不要抢着推进下一销售阶段。",
		},
	}
}

type ConversionProgressStrategy struct{}

func (ConversionProgressStrategy) Name() string         { return "progress.conversion_momentum" }
func (ConversionProgressStrategy) Dimension() Dimension { return DimensionProgress }
func (ConversionProgressStrategy) Priority() int        { return 600 }
func (ConversionProgressStrategy) Match(s Snapshot) bool {
	return s.Signals.Orders30s > 0 || s.Signals.OrderSignals30s > 0 || strings.EqualFold(strings.TrimSpace(s.ProgressHint), "CONVERSION")
}
func (ConversionProgressStrategy) Apply(_ Snapshot, _ Plan) Contribution {
	return Contribution{
		Progress:      ProgressConvert,
		TargetSeconds: 8,
		PromptDirectives: []string{
			"保持当前成交势能，回答后自然带一个轻量选择或行动提醒，不要突然变成长促销口号。",
		},
	}
}

type ContinueStoryStrategy struct{}

func (ContinueStoryStrategy) Name() string         { return "progress.continue_story" }
func (ContinueStoryStrategy) Dimension() Dimension { return DimensionProgress }
func (ContinueStoryStrategy) Priority() int        { return 100 }
func (ContinueStoryStrategy) Match(Snapshot) bool  { return true }
func (ContinueStoryStrategy) Apply(s Snapshot, _ Plan) Contribution {
	directive := "回答要服务于当前主线，结束后继续围绕当前商品故事往下讲。"
	if !s.ProductStoryRemain {
		directive = "当前主线素材接近耗尽，回答后不要机械重复旧内容，准备进入新的安全话题入口。"
	}
	return Contribution{Progress: ProgressContinueStory, PromptDirectives: []string{directive}}
}

type ComplaintCalmStrategy struct{}

func (ComplaintCalmStrategy) Name() string         { return "atmosphere.calm_and_focus" }
func (ComplaintCalmStrategy) Dimension() Dimension { return DimensionAtmosphere }
func (ComplaintCalmStrategy) Priority() int        { return 900 }
func (ComplaintCalmStrategy) Match(s Snapshot) bool {
	return s.Signals.NegativeFeedback30s > 0 || strings.EqualFold(strings.TrimSpace(s.Signals.PaceLevel), "OVERHEATED")
}
func (ComplaintCalmStrategy) Apply(_ Snapshot, _ Plan) Contribution {
	return Contribution{
		Atmosphere: AtmosphereCalmAndFocus,
		HumorSet:   true,
		HumorLevel: 0,
		PromptDirectives: []string{
			"房间当前需要稳住情绪：语气放稳、先回应核心问题，不开玩笑、不调侃观众。",
		},
	}
}

type OrderMomentumStrategy struct{}

func (OrderMomentumStrategy) Name() string         { return "atmosphere.positive_momentum" }
func (OrderMomentumStrategy) Dimension() Dimension { return DimensionAtmosphere }
func (OrderMomentumStrategy) Priority() int        { return 700 }
func (OrderMomentumStrategy) Match(s Snapshot) bool {
	return s.Signals.Orders30s > 0 || s.Signals.OrderSignals30s > 0 || s.Signals.Follows30s >= 3
}
func (OrderMomentumStrategy) Apply(s Snapshot, _ Plan) Contribution {
	humor := 0
	instruction := ""
	if s.Signals.SecondsSinceHumor >= 120 && s.Signals.NegativeFeedback30s == 0 {
		humor = 1
		instruction = "可以顺手加一句轻松、善意的小玩笑或俏皮承接，但不要讽刺观众，也不要拿商品事实开虚构玩笑。"
	}
	return Contribution{
		Atmosphere:       AtmosphereCelebrate,
		HumorSet:         true,
		HumorLevel:       humor,
		HumorInstruction: instruction,
		PromptDirectives: []string{
			"房间正向势能不错，语气可以更有精神，轻轻庆祝一下，但不要连续喊单。",
		},
	}
}

type ColdRoomWarmUpStrategy struct{}

func (ColdRoomWarmUpStrategy) Name() string         { return "atmosphere.cold_room_warmup" }
func (ColdRoomWarmUpStrategy) Dimension() Dimension { return DimensionAtmosphere }
func (ColdRoomWarmUpStrategy) Priority() int        { return 500 }
func (ColdRoomWarmUpStrategy) Match(s Snapshot) bool {
	return s.Signals.Events30s <= 2 && s.Signals.Chat30s == 0 && s.Signals.NegativeFeedback30s == 0
}
func (ColdRoomWarmUpStrategy) Apply(s Snapshot, _ Plan) Contribution {
	humor := 0
	instruction := ""
	if s.Signals.SecondsSinceHumor >= 120 {
		humor = 1
		instruction = "可以有一句非常轻的自嘲式或生活化玩笑把气氛拉松，但不要硬讲段子。"
	}
	return Contribution{
		Atmosphere:       AtmosphereWarmUp,
		HumorSet:         true,
		HumorLevel:       humor,
		HumorInstruction: instruction,
		PromptDirectives: []string{
			"房间偏冷，表达更主动、更有人情味；可增加一个自然互动钩子，但不要连环提问。",
		},
	}
}

type NeutralAtmosphereStrategy struct{}

func (NeutralAtmosphereStrategy) Name() string         { return "atmosphere.neutral" }
func (NeutralAtmosphereStrategy) Dimension() Dimension { return DimensionAtmosphere }
func (NeutralAtmosphereStrategy) Priority() int        { return 100 }
func (NeutralAtmosphereStrategy) Match(Snapshot) bool  { return true }
func (NeutralAtmosphereStrategy) Apply(_ Snapshot, _ Plan) Contribution {
	return Contribution{
		Atmosphere: AtmosphereNeutral,
		HumorSet:   true,
		HumorLevel: 0,
		PromptDirectives: []string{
			"保持自然直播状态，不强行热场，也不为了变化而刻意搞笑。",
		},
	}
}

type AnchorStyleStrategy struct{}

func (AnchorStyleStrategy) Name() string         { return "style.anchor_profile" }
func (AnchorStyleStrategy) Dimension() Dimension { return DimensionStyle }
func (AnchorStyleStrategy) Priority() int        { return 100 }
func (AnchorStyleStrategy) Match(Snapshot) bool  { return true }
func (AnchorStyleStrategy) Apply(s Snapshot, _ Plan) Contribution {
	directives := []string{}
	if strings.TrimSpace(s.Style.Tone) != "" {
		directives = append(directives, "主播整体语气："+strings.TrimSpace(s.Style.Tone))
	}
	if strings.TrimSpace(s.Style.SentenceRhythm) != "" {
		directives = append(directives, "句式节奏："+strings.TrimSpace(s.Style.SentenceRhythm))
	}
	if strings.TrimSpace(s.Style.CTAStyle) != "" {
		directives = append(directives, "CTA习惯："+strings.TrimSpace(s.Style.CTAStyle))
	}
	return Contribution{PromptDirectives: directives}
}
