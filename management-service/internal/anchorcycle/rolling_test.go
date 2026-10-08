package anchorcycle

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"livecompanion/management/internal/anchorstylescore"
)

func rollingFixture(t *testing.T, cycles int) State {
	t.Helper()
	s, err := New([]Task{{SellingPoint: "老坊", Angle: "产地"}, {SellingPoint: "新油", Angle: "年份"}, {SellingPoint: "试吃", Angle: "规则"}}, Config{Threshold: 40, MaxRetries: 3, MaxChars: 300, PointGap: 2, CombinationGap: 2, Rolling: true, BodySegments: 2, MaxCycles: cycles})
	if err != nil {
		t.Fatal(err)
	}
	s.StyleMD = "高亢。称呼：‘老乡’‘哥哥姐姐们’。自称‘我们家’。"
	s.SessionID = "sesn_same-live"
	return s
}

func TestRollingOpeningCountAndTaskAdvanceOnlyAfterAdoption(t *testing.T) {
	s := rollingFixture(t, 2)
	first := s.Task()
	if add(t, &s, "拒绝稿", 39) || s.OpeningCount != 0 || s.OpeningVersion() != "完整版" || s.Task() != first {
		t.Fatal("retry advanced opening")
	}
	add(t, &s, "首场身份和卖点钩子", 74)
	if s.OpeningCount != 1 || s.Accepted[0].OpeningVersion != "完整版" || s.BodyCount != 0 {
		t.Fatal(s)
	}
	add(t, &s, "第一段中间", 62)
	add(t, &s, "第二段中间", 65)
	if s.Phase != "closing" || s.OpeningCount != 1 {
		t.Fatal("entering closing changed count")
	}
	window := s.StyleWindow
	add(t, &s, "收口留话头", 70)
	if s.Phase != "opening" || s.CycleNumber != 2 || s.OpeningCount != 1 || s.OpeningVersion() != "接新版" || s.SessionID != "sesn_same-live" || !strings.Contains(s.StyleWindow, window) || len(s.Used) != 3 {
		t.Fatal("closing reset live memory", s)
	}
	add(t, &s, "接新简短身份和钩子", 70)
	if s.OpeningCount != 2 || s.Accepted[4].OpeningVersion != "接新版" || s.Accepted[4].CycleNumber != 2 {
		t.Fatal("bad reentry")
	}
	add(t, &s, "再讲第一段", 70)
	add(t, &s, "再讲第二段", 70)
	add(t, &s, "第二轮收口", 70)
	if s.Phase != "ended" || s.CycleNumber != 2 || s.OpeningCount != 2 || len(s.Accepted) != 8 {
		t.Fatal(s)
	}
}

func TestRollingPoolReusePreservesCountsAndSpacingAcrossManyCycles(t *testing.T) {
	s := rollingFixture(t, 0)
	for i := 0; i < 80; i++ {
		task, phase := s.Task(), s.Phase
		if phase != "closing" {
			for _, prior := range s.Accepted[max(0, len(s.Accepted)-s.Config.PointGap):] {
				if prior.Phase != "closing" && prior.Task.SellingPoint == task.SellingPoint {
					t.Fatal("adjacent point repeated")
				}
			}
			if u := s.Used[task.Key]; u.Count > 0 && len(s.Accepted)-u.LastIndex < s.Config.CombinationGap {
				t.Fatal("combination cooldown violated")
			}
		}
		add(t, &s, fmt.Sprintf("第%d次采纳的不同讲法%s", i, strings.Repeat("啊", 30)), 70)
		if s.Status == "spacing_blocked" || s.Phase == "ended" {
			t.Fatal("continuous run stopped prematurely", s.Error)
		}
	}
	if s.OpeningCount != 20 || s.CycleNumber != 21 || len(s.Used) != 3 || s.Cursor != 60 || s.MacroReport.Stats.Chars != 1000 {
		t.Fatal("cross-cycle counters/window reset", s)
	}
	for _, u := range s.Used {
		if u.Count != 20 || u.LastSummary == "" {
			t.Fatal("used pool was cleared", u)
		}
	}
}

func TestRollingBlockedPoolKeepsAdoptionAndDoesNotInventNextTask(t *testing.T) {
	s, err := New([]Task{{SellingPoint: "唯一卖点", Angle: "唯一角度"}}, Config{Threshold: 40, MaxRetries: 3, MaxChars: 300, Rolling: true, BodySegments: 2, PointGap: 2, CombinationGap: 2})
	if err != nil {
		t.Fatal(err)
	}
	add(t, &s, "唯一卖点采纳稿", 70)
	if s.Status != "spacing_blocked" || s.Uncertain || len(s.Accepted) != 1 || s.OpeningCount != 1 || s.Cursor != 1 || s.FeedbackPending {
		t.Fatal("spacing failure discarded adoption or cost another call", s)
	}
	if _, err := s.AddCandidate(candidate("不应请求的重复稿", 90), anchorstylescore.Baseline{}); err == nil {
		t.Fatal("blocked run adopted again")
	}
}

func TestGenerationPreviewReusesSafeTaskWhenPoolCannotFillDuration(t *testing.T) {
	s, err := New([]Task{{SellingPoint: "唯一卖点", Angle: "唯一角度"}}, Config{Threshold: 40, MaxRetries: 3, MaxChars: 300, Rolling: true, BodySegments: 2, PointGap: 2, CombinationGap: 8})
	if err != nil {
		t.Fatal(err)
	}
	s.Surface = "generation_preview"
	add(t, &s, "第一种安全讲法", 70)
	if s.Status == "spacing_blocked" || s.Phase == "ended" || s.TaskIndex != 0 {
		t.Fatal("duration-driven preview stopped on a small safe pool", s.Status, s.Error)
	}
	if len(s.Warnings) == 0 || !strings.Contains(s.Warnings[len(s.Warnings)-1], "补足2分钟") {
		t.Fatal("preview fallback was not recorded")
	}
}

func TestRollingRoundTripRetainsNextTaskAndReentryVersion(t *testing.T) {
	s := rollingFixture(t, 2)
	for i := 0; i < 4; i++ {
		add(t, &s, "采纳后保留的原稿", 70)
	}
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	var restored State
	if err = json.Unmarshal(raw, &restored); err != nil {
		t.Fatal(err)
	}
	if restored.CycleNumber != 2 || restored.OpeningCount != 1 || restored.OpeningVersion() != "接新版" || restored.Task() != s.Task() || restored.StyleWindow != s.StyleWindow {
		t.Fatal("same-process pause/reload lost opening state")
	}
}
