package decisionexecutor

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/interactionplan"
	"livecompanion/management/internal/model"
)

type welcomeStoreFixture struct {
	*v2Store
	claims []model.PlanMaterialClaim
}

func (s *welcomeStoreFixture) ListPlanMaterialClaims(_ context.Context, tenant, plan int64) ([]model.PlanMaterialClaim, error) {
	if tenant != 7 || plan != 3 {
		return nil, nil
	}
	return s.claims, nil
}

func TestWelcomeMaterialsExcludeConditionalAndUnreviewedClaims(t *testing.T) {
	claims := []model.PlanMaterialClaim{
		{ID: 1, ScopeKind: "plan", Content: "正宗南充特产", Status: "active"},
		{ID: 2, ScopeKind: "plan", Content: "全场包邮", Status: "active", Conditions: json.RawMessage(`[{"field":"region","op":"not_in","values":["新疆"]}]`)},
		{ID: 3, ScopeKind: "plan", Content: "全场包邮", Status: "active", Exceptions: json.RawMessage(`[{"field":"region","op":"in","values":["新疆"]}]`)},
		{ID: 4, ScopeKind: "plan", Content: "全场包邮", SourceQuote: "全场包邮，除新疆外", Status: "active"},
		{ID: 5, ScopeKind: "plan", Content: "随机优惠券", Status: "active"},
		{ID: 6, ScopeKind: "store", Content: "门店价95元", Status: "active"},
		{ID: 7, ScopeKind: "product", ProductLinkID: 7, Content: "整只900克", Status: "active"},
		{ID: 8, ScopeKind: "product", ProductLinkID: 0, Content: "未归属商品", Status: "active"},
		{ID: 9, ScopeKind: "plan", Content: "待审核商品信息", Status: "active", MaterialClaimDetails: model.MaterialClaimDetails{NeedsConfirmation: true}},
	}
	got := selectWelcomeMaterials(claims)
	if len(got) != 2 || got[0].ClaimID != 1 || got[1].ClaimID != 7 || got[1].LinkID != 7 {
		t.Fatalf("conditional or unresolved fact entered welcome: %#v", got)
	}
}

func TestWelcomeBenefitLabelDoesNotBypassConditions(t *testing.T) {
	claims := []model.PlanMaterialClaim{
		{ID: 1, ScopeKind: "plan", Content: "新品试吃", Status: "active", MaterialClaimDetails: model.MaterialClaimDetails{IsBenefit: true}},
		{ID: 2, ScopeKind: "plan", Content: "满99赠品", Status: "active", MaterialClaimDetails: model.MaterialClaimDetails{IsBenefit: true}, Conditions: json.RawMessage(`[{"field":"amount","op":"gte","values":[99]}]`)},
	}
	pool := selectWelcomeMaterials(claims)
	if len(pool) != 1 || pool[0].ClaimID != 1 || pool[0].Kind != "offer" {
		t.Fatalf("benefit label classification or condition guard failed: %#v", pool)
	}
}

func TestWelcomeDoesNotPairDifferentProductOffer(t *testing.T) {
	pool := []welcomeMaterial{
		{ClaimID: 1, Kind: "product", LinkID: 7},
		{ClaimID: 2, Kind: "offer", LinkID: 8},
		{ClaimID: 3, Kind: "offer", LinkID: 0},
	}
	got := pickWelcomeMaterials(pool, nil, "same task")
	if len(got) != 2 || got[0].ClaimID != 1 || got[1].ClaimID != 3 {
		t.Fatalf("cross-product offer paired with product: %#v", got)
	}
}

func TestWelcomeWriterRequiresClaimAttribution(t *testing.T) {
	if _, _, ok := welcomeWriterJSON(`{"body":"欢迎看看今天的产品。","used_claim_ids":[]}`, []int64{3}); ok {
		t.Fatal("unattributed welcome accepted")
	}
	if _, _, ok := welcomeWriterJSON(`{"body":"欢迎看看今天的产品。","used_claim_ids":[4]}`, []int64{3}); ok {
		t.Fatal("foreign claim accepted")
	}
	if body, used, ok := welcomeWriterJSON(`{"body":"欢迎看看今天的产品。","used_claim_ids":[3]}`, []int64{3}); !ok || body == "" || len(used) != 1 || used[0] != 3 {
		t.Fatal("valid selected claim rejected")
	}
}

func TestWelcomeRetryKeepsTheSameMaterialRequest(t *testing.T) {
	room := welcomeMaterialKey{tenant: 901, room: 902, plan: 903}
	key := welcomeDraftKey{welcomeMaterialKey: room, session: 904, task: "one-task"}
	pool := []welcomeMaterial{{ClaimID: 11, Kind: "product", Text: "整只900克"}, {ClaimID: 12, Kind: "product", Text: "五香口味"}}
	first, err := welcomeDraftFor(key, pool, "直播间简介")
	if err != nil {
		t.Fatal(err)
	}
	rememberWelcome(room, "第一条已经生成", []int64{first.selected[0].ClaimID})
	second, err := welcomeDraftFor(key, pool, "修改后的简介")
	if err != nil || len(second.selected) != len(first.selected) || second.selected[0].ClaimID != first.selected[0].ClaimID ||
		second.intro != first.intro || len(second.recent) != len(first.recent) {
		t.Fatalf("same task changed its paid request after retry: first=%#v second=%#v err=%v", first, second, err)
	}
	for i := range pool {
		if pool[i].ClaimID == first.selected[0].ClaimID {
			pool[i].Text = "已修改"
		}
	}
	if _, err = welcomeDraftFor(key, pool, "直播间简介"); err == nil {
		t.Fatal("changed material reused an old welcome task")
	}
}

func TestWelcomeWriterUsesUnconditionalSemanticClaim(t *testing.T) {
	w, store, _, _, _, session, _ := v2Fixture(t, 50601)
	store.publishedVersion.GenerationContext = map[string]any{"room_intro": "四川南充特产直播间"}
	w.store = &welcomeStoreFixture{v2Store: store, claims: []model.PlanMaterialClaim{
		{ID: 71, ScopeKind: "plan", Content: "正宗南充特产", SourceQuote: "正宗南充特产", Status: "active"},
		{ID: 72, ScopeKind: "plan", Content: "全场包邮", SourceQuote: "全场包邮，除新疆外", Status: "active"},
	}}
	snapshot, _ := interactionplan.Frozen.ForSession(session.TenantID, session.RoomID, session.ID)
	requests := []string{}
	complete := func(_ context.Context, req agentgateway.Request) (agentgateway.Response, error) {
		if req.Stage != "interaction_welcome_writer" || req.ResponseFormat != agentgateway.ResponseJSON || len(req.Messages) != 2 {
			t.Fatalf("welcome used another model route: %#v", req)
		}
		payload := req.Messages[1].Content
		if !strings.Contains(payload, "四川南充特产直播间") || !strings.Contains(payload, "正宗南充特产") ||
			strings.Contains(payload, "全场包邮") || strings.Contains(payload, "除新疆") {
			t.Fatalf("welcome facts were not filtered: %s", payload)
		}
		requests = append(requests, payload)
		return agentgateway.Response{Text: `{"body":"欢迎来看看南充特产。","used_claim_ids":[71]}`}, nil
	}
	for i := 0; i < 2; i++ {
		body, err := w.PrepareWelcomeV2Body(context.Background(), session, "same-task", "welcome_batch", snapshot, complete)
		if err != nil || body != "欢迎来看看南充特产。" {
			t.Fatal(body, err)
		}
	}
	if len(requests) != 2 || requests[0] != requests[1] {
		t.Fatal("same task changed its model payload on retry", requests)
	}
}
