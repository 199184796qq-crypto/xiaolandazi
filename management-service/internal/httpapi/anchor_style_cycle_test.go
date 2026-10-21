package httpapi

import (
	"context"
	"strings"
	"testing"

	"livecompanion/management/internal/anchorcycle"
)

func cycleTestState(t *testing.T) anchorcycle.State {
	t.Helper()
	s, err := anchorcycle.New([]anchorcycle.Task{{SellingPoint: "老坊", Angle: "产地"}}, anchorcycle.Config{Threshold: 80, MaxRetries: 3, MaxChars: 300})
	if err != nil {
		t.Fatal(err)
	}
	s.StyleMD = "这是已确认的主播风格"
	s.Facts = "这是正式事实包"
	return s
}
func TestCycleFirstPromptAndContinuationStaySeparate(t *testing.T) {
	s := cycleTestState(t)
	first, err := renderAnchorCyclePrompt(&s)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(first, s.StyleMD) || !strings.Contains(first, s.Facts) || !strings.Contains(first, "老坊 × 产地") || !strings.Contains(first, "当前环节：开场") {
		t.Fatal(first)
	}
	s.Attempts = append(s.Attempts, anchorcycle.Candidate{})
	s.SegmentDirective = "轻微修正当前任务"
	retry, err := renderAnchorCyclePrompt(&s)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(retry, s.StyleMD) || strings.Contains(retry, s.Facts) || !strings.Contains(retry, "重写当前任务") || !strings.Contains(retry, "轻微修正当前任务") || !strings.Contains(retry, "第2个") {
		t.Fatal(retry)
	}
	s.Attempts = nil
	s.Phase = "closing"
	s.RecentSummaries = []string{"采纳稿：老乡起头；催问结束"}
	s.Accepted = []anchorcycle.Segment{{Task: s.Queue[0]}}
	closing, err := renderAnchorCyclePrompt(&s)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(closing, "本轮收口") || !strings.Contains(closing, s.RecentSummaries[0]) || !strings.Contains(closing, "不代表直播下播") {
		t.Fatal(closing)
	}
}

func TestReentryPromptUsesNewTemplateAndOldExpressionWithoutRebootstrapping(t *testing.T) {
	s := cycleTestState(t)
	s.Config.Rolling = true
	s.Config.BodySegments = 2
	s.Config.MaxCycles = 2
	s.CycleNumber = 2
	s.OpeningCount = 1
	s.Cursor = 3
	s.TaskIndex = 0
	s.Accepted = []anchorcycle.Segment{{Phase: "closing", Selected: anchorcycle.Candidate{Text: "上一轮已收口"}}}
	s.Used[s.Queue[0].Key] = anchorcycle.Use{Count: 1, LastIndex: 1, LastSummary: "上次围绕老油坊所在的产地介绍。"}
	prompt, err := renderAnchorCyclePrompt(&s)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"循环开场：接新版", "第2轮", "身份只用一句话", "上次围绕老油坊所在的产地介绍。", "已在本场被采纳1次"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("missing %s: %s", want, prompt)
		}
	}
	for _, absent := range []string{"首次开场：完整版", s.StyleMD, s.Facts, "{{"} {
		if strings.Contains(prompt, absent) {
			t.Fatalf("unexpected %s in reentry", absent)
		}
	}
	s.Attempts = []anchorcycle.Candidate{{}}
	retry, err := renderAnchorCyclePrompt(&s)
	if err != nil || !strings.Contains(retry, "循环开场：接新版") || strings.Contains(retry, "首次开场：完整版") {
		t.Fatal("reentry retry reverted to first opening", err)
	}
}
func TestCycleContextEndCancellationIsRoomScoped(t *testing.T) {
	server := &Server{}
	first, doneFirst := server.cycleContext(context.Background(), "one", 1, 10, "run-a")
	defer doneFirst()
	second, doneSecond := server.cycleContext(context.Background(), "two", 1, 11, "run-b")
	defer doneSecond()
	server.cycleCancels.Lock()
	for _, entry := range server.cycleCancels.entries {
		if entry.tenant == 1 && entry.room == 10 {
			entry.cancel()
		}
	}
	server.cycleCancels.Unlock()
	if first.Err() == nil || second.Err() != nil {
		t.Fatal("wrong room cancelled")
	}
}

func TestBrowserDisconnectDoesNotLoseAttempt(t *testing.T) {
	server := &Server{}
	request, cancelRequest := context.WithCancel(context.Background())
	cycle, done := server.cycleContext(request, "request", 1, 10, "run-a")
	defer done()
	cancelRequest()
	if cycle.Err() != nil {
		t.Fatal("browser refresh aborted an in-flight upstream attempt")
	}
	done()
	if cycle.Err() == nil {
		t.Fatal("explicit cancellation failed")
	}
}
