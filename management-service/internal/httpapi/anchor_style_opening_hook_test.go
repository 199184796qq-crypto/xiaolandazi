package httpapi

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"livecompanion/management/internal/anchorcycle"
)

func TestOpeningHookFormalSlotBindingsAndNoStyleFactLeak(t *testing.T) {
	md, _, err := loadOpeningHookMother()
	if err != nil {
		t.Fatal(err)
	}
	library, err := anchorcycle.ParseOpeningHooks(md)
	if err != nil {
		t.Fatal(err)
	}
	s := cycleTestState(t)
	s.StyleMD = "样本福利：免费吃999斤；我们家100年。"
	s.Queue[0].TargetAudience, s.Queue[0].PainPoint = "家庭厨房", "担心味道不合适"
	s.Facts = "- [有效福利] 链接=1号链接；活动=新人开盖试吃5斤，不满意退剩余全额退款；活动价=未设置\n- [identity_location/品牌背书] 河清镇39年油坊；安全改写=历史介绍；禁说=独家"
	values := openingHookSlotValues(&s, library)
	if values["福利"] != "1号链接：新人开盖试吃5斤，不满意退剩余全额退款" || values["背书"] != "河清镇39年油坊" || values["目标人群"] != "家庭厨房" || values["痛点"] != "担心味道不合适" {
		t.Fatal(values)
	}
	library.Bindings["历史长度"] = anchorcycle.OpeningHookBinding{Source: "事实条目", Category: "identity_location", Pattern: `([0-9]+年)`}
	if openingHookSlotValues(&s, library)["历史长度"] != "39年" {
		t.Fatal("configurable regex binding failed")
	}
}

func TestOpeningHookMotherHotReloadAndConfirmedPlanSnapshot(t *testing.T) {
	md, _, err := loadOpeningHookMother()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "opening-hook-library.md")
	t.Setenv("ANCHOR_STYLE_OPENING_HOOK_LIBRARY_PATH", path)
	if err := os.WriteFile(path, []byte(md), 0600); err != nil {
		t.Fatal(err)
	}
	s := cycleTestState(t)
	first, err := renderAnchorOpeningHook(&s)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := s.PendingOpeningHook.Source
	changed := strings.ReplaceAll(md, "先别光盯着价格", "新版先别光盯着价格")
	if err := os.WriteFile(path, []byte(changed), 0600); err != nil {
		t.Fatal(err)
	}
	retry, err := renderAnchorOpeningHook(&s)
	if err != nil || first != retry || snapshot != s.PendingOpeningHook.Source {
		t.Fatal("retry changed chosen snapshot", err)
	}
	newRun := cycleTestState(t)
	if _, err := renderAnchorOpeningHook(&newRun); err != nil || newRun.PendingOpeningHook.Source == snapshot {
		t.Fatal("new opening ignored changed mother", err)
	}
	pinned := cycleTestState(t)
	pinned.OpeningHookLibraryMD, pinned.OpeningHookLibrarySource = md, "plan-version-7"
	if _, err := renderAnchorOpeningHook(&pinned); err != nil || pinned.PendingOpeningHook.Source != "plan-version-7" {
		t.Fatal("mother overwrote plan snapshot", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err := renderAnchorOpeningHook(&pinned); err != nil {
		t.Fatal("confirmed snapshot depends on mother file", err)
	}
	if _, err := renderAnchorOpeningHook(&newRun); err == nil {
		t.Fatal("missing project file silently fell back")
	}
}

func TestOpeningHookFirstAndReentryPromptIncludeChoiceOnlyOnOpening(t *testing.T) {
	for _, count := range []int{0, 1} {
		s := cycleTestState(t)
		s.OpeningCount = count
		prompt, err := renderAnchorCyclePrompt(&s)
		if err != nil || s.PendingOpeningHook == nil || !strings.Contains(prompt, s.PendingOpeningHook.ID) || !strings.Contains(prompt, "钩子第一句") || strings.Contains(prompt, "{{opening_hook}}") {
			t.Fatal(prompt, err)
		}
		for _, phase := range []string{"body", "closing"} {
			s.Phase = phase
			prompt, err := renderAnchorCyclePrompt(&s)
			if err != nil || strings.Contains(prompt, s.PendingOpeningHook.ID) {
				t.Fatal("hook injected into other stage", phase, err)
			}
		}
	}
}
