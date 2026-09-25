package humanization

import (
	"errors"
	"sort"
	"strings"
)

type Kind string

const (
	KindNone           Kind = "NONE"
	KindPause          Kind = "PAUSE"
	KindFiller         Kind = "FILLER"
	KindRepeatFragment Kind = "REPEAT_FRAGMENT"
	KindSelfCorrection Kind = "SELF_CORRECTION"
	KindInversion      Kind = "INVERSION"
	KindRehook         Kind = "REHOOK"
	KindThroatClear    Kind = "THROAT_CLEAR"
	KindCough          Kind = "COUGH"
)

type Delivery string

const (
	DeliveryTextDirective Delivery = "TEXT_DIRECTIVE"
	DeliveryTTSMark       Delivery = "TTS_MARK"
	DeliveryAudioSegment  Delivery = "AUDIO_MICRO_SEGMENT"
	DeliveryPauseOnly     Delivery = "PAUSE_ONLY"
)

type Capabilities struct {
	SupportsParalinguisticMarks bool
	HasThroatClearAsset         bool
	HasCoughAsset               bool
}

type Context struct {
	Opportunity bool

	NegativeFeedback bool
	Complaint        bool
	AfterSale        bool
	PriceDispute     bool
	HighIntentClose  bool

	SecondsSinceLastBehavior int
	BehaviorCount60s         int
	MaxBehaviorsPerMinute    int

	SecondsSinceThroatClear int
	SecondsSinceCough       int
	SecondsSinceCorrection  int
	SecondsSinceRepeat      int
	SecondsSinceInversion   int

	PreferLightDisfluency bool
	PreferRepetition      bool
	PreferInversion       bool
	AllowNonVerbal        bool

	Capabilities Capabilities
}

type Event struct {
	Kind        Kind
	Delivery    Delivery
	Instruction string
	AssetKey    string
	MaxCount    int
}

type Plan struct {
	Strategy         string
	Enabled          bool
	Event            Event
	PromptDirectives []string
	Reason           string
}

type Thresholds struct {
	GeneralCooldownSeconds    int
	CorrectionCooldownSeconds int
	RepeatCooldownSeconds     int
	InversionCooldownSeconds  int
	ThroatClearCooldownSecs   int
	CoughCooldownSecs         int
	DefaultMaxPerMinute       int
}

func DefaultThresholds() Thresholds {
	return Thresholds{
		GeneralCooldownSeconds:    35,
		CorrectionCooldownSeconds: 90,
		RepeatCooldownSeconds:     75,
		InversionCooldownSeconds:  60,
		ThroatClearCooldownSecs:   180,
		CoughCooldownSecs:         300,
		DefaultMaxPerMinute:       1,
	}
}

type Strategy interface {
	Name() string
	Priority() int
	CanHandle(Context, Thresholds) bool
	Build(Context, Thresholds) (Plan, error)
}

type Registry struct {
	thresholds Thresholds
	strategies []Strategy
}

func NewRegistry(thresholds Thresholds, strategies ...Strategy) *Registry {
	r := &Registry{thresholds: thresholds}
	for _, strategy := range strategies {
		r.Register(strategy)
	}
	return r
}

func NewDefaultRegistry() *Registry {
	return NewRegistry(
		DefaultThresholds(),
		SuppressStrategy{},
		ThroatClearStrategy{},
		CoughStrategy{},
		SelfCorrectionStrategy{},
		RepeatFragmentStrategy{},
		InversionStrategy{},
		RehookStrategy{},
		NaturalPauseStrategy{},
		NoopStrategy{},
	)
}

func (r *Registry) Register(strategy Strategy) {
	if strategy == nil || strings.TrimSpace(strategy.Name()) == "" {
		return
	}
	r.strategies = append(r.strategies, strategy)
	sort.SliceStable(r.strategies, func(i, j int) bool {
		return r.strategies[i].Priority() > r.strategies[j].Priority()
	})
}

func (r *Registry) Plan(ctx Context) (Plan, error) {
	if r == nil {
		return Plan{}, errors.New("humanization registry is nil")
	}
	if ctx.MaxBehaviorsPerMinute <= 0 {
		ctx.MaxBehaviorsPerMinute = r.thresholds.DefaultMaxPerMinute
	}
	for _, strategy := range r.strategies {
		if !strategy.CanHandle(ctx, r.thresholds) {
			continue
		}
		plan, err := strategy.Build(ctx, r.thresholds)
		if err != nil {
			return Plan{}, err
		}
		if plan.Strategy == "" {
			plan.Strategy = strategy.Name()
		}
		plan.Event.Instruction = strings.TrimSpace(plan.Event.Instruction)
		return plan, nil
	}
	return Plan{}, errors.New("no humanization strategy matched")
}

func blocked(ctx Context) bool {
	return ctx.NegativeFeedback ||
		ctx.Complaint ||
		ctx.AfterSale ||
		ctx.PriceDispute ||
		ctx.HighIntentClose
}

func rateLimited(ctx Context, t Thresholds) bool {
	return !ctx.Opportunity ||
		ctx.SecondsSinceLastBehavior < t.GeneralCooldownSeconds ||
		ctx.BehaviorCount60s >= ctx.MaxBehaviorsPerMinute
}

type SuppressStrategy struct{}

func (SuppressStrategy) Name() string  { return "humanization.suppress_sensitive" }
func (SuppressStrategy) Priority() int { return 2000 }
func (SuppressStrategy) CanHandle(ctx Context, _ Thresholds) bool {
	return blocked(ctx)
}
func (SuppressStrategy) Build(_ Context, _ Thresholds) (Plan, error) {
	return Plan{
		Enabled: false,
		Event:   Event{Kind: KindNone},
		Reason:  "sensitive/high-intent context; keep delivery clean and precise",
	}, nil
}

type ThroatClearStrategy struct{}

func (ThroatClearStrategy) Name() string  { return "humanization.throat_clear" }
func (ThroatClearStrategy) Priority() int { return 900 }
func (ThroatClearStrategy) CanHandle(ctx Context, t Thresholds) bool {
	return !blocked(ctx) &&
		!rateLimited(ctx, t) &&
		ctx.AllowNonVerbal &&
		ctx.SecondsSinceThroatClear >= t.ThroatClearCooldownSecs &&
		(ctx.Capabilities.SupportsParalinguisticMarks || ctx.Capabilities.HasThroatClearAsset)
}
func (ThroatClearStrategy) Build(ctx Context, _ Thresholds) (Plan, error) {
	event := Event{
		Kind:        KindThroatClear,
		Instruction: "在自然换气点非常轻地清一下嗓，不要夸张，也不要连续出现。",
		MaxCount:    1,
	}
	switch {
	case ctx.Capabilities.SupportsParalinguisticMarks:
		event.Delivery = DeliveryTTSMark
	case ctx.Capabilities.HasThroatClearAsset:
		event.Delivery = DeliveryAudioSegment
		event.AssetKey = "humanization.throat_clear.light"
	default:
		event.Delivery = DeliveryPauseOnly
		event.Instruction = "当前TTS不支持清嗓，降级为一次自然短停顿。"
	}
	return Plan{
		Enabled:          true,
		Event:            event,
		PromptDirectives: []string{"清嗓只是声音层行为，不得改变正文事实或额外编造内容。"},
		Reason:           "rare non-verbal human cue is available and cooldown has elapsed",
	}, nil
}

type CoughStrategy struct{}

func (CoughStrategy) Name() string  { return "humanization.cough" }
func (CoughStrategy) Priority() int { return 850 }
func (CoughStrategy) CanHandle(ctx Context, t Thresholds) bool {
	return !blocked(ctx) &&
		!rateLimited(ctx, t) &&
		ctx.AllowNonVerbal &&
		ctx.SecondsSinceCough >= t.CoughCooldownSecs &&
		ctx.Capabilities.HasCoughAsset
}
func (CoughStrategy) Build(_ Context, _ Thresholds) (Plan, error) {
	return Plan{
		Enabled: true,
		Event: Event{
			Kind:        KindCough,
			Delivery:    DeliveryAudioSegment,
			Instruction: "在句间插入一次很轻的咳嗽，不能盖住关键词，不能连续咳。",
			AssetKey:    "humanization.cough.light",
			MaxCount:    1,
		},
		Reason: "rare cough asset is available and cooldown has elapsed",
	}, nil
}

type SelfCorrectionStrategy struct{}

func (SelfCorrectionStrategy) Name() string  { return "humanization.self_correction" }
func (SelfCorrectionStrategy) Priority() int { return 700 }
func (SelfCorrectionStrategy) CanHandle(ctx Context, t Thresholds) bool {
	return !blocked(ctx) &&
		!rateLimited(ctx, t) &&
		ctx.PreferLightDisfluency &&
		ctx.SecondsSinceCorrection >= t.CorrectionCooldownSeconds
}
func (SelfCorrectionStrategy) Build(_ Context, _ Thresholds) (Plan, error) {
	return Plan{
		Enabled: true,
		Event: Event{
			Kind:        KindSelfCorrection,
			Delivery:    DeliveryTextDirective,
			Instruction: "允许一句轻微自我修正，例如前半句自然改口后把意思说完整；修正只能改表达，不能改商品事实。",
			MaxCount:    1,
		},
		PromptDirectives: []string{
			"自我修正必须像自然口误后的改口，不得故意制造错误数字、价格、规格或商品事实。",
		},
		Reason: "learned style allows light disfluency",
	}, nil
}

type RepeatFragmentStrategy struct{}

func (RepeatFragmentStrategy) Name() string  { return "humanization.repeat_fragment" }
func (RepeatFragmentStrategy) Priority() int { return 650 }
func (RepeatFragmentStrategy) CanHandle(ctx Context, t Thresholds) bool {
	return !blocked(ctx) &&
		!rateLimited(ctx, t) &&
		ctx.PreferRepetition &&
		ctx.SecondsSinceRepeat >= t.RepeatCooldownSeconds
}
func (RepeatFragmentStrategy) Build(_ Context, _ Thresholds) (Plan, error) {
	return Plan{
		Enabled: true,
		Event: Event{
			Kind:        KindRepeatFragment,
			Delivery:    DeliveryTextDirective,
			Instruction: "允许把一个短关键词或半句自然重复一次作强调，第二遍可换一个轻微说法；不要整句机械复读。",
			MaxCount:    1,
		},
		Reason: "anchor style prefers occasional natural repetition",
	}, nil
}

type InversionStrategy struct{}

func (InversionStrategy) Name() string  { return "humanization.inversion" }
func (InversionStrategy) Priority() int { return 600 }
func (InversionStrategy) CanHandle(ctx Context, t Thresholds) bool {
	return !blocked(ctx) &&
		!rateLimited(ctx, t) &&
		ctx.PreferInversion &&
		ctx.SecondsSinceInversion >= t.InversionCooldownSeconds
}
func (InversionStrategy) Build(_ Context, _ Thresholds) (Plan, error) {
	return Plan{
		Enabled: true,
		Event: Event{
			Kind:        KindInversion,
			Delivery:    DeliveryTextDirective,
			Instruction: "允许偶尔用口语倒装或后置强调，让句子像真人临场组织语言；必须保持意思清楚。",
			MaxCount:    1,
		},
		Reason: "anchor style allows occasional spoken inversion",
	}, nil
}

type RehookStrategy struct{}

func (RehookStrategy) Name() string  { return "humanization.rehook" }
func (RehookStrategy) Priority() int { return 500 }
func (RehookStrategy) CanHandle(ctx Context, t Thresholds) bool {
	return !blocked(ctx) && !rateLimited(ctx, t)
}
func (RehookStrategy) Build(_ Context, _ Thresholds) (Plan, error) {
	return Plan{
		Enabled: true,
		Event: Event{
			Kind:        KindRehook,
			Delivery:    DeliveryTextDirective,
			Instruction: "允许在讲完一个点后用很短的口语回勾重新抓住重点，例如‘我跟你讲啊，这个点你记住’一类，但不要固定成同一句。",
			MaxCount:    1,
		},
		Reason: "low-risk human emphasis cue",
	}, nil
}

type NaturalPauseStrategy struct{}

func (NaturalPauseStrategy) Name() string  { return "humanization.natural_pause" }
func (NaturalPauseStrategy) Priority() int { return 300 }
func (NaturalPauseStrategy) CanHandle(ctx Context, t Thresholds) bool {
	return !blocked(ctx) && !rateLimited(ctx, t)
}
func (NaturalPauseStrategy) Build(_ Context, _ Thresholds) (Plan, error) {
	return Plan{
		Enabled: true,
		Event: Event{
			Kind:        KindPause,
			Delivery:    DeliveryPauseOnly,
			Instruction: "在自然换气或思考位置增加一次很短停顿，不能切断数字、规格、价格或因果句。",
			MaxCount:    1,
		},
		Reason: "safe fallback human cue",
	}, nil
}

type NoopStrategy struct{}

func (NoopStrategy) Name() string                       { return "humanization.none" }
func (NoopStrategy) Priority() int                      { return 0 }
func (NoopStrategy) CanHandle(Context, Thresholds) bool { return true }
func (NoopStrategy) Build(_ Context, _ Thresholds) (Plan, error) {
	return Plan{Enabled: false, Event: Event{Kind: KindNone}, Reason: "no humanization behavior is appropriate now"}, nil
}
