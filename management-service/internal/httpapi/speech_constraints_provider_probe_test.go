package httpapi

import (
	"context"
	"encoding/json"
	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/config"
	"livecompanion/management/internal/contentpools"
	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
	"os"
	"strings"
	"testing"
	"time"
)

func TestSpeechConstraintNaturalLanguageProvider(t *testing.T) {
	if os.Getenv("FACT_SPEECH_PREVIEW_PROBE") != "1" {
		t.Skip("explicit paid readonly replay required")
	}
	store, e := appdb.Open(config.Load())
	if e != nil {
		t.Fatal(e)
	}
	defer store.Close()
	s := &Server{store: store}
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()
	source := contentpools.Sources{Products: []contentpools.Product{{ID: 1, Link: "1", Name: "开袋即食鸭肉"}}}
	f := model.LiveAgentPlanFact{Key: "食用方式限制", Value: "开袋即食，不用二次加工"}
	c, e := s.normalizeSpeechConstraint(ctx, 14, 4, f, "早上赶时间，撕开包装就能吃，省时省心。", "不要拿早餐举例", source)
	if e != nil {
		t.Fatal(e)
	}
	if *c.Question != "" || *c.Constraint == "" || !strings.Contains(*c.Constraint, "早餐") {
		t.Fatal("clear intent was not normalized", c)
	}
	t.Log("NORMALIZED_NATURAL_REQUIREMENT", *c.Constraint)
	state := anchorcycle.State{TenantID: 14, PlanID: 4, RoomID: 26, LatestExpressionConstraints: []map[string]any{{"表达限制": *c.Constraint, "用户原话（理解纠正范围，不能扩张限制）": "不要拿早餐举例", "用户采用的实时表达约束": true}}}
	if e = installP0Mother(&state); e != nil {
		t.Fatal(e)
	}
	for _, tc := range []struct {
		text string
		pass bool
	}{{"早上赶时间，撕开包装就能吃，省时省心。", false}, {"拿它当早餐，开袋吃很方便。", false}, {"超过下午6点下单，一般第二天早上发货。", true}, {"下班累了，打开包装就能直接吃。", true}} {
		candidate := lectureTextCandidate(tc.text)
		s.auditSpeechExpressionConstraints(ctx, &state, &candidate)
		if candidate.ExpressionAudit == nil || candidate.ExpressionAudit.Error != "" || candidate.ExpressionAudit.Passed != tc.pass {
			t.Errorf("semantic constraint mismatch %s %+v", tc.text, candidate.ExpressionAudit)
		} else {
			t.Log("SEMANTIC_CONSTRAINT", tc.text, candidate.ExpressionAudit.Passed)
		}
	}
	// Use the same normal generation materials and speech-model role, with only
	// the unadopted fixture in memory. Never write customer rules/facts/versions.
	synthetic := model.LiveAgentPlanFact{ID: 99001, TenantID: 14, PlanID: 4, VersionNo: 1, Key: f.Key, Value: f.Value}
	prepared, e := s.prepareFactSpeechPreview(ctx, 14, 4, 26, synthetic, "约束通用性模拟")
	if e != nil {
		t.Fatal(e)
	}
	prepared.LatestExpressionConstraints = state.LatestExpressionConstraints
	messages, e := renderCycleTextMessages(&prepared)
	if e != nil {
		t.Fatal(e)
	}
	response, _, e := s.completeAnchorStyleWorkbench(ctx, prepared.TextModel.ID, "mainline_text", agentgateway.ResponseText, messages, 700, false)
	if e != nil {
		t.Fatal(e)
	}
	candidate := lectureTextCandidate(response.Text)
	s.auditSpeechExpressionConstraints(ctx, &prepared, &candidate)
	if candidate.ExpressionAudit == nil || !candidate.ExpressionAudit.Passed || candidate.ExpressionAudit.Error != "" {
		t.Fatal("same speech model ignored adopted constraint", candidate.ExpressionAudit)
	}
	raw, _ := json.Marshal(map[string]any{"text": response.Text, "review": candidate.ExpressionAudit})
	t.Log("GENERATION_WITH_CONSTRAINT", string(raw))
}
