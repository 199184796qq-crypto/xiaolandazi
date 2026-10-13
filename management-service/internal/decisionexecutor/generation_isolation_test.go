package decisionexecutor

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"livecompanion/management/internal/model"
)

type isolatedGenerationStore struct {
	*fakeStore
	legacyReads int
	memories    []model.AgentMemoryItem
}

func (s *isolatedGenerationStore) LoadLivePolicyLayers(context.Context, int64, int64) (string, *model.LivePolicyVersion, *model.LivePolicyVersion, *model.LivePolicyVersion, error) {
	s.legacyReads++
	panic("generation read legacy policy")
}
func (s *isolatedGenerationStore) ListActiveAgentMemories(context.Context, int64, int64) ([]model.AgentMemoryItem, error) {
	return s.memories, nil
}

func TestGenerationIsolatesRoomsPlansAndChangedFactsAcrossCategories(t *testing.T) {
	s := &isolatedGenerationStore{fakeStore: readyVoiceStore()}
	w := New(s, &fakeCore{}, &fakeAgent{}, &fakeTTS{})
	for i, facts := range []string{
		"即食卤炸鸭900克，京东物流", "菜籽油5升，顺丰快递", "纯棉短袖，冷水手洗",
		"3升电饭煲，仅支持煮饭", "保湿霜50克，仅供外用", "摄影课程6次，录播回看30天",
		"同款保湿霜80克，仅供外用",
	} {
		room, plan := int64(11+i), int64(30+i)
		if i == 6 {
			room, plan = 15, 34
		} // Same product/plan, changed fact version.
		s.plan = model.LiveAgentPlan{ID: plan, TenantID: 7, Name: "已保存方案", Status: "active"}
		s.facts = []model.LiveAgentPlanFact{{Key: "当前商品", Value: facts, Status: "active"}}
		memory := func(tenant, roomID, planID int64, text string) model.AgentMemoryItem {
			return model.AgentMemoryItem{TenantID: tenant, RoomID: roomID, Status: "active", MemoryType: model.AgentMemoryTypeSemantic,
				CurrentVersion: &model.AgentMemoryVersion{ContentText: text, Structured: map[string]any{"review_plan_id": planID}}}
		}
		s.memories = []model.AgentMemoryItem{
			memory(7, room, plan, "本方案已采用约束"), memory(8, room, plan, "其他租户哨兵"), memory(7, room+100, plan, "其他房间哨兵"), memory(7, room, plan+100, "其他方案哨兵"), memory(7, room, 0, "旧菜籽油与中通哨兵"),
		}
		item := decisionItem{BusinessCoreOnly: true, BusinessPlanning: true, SampleQuestions: []string{"介绍当前商品"}}
		prompt, err := w.answerPrompt(context.Background(), model.LiveRuntimeSession{TenantID: 7, RoomID: room}, item)
		if err != nil {
			t.Fatal(err)
		}
		for _, bad := range []string{"其他租户哨兵", "其他房间哨兵", "其他方案哨兵", "旧菜籽油与中通哨兵", "source_layer\":\"L3"} {
			if strings.Contains(prompt, bad) {
				t.Fatal("scope leaked", bad, prompt)
			}
		}
		if !strings.Contains(prompt, facts) || !strings.Contains(prompt, "本方案已采用约束") || !strings.Contains(prompt, "完整有效P0") {
			t.Fatal("selected content/P0 lost", prompt)
		}
		var packet map[string]any
		if err := json.Unmarshal([]byte(prompt), &packet); err != nil {
			t.Fatal(err)
		}
		if len(packet["current_rules"].([]any)) == 0 {
			t.Fatal("P0 absent")
		}
	}
	if s.legacyReads != 0 {
		t.Fatal("legacy policy was fetched")
	}
}

func TestLegacyPolicyChangesCannotAuthorizeCachedSpeech(t *testing.T) {
	w, s, core, agent, _, _ := newFAQTestWorker()
	executeFAQ(t, w, core, 11, "多少钱")
	s.layer = &model.LivePolicyVersion{ID: 19, Overrides: []model.LivePolicyOverride{{Text: "旧菜籽油在2号链接"}}}
	executeFAQ(t, w, core, 11, "多少钱")
	if agent.calls != 1 {
		t.Fatal("unused historical L3 changed live cache")
	}
}
