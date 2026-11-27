package decisionexecutor

import (
	"math/rand"
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/interactionplan"
	"livecompanion/management/internal/interactionpolicy"
	"livecompanion/management/internal/model"
	"sort"
	"strings"
	"sync"
	"time"
)

// welcomeMaterial is one speakable claim selected from the plan's semantic
// library. Kind is only an internal rotation tag (product/offer); it is not a
// business "selling point angle".
type welcomeMaterial struct {
	ClaimID   int64  `json:"claim_id"`
	Kind      string `json:"kind"`
	Topic     string `json:"topic"`
	Text      string `json:"text"`
	Source    string `json:"source_quote"`
	Condition string `json:"condition,omitempty"`
	Safe      bool   `json:"safe"`
	LinkID    int64  `json:"link_id,omitempty"`
}

type welcomeMaterialStore interface {
	ListPlanMaterialClaims(context.Context, int64, int64) ([]model.PlanMaterialClaim, error)
}

type welcomeWriterResult struct {
	Body         string  `json:"body"`
	UsedClaimIDs []int64 `json:"used_claim_ids"`
}

var welcomeMaterialMu sync.Mutex

type welcomeMaterialKey struct{ tenant, room, plan int64 }
type welcomeDraftKey struct {
	welcomeMaterialKey
	session int64
	task    string
}
type welcomeDraft struct {
	selected []welcomeMaterial
	recent   []string
	intro    string
	at       time.Time
}

var recentWelcomeClaims = map[welcomeMaterialKey][]int64{}
var recentWelcomeBenefitClaims = map[welcomeMaterialKey][]int64{}
var recentWelcomeTexts = map[welcomeMaterialKey][]string{}
var welcomeDrafts = map[welcomeDraftKey]welcomeDraft{}

// PrepareWelcomeV2Body is the shared entry point for the upstream prepared
// welcome and the executor's unprepared/manual path.
func (w *Worker) PrepareWelcomeV2Body(ctx context.Context, session model.LiveRuntimeSession, task, kind string, snapshot interactionplan.Snapshot, complete interactionplan.Complete) (string, error) {
	if w == nil || !isWelcomeMission(kind) || task == "" ||
		snapshot.Scope.Tenant != session.TenantID || snapshot.Scope.Room != session.RoomID ||
		snapshot.Scope.Session != session.ID || snapshot.Scope.Plan <= 0 {
		return "", fmt.Errorf("welcome writer: task scope mismatch")
	}
	body, _, err := w.generateWelcomeV2Body(ctx, session, decisionItem{ID: task, MissionKind: kind}, snapshot, complete)
	return body, err
}

func welcomeOfferText(text string) bool {
	for _, word := range []string{"包邮", "免邮", "优惠", "券", "赠", "减", "福利", "礼品", "活动"} {
		if strings.Contains(text, word) {
			return true
		}
	}
	return false
}

func welcomeHasConditions(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) > 0 && !bytes.Equal(trimmed, []byte("[]")) && !bytes.Equal(trimmed, []byte("null"))
}

// Imported structured conditions take precedence. The wording check catches
// older rows whose condition was left only in the quoted material.
func welcomeConditionalText(text string) bool {
	for _, marker := range []string{"如果", "若是", "只有", "仅限", "限于", "满", "除", "不含", "部分", "指定", "需要", "需另", "额外", "加收", "随机", "以实际", "以物流"} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func (w *Worker) welcomeMaterialPool(ctx context.Context, session model.LiveRuntimeSession, planID int64) ([]welcomeMaterial, error) {
	store, ok := w.store.(welcomeMaterialStore)
	if !ok || w.store == nil || planID <= 0 {
		return nil, fmt.Errorf("welcome materials: semantic library unavailable")
	}
	claims, err := store.ListPlanMaterialClaims(ctx, session.TenantID, planID)
	if err != nil {
		return nil, err
	}
	materials := selectWelcomeMaterials(claims)
	seenIDs := map[int64]bool{}
	for _, m := range materials {
		seenIDs[m.ClaimID] = true
	}
	for _, benefit := range w.loadWelcomeBenefitPool(ctx, session.TenantID, planID) {
		if !seenIDs[benefit.ClaimID] {
			materials = append(materials, benefit)
			seenIDs[benefit.ClaimID] = true
		}
	}
	return materials, nil
}

func selectWelcomeMaterials(claims []model.PlanMaterialClaim) []welcomeMaterial {
	out := []welcomeMaterial{}
	seen := map[string]bool{}
	for _, c := range claims {
		if c.Status != "" && c.Status != "active" {
			continue
		}
		// A welcome has no destination, platform or other viewer-specific
		// qualifiers. A conditional or unreviewed claim cannot be shortened
		// into an unconditional spoken promise, including a benefit.
		if c.Destination != "" && c.Destination != "fact" {
			continue
		}
		if c.NeedsConfirmation || c.NeedsSplit || len(c.Risks) > 0 || len(c.RiskFlags) > 0 ||
			len(c.P0Hits) > 0 || welcomeHasConditions(c.Conditions) ||
			welcomeHasConditions(c.Exceptions) || welcomeConditionalText(c.Content+" "+c.SourceQuote+" "+c.SourceSentence) {
			continue
		}
		if c.ScopeKind != "plan" && c.ScopeKind != "product" && c.ScopeKind != "object" {
			continue
		}
		// 对象目录已支持自定义 object：只要有对象绑定就可用，不再要求必须是商品链接。
		// 商品链接仍按 LinkID 归组；自定义 object 保持 0，仅参与去重。
		linkID := c.ProductLinkID
		drop := false
		for _, target := range c.ObjectTargets {
			switch target.Kind {
			case "global", "plan":
				continue
			case "product":
				if target.LinkID <= 0 || (linkID > 0 && linkID != target.LinkID) {
					drop = true
				} else {
					linkID = target.LinkID
				}
			case "object":
				// 自定义对象：没有链接概念，直接可用。
			default:
				drop = true
			}
			if drop {
				break
			}
		}
		if drop || c.ScopeKind == "product" && linkID == 0 {
			continue
		}
		// 欢迎用短值：属性取 Attribute，值取 Value，避免整句被照抄成念稿。
		text := strings.TrimSpace(c.Value)
		if text == "" {
			text = strings.TrimSpace(c.Content)
		}
		// 带条件的素材不进欢迎池：短值脱离上下文会变成错误承诺。
		if welcomeHasConditions(c.Conditions) || welcomeHasConditions(c.Exceptions) {
			continue
		}
		key := fmt.Sprintf("%d:%s", linkID, text)
		if text == "" || seen[key] {
			continue
		}
		source := strings.TrimSpace(c.SourceQuote)
		if source == "" {
			source = text
		}
		kind := "product"
		if c.IsBenefit || welcomeOfferText(c.Topic+" "+text) {
			kind = "offer"
		}
		condition := ""
		if c.IsBenefit {
			condition = strings.TrimSpace(string(c.Conditions))
			if condition == "" {
				condition = strings.TrimSpace(string(c.Exceptions))
			}
			if condition == "" && welcomeConditionalText(text) {
				condition = "原文含条件，需按原文表述"
			}
		}
		safe := c.IsBenefit && !c.NeedsConfirmation && !c.NeedsSplit && len(c.Risks) == 0 && len(c.RiskFlags) == 0 && len(c.P0Hits) == 0 && condition == ""
		seen[key] = true
		out = append(out, welcomeMaterial{ClaimID: c.ID, Kind: kind, Topic: strings.TrimSpace(c.Topic), Text: text, Source: source, Condition: condition, Safe: safe, LinkID: linkID})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ClaimID < out[j].ClaimID })
	return out
}

func welcomeDraftFor(key welcomeDraftKey, pool []welcomeMaterial, intro string) (welcomeDraft, error) {
	welcomeMaterialMu.Lock()
	defer welcomeMaterialMu.Unlock()
	for old, draft := range welcomeDrafts {
		if time.Since(draft.at) > 10*time.Minute {
			delete(welcomeDrafts, old)
		}
	}
	if draft, ok := welcomeDrafts[key]; ok {
		available := map[int64]welcomeMaterial{}
		for _, claim := range pool {
			available[claim.ClaimID] = claim
		}
		for _, selected := range draft.selected {
			if current, ok := available[selected.ClaimID]; !ok || current.Text != selected.Text || current.Source != selected.Source {
				return welcomeDraft{}, fmt.Errorf("welcome writer: selected material changed")
			}
		}
		return draft, nil
	}
	seed := fmt.Sprintf("%d:%d:%d:%d:%s", key.tenant, key.room, key.plan, key.session, key.task)
	draft := welcomeDraft{
		selected: pickWelcomeMaterialsSplit(pool, recentWelcomeClaims[key.welcomeMaterialKey], recentWelcomeBenefitClaims[key.welcomeMaterialKey], seed),
		recent:   append([]string(nil), recentWelcomeTexts[key.welcomeMaterialKey]...),
		intro:    intro,
		at:       time.Now(),
	}
	if len(draft.selected) == 0 {
		return welcomeDraft{}, fmt.Errorf("welcome writer: no selected materials")
	}
	welcomeDrafts[key] = draft
	return draft, nil
}

func rememberWelcome(key welcomeMaterialKey, body string, claims []int64) {
	rememberWelcomeParts(key, body, claims, nil)
}

func rememberWelcomeParts(key welcomeMaterialKey, body string, products, benefits []int64) {
	welcomeMaterialMu.Lock()
	defer welcomeMaterialMu.Unlock()
	recentWelcomeClaims[key] = append(recentWelcomeClaims[key], products...)
	if len(recentWelcomeClaims[key]) > 16 {
		recentWelcomeClaims[key] = recentWelcomeClaims[key][len(recentWelcomeClaims[key])-16:]
	}
	recentWelcomeBenefitClaims[key] = append(recentWelcomeBenefitClaims[key], benefits...)
	if len(recentWelcomeBenefitClaims[key]) > 8 {
		recentWelcomeBenefitClaims[key] = recentWelcomeBenefitClaims[key][len(recentWelcomeBenefitClaims[key])-8:]
	}
	recentWelcomeTexts[key] = append(recentWelcomeTexts[key], body)
	if len(recentWelcomeTexts[key]) > 5 {
		recentWelcomeTexts[key] = recentWelcomeTexts[key][len(recentWelcomeTexts[key])-5:]
	}
}

func pickWelcomeMaterials(pool []welcomeMaterial, recent []int64, seed string) []welcomeMaterial {
	return pickWelcomeMaterialsSplit(pool, recent, nil, seed)
}

func pickWelcomeMaterialsSplit(pool []welcomeMaterial, recentProducts, recentBenefits []int64, seed string) []welcomeMaterial {
	if len(pool) == 0 {
		return nil
	}
	usedProducts := map[int64]bool{}
	for _, id := range recentProducts {
		usedProducts[id] = true
	}
	usedBenefits := map[int64]bool{}
	for _, id := range recentBenefits {
		usedBenefits[id] = true
	}
	offset := 0
	if sum := sha1.Sum([]byte(seed)); len(sum) > 0 {
		offset = int(sum[0]) + int(sum[1])<<8
	}
	ordered := make([]welcomeMaterial, 0, len(pool))
	for i := 0; i < len(pool); i++ {
		ordered = append(ordered, pool[(offset+i)%len(pool)])
	}
	var product, offer *welcomeMaterial
	for i := range ordered {
		if ordered[i].Kind != "offer" && !usedProducts[ordered[i].ClaimID] {
			product = &ordered[i]
			break
		}
	}
	if product == nil {
		for i := range ordered {
			if ordered[i].Kind != "offer" {
				product = &ordered[i]
				break
			}
		}
	}
	for i := range ordered {
		if ordered[i].Kind != "offer" || usedBenefits[ordered[i].ClaimID] {
			continue
		}
		if product != nil && ordered[i].LinkID != 0 && ordered[i].LinkID != product.LinkID {
			continue
		}
		offer = &ordered[i]
		break
	}
	if offer == nil {
		for i := range ordered {
			if ordered[i].Kind != "offer" {
				continue
			}
			if product != nil && ordered[i].LinkID != 0 && ordered[i].LinkID != product.LinkID {
				continue
			}
			offer = &ordered[i]
			break
		}
	}
	selected := []welcomeMaterial{}
	if product != nil {
		selected = append(selected, *product)
	}
	if offer != nil {
		selected = append(selected, *offer)
	}
	return selected
}

func welcomeWriterJSON(body string, claims []int64) (string, []int64, bool) {
	body = strings.TrimSpace(body)
	body = strings.TrimPrefix(body, "```json")
	body = strings.TrimPrefix(body, "```")
	body = strings.TrimSuffix(body, "```")
	body = strings.TrimSpace(body)
	var parsed welcomeWriterResult
	if json.Unmarshal([]byte(body), &parsed) != nil || strings.TrimSpace(parsed.Body) == "" || len(parsed.UsedClaimIDs) == 0 {
		return "", nil, false
	}
	allowed := map[int64]bool{}
	for _, id := range claims {
		allowed[id] = true
	}
	for _, id := range parsed.UsedClaimIDs {
		if !allowed[id] {
			return "", nil, false
		}
	}
	return strings.TrimSpace(parsed.Body), parsed.UsedClaimIDs, true
}

// generateWelcomeV2Body produces the V2 welcome body from a rotated slice of
// semantic-library claims. It deliberately does not use the generic interaction
// batch, because that generic prompt converges on the same full fact list.
func (w *Worker) generateWelcomeV2Body(ctx context.Context, session model.LiveRuntimeSession, item decisionItem, snapshot interactionplan.Snapshot, complete interactionplan.Complete) (string, []int64, error) {
	pool, err := w.welcomeMaterialPool(ctx, session, snapshot.Scope.Plan)
	if err != nil {
		return "", nil, fmt.Errorf("welcome writer: semantic materials unavailable: %w", err)
	}
	if len(pool) == 0 {
		return "", nil, fmt.Errorf("welcome writer: no unconditional semantic materials")
	}
	key := welcomeMaterialKey{session.TenantID, session.RoomID, snapshot.Scope.Plan}
	intro, err := w.welcomeRoomIntro(ctx, session, snapshot)
	if err != nil {
		return "", nil, err
	}
	draft, err := welcomeDraftFor(welcomeDraftKey{welcomeMaterialKey: key, session: session.ID, task: item.ID}, pool, intro)
	if err != nil {
		return "", nil, err
	}
	selected := draft.selected
	// 80% 的欢迎带一条福利；剩下 20% 不带，收口换成规格/场景/互动/身份。
	// 概率由程序控制：不带 offer 素材时，模型无从提福利。
	includeOffer := rand.Float64() < 0.8
	hasOffer := false
	filtered := make([]welcomeMaterial, 0, len(selected))
	for _, m := range selected {
		if m.Kind == "offer" {
			hasOffer = true
			if !includeOffer {
				continue
			}
		}
		filtered = append(filtered, m)
	}
	if hasOffer || !includeOffer {
		selected = filtered
	}

	prompt, err := interactionpolicy.Read("interaction-welcome-writer.md")
	if err != nil {
		return "", nil, err
	}
	payload, _ := json.Marshal(map[string]any{
		"room_intro":     draft.intro,
		"persona":        snapshot.PlanSections.PersonaMD,
		"red_lines":      snapshot.PlanSections.RedLinesMD,
		"materials":      selected,
		"recent_welcome": draft.recent,
	})
	request := agentgateway.Request{Stage: "interaction_welcome_writer", Messages: []agentgateway.Message{{Role: "system", Content: string(prompt) + "\n参考 persona 的主播口吻，并遵守 red_lines。"}, {Role: "user", Content: string(payload)}}, MaxTokens: 400, ResponseFormat: agentgateway.ResponseJSON, EnableThinking: false, Timeout: 8 * time.Second}
	response, err := complete(ctx, request)
	if err != nil {
		return "", nil, err
	}
	claimIDs := make([]int64, 0, len(selected))
	for _, m := range selected {
		claimIDs = append(claimIDs, m.ClaimID)
	}
	body, usedClaimIDs, ok := welcomeWriterJSON(response.Text, claimIDs)
	if !ok {
		return "", nil, fmt.Errorf("welcome writer: invalid JSON")
	}
	body = sanitizeWelcomeBody(body)
	if body == "" {
		return "", nil, fmt.Errorf("welcome writer: unsafe body")
	}
	if err := interactionplan.Validate(snapshot, body); err != nil {
		return "", nil, err
	}
	usedSet := map[int64]bool{}
	for _, id := range usedClaimIDs {
		usedSet[id] = true
	}
	products, benefits := []int64{}, []int64{}
	for _, m := range selected {
		if !usedSet[m.ClaimID] {
			continue
		}
		if m.Kind == "offer" {
			benefits = append(benefits, m.ClaimID)
		} else {
			products = append(products, m.ClaimID)
		}
	}
	rememberWelcomeParts(key, body, products, benefits)
	return body, usedClaimIDs, nil
}

func (w *Worker) welcomeRoomIntro(ctx context.Context, session model.LiveRuntimeSession, snapshot interactionplan.Snapshot) (string, error) {
	if w == nil || w.store == nil {
		return "", fmt.Errorf("welcome writer: plan store unavailable")
	}
	version, err := w.store.GetPublishedLiveAgentPlanVersionForRoom(ctx, session.TenantID, session.RoomID)
	if err != nil {
		return "", err
	}
	if version.PlanID != snapshot.Scope.Plan || session.ID > 0 && version.ID != snapshot.Scope.Version {
		return "", fmt.Errorf("welcome writer: published plan changed")
	}
	intro, _ := version.GenerationContext["room_intro"].(string)
	return strings.TrimSpace(intro), nil
}

func welcomeMaterialDigest(ids []int64) string {
	raw := fmt.Sprint(ids)
	sum := sha1.Sum([]byte(raw))
	return hex.EncodeToString(sum[:])
}
