package httpapi

import (
	"encoding/json"
	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/anchorcycle"
	"strings"
	"testing"
)

func TestHookPacketUsesSelectedCardAndWorkMemoryWithoutOldSpeech(t *testing.T) {
	s := cycleTestState(t)
	s.BaselineJSON = textTestBaseline()
	s.TextModel = &agentgateway.SpeechModel{ID: "flash", Model: "qwen3.7-flash"}
	s.AnglePlanMotherSHA = "m1"
	s.Queue[0].Angle = "参考台词整只一桌人吃不到90块"
	s.Queue[0].Key = s.Queue[0].Angle
	s.Facts = "当前价格89.8元，日常92元"
	h := anchorcycle.AngleHook{ID: "h1", Title: "价差", Concern: "优惠差多少", Entry: "从价格关系进入", Action: "解释差额", Facts: []string{"F001"}, Products: []string{"current"}, Benefits: []string{}}
	p := &anchorcycle.AnglePlan{Goal: "理解价差", Cards: []anchorcycle.AngleHook{h}, Calculations: []anchorcycle.HookCalculation{{Label: "价差", Operator: "subtract", Operands: []string{"92", "89.8"}, Facts: []string{"F001"}, Unit: "元"}}}
	s.AnglePlans = map[string]*anchorcycle.AnglePlan{s.AnglePlanKey(): p}
	s.Accepted = []anchorcycle.Segment{{Task: s.Task(), Selected: anchorcycle.Candidate{AngleHook: &h, Text: "上轮错误说省3块多，一桌人吃"}}}
	m, e := renderCycleTextMessages(&s)
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(m[1].Content, "一桌人吃") || strings.Contains(m[1].Content, "省3块多") || !strings.Contains(m[1].Content, `"result":"2.2"`) {
		t.Fatal(m[1].Content)
	}
	var packet map[string]any
	json.Unmarshal([]byte(m[1].Content), &packet)
	if packet["角度钩子工作卡"] == nil || packet["近期已采用任务记录（不代表已播放）"] == nil {
		t.Fatal(packet)
	}
	if s.Task().Angle != "参考台词整只一桌人吃不到90块" {
		t.Fatal("saved angle mutated")
	}
}
func TestHookFallbackStillRendersAndDoesNotReciteOldAngle(t *testing.T) {
	s := cycleTestState(t)
	s.BaselineJSON = textTestBaseline()
	s.AnglePlanMotherSHA = "m1"
	s.Queue[0].Angle = "旧范文原句"
	s.Facts = "已确认事实"
	s.AnglePlans = map[string]*anchorcycle.AnglePlan{s.AnglePlanKey(): {PlanError: "timeout"}}
	m, e := renderCycleTextMessages(&s)
	if e != nil || strings.Contains(m[1].Content, "旧范文原句") {
		t.Fatal(m, e)
	}
}

func TestUnavailablePlannerDoesNotSendOldExampleOrOldSpeech(t *testing.T) {
	s := cycleTestState(t)
	s.BaselineJSON = textTestBaseline()
	s.TextModel = &agentgateway.SpeechModel{ID: "flash", Model: "qwen3.7-flash"}
	s.Queue[0].Angle = "旧范文例句"
	s.Queue[0].Key = "旧范文例句"
	s.Accepted = []anchorcycle.Segment{{Selected: anchorcycle.Candidate{Text: "旧口播全文"}}}
	m, e := renderCycleTextMessages(&s)
	if e != nil || strings.Contains(m[1].Content, "旧范文例句") || strings.Contains(m[1].Content, "旧口播全文") {
		t.Fatal(m, e)
	}
}
func TestHookCacheIdentityRetainsArchivedSelectedCardAfterCursorAdvances(t *testing.T) {
	s := cycleTestState(t)
	s.TextModel = &agentgateway.SpeechModel{ID: "flash", Model: "qwen3.7-flash"}
	s.AnglePlanMotherSHA = "m1"
	p := &anchorcycle.AnglePlan{Goal: "intent", Cards: []anchorcycle.AngleHook{{ID: "one"}, {ID: "two"}}}
	s.AnglePlans = map[string]*anchorcycle.AnglePlan{s.AnglePlanKey(): p}
	seg := anchorcycle.Segment{Phase: "body", Task: s.Task(), Selected: anchorcycle.Candidate{AnglePlan: p, AngleHook: s.CurrentAngleHook()}}
	one := cycleVoiceCacheGroup(&s, seg)
	s.Used[s.Task().Key] = anchorcycle.Use{Count: 1}
	if cycleVoiceCacheGroup(&s, seg) != one {
		t.Fatal("archived cache follows live cursor")
	}
	if cycleVoiceCacheGroup(&s, anchorcycle.Segment{Phase: "body", Task: s.Task()}) == one {
		t.Fatal("new hook reused old speech cache")
	}
}
