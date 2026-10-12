package httpapi

import (
	"context"
	"encoding/json"
	"livecompanion/management/internal/anchorcycle"
	"reflect"
	"strings"
	"testing"
)

func productScopeInputs() (*anchorcycle.ProductFocus, []cycleScopeFactInput, []anchorcycle.Task) {
	return &anchorcycle.ProductFocus{Products: []anchorcycle.FocusProduct{{LinkKey: "1", Roles: []string{"main"}}, {LinkKey: "2", Roles: []string{"traffic"}}}}, []cycleScopeFactInput{{ID: 1, Text: "- [商品卡] 链接=1；商品=整只鸭；规格=900g；日常价=87.6"}, {ID: 2, Text: "- [商品卡] 链接=2；商品=半只鸭；规格=450g；日常价=48.8"}, {ID: 3, Text: "- [other/品牌] 共同品牌历史"}}, []anchorcycle.Task{{SellingPoint: "工艺", Angle: "传统制作", Key: "工艺×传统制作"}}
}

func TestCycleProductFocusTTSProofAndCacheIsolation(t *testing.T) {
	scope, facts, tasks := productScopeInputs()
	if err := parseCycleProductScope(validScopeResponse, scope, facts, tasks); err != nil {
		t.Fatal(err)
	}
	policy, _, err := loadCycleProductPolicy()
	if err != nil {
		t.Fatal(err)
	}
	scope.Policy = policy
	s := &anchorcycle.State{TenantID: 81001, PlanID: 92002, RoomID: 73003, Phase: "body", CycleNumber: 1, Queue: tasks, ProductFocus: scope}
	s.Config.MaxChars = 300
	s.EnsureProductFocus()
	c := anchorcycle.Candidate{Text: "2号链接越界了"}
	(&Server{}).auditCycleProductCandidate(context.Background(), s, &c)
	if c.Text != "2号链接越界了" || c.OutputText != "2号链接越界了。" || c.ProductAudit == nil || c.ProductAudit.Passed || c.ProductAudit.TextSHA != anchorcycle.ProductDigest(cycleVoiceText(c)) {
		t.Fatal("TTS normalization proof mismatch or raw text lost", c)
	}
	segment := anchorcycle.Segment{Phase: "body", Task: s.Task(), Selected: c}
	group := cycleVoiceCacheGroup(s, segment)
	s.RoomID++
	if group == cycleVoiceCacheGroup(s, segment) {
		t.Fatal("room cache not isolated")
	}
	s.RoomID--
	s.PlanID++
	if group == cycleVoiceCacheGroup(s, segment) {
		t.Fatal("plan cache not isolated")
	}
	s.PlanID--
	s.TenantID++
	if group == cycleVoiceCacheGroup(s, segment) {
		t.Fatal("tenant cache not isolated")
	}
	s.TenantID--
	s.ProductFocus.Policy.MainSharePercent = 80
	if group == cycleVoiceCacheGroup(s, segment) {
		t.Fatal("new policy reused old cache")
	}
	if _, err := (&Server{}).cachedCycleVoice(context.Background(), s, segment); err == nil {
		t.Fatal("cached voice bypassed product audit")
	}
	if _, err := (&Server{}).archiveCycleVoiceCache(context.Background(), s, segment, nil, nil, 0); err == nil {
		t.Fatal("unaudited voice archived")
	}
}

const validScopeResponse = `{"facts":[{"id":1,"links":["1"],"shared":false},{"id":2,"links":["2"],"shared":false},{"id":3,"links":[],"shared":true}],"tasks":[{"id":1,"links":[],"shared":true,"comparison":false}]}`

func TestCycleProductFocusScopeOutputValidation(t *testing.T) {
	scope, facts, tasks := productScopeInputs()
	if err := parseCycleProductScope(validScopeResponse, scope, facts, tasks); err != nil {
		t.Fatal(err)
	}
	if scope.Facts[0].Text != facts[0].Text || !scope.Facts[2].Shared {
		t.Fatal("model rewrote original facts")
	}
	for _, raw := range []string{strings.Replace(validScopeResponse, `"links":["1"]`, `"links":["2"]`, 1), strings.Replace(validScopeResponse, `"links":["1"]`, `"links":["99"]`, 1), strings.Replace(validScopeResponse, `"shared":false`, `"extra":false`, 1), strings.Replace(validScopeResponse, `"links":[]`, `"links":null`, 1), strings.Replace(validScopeResponse, `"id":2`, `"id":1`, 1), strings.Replace(validScopeResponse, `"comparison":false`, `"comparison":true`, 1), validScopeResponse + ` {}`, `{"facts":[],"tasks":[]}`} {
		scope, facts, tasks = productScopeInputs()
		if parseCycleProductScope(raw, scope, facts, tasks) == nil {
			t.Fatal("invalid scope accepted", raw)
		}
	}
}
func TestCycleProductFocusPromptUsesScopedFactsAndReturnsToSingle(t *testing.T) {
	scope, facts, tasks := productScopeInputs()
	if err := parseCycleProductScope(validScopeResponse, scope, facts, tasks); err != nil {
		t.Fatal(err)
	}
	p, source, err := loadCycleProductPolicy()
	if err != nil {
		t.Fatal(err)
	}
	scope.Policy, scope.PolicySource = p, source
	s := &anchorcycle.State{Phase: "opening", CycleNumber: 1, ProductFocus: scope, Queue: tasks, Facts: "全量事实包含他品"}
	s.EnsureProductFocus()
	md, err := renderCycleProductScope(s)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(md, "87.6") || strings.Contains(md, "48.8") || strings.Contains(md, "全量事实包含他品") {
		t.Fatal("other product facts leaked", md)
	}
	c := anchorcycle.Candidate{ContentIssue: "product_focus_cross_link", ProductAudit: &anchorcycle.ProductAudit{Reason: "出现2号链接"}}
	s.Attempts = []anchorcycle.Candidate{c}
	md, err = renderCycleProductScope(s)
	if err != nil || !strings.Contains(md, "出现2号链接") {
		t.Fatal("no retry feedback", md, err)
	}
	raw, _ := json.Marshal(liveAnchorScheme{Version: 1, ProductFocus: scope})
	var saved liveAnchorScheme
	_ = json.Unmarshal(raw, &saved)
	if !reflect.DeepEqual(saved.ProductFocus.Policy, p) || saved.ProductFocus.Facts[0].Text != facts[0].Text {
		t.Fatal("version snapshot lost")
	}
}
