package resume

import (
	"errors"
	"sort"
	"strings"
)

type Mode string

const (
	ModeDirect      Mode = "DIRECT"
	ModeBridge      Mode = "BRIDGE"
	ModeFusionSkip  Mode = "FUSION_SKIP"
	ModeCrossResume Mode = "CROSS_RESUME"
	ModeReAnchor    Mode = "RE_ANCHOR"
	ModeSwitchPlan  Mode = "SWITCH_PLAN"
)

type Cursor struct {
	PlanID    string
	TrackID   string
	UnitID    string
	SegmentID string
	OffsetMS  int64
}

type MainlineUnit struct {
	ID               string
	Topic            string
	FactIDs          []string
	Cursor           Cursor
	IndependentEntry bool
	AlreadySpoken    bool
}

type Context struct {
	CurrentCursor          Cursor
	NextUnits              []MainlineUnit
	InteractionDurationMS  int64
	ContinuousInteractions int

	ProductContextChanged bool
	SalesStageChanged     bool
	MainlineStillValid    bool

	CoveredTopics  []string
	CoveredFactIDs []string

	SuggestedMode       Mode
	SuggestedResumeUnit string
	SuggestedSkipUnits  []string
	SuggestedResumeTail string
}

type Plan struct {
	Strategy   string
	Mode       Mode
	BridgeText string

	ResumeCursor Cursor
	ResumeUnit   string
	SkipUnits    []string

	AbandonCurrentPlan bool
	ReAnchor           bool
	Reason             string
}

type Thresholds struct {
	BridgeAfterMS          int64
	ReAnchorAfterMS        int64
	SwitchPlanAfterMS      int64
	SwitchPlanInteractionN int
}

func DefaultThresholds() Thresholds {
	return Thresholds{
		BridgeAfterMS:          5_000,
		ReAnchorAfterMS:        10_000,
		SwitchPlanAfterMS:      25_000,
		SwitchPlanInteractionN: 3,
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
		SwitchPlanStrategy{},
		ReAnchorStrategy{},
		CrossResumeStrategy{},
		FusionSkipStrategy{},
		BridgeStrategy{},
		DirectStrategy{},
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
		return Plan{}, errors.New("resume registry is nil")
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
		if plan.Mode == "" {
			return Plan{}, errors.New("resume strategy returned empty mode")
		}
		plan.SkipUnits = uniqueStrings(plan.SkipUnits)
		plan.BridgeText = strings.TrimSpace(plan.BridgeText)
		return plan, nil
	}
	return Plan{}, errors.New("no resume strategy matched")
}

func suggested(ctx Context, mode Mode) bool {
	return ctx.SuggestedMode == mode
}

func uniqueStrings(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func unitByID(ctx Context, id string) (MainlineUnit, bool) {
	id = strings.TrimSpace(id)
	if id == "" {
		return MainlineUnit{}, false
	}
	for _, unit := range ctx.NextUnits {
		if strings.TrimSpace(unit.ID) == id {
			return unit, true
		}
	}
	return MainlineUnit{}, false
}

func skipSet(ctx Context) map[string]struct{} {
	out := map[string]struct{}{}
	for _, id := range ctx.SuggestedSkipUnits {
		id = strings.TrimSpace(id)
		if id != "" {
			out[id] = struct{}{}
		}
	}
	return out
}

func firstEligibleUnit(ctx Context, skipped map[string]struct{}) (MainlineUnit, bool) {
	for _, unit := range ctx.NextUnits {
		if unit.AlreadySpoken || !unit.IndependentEntry {
			continue
		}
		if skipped != nil {
			if _, exists := skipped[unit.ID]; exists {
				continue
			}
		}
		return unit, true
	}
	return MainlineUnit{}, false
}

type DirectStrategy struct{}

func (DirectStrategy) Name() string                       { return "builtin.direct" }
func (DirectStrategy) Priority() int                      { return 100 }
func (DirectStrategy) CanHandle(Context, Thresholds) bool { return true }
func (DirectStrategy) Build(ctx Context, _ Thresholds) (Plan, error) {
	if unit, ok := unitByID(ctx, ctx.SuggestedResumeUnit); ok {
		return Plan{Mode: ModeDirect, ResumeCursor: unit.Cursor, ResumeUnit: unit.ID, Reason: "resume at suggested complete unit"}, nil
	}
	if unit, ok := firstEligibleUnit(ctx, nil); ok {
		return Plan{Mode: ModeDirect, ResumeCursor: unit.Cursor, ResumeUnit: unit.ID, Reason: "short interaction; continue at next independent unit"}, nil
	}
	return Plan{Mode: ModeDirect, ResumeCursor: ctx.CurrentCursor, Reason: "fallback to current semantic cursor"}, nil
}

type BridgeStrategy struct{}

func (BridgeStrategy) Name() string  { return "builtin.bridge" }
func (BridgeStrategy) Priority() int { return 300 }
func (BridgeStrategy) CanHandle(ctx Context, thresholds Thresholds) bool {
	return suggested(ctx, ModeBridge) ||
		(ctx.InteractionDurationMS >= thresholds.BridgeAfterMS &&
			ctx.InteractionDurationMS < thresholds.ReAnchorAfterMS &&
			strings.TrimSpace(ctx.SuggestedResumeTail) != "")
}
func (BridgeStrategy) Build(ctx Context, thresholds Thresholds) (Plan, error) {
	base, err := DirectStrategy{}.Build(ctx, thresholds)
	if err != nil {
		return Plan{}, err
	}
	base.Mode = ModeBridge
	base.BridgeText = ctx.SuggestedResumeTail
	base.Reason = "medium interaction; use pre-generated bridge before mainline"
	return base, nil
}

type FusionSkipStrategy struct{}

func (FusionSkipStrategy) Name() string  { return "builtin.fusion_skip" }
func (FusionSkipStrategy) Priority() int { return 500 }
func (FusionSkipStrategy) CanHandle(ctx Context, _ Thresholds) bool {
	return suggested(ctx, ModeFusionSkip) || len(ctx.SuggestedSkipUnits) == 1
}
func (FusionSkipStrategy) Build(ctx Context, _ Thresholds) (Plan, error) {
	skipped := skipSet(ctx)
	if len(skipped) == 0 {
		return Plan{}, errors.New("FUSION_SKIP requires a skip unit")
	}
	if unit, ok := unitByID(ctx, ctx.SuggestedResumeUnit); ok {
		return Plan{Mode: ModeFusionSkip, BridgeText: ctx.SuggestedResumeTail, ResumeCursor: unit.Cursor, ResumeUnit: unit.ID, SkipUnits: ctx.SuggestedSkipUnits, Reason: "interaction covered next mainline unit"}, nil
	}
	unit, ok := firstEligibleUnit(ctx, skipped)
	if !ok {
		return Plan{}, errors.New("FUSION_SKIP found no safe unit after covered content")
	}
	return Plan{Mode: ModeFusionSkip, BridgeText: ctx.SuggestedResumeTail, ResumeCursor: unit.Cursor, ResumeUnit: unit.ID, SkipUnits: ctx.SuggestedSkipUnits, Reason: "skip covered content and continue at next independent unit"}, nil
}

type CrossResumeStrategy struct{}

func (CrossResumeStrategy) Name() string  { return "builtin.cross_resume" }
func (CrossResumeStrategy) Priority() int { return 600 }
func (CrossResumeStrategy) CanHandle(ctx Context, _ Thresholds) bool {
	return suggested(ctx, ModeCrossResume) || len(ctx.SuggestedSkipUnits) > 1
}
func (CrossResumeStrategy) Build(ctx Context, _ Thresholds) (Plan, error) {
	skipped := skipSet(ctx)
	if unit, ok := unitByID(ctx, ctx.SuggestedResumeUnit); ok && unit.IndependentEntry {
		return Plan{Mode: ModeCrossResume, BridgeText: ctx.SuggestedResumeTail, ResumeCursor: unit.Cursor, ResumeUnit: unit.ID, SkipUnits: ctx.SuggestedSkipUnits, Reason: "cross-resume at suggested independent unit"}, nil
	}
	unit, ok := firstEligibleUnit(ctx, skipped)
	if !ok {
		return Plan{}, errors.New("CROSS_RESUME found no independent uncovered unit")
	}
	return Plan{Mode: ModeCrossResume, BridgeText: ctx.SuggestedResumeTail, ResumeCursor: unit.Cursor, ResumeUnit: unit.ID, SkipUnits: ctx.SuggestedSkipUnits, Reason: "cross covered units and resume at next safe independent unit"}, nil
}

type ReAnchorStrategy struct{}

func (ReAnchorStrategy) Name() string  { return "builtin.re_anchor" }
func (ReAnchorStrategy) Priority() int { return 700 }
func (ReAnchorStrategy) CanHandle(ctx Context, thresholds Thresholds) bool {
	return suggested(ctx, ModeReAnchor) ||
		(ctx.InteractionDurationMS >= thresholds.ReAnchorAfterMS &&
			ctx.InteractionDurationMS < thresholds.SwitchPlanAfterMS &&
			ctx.MainlineStillValid)
}
func (ReAnchorStrategy) Build(ctx Context, _ Thresholds) (Plan, error) {
	unit, ok := firstEligibleUnit(ctx, skipSet(ctx))
	if !ok {
		return Plan{}, errors.New("RE_ANCHOR found no safe mainline entry")
	}
	return Plan{Mode: ModeReAnchor, BridgeText: ctx.SuggestedResumeTail, ResumeCursor: unit.Cursor, ResumeUnit: unit.ID, SkipUnits: ctx.SuggestedSkipUnits, ReAnchor: true, Reason: "rebuild context anchor before resuming"}, nil
}

type SwitchPlanStrategy struct{}

func (SwitchPlanStrategy) Name() string  { return "builtin.switch_plan" }
func (SwitchPlanStrategy) Priority() int { return 900 }
func (SwitchPlanStrategy) CanHandle(ctx Context, thresholds Thresholds) bool {
	return suggested(ctx, ModeSwitchPlan) ||
		ctx.ProductContextChanged ||
		ctx.SalesStageChanged ||
		!ctx.MainlineStillValid ||
		ctx.InteractionDurationMS >= thresholds.SwitchPlanAfterMS ||
		ctx.ContinuousInteractions >= thresholds.SwitchPlanInteractionN
}
func (SwitchPlanStrategy) Build(ctx Context, _ Thresholds) (Plan, error) {
	return Plan{Mode: ModeSwitchPlan, BridgeText: ctx.SuggestedResumeTail, SkipUnits: ctx.SuggestedSkipUnits, AbandonCurrentPlan: true, Reason: "old mainline context is stale; choose a new safe plan entry"}, nil
}
