package livecost

import (
	"errors"
	"math"
	"strings"
	"time"
)

const (
	BudgetMainline    = "mainline"
	BudgetInteraction = "interaction"
	BudgetRewrite     = "rewrite"
)

var ErrRoomBudget = errors.New("本直播间滚动小时费用额度不足，暂停付费生成并继续复用已有内容")

// Amounts are micro-CNY. The safety reserve absorbs quote/usage differences;
// it is never available for new purchases or borrowed by another category.
type RoomBudget struct {
	Pool                                          string
	Total, Mainline, Interaction, Rewrite, Safety int64
}

func RoomBudgetForPool(pool string) (RoomBudget, error) {
	switch pool {
	case "standard":
		return RoomBudget{pool, 1_300_000, 800_000, 350_000, 50_000, 100_000}, nil
	case "high_fidelity":
		return RoomBudget{pool, 3_000_000, 2_100_000, 600_000, 100_000, 200_000}, nil
	}
	return RoomBudget{}, errors.New("当前语音池缺少房间费用限额")
}

func (b RoomBudget) Validate() error {
	for _, amount := range []int64{b.Mainline, b.Interaction, b.Rewrite, b.Safety} {
		if amount < 0 || amount > MaxConfiguredMicros {
			return errors.New("房间费用分类越界")
		}
	}
	if b.Pool != "standard" && b.Pool != "high_fidelity" {
		return errors.New("房间费用语音池无效")
	}
	if b.Total <= 0 || b.Total > MaxConfiguredMicros || b.Mainline < 0 || b.Interaction < 0 || b.Rewrite < 0 || b.Safety < 0 || b.Mainline+b.Interaction+b.Rewrite+b.Safety != b.Total {
		return errors.New("房间费用分类与总额不一致")
	}
	return nil
}

func (b RoomBudget) Policy() Policy {
	return Policy{Configured: true, Window: time.Hour, MainlineTargetMicros: b.Mainline, OptionalLimitMicros: b.Total - b.Safety, TotalTargetMicros: b.Total, RoomBudget: &b}
}

// Classify old records as well as new ones. Model+TTS interaction stages share
// one category; rewriting TTS belongs to mainline, rewriting model to rewrite.
func BudgetClass(r Request) string {
	if r.BudgetClass != "" {
		return r.BudgetClass
	}
	source := ""
	if r.Attribution != nil {
		source = r.Attribution.Source
	}
	if source == "interaction" || strings.Contains(r.Reason, "互动") || strings.Contains(r.Reason, "问题归类") {
		return BudgetInteraction
	}
	if r.Kind == KindTTS {
		return BudgetMainline
	}
	if source == "recording_mainline" || source == "script_rewrite" || source == "script_mainline" || strings.Contains(r.Reason, "改写") || strings.Contains(r.Reason, "主线") {
		return BudgetRewrite
	}
	return BudgetInteraction
}

func (b RoomBudget) Allows(r Request, s Summary) bool {
	if b.Validate() != nil || r.EstimatedMicros <= 0 || s.TotalMicros < 0 {
		return false
	}
	spendable := b.Total - b.Safety
	if s.TotalMicros > spendable || r.EstimatedMicros > spendable-s.TotalMicros {
		return false
	}
	var used, limit int64
	switch BudgetClass(r) {
	case BudgetMainline:
		used, limit = s.MainlineMicros, b.Mainline
	case BudgetInteraction:
		used, limit = s.InteractionMicros, b.Interaction
	case BudgetRewrite:
		used, limit = s.RewriteMicros, b.Rewrite
	default:
		return false
	}
	return used >= 0 && used <= limit && r.EstimatedMicros <= limit-used
}

func addBudgetAmount(total *int64, amount int64) error {
	if amount < 0 || *total < 0 || *total > math.MaxInt64-amount {
		return errors.New("房间费用合计越界")
	}
	*total += amount
	return nil
}

// Include unresolved requests regardless of age. An absent response is neither
// zero-cost nor permission to buy again after a rolling-window boundary.
func (s *Summary) AddBudgetEntry(e Entry, aged bool) error {
	amount := int64(0)
	switch e.Status {
	case StatusReserved, StatusUnknown:
		amount = e.ReservedMicros
		if amount <= 0 {
			return errors.New("未决费用缺少有效预留，禁止当作零费用")
		}
		if err := addBudgetAmount(&s.ReservedMicros, amount); err != nil {
			return err
		}
		s.UnresolvedRequests++
		if e.Status == StatusUnknown {
			if err := addBudgetAmount(&s.UnknownMicros, amount); err != nil {
				return err
			}
		}
		if aged {
			s.AgedUnresolvedRequests++
			if err := addBudgetAmount(&s.AgedUnresolvedMicros, amount); err != nil {
				return err
			}
		}
	case StatusSettled:
		amount = e.ChargedMicros
		s.SettledRequests++
		if e.Request.Kind == KindTTS {
			if err := addBudgetAmount(&s.TTSMicros, amount); err != nil {
				return err
			}
		} else {
			if err := addBudgetAmount(&s.ModelMicros, amount); err != nil {
				return err
			}
		}
		switch e.UsageSource {
		case SourceSupplierUsage:
			if err := addBudgetAmount(&s.SupplierUsageMicros, amount); err != nil {
				return err
			}
		case SourceSupplierInvoice:
			if err := addBudgetAmount(&s.SupplierInvoiceMicros, amount); err != nil {
				return err
			}
		default:
			if err := addBudgetAmount(&s.EstimatedMicros, amount); err != nil {
				return err
			}
		}
	default:
		return nil
	}
	if e.Request.Priority == PriorityOptional {
		if err := addBudgetAmount(&s.OptionalMicros, amount); err != nil {
			return err
		}
	}
	if err := addBudgetAmount(&s.TotalMicros, amount); err != nil {
		return err
	}
	switch BudgetClass(e.Request) {
	case BudgetMainline:
		return addBudgetAmount(&s.MainlineMicros, amount)
	case BudgetInteraction:
		return addBudgetAmount(&s.InteractionMicros, amount)
	case BudgetRewrite:
		return addBudgetAmount(&s.RewriteMicros, amount)
	}
	return errors.New("请求费用分类无效")
}
