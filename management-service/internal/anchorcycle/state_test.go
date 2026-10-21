package anchorcycle

import (
	"encoding/json"
	"strings"
	"testing"

	"livecompanion/management/internal/anchorstylescore"
)

func fixture(t *testing.T, n int) State {
	t.Helper()
	tasks := []Task{}
	for i := 0; i < n; i++ {
		tasks = append(tasks, Task{SellingPoint: string(rune('甲' + i)), Angle: "角度"})
	}
	s, err := New(tasks, Config{Threshold: 80, MaxRetries: 3, MaxChars: 300})
	if err != nil {
		t.Fatal(err)
	}
	s.StyleMD = "称呼：‘老乡’‘哥哥姐姐们’。自称‘我们家’。"
	return s
}
func candidate(text string, score int) Candidate {
	return Candidate{ID: ID(), Text: text, Score: anchorstylescore.Result{OverallScore: score}}
}
func add(t *testing.T, s *State, text string, score int) bool {
	t.Helper()
	adopted, err := s.AddCandidate(candidate(text, score), anchorstylescore.Baseline{})
	if err != nil {
		t.Fatal(err)
	}
	return adopted
}

// 方案没绑主播风格时本场不打分：首个过 P0/终审的候选直接采纳，
// 不能因为空基准下分数恒低于门槛而把重试次数耗光。
func TestFreeStyleAdoptsWithoutScoreGate(t *testing.T) {
	s := fixture(t, 2)
	s.StyleMD = ""
	s.FreeStyle = true
	if !s.NoStyle() || s.Gradable() {
		t.Fatal("free style should disable grading")
	}
	if !add(t, &s, "哥哥姐姐们，这段话没有风格基准也要能采纳。", 20) {
		t.Fatal("free style draft rejected by score gate")
	}
	if len(s.Accepted) != 1 || s.Accepted[0].Reason != "no_style_adopted" {
		t.Fatalf("unexpected adoption reason: %+v", s.Accepted)
	}
	if len(s.Attempts) != 0 {
		t.Fatal("free style must not keep retrying")
	}
}

// 有风格时门槛照旧：低于阈值不采纳。
func TestStyleModeStillEnforcesThreshold(t *testing.T) {
	s := fixture(t, 1)
	if s.NoStyle() || !s.Gradable() {
		t.Fatal("style state must be graded")
	}
	if add(t, &s, "低于门槛的稿子", 50) {
		t.Fatal("below gate adopted")
	}
}
func TestOnlyAdoptedDraftUpdatesMemory(t *testing.T) {
	s := fixture(t, 2)
	if add(t, &s, "哥哥姐姐们，拒绝稿。", 50) {
		t.Fatal("premature adoption")
	}
	if s.Cursor != 0 || len(s.Accepted) != 0 || len(s.Used) != 0 || len(s.RecentSummaries) != 0 {
		t.Fatal("candidate polluted memory")
	}
	if !add(t, &s, "老乡，采纳稿。", 80) {
		t.Fatal("passed candidate rejected")
	}
	if s.Cursor != 1 || s.Phase != "body" || len(s.Accepted) != 1 || len(s.Accepted[0].Attempts) != 2 || s.Used[s.Queue[0].Key].Count != 1 {
		t.Fatalf("bad adoption: %+v", s)
	}
}

func TestFortyPointGateAcceptsRawScoreWithoutInflatingPunctuation(t *testing.T) {
	s := fixture(t, 2)
	s.Config.Threshold = 40
	if add(t, &s, "39分仍重写", 39) {
		t.Fatal("below gate passed")
	}
	text := "我们家这个油，哥哥姐姐们，都买好了不"
	output, edits := RepairPunctuation(text, "高亢", 300)
	c := candidate(text, 40)
	c.RawText, c.OutputText, c.PunctuationEdits, c.ScoreScope = "  "+text+"  ", output, edits, "model_raw_text"
	adopted, err := s.AddCandidate(c, anchorstylescore.Baseline{})
	if err != nil || !adopted || s.Cursor != 1 {
		t.Fatal("40-point raw score not adopted", err)
	}
	selected := s.Accepted[0].Selected
	if selected.Score.OverallScore != 40 || selected.Text != text || selected.RawText != c.RawText || !strings.HasSuffix(selected.OutputText, "？") || len(selected.PunctuationEdits) == 0 {
		t.Fatal("raw text/score/repair trace not preserved")
	}
}
func TestRetryLimitIncludesInitialAttemptAndSelectsBest(t *testing.T) {
	s := fixture(t, 1)
	for i, score := range []int{79, 40, 65, 70} {
		adopted := add(t, &s, []string{"最高分原稿", "第二稿", "第三稿", "第四稿"}[i], score)
		if adopted != (i == 3) {
			t.Fatalf("attempt %d adoption=%v", i+1, adopted)
		}
	}
	if s.Accepted[0].Selected.Text != "最高分原稿" || s.Accepted[0].Selected.Attempt != 1 || s.Accepted[0].Reason != "retry_limit_best" || len(s.Accepted[0].Attempts) != 4 || s.Phase != "closing" || len(s.Attempts) != 0 {
		t.Fatal(s)
	}
}
func TestFixedQueueAndClosingEndWithoutModuloLoop(t *testing.T) {
	s := fixture(t, 3)
	for i := 0; i < 3; i++ {
		if s.Task().Key != s.Queue[i].Key {
			t.Fatal("queue changed")
		}
		add(t, &s, "不同开头的采纳段落", 90)
	}
	if s.Phase != "closing" || s.Cursor != 3 {
		t.Fatal(s)
	}
	add(t, &s, "总结并自然承接下一轮", 90)
	if s.Phase != "ended" || s.Status != "completed" || s.Cursor != 3 || len(s.Accepted) != 4 || len(s.Used) != 3 {
		t.Fatal(s)
	}
	if _, err := s.AddCandidate(candidate("结束后不应生成", 100), anchorstylescore.Baseline{}); err == nil {
		t.Fatal("ended run accepted late draft")
	}
}
func TestEmptyQueueClosesWithoutInventingPoint(t *testing.T) {
	s := fixture(t, 0)
	if s.Phase != "closing" {
		t.Fatal(s)
	}
	add(t, &s, "本轮收口", 90)
	if len(s.Used) != 0 || s.Phase != "ended" {
		t.Fatal(s)
	}
}
func TestMemoryKeepsThreeSummaries(t *testing.T) {
	s := fixture(t, 7)
	for i := 0; i < 6; i++ {
		add(t, &s, strings.Repeat(string(rune('甲'+i)), 300), 90)
	}
	// 1000 字宏观风格窗口已随 accepted_macro_feedback 一起下线，这里只校验近期摘要保留 3 条。
	if len(s.RecentSummaries) != 3 || len(s.Used) != 6 {
		t.Fatal(s)
	}
}
func TestWindowUnicodeAndCountingMatchBaseline(t *testing.T) {
	if got := Window("啊 \n哟😀！"); got != "啊 \n哟😀！" {
		t.Fatal(got)
	}
	if got := Window(strings.Repeat("甲", 1000) + "😀"); len([]rune(got)) != 1000 || !strings.HasSuffix(got, "😀") {
		t.Fatal("Unicode broken")
	}
}
func TestStateRoundTripRestoresCandidateWithoutConsumingPoint(t *testing.T) {
	s := fixture(t, 2)
	add(t, &s, "尚未采纳的候选", 50)
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	var restored State
	if err = json.Unmarshal(raw, &restored); err != nil {
		t.Fatal(err)
	}
	if restored.Cursor != 0 || len(restored.Attempts) != 1 || len(restored.Accepted) != 0 {
		t.Fatal(restored)
	}
	add(t, &restored, "恢复后采纳", 90)
	if restored.Cursor != 1 || len(restored.Accepted[0].Attempts) != 2 {
		t.Fatal(restored)
	}
}
func TestNoRetryAndInvalidConfigs(t *testing.T) {
	s := fixture(t, 1)
	s.Config.MaxRetries = 0
	add(t, &s, "零重试接受次优", 20)
	if s.Cursor != 1 || s.Accepted[0].Reason != "retry_limit_best" {
		t.Fatal(s)
	}
	for _, cfg := range []Config{{Threshold: 101, MaxRetries: 3, MaxChars: 300}, {Threshold: 80, MaxRetries: 6, MaxChars: 300}, {Threshold: 80, MaxRetries: 3, MaxChars: 0}} {
		if _, err := New(nil, cfg); err == nil {
			t.Fatal("invalid config accepted")
		}
	}
	if _, err := New([]Task{{SellingPoint: "同", Angle: "一"}, {SellingPoint: "同", Angle: "一"}}, Config{Threshold: 80, MaxRetries: 3, MaxChars: 300}); err == nil {
		t.Fatal("duplicate fixed queue")
	}
}

func TestFixedQueueSpacingKeepsOpeningAndNeverRepeatsCombinations(t *testing.T) {
	tasks := []Task{}
	for angle := 0; angle < 4; angle++ {
		for _, point := range []string{"甲", "乙", "丙", "丁"} {
			tasks = append(tasks, Task{SellingPoint: point, Angle: string(rune('一' + angle)), Key: point + string(rune('一'+angle))})
		}
	}
	queue, err := Arrange(tasks, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(queue) != len(tasks) || queue[0].Key != tasks[0].Key {
		t.Fatal(queue)
	}
	seen := map[string]bool{}
	for i, task := range queue {
		if seen[task.Key] {
			t.Fatal("combination repeated")
		}
		seen[task.Key] = true
		for _, prior := range queue[max(0, i-2):i] {
			if prior.SellingPoint == task.SellingPoint {
				t.Fatal("point spacing violated")
			}
		}
	}
	if _, err = Arrange([]Task{{SellingPoint: "甲"}, {SellingPoint: "乙"}, {SellingPoint: "甲"}}, 2); err == nil {
		t.Fatal("silently violated spacing")
	}
}
