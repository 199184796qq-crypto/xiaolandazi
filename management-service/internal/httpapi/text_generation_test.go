package httpapi

import (
	"encoding/json"
	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/anchorcycle"
	"os"
	"strings"
	"testing"
)

func textTestBaseline() string {
	identity := map[string]any{}
	for _, key := range []string{"语气词系统", "称呼体系", "自指体系", "句式节奏", "数字比价习惯", "方言口头禅", "重复回环习惯", "情绪特征"} {
		identity[key] = map[string]any{}
	}
	quant := map[string]any{}
	for _, key := range []string{"每百字_主力语气词", "每百字_辅助语气词1", "每百字_辅助语气词2", "每百字_主力称呼", "每百字_自指词", "每百字_问号", "每百字_感叹号"} {
		quant[key] = 0
	}
	raw, _ := json.Marshal(map[string]any{"主播识别信息": identity, "量化基准（供打分用）": quant, "禁用词（反推）": map[string]any{}})
	return string(raw)
}
func TestDirectMainlineOwnsBoundedContextAndNoFilledHookSkeleton(t *testing.T) {
	state := cycleTestState(t)
	state.Config.TextProfileID = "plus"
	state.BaselineJSON = textTestBaseline()
	state.StyleMD = "旧范文：油69.9元假一赔十"
	state.Facts = "本场鸭子84.5元"
	state.PendingOpeningHook = &anchorcycle.OpeningHook{Kind: "痛点钩", Template: "{目标人群}关心的{角度}", FilledTemplate: "注重购物保障消费者关心", SoftValues: map[string]string{"顾虑": "售后"}}
	state.Accepted = []anchorcycle.Segment{{Selected: anchorcycle.Candidate{Text: strings.Repeat("旧表达", 3000)}}}
	messages, err := renderCycleTextMessages(&state)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 2 {
		t.Fatal(messages)
	}
	// 可说事实已挪到 user（保持 system 稳定前缀以命中上下文缓存），
	// 校验 facts 出现在 user 而非 system。
	if strings.Contains(messages[0].Content, state.Facts) {
		t.Fatal("可说事实不应留在 system，否则前缀缓存每段失效")
	}
	if !strings.Contains(messages[1].Content, state.Facts) {
		t.Fatal("可说事实应出现在 user 中")
	}
	if strings.Contains(messages[0].Content, "假一赔十") || strings.Contains(messages[1].Content, "{目标人群}") || strings.Contains(messages[1].Content, "FilledTemplate") {
		t.Fatal("sample claim or filled skeleton leaked")
	}
	var dynamic map[string]any
	if json.Unmarshal([]byte(messages[1].Content), &dynamic) != nil {
		t.Fatal(messages[1].Content)
	}
	if len([]rune(dynamic["近期已采用内容仅供避重"].(string))) > 2400 {
		t.Fatal("unbounded context")
	}
	state.BootstrapSent = true
	again, err := renderCycleTextMessages(&state)
	if err != nil || messages[0].Content != again[0].Content {
		t.Fatal("facts/style disappeared after first call")
	}
	for _, text := range []string{"84.5元，不到90块啊。", "48.8元，不到50。", "一桌朋友分着吃。"} {
		if textControlLeak(text) {
			t.Fatal("semantic/price policing", text)
		}
	}
	if !textControlLeak("本次任务：现在生成正文") {
		t.Fatal("internal fields accepted")
	}
}

func TestAnglePlanPacketCarriesIntentInsteadOfOriginalReadyMadeSpeech(t *testing.T) {
	s := cycleTestState(t)
	s.AnglePlanMotherSHA = "test-mother"
	s.BaselineJSON = textTestBaseline()
	s.Queue[0].Angle = "价格原句：一桌人吃不到90块，这是原角度例句"
	p := &anchorcycle.AnglePlan{AudienceConcern: "整只半只怎么选", Goal: "理解份量选择", Action: "结合饭量和其他菜解释", Hooks: []string{"从尝口味或分享进入"}}
	p.SourceSHA = s.AnglePlanKey()
	s.AnglePlans = map[string]*anchorcycle.AnglePlan{p.SourceSHA: p}
	packet := map[string]any{"内容方向与采用条件": "旧角度候选库"}
	applyCycleAnglePlanPacket(&s, packet)
	raw, _ := json.Marshal(packet)
	if strings.Contains(string(raw), "一桌人吃不到90") || strings.Contains(string(raw), "旧角度候选库") || !strings.Contains(string(raw), "从尝口味或分享进入") {
		t.Fatal(string(raw))
	}
	if s.Task().Angle != "价格原句：一桌人吃不到90块，这是原角度例句" {
		t.Fatal("saved user angle rewritten")
	}
}

// Export exact compiled production packets for a read-only real-provider probe.
func TestExportTextModelProbePackets(t *testing.T) {
	input, output := os.Getenv("TEXT_PROBE_STATE_INPUT"), os.Getenv("TEXT_PROBE_PACKET_OUTPUT")
	if input == "" || output == "" {
		t.Skip("explicit text-only fixture export required")
	}
	raw, err := os.ReadFile(input)
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name        string            `json:"name"`
		State       anchorcycle.State `json:"state"`
		Interaction string            `json:"interaction"`
	}
	if err = json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	packets := []map[string]any{}
	for _, c := range cases {
		var messages []agentgateway.Message
		stage := "mainline_text"
		format := agentgateway.ResponseText
		if c.Interaction != "" {
			stage = "speech_generation"
			format = agentgateway.ResponseJSON
			mother, _, err := loadReplaceablePromptAsset("", "configs/prompts/anchor-style/interaction-answer-assessment.md", "互动正文")
			if err != nil {
				t.Fatal(err)
			}
			messages = []agentgateway.Message{{Role: "system", Content: mother}, {Role: "user", Content: c.Interaction}}
		} else {
			var err error
			messages, err = renderCycleTextMessages(&c.State)
			if err != nil {
				t.Fatal(err)
			}
		}
		req, err := agentgateway.PrepareTextRequest(agentgateway.SpeechModel{Model: "qwen3.7-plus"}, agentgateway.Request{Stage: stage, Messages: messages, ResponseFormat: format, MaxTokens: 1800})
		if err != nil {
			t.Fatal(err)
		}
		packets = append(packets, map[string]any{"name": c.Name, "request": req})
	}
	raw, _ = json.MarshalIndent(packets, "", "  ")
	if err = os.WriteFile(output, raw, 0600); err != nil {
		t.Fatal(err)
	}
}
