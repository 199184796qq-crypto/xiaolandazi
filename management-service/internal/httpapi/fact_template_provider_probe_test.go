package httpapi

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/config"
	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/lecturetask"
	"livecompanion/management/internal/model"
)

func TestFactReviewPolicyProvider(t *testing.T) {
	if os.Getenv("FACT_SPEECH_PREVIEW_PROBE") != "1" {
		t.Skip("explicit paid replay required")
	}
	store, err := appdb.Open(config.Load())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	s := &Server{store: store}
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()
	if cfg, e := store.SpeechModels(ctx); e == nil {
		selected := cfg.ReviewDefaultID
		if selected == "" {
			selected = cfg.SystemDefaultID
		}
		for _, p := range cfg.Profiles {
			if p.ID == selected {
				t.Logf("ACTUAL_REVIEW_ROLE alias=%s model=%s explicit_review=%t", p.Name, p.Model, cfg.ReviewDefaultID != "")
			}
		}
	}
	base := anchorcycle.State{TenantID: 14, PlanID: 4, RoomID: 26}
	if err = installP0Mother(&base); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, facts, negative, text string
		pass, advice                bool
	}{
		{"物流无负要求", "一般当天发货", "", "今天发，明天到。", true, true},
		{"物流有负要求", "一般当天发货", "不准说物流时效。", "今天发，明天到。", false, false},
		{"普通手感", "杯身使用304不锈钢", "", "杯身是304不锈钢，拿着沉甸甸，质感扎实。", true, false},
		{"功能未确认", "杯身使用304不锈钢", "", "这个杯子能保温12小时。", false, false},
		{"即食新增用法", "这款成品开袋即可直接吃", "", "开袋可以吃，还可以煮汤炖菜。", false, false},
		{"明确支持加热", "这款成品开袋直接吃，也可以用空气炸锅加热", "", "开袋直接吃，也可以用空气炸锅加热。", true, false},
		{"普通来源待核对", "土麻鸭产自嘉陵江流域，稻田水塘中散养", "", "鸭子喝着嘉陵江水，吃着稻谷和自然草虫。", true, true},
	} {
		guidance := []map[string]string{}
		if tc.negative != "" {
			guidance = append(guidance, map[string]string{"要求": "负向表达约束", "内容": tc.negative})
		}
		task := anchorcycle.Task{ProductMode: "lecture", LectureFacts: tc.facts, LecturePurpose: "审核当前表述", Angle: "当前内容", FactScope: &anchorcycle.TaskFactScope{Facts: []anchorcycle.HookFact{{Data: tc.facts, ExpressionGuidance: guidance}}}}
		state := base
		state.Lectures = &anchorcycle.LectureRuntime{Active: &task}
		c := lectureTextCandidate(tc.text)
		s.auditLectureCandidate(ctx, &state, &c)
		if c.LectureAudit == nil || c.LectureAudit.Error != "" || c.LectureAudit.Passed != tc.pass || tc.advice && len(c.LectureAudit.Suggestions) == 0 {
			t.Errorf("%s unexpected audit %+v", tc.name, c.LectureAudit)
		} else {
			raw, _ := json.Marshal(c.LectureAudit)
			t.Logf("REVIEW_POLICY %s %s", tc.name, raw)
		}
	}
}

// Read-only contrast: same facts, three purposes, same/change/no example;
// independent purpose/copying/boundary evaluation, not just lexical diversity.
func TestFactFieldsNormalizationAndTemplateInfluenceProvider(t *testing.T) {
	if os.Getenv("FACT_SPEECH_PREVIEW_PROBE") != "1" {
		t.Skip("explicit paid replay required")
	}
	store, err := appdb.Open(config.Load())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	s := &Server{store: store}
	if cfg, e := store.SpeechModels(context.Background()); e == nil {
		for _, p := range cfg.Profiles {
			if p.ID == cfg.ReviewDefaultID {
				t.Logf("CONFIGURED_REVIEW_MODEL alias=%s model=%s", p.Name, p.Model)
			}
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 220*time.Second)
	defer cancel()
	ctx = context.WithValue(ctx, factExamplesEphemeralKey{}, true)
	items, err := store.ListLiveAgentPlanFacts(ctx, 14, 4)
	if err != nil {
		t.Fatal(err)
	}
	var f model.LiveAgentPlanFact
	for _, item := range items {
		if item.Key == "发货时效" {
			f = item
			break
		}
	}
	if f.ID == 0 {
		t.Fatal("explicit shipping fixture missing")
	}
	input := model.LiveAgentPlanFactCandidate{Category: f.Category, Key: f.Key, Value: f.Value, ForbiddenWording: f.ForbiddenWording, SafeRewrite: f.SafeRewrite}
	normalized, _, err := s.normalizePlanFactFields(ctx, 14, 4, input)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(map[string]any{"original": factFieldsContent(input), "normalized": factFieldsContent(normalized)})
	t.Log("NORMALIZATION " + string(raw))
	for _, instruction := range []string{"自由组织口语", "保留‘一般’", "不报具体日期"} {
		if strings.Contains(normalized.SafeRewrite, instruction) {
			t.Fatal("writer instruction remains in positive example", normalized.SafeRewrite)
		}
	}
	f.Value, f.ForbiddenWording = normalized.Value, normalized.ForbiddenWording
	negative := model.LiveAgentPlanFactCandidate{Category: "other", Key: "新鲜制作", Value: "每天制作，没有隔夜货。把这条讲自然一点，不要照背。"}
	neg, _, err := s.normalizePlanFactFields(ctx, 14, 4, negative)
	if err != nil || !strings.Contains(neg.Value, "没有隔夜货") {
		t.Fatal("negative factual content lost", neg, err)
	}
	raw, _ = json.Marshal(factFieldsContent(neg))
	t.Log("NEGATIVE_FACT " + string(raw))
	conflict := input
	conflict.Value = "下午6点前订单一般当天发。下午6点前订单统一第二天上午发。"
	if _, _, err = s.normalizePlanFactFields(ctx, 14, 4, conflict); err == nil {
		t.Error("contradictory input silently saved")
	}
	purposes := []struct{ goal, concern string }{
		{"帮助下午6点后下单的观众理解对应的发货安排，只解释晚下单这种情况，适当补自然衔接，不必复述全部事实", "晚上下单是不是马上发"},
		{"帮助观众选择下单时间，对比6点前后发货安排，并给出一个有依据的日常选择建议", "希望早点安排发出，怎么选择下单时间"},
		{"消除观众把发货当成收到货的误解，说明发出与送达的区别，再简要说明已确认发货安排", "当天发是不是明天就能收到"},
	}
	alt := "咱先把发出和收到分清。一般当天发，下午6点以后下单的第二天上午发，收到的具体日期可不能按发出时间直接算。"
	cases := []struct {
		label, example string
		purpose        int
	}{{"A-晚下单", normalized.SafeRewrite, 0}, {"A-选择时间", normalized.SafeRewrite, 1}, {"A-发出与送达", normalized.SafeRewrite, 2}, {"B-晚下单", alt, 0}, {"B-选择时间", alt, 1}, {"无模板-选择时间", "", 1}}
	mother, _, err := loadReplaceablePromptAsset("ANCHOR_FACT_TEMPLATE_TEST_REVIEW_PATH", "configs/prompts/anchor-style/fact-template-influence-test-review.md", "模板影响对照母版")
	if err != nil {
		t.Fatal(err)
	}
	templateDependent := 0
	for _, tc := range cases {
		f.SafeRewrite = tc.example
		state, err := s.prepareFactSpeechPreview(ctx, 14, 4, 26, f, "模板影响对照")
		if err != nil {
			t.Fatal(err)
		}
		state.Queue[0].Angle, state.Queue[0].PainPoint = purposes[tc.purpose].goal, purposes[tc.purpose].concern
		output, err := s.generateFactSpeechPreview(ctx, &state)
		if err != nil {
			t.Fatal(err)
		}
		text, _ := output["text"].(string)
		confirmed, guidance := generationMaterialViews(state.HookFacts())
		payload, _ := json.Marshal(map[string]any{"facts": confirmed, "expression_guidance": guidance, "purpose": purposes[tc.purpose].goal, "angle": purposes[tc.purpose].concern, "example": tc.example, "text": text, "effective_p0": generationP0View(state.P0PolicyMD, state.Config.ComplianceScope)})
		review, _, err := s.completeAnchorStyleWorkbench(ctx, "", "fact_template_influence_review", agentgateway.ResponseJSON, []agentgateway.Message{{Role: "system", Content: mother}, {Role: "user", Content: string(payload)}}, 1000, false)
		if err != nil {
			t.Fatal(err)
		}
		var verdict struct {
			Purpose  *bool  `json:"purpose_followed"`
			Copied   *bool  `json:"template_copied"`
			Leak     *bool  `json:"instruction_leak"`
			Boundary *bool  `json:"fact_boundary_ok"`
			Note     string `json:"note"`
		}
		if decodeLectureContent(review.Text, &verdict) != nil || verdict.Purpose == nil || verdict.Copied == nil || verdict.Leak == nil || verdict.Boundary == nil {
			t.Fatal("contrast evaluation incomplete", review.Text)
		}
		raw, _ = json.Marshal(map[string]any{"case": tc.label, "purpose": purposes[tc.purpose].goal, "example": tc.example, "text": text, "verdict": verdict, "speech_review": output["review_state"], "issues": output["issues"]})
		t.Log("CONTRAST " + string(raw))
		if !*verdict.Purpose {
			templateDependent++
		}
		if *verdict.Leak || !*verdict.Boundary {
			t.Error("instruction/factual boundary failed", tc.label, verdict.Note)
		}
		if output["review_state"] != "approved" {
			t.Error("speech preview did not pass review", tc.label)
		}
	}
	t.Logf("TEMPLATE_DEPENDENT %d/%d", templateDependent, len(cases))
	if templateDependent > 0 {
		t.Errorf("template influence requires mitigation: %d cases", templateDependent)
	}
	for _, forbidden := range []bool{false, true} {
		f.ForbiddenWording, f.SafeRewrite = "", ""
		if forbidden {
			f.ForbiddenWording = "不准说物流时效。"
		}
		state, err := s.prepareFactSpeechPreview(ctx, 14, 4, 26, f, "物流负向要求对照")
		if err != nil {
			t.Fatal(err)
		}
		// Paired fixtures vary only the explicit logistics requirement. Other
		// adopted plan restrictions must not make the 'no requirement' case false.
		state.Queue[0].FactScope.Corrections = nil
		reviewState := factPreviewReviewState(state)
		c := lectureTextCandidate("今天发，明天到。")
		s.auditLectureCandidate(ctx, &reviewState, &c)
		if c.LectureAudit == nil || c.LectureAudit.Error != "" || c.LectureAudit.Passed == forbidden {
			t.Error("logistics explicit constraint/default pass mismatch", forbidden, c.LectureAudit)
		} else {
			t.Logf("LOGISTICS_CONSTRAINT forbidden=%t audit=%+v", forbidden, c.LectureAudit)
		}
		if !forbidden && c.LectureAudit != nil && len(c.LectureAudit.Suggestions) == 0 && strings.TrimSpace(c.LectureAudit.Reason) == "" {
			t.Error("nonblocking logistics advice missing")
		}
	}
}

// Real configured speech model/P0, read-only room replay plus cross-category
// fixtures. Fixtures are never written into a customer fact or lecture library.
func TestSecondaryLectureAcrossRoomsAndProductsProvider(t *testing.T) {
	if os.Getenv("FACT_SPEECH_PREVIEW_PROBE") != "1" {
		t.Skip("explicit paid replay required")
	}
	store, err := appdb.Open(config.Load())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	s := &Server{store: store}
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	facts, err := store.ListLiveAgentPlanFacts(ctx, 14, 4)
	if err != nil {
		t.Fatal(err)
	}
	var root model.LiveAgentPlanFact
	for _, f := range facts {
		if f.ID == 36 {
			root = f
		}
	}
	if root.ID == 0 {
		t.Fatal("explicit secondary fixture missing")
	}
	for _, room := range []struct {
		id   int64
		name string
	}{{26, "绸都杨鸭子"}, {15, "菜籽油"}} {
		state, err := s.prepareFactSpeechPreview(ctx, 14, 4, room.id, root, room.name)
		if err != nil {
			t.Fatal(err)
		}
		if state.Task().ProductMode != "lecture" || !strings.Contains(state.Task().LectureFacts, "资料对象：") {
			t.Fatal("secondary routed as ordinary mainline")
		}
		out, err := s.generateFactSpeechPreview(ctx, &state)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(out)
		t.Logf("ACTUAL_ROOM %d %s", room.id, raw)
		if out["preview_kind"] != "lecture" || out["review_state"] != "approved" {
			t.Error("secondary room review did not pass", room.id)
		}
	}
	boundaryState, err := s.prepareFactSpeechPreview(ctx, 14, 4, 26, root, "审核反例回放")
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"这是基础认知，不是搞什么功效宣传，资料没有确认，咱不瞎猜。"} {
		c := lectureTextCandidate(bad)
		s.auditLectureCandidate(ctx, &boundaryState, &c)
		if c.LectureAudit == nil || c.LectureAudit.Error != "" || c.LectureAudit.Passed {
			t.Error("known unsupported fact/control leakage approved", bad, c.LectureAudit)
		} else {
			t.Log("NEGATIVE_BOUNDARY correctly rejected", c.LectureAudit.Reason)
		}
	}
	uncertain := lectureTextCandidate("土麻鸭喝着嘉陵江水，吃着稻谷和自然草虫。")
	sourcingFixture := boundaryState
	fixtureTask := *boundaryState.Lectures.Active
	fixtureScope := *fixtureTask.FactScope
	fixtureScope.Corrections = nil
	// This contrast concerns ordinary unconfirmed sourcing, not the current
	// customer's stored object/angle restrictions. Keep full P0, isolate material.
	fixtureTask.LectureFacts = "土麻鸭产自嘉陵江流域，稻田水塘中散养。"
	fixtureTask.LecturePurpose = "说明原料来源与生活环境"
	fixtureTask.Angle = "按原料来源介绍，未额外限制饲喂细节"
	fixtureTask.LecturePlanJSON = ""
	fixtureScope.Facts = []anchorcycle.HookFact{{Data: fixtureTask.LectureFacts}}
	fixtureTask.FactScope = &fixtureScope
	sourcingFixture.Lectures = &anchorcycle.LectureRuntime{Active: &fixtureTask, Library: boundaryState.Lectures.Library}
	s.auditLectureCandidate(ctx, &sourcingFixture, &uncertain)
	if uncertain.LectureAudit == nil || uncertain.LectureAudit.Error != "" || !uncertain.LectureAudit.Passed || len(uncertain.LectureAudit.Suggestions) == 0 {
		t.Error("ordinary sourcing question must retain nonblocking advice", uncertain.LectureAudit)
	} else {
		t.Log("SOURCING_ADVICE nonblocking", uncertain.LectureAudit.Suggestions)
	}
	scenarios := []struct {
		subject, relation, detail, purpose, angle, unsupported string
		room                                                   int64
	}{
		{"燕麦", "这款饼干使用燕麦原料", "燕麦是谷物原料，资料未确认具体营养含量", "帮助观众认识饼干使用的原料", "讲清原料名称与谷物类别，不讲成品功效", "这款饼干实打实用燕麦做的，不是那种乱七八糟的添加物堆出来的。", 26},
		{"杯身钢材", "这款杯子的杯身使用304不锈钢", "杯身钢材牌号为304，未提供保温时长资料", "说明杯身选用的材料", "解释已确认部位和材质，不推导保温时长", "杯身用的是304不锈钢，重500克，可以保温12小时。", 15},
		{"菜籽原料产地", "这款菜籽油的菜籽来自地区甲", "地区甲有丘陵农田，未确认实际种植方式", "讲清油品与原料产地的关系", "从产地背景讲起，不推断有机种植或健康功效", "菜籽来自地区甲，采用有机种植，油品特别健康。", 15},
		{"熟食成品", "这款熟食成品开袋即可直接吃", "商品为开袋即食熟食", "讲清成品的食用方式", "说明开袋直接吃，不增加其它烹饪方法", "这款熟食开袋就能吃，也可以拿来煮汤、炖菜。", 26},
	}
	for i, tc := range scenarios {
		state, err := s.prepareFactSpeechPreview(ctx, 14, 4, tc.room, root, "通用品类模拟")
		if err != nil {
			t.Fatal(err)
		}
		f := model.LiveAgentPlanFact{ID: int64(9000 + i), VersionNo: 1, Value: tc.relation, Key: "对象关联", SourceType: "story_subject"}
		l := lecturetask.Task{ID: int64(8000 + i), Revision: 1, Subject: tc.subject, Title: tc.subject + "讲解", RootFact: &lecturetask.Fact{ID: f.ID, Version: 1, Text: f.Value}, Facts: []lecturetask.Fact{{Storage: "lecture", ID: 1, Name: "确认资料及边界", Text: tc.detail}}, Purpose: tc.purpose, Angles: []lecturetask.Angle{{Title: "当前角度", Goal: tc.angle}}, Valid: true}
		task := factLecturePreviewTask(l, factPreviewTask(f, nil).FactScope)
		state.Facts = f.Value
		state.Queue = []anchorcycle.Task{task}
		state.Lectures = &anchorcycle.LectureRuntime{Active: &task, Library: lecturetask.Library{Tasks: []lecturetask.Task{l}}}
		bad := lectureTextCandidate(tc.unsupported)
		s.auditLectureCandidate(ctx, &state, &bad)
		if bad.LectureAudit == nil || bad.LectureAudit.Error != "" || bad.LectureAudit.Passed {
			t.Error("unsupported composition/parameter/comparison approved", tc.subject, bad.LectureAudit)
		} else {
			t.Log("CATEGORY_NEGATIVE correctly rejected", tc.subject, bad.LectureAudit.Reason)
		}
		if tc.subject == "杯身钢材" {
			natural := lectureTextCandidate("杯身用的是304不锈钢，拿在手里沉甸甸的，质感很扎实。")
			s.auditLectureCandidate(ctx, &state, &natural)
			if natural.LectureAudit == nil || natural.LectureAudit.Error != "" || !natural.LectureAudit.Passed {
				t.Error("user-authorized natural sensory expansion rejected", natural.LectureAudit)
			} else {
				t.Log("NATURAL_SENSORY accepted", natural.LectureAudit.Reason)
			}
		}
		if tc.subject == "熟食成品" {
			permitted := state
			permitted.Queue = append([]anchorcycle.Task(nil), state.Queue...)
			permitted.Queue[0].LectureFacts += "\n商品明确支持：也可以用空气炸锅加热。"
			permitted.Lectures = &anchorcycle.LectureRuntime{Active: &permitted.Queue[0], Library: state.Lectures.Library}
			allowed := lectureTextCandidate("这款熟食开袋直接吃，也可以用空气炸锅加热。")
			s.auditLectureCandidate(ctx, &permitted, &allowed)
			if allowed.LectureAudit == nil || allowed.LectureAudit.Error != "" || !allowed.LectureAudit.Passed {
				t.Error("explicitly supported product use rejected", allowed.LectureAudit)
			} else {
				t.Log("SUPPORTED_PRODUCT_USE accepted", allowed.LectureAudit.Reason)
			}
		}
		out, err := s.generateFactSpeechPreview(ctx, &state)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(out)
		t.Logf("CATEGORY_FIXTURE %s %s", tc.subject, raw)
		if out["review_state"] != "approved" {
			t.Error("generic lecture review did not pass", tc.subject)
		}
	}
}
