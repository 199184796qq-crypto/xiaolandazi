package httpapi

import (
	"context"
	"encoding/json"
	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/anchorstylescore"
	"livecompanion/management/internal/config"
	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/ttsgateway"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Explicit paid operational demo. Produces local audio and exchange logs only;
// never starts a room, writes plan assets, adopts facts or publishes a version.
func TestLectureFlowProviderDemo(t *testing.T) {
	if os.Getenv("LECTURE_FLOW_DEMO") != "1" {
		t.Skip("explicit demo required")
	}
	// A deliberate three-part preview uses the selected purpose directly. It
	// does not run an unrelated legacy hook derivation or advance live cursors.
	t.Setenv("ANCHOR_STYLE_ANGLE_HOOK_PLAN_ENABLED", "0")
	tenant, _ := strconv.ParseInt(os.Getenv("LECTURE_DEMO_TENANT"), 10, 64)
	plan, _ := strconv.ParseInt(os.Getenv("LECTURE_DEMO_PLAN"), 10, 64)
	room, _ := strconv.ParseInt(os.Getenv("LECTURE_DEMO_ROOM"), 10, 64)
	out := os.Getenv("LECTURE_DEMO_OUTPUT")
	if tenant <= 0 || plan <= 0 || room <= 0 || out == "" {
		t.Fatal("explicit scope/output required")
	}
	store, err := appdb.Open(config.Load())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	server := &Server{store: store}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	versions, err := store.ListLiveAgentPlanVersions(ctx, tenant, plan, room)
	if err != nil || len(versions) == 0 {
		t.Fatal("no saved plan", err)
	}
	version := versions[0]
	scheme, err := realtimeScheme(version.GenerationContext)
	if err != nil {
		t.Fatal(err)
	}
	ref := asSupportMap(version.GenerationContext["derived_safety"])
	hash, _ := ref["source_sha256"].(string)
	safety, err := store.GetLiveDerivedSafety(ctx, tenant, plan, hash)
	if err != nil {
		t.Fatal(err)
	}
	set, err := server.savedContentPoolSet(ctx, tenant, plan, version.GenerationContext)
	if err != nil || set == nil || scheme.PoolPolicy == nil {
		t.Fatal("saved pool required", err)
	}
	tasks, focus, err := contentPoolRuntime(*set, *scheme.PoolPolicy)
	if err != nil {
		t.Fatal(err)
	}
	cfg := scheme.Config
	cfg.MaxChars = 220
	cfg.MaxCycles = 0
	cfg.MaxRetries = 1
	state, err := anchorcycle.New(tasks, cfg)
	if err != nil {
		t.Fatal(err)
	}
	state.TenantID, state.PlanID, state.RoomID = tenant, plan, room
	state.Surface = "generation_preview"
	state.StyleMD, state.BaselineJSON, state.Facts = scheme.StyleMD, scheme.BaselineJSON, safety.SafeFacts
	state.ProductFocus = focus
	focus.ResetRun()
	state.SelectProductTask()
	state.Phase = "body"
	state.CycleNumber = 5
	// The sample begins mid-show. Use the actual saved specification/price task.
	for i, task := range state.Queue {
		if strings.Contains(task.SellingPoint, "规格") && (task.FocusLink == "" || task.FocusLink == focus.MainLink()) {
			state.TaskIndex = i
			break
		}
	}
	if err = server.installCycleTextModel(ctx, &state); err != nil {
		t.Fatal(err)
	}
	demoMother, _, err := loadReplaceablePromptAsset("ANCHOR_LECTURE_FLOW_DEMO_PATH", "configs/prompts/anchor-style/lecture-flow-demo.md", "连续直播试听范围母版")
	if err != nil || state.TextModel == nil {
		t.Fatal("demo text model/mother unavailable", err)
	}
	state.TextModel.SystemPrompt += "\n\n" + demoMother
	if err = installP0Mother(&state); err != nil {
		t.Fatal(err)
	}
	b, err := anchorstylescore.Parse(state.BaselineJSON)
	if err != nil {
		t.Fatal(err)
	}
	texts := []string{}
	phases := []string{"正常讲品", "转入原料故事", "回到讲品"}
	for part := 0; part < 3; part++ {
		state.Config.MaxChars = 110
		state.SegmentDirective = "本次只完成规格选择片段，约80—110字，沿用当前商品事实。"
		state.Queue[state.TaskIndex].Angle = "解释当前整鸭商品的900克分量与相关价格，不补其它规格或物流"
		if part == 1 {
			state.Config.MaxChars = 200
			lecture := anchorcycle.Task{SellingPoint: "南充土麻鸭与本地老卤", Angle: "原料选择：从南充水土与本地鸭、本地卤解释地道口味；只讲这个角度，避免营养功效、绝对化口感和未经确认的养殖推论", Key: "demo-lecture", ProductMode: "lecture", LectureTopic: 1, LecturePurpose: "让观众理解杨鸭子为何选择本地原料，建立对原料与地方风味的认识", LectureFacts: "用户提供的原文材料：\n这杨鸭子用的土麻鸭，就产自咱们四川南充——嘉陵江穿城而过，丘陵水田、气候温润，鸭子就在这些稻田水塘里散养，天然好水土养出来的。南充本来就是吃鸭的老地方，杨鸭子更是咱们本地的老字号招牌。\n一是肉紧，散养运动多，咬下去有嚼头。\n本地水土、本地鸭子、本地老卤，这才做得出地道的杨鸭子。", ProductMaxChars: 220}
			state.Lectures = &anchorcycle.LectureRuntime{Active: &lecture}
			state.Phase = "lecture"
			state.SegmentDirective = "本次完成原料故事片段，承接刚才选鸭子的介绍，约160—200字。"
		}
		if part == 2 {
			state.Lectures.Active = nil
			state.Phase = "body"
			state.SegmentDirective = "本次只完成回到口味选择片段，约80—110字，首句自然接住刚才的地方风味。"
			state.TaskIndex = 0
			for i, task := range state.Queue {
				if strings.Contains(task.SellingPoint, "口味") || strings.Contains(task.SellingPoint, "包装") {
					state.TaskIndex = i
					break
				}
			}
			state.Queue[state.TaskIndex].Angle = "帮助观众按口味偏好在麻辣和五香之间选择，不讲保存、物流或价格"
			// Scope the return to the flavor facts shown in the user's product
			// material, instead of inviting another all-product introduction.
			state.ProductFocus.Facts = []anchorcycle.ScopedFact{{Text: "麻辣和五香两种口味均可选，喜欢辣味可以选麻辣，不喜欢辣味可以选五香。", Shared: true}}
		}
		baseFeedback := state.SegmentDirective
		var candidate anchorcycle.Candidate
		passed := false
		for attempt := 0; attempt < 5; attempt++ {
			response, _, e := server.completeCycleText(ctx, &state)
			if e != nil {
				t.Fatal(e)
			}
			candidate = anchorcycle.Candidate{ID: anchorcycle.ID(), Text: strings.TrimSpace(response.Text), RawText: response.Text, Score: anchorstylescore.Score(response.Text, b, state.Task().SellingPoint)}
			candidate.OutputText = cycleVoiceText(candidate)
			cleanCycleOpeningInstruction(&state, &candidate, "")
			cleanCycleConsecutiveSpeech(&state, &candidate)
			if err = state.CheckP0Candidate(&candidate); err != nil {
				t.Fatal(err)
			}
			server.auditLectureCandidate(ctx, &state, &candidate)
			if part != 1 {
				scopeState := state
				scopeTask := state.Task()
				scopeTask.ProductMode = "lecture"
				scopeTask.LectureFacts = state.ProductFacts(state.Task())
				scopeTask.LecturePurpose = state.SegmentDirective
				scopeState.Lectures = &anchorcycle.LectureRuntime{Active: &scopeTask}
				server.auditLectureCandidate(ctx, &scopeState, &candidate)
			}
			if candidate.P0Report != nil && candidate.P0Report.Passed && !textControlLeak(cycleVoiceText(candidate)) && candidate.LectureAudit != nil && candidate.LectureAudit.Passed {
				passed = true
				break
			}
			state.SegmentDirective = baseFeedback + "上一段未通过文字审核，请依照当前事实和表达边界重新生成。"
			t.Logf("part=%d retry=%d text=%s p0=%+v audit=%+v", part, attempt, cycleVoiceText(candidate), candidate.P0Report, candidate.LectureAudit)
			if candidate.LectureAudit != nil {
				state.SegmentDirective += "问题原句：" + candidate.LectureAudit.Quote + "；原因：" + candidate.LectureAudit.Reason
			}
		}
		if !passed {
			t.Fatal("demo paragraph failed review", part)
		}
		task := state.Task()
		texts = append(texts, cycleVoiceText(candidate))
		state.Accepted = append(state.Accepted, anchorcycle.Segment{Phase: state.Phase, Task: task, Selected: candidate})
		state.SegmentDirective = ""
		t.Log(phases[part] + "：" + texts[len(texts)-1])
	}
	voiceID, modelName, err := server.resolveFormalVoice(ctx, tenant, fullShowVariantVoiceInput{Source: version.VoiceIdentity.Source, VoiceID: version.VoiceIdentity.VoiceID, ProfileID: version.VoiceIdentity.ProfileID, BindingID: version.VoiceIdentity.BindingID, Model: version.VoiceIdentity.Model})
	if err != nil {
		t.Fatal(err)
	}
	gateway := ttsgateway.NewFromEnv()
	ttsCfg := ttsgateway.QwenConfigFromEnv()
	if ttsCfg.APIKey == "" {
		profile, e := store.ResolveSpeechModel(ctx, cfg.CredentialID)
		if e != nil || profile == nil || profile.APIKey == "" {
			t.Fatal("TTS credential unavailable")
		}
		ttsCfg.APIKey = profile.APIKey
		gateway.Register(ttsgateway.NewQwenProvider(ttsCfg), "qwen", "dashscope", "qwen_audio_3_0")
		gateway.Register(ttsgateway.NewCosyVoiceProvider(ttsCfg), "aliyun_cosyvoice", "aliyun_cosyvoice_clone")
	}
	voice := anchorcycle.VoiceConfig{VoiceID: voiceID, Model: modelName, Rate: version.VoiceIdentity.Rate}
	if voice.Rate == 0 {
		voice.Rate = 1
	}
	scoped := withExchangeScope(ctx, appdb.ModelExchangeScope{TenantID: tenant, PlanID: plan, RoomID: room, RunID: state.RunID})
	raw, timeline, duration, err := synthesizeMainlinePreview(scoped, journaledMainlineSynthesizer{s: server, gateway: gateway, profile: cfg.CredentialID}, voice, strings.Join(texts, "\n\n"), "lecture-flow-demo")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(out, 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(out, "lecture-flow-demo.wav"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	result := map[string]any{"voice_name": version.VoiceIdentity.Name, "voice_model": modelName, "plan_version": version.VersionNo, "duration_ms": duration, "phases": phases, "texts": texts, "timeline": timeline}
	data, _ := json.MarshalIndent(result, "", "  ")
	if err = os.WriteFile(filepath.Join(out, "lecture-flow-demo.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	t.Log("demo produced", duration, "ms", version.VoiceIdentity.Name, modelName)
}
