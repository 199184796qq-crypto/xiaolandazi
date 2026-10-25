// Package livecost accounts for paid requests, never media duration or playback.
package livecost

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"strings"
	"time"
	"unicode"
)

const (
	PriorityOptional       = "optional"
	PriorityNecessary      = "necessary"
	KindModel              = "model"
	KindTTS                = "tts"
	StatusReserved         = "reserved"
	StatusUnknown          = "unknown"
	StatusSettled          = "settled"
	StatusReleased         = "released"
	StatusDeclined         = "declined"
	ReceiptSuccess         = "success"
	ReceiptUnknown         = "unknown"
	ReceiptNotSent         = "not_sent"
	SourceSentTextEstimate = "sent_text_estimate"
	SourceSupplierUsage    = "supplier_usage"
	SourceSupplierInvoice  = "supplier_invoice"
)

var (
	ErrAccountingUnavailable = errors.New("请求费用账本暂不可用")
	ErrRequestConflict       = errors.New("费用请求标识与已有请求不一致")
	ErrReceiptConflict       = errors.New("费用回执与已有结算不一致")
	ErrRequestNotFound       = errors.New("费用请求不存在")
)

type Request struct {
	Key               string       `json:"key"`
	TenantID          int64        `json:"tenant_id"`
	RoomID            int64        `json:"room_id"`
	PlanID            int64        `json:"plan_id"`
	SessionID         int64        `json:"session_id"`
	RunID             string       `json:"run_id"`
	Kind              string       `json:"kind"`
	Provider          string       `json:"provider"`
	Model             string       `json:"model"`
	Priority          string       `json:"priority"`
	Reason            string       `json:"reason"`
	Fingerprint       string       `json:"fingerprint"`
	EstimatedMicros   int64        `json:"estimated_micros"`
	InputTokens       int64        `json:"input_tokens"`
	CachedInputTokens int64        `json:"cached_input_tokens"`
	OutputTokens      int64        `json:"output_tokens"`
	Characters        int64        `json:"characters"`
	Attribution       *Attribution `json:"attribution,omitempty"`
}

// Attribution locates paid work without retaining its text or media URL. Round
// is one-based for the recording mainline and zero when no round applies;
// SourceIndex is the zero-based source sentence within TabID.
// Older requests omit the entire object, preserving their existing identity.
type Attribution struct {
	Source      string `json:"source"`
	Round       int    `json:"round"`
	TabID       string `json:"tab_id,omitempty"`
	SourceIndex int    `json:"source_index"`
}

type Receipt struct {
	TenantID          int64  `json:"tenant_id"`
	RoomID            int64  `json:"room_id"`
	Status            string `json:"status"`
	ActualMicros      int64  `json:"actual_micros"`
	ActualKnown       bool   `json:"actual_known"`
	EstimatedMicros   *int64 `json:"estimated_micros,omitempty"`
	ProviderRequestID string `json:"provider_request_id"`
	UsageSource       string `json:"usage_source"`
	InputTokens       int64  `json:"input_tokens"`
	CachedInputTokens int64  `json:"cached_input_tokens"`
	OutputTokens      int64  `json:"output_tokens"`
	Characters        int64  `json:"characters"`
}

type Policy struct {
	Window              time.Duration
	OptionalLimitMicros int64
	TotalTargetMicros   int64
	SpoolDir            string
}

func DefaultPolicy() Policy {
	return Policy{Window: time.Hour, OptionalLimitMicros: 1200000, TotalTargetMicros: 1300000}
}

type Scope struct {
	TenantID int64 `json:"tenant_id"`
	RoomID   int64 `json:"room_id"`
}
type Reservation struct {
	Key                   string `json:"key"`
	Allowed               bool   `json:"allowed"`
	Existing              bool   `json:"existing"`
	Status                string `json:"status"`
	OverBudget            bool   `json:"over_budget"`
	AccountingUnavailable bool   `json:"accounting_unavailable"`
	DegradedRecorded      bool   `json:"degraded_recorded"`
	EstimatedMicros       int64  `json:"estimated_micros"`
}
type Summary struct {
	Scope                  Scope     `json:"scope"`
	WindowStart            time.Time `json:"window_start"`
	WindowEnd              time.Time `json:"window_end"`
	ModelMicros            int64     `json:"model_micros"`
	TTSMicros              int64     `json:"tts_micros"`
	EstimatedMicros        int64     `json:"estimated_micros"`
	SupplierUsageMicros    int64     `json:"supplier_usage_micros"`
	SupplierInvoiceMicros  int64     `json:"supplier_invoice_micros"`
	ReservedMicros         int64     `json:"reserved_micros"`
	UnknownMicros          int64     `json:"unknown_micros"`
	UnresolvedRequests     int64     `json:"unresolved_requests"`
	AgedUnresolvedMicros   int64     `json:"aged_unresolved_micros"`
	AgedUnresolvedRequests int64     `json:"aged_unresolved_requests"`
	SettledRequests        int64     `json:"settled_requests"`
	TotalMicros            int64     `json:"total_micros"`
}

type Store interface {
	ReserveLiveCost(context.Context, Request, Policy, time.Time) (Reservation, error)
	FinishLiveCost(context.Context, Scope, string, Receipt, time.Time) error
	LiveCostSummary(context.Context, Scope, Policy, time.Time) (Summary, error)
}

type Controller struct {
	store  Store
	policy Policy
	now    func() time.Time
}

func New(store Store, policy Policy) *Controller {
	defaults := DefaultPolicy()
	if policy.Window <= 0 {
		policy.Window = defaults.Window
	}
	if policy.OptionalLimitMicros <= 0 {
		policy.OptionalLimitMicros = defaults.OptionalLimitMicros
	}
	if policy.TotalTargetMicros <= 0 {
		policy.TotalTargetMicros = defaults.TotalTargetMicros
	}
	return &Controller{store: store, policy: policy, now: func() time.Time { return time.Now().UTC() }}
}
func (c *Controller) Begin(ctx context.Context, request Request) (Reservation, error) {
	r, err := NormalizeRequest(request)
	if err != nil {
		return Reservation{}, err
	}
	var claimErr error
	if c != nil {
		exists, e := c.hasSpoolBegin(r)
		if e != nil {
			if r.Priority != PriorityNecessary {
				return Reservation{AccountingUnavailable: true}, fmt.Errorf("%w: %v", ErrAccountingUnavailable, e)
			}
			claimErr = e
		}
		if exists {
			return Reservation{Key: r.Key, Existing: true, Status: StatusUnknown, AccountingUnavailable: true, DegradedRecorded: true, EstimatedMicros: r.EstimatedMicros}, nil
		}
	}
	claimed := false
	if c != nil && r.Priority == PriorityNecessary && c.policy.SpoolDir != "" && claimErr == nil {
		claimErr = c.writeSpool(spoolEvent{Request: &r})
		claimed = claimErr == nil
		if errors.Is(claimErr, os.ErrExist) {
			return Reservation{Key: r.Key, Existing: true, Status: StatusUnknown, AccountingUnavailable: true, DegradedRecorded: true, EstimatedMicros: r.EstimatedMicros}, nil
		}
	}
	if c != nil && c.store != nil {
		reservation, e := c.store.ReserveLiveCost(ctx, r, c.policy, c.now())
		if e == nil {
			return reservation, nil
		}
		err = e
	}
	if errors.Is(err, ErrRequestConflict) || errors.Is(err, ErrReceiptConflict) {
		return Reservation{}, err
	}
	// Necessary supply is permitted to continue, but the caller must durably log
	// degraded accounting and must not describe its price as supplier-confirmed.
	reservation := Reservation{Key: r.Key, Allowed: r.Priority == PriorityNecessary, Status: StatusUnknown, AccountingUnavailable: true, EstimatedMicros: r.EstimatedMicros}
	if reservation.Allowed && c != nil {
		spoolErr := claimErr
		if !claimed && spoolErr == nil {
			spoolErr = c.writeSpool(spoolEvent{Request: &r})
		}
		reservation.DegradedRecorded = claimed || spoolErr == nil
		if errors.Is(spoolErr, os.ErrExist) {
			reservation.Allowed = false
			reservation.Existing = true
			reservation.DegradedRecorded = true
		} else if spoolErr != nil {
			err = fmt.Errorf("%v; 降级留痕失败: %v", err, spoolErr)
		}
	}
	return reservation, fmt.Errorf("%w: %v", ErrAccountingUnavailable, err)
}
func (c *Controller) Finish(ctx context.Context, key string, receipt Receipt) error {
	if err := ValidateReceipt(receipt); err != nil {
		return err
	}
	if c == nil {
		return ErrAccountingUnavailable
	}
	var err error
	if c.store != nil {
		err = c.store.FinishLiveCost(ctx, Scope{TenantID: receipt.TenantID, RoomID: receipt.RoomID}, key, receipt, c.now())
		if err == nil {
			return nil
		}
	} else {
		err = ErrAccountingUnavailable
	}
	if errors.Is(err, ErrRequestConflict) || errors.Is(err, ErrReceiptConflict) {
		return err
	}
	if spoolErr := c.writeSpool(spoolEvent{Key: key, Receipt: &receipt}); spoolErr != nil {
		return fmt.Errorf("%w: %v; 回执留痕失败: %v", ErrAccountingUnavailable, err, spoolErr)
	}
	return fmt.Errorf("%w: %v; 回执已耐久保留待补记", ErrAccountingUnavailable, err)
}
func (c *Controller) Summary(ctx context.Context, scope Scope) (Summary, error) {
	if c == nil || c.store == nil {
		return Summary{}, ErrAccountingUnavailable
	}
	if scope.TenantID <= 0 || scope.RoomID <= 0 {
		return Summary{}, errors.New("费用统计房间身份无效")
	}
	return c.store.LiveCostSummary(ctx, scope, c.policy, c.now())
}

func NormalizeRequest(r Request) (Request, error) {
	r.Key = strings.TrimSpace(r.Key)
	r.Provider = strings.TrimSpace(r.Provider)
	r.Priority = strings.TrimSpace(r.Priority)
	r.Reason = strings.TrimSpace(r.Reason)
	if r.Attribution != nil {
		attribution := *r.Attribution
		attribution.Source = strings.TrimSpace(attribution.Source)
		attribution.TabID = strings.TrimSpace(attribution.TabID)
		if attribution.Source == "" || len(attribution.Source) > 64 || len(attribution.TabID) > 128 || attribution.Round < 0 || attribution.SourceIndex < 0 ||
			(attribution.Source == "recording_mainline" && (attribution.Round < 1 || attribution.TabID == "")) {
			return r, errors.New("费用请求轮次归属无效")
		}
		r.Attribution = &attribution
	}
	if r.Priority == "" {
		r.Priority = PriorityOptional
	}
	if r.Key == "" || len(r.Key) > 128 || r.TenantID <= 0 || r.RoomID <= 0 || r.PlanID < 0 || r.SessionID < 0 || r.Provider == "" || len(r.Provider) > 96 || len(r.Model) > 160 || len(r.RunID) > 128 || len(r.Reason) > 512 || len(r.Fingerprint) > 256 {
		return r, errors.New("费用请求身份或字段无效")
	}
	for _, character := range r.Key {
		if character < 33 || character > 126 {
			return r, errors.New("费用请求标识须为可打印ASCII字符")
		}
	}
	if r.Kind != KindTTS && r.Kind != KindModel {
		return r, errors.New("费用请求类型无效")
	}
	if r.Priority != PriorityOptional && r.Priority != PriorityNecessary {
		return r, errors.New("费用请求优先级无效")
	}
	if r.Priority == PriorityNecessary && r.Reason == "" {
		return r, errors.New("必要超用请求必须记录业务原因")
	}
	if r.InputTokens < 0 || r.CachedInputTokens < 0 || r.CachedInputTokens > r.InputTokens || r.OutputTokens < 0 || r.Characters < 0 || r.EstimatedMicros < 0 {
		return r, errors.New("费用请求用量无效")
	}
	if r.EstimatedMicros == 0 && r.Kind == KindTTS && r.Characters > 0 {
		var err error
		r.EstimatedMicros, err = EstimateTTS(r.Characters, 160)
		if err != nil {
			return r, err
		}
	}
	if r.EstimatedMicros <= 0 {
		return r, errors.New("付费请求必须先提供大于零的费用预估")
	}
	return r, nil
}
func ValidateReceipt(r Receipt) error {
	if r.TenantID <= 0 || r.RoomID <= 0 {
		return errors.New("费用回执房间身份无效")
	}
	if r.Status != ReceiptSuccess && r.Status != ReceiptUnknown && r.Status != ReceiptNotSent {
		return errors.New("费用回执状态无效")
	}
	if r.ActualMicros < 0 || r.InputTokens < 0 || r.CachedInputTokens < 0 || r.CachedInputTokens > r.InputTokens || r.OutputTokens < 0 || r.Characters < 0 || len(r.ProviderRequestID) > 192 {
		return errors.New("费用回执用量无效")
	}
	if r.UsageSource != "" && r.UsageSource != SourceSentTextEstimate && r.UsageSource != SourceSupplierUsage && r.UsageSource != SourceSupplierInvoice {
		return errors.New("费用回执来源无效")
	}
	if (r.UsageSource == SourceSupplierUsage || r.UsageSource == SourceSupplierInvoice) && !r.ActualKnown {
		return errors.New("供应商费用必须有确定金额")
	}
	if r.ActualKnown && r.UsageSource != SourceSupplierUsage && r.UsageSource != SourceSupplierInvoice {
		return errors.New("预估费用不能标为供应商实际费用")
	}
	if r.UsageSource == SourceSupplierUsage && r.InputTokens == 0 && r.OutputTokens == 0 && r.Characters == 0 {
		return errors.New("供应商计量必须有实际用量")
	}
	if r.UsageSource == SourceSupplierInvoice && strings.TrimSpace(r.ProviderRequestID) == "" {
		return errors.New("供应商账单必须有关联请求号")
	}
	if r.Status != ReceiptSuccess && r.ActualKnown {
		return errors.New("非成功回执不能直接确认费用")
	}
	if r.EstimatedMicros != nil && (*r.EstimatedMicros < 0 || r.ActualKnown || r.Status != ReceiptSuccess || (r.UsageSource != "" && r.UsageSource != SourceSentTextEstimate)) {
		return errors.New("估计结算金额只能用于成功的非实际费用回执")
	}
	return nil
}

// SentCharacters is a conservative sent-text estimate, not supplier usage.
func SentCharacters(text string) int64 {
	var n int64
	for _, r := range text {
		if !unicode.IsSpace(r) {
			n++
		}
	}
	return n
}

// EstimateTTS defaults to the user's 1.6 CNY / 10,000 characters (160 micro-CNY / character).
func EstimateTTS(characters, microsPerCharacter int64) (int64, error) {
	if characters < 0 || microsPerCharacter < 0 || (microsPerCharacter > 0 && characters > math.MaxInt64/microsPerCharacter) {
		return 0, errors.New("语音费用预估越界")
	}
	return characters * microsPerCharacter, nil
}

// EstimateTokens prices supplier token usage using explicitly configured micro-CNY / million tokens.
func EstimateTokens(input, cached, output, inputRate, cachedRate, outputRate int64) (int64, error) {
	if input < 0 || cached < 0 || cached > input || output < 0 || inputRate < 0 || cachedRate < 0 || outputRate < 0 {
		return 0, errors.New("模型费用预估无效")
	}
	var total int64
	for _, x := range [][2]int64{{input - cached, inputRate}, {cached, cachedRate}, {output, outputRate}} {
		if x[1] > 0 && x[0] > math.MaxInt64/x[1] {
			return 0, errors.New("模型费用预估越界")
		}
		part := x[0] * x[1]
		if total > math.MaxInt64-part {
			return 0, errors.New("模型费用预估越界")
		}
		total += part
	}
	return total/1000000 + boolInt(total%1000000 != 0), nil
}
func boolInt(v bool) int64 {
	if v {
		return 1
	}
	return 0
}
