package httpapi

import "testing"

func TestNormalizeSpeechMinerGuideResponseOnlyKeepsLatestUserEvidence(t *testing.T) {
	raw := `{
  "reply":"我记下了，再确认一下保存方式。",
  "options":[{"label":"常温","value":"常温保存"},{"label":"常温重复","value":"常温保存"}],
  "captured_facts":[
    {"category":"product","key":"保质期","value":"30天","source_quote":"保质期30天"},
    {"category":"product","key":"保存方式","value":"冷藏","source_quote":"冷藏"},
    {"category":"unknown","key":"错误","value":"错误","source_quote":"保质期30天"}
  ],
  "completed":false
}`
	got, err := normalizeSpeechMinerGuideResponse(raw, "保质期30天")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Options) != 1 || len(got.CapturedFacts) != 1 || got.CapturedFacts[0].Key != "保质期" {
		t.Fatal(got)
	}
}

func TestNormalizeSpeechMinerGuideResponseCompletedClearsOptions(t *testing.T) {
	raw := `{"reply":"这轮挖掘完成。","options":[{"label":"继续","value":"继续"}],"captured_facts":[],"completed":true}`
	got, err := normalizeSpeechMinerGuideResponse(raw, "完成")
	if err != nil || !got.Completed || len(got.Options) != 0 {
		t.Fatal(got, err)
	}
}

func TestNormalizeSpeechMinerGuideResponseKeepsValidMultipleChoice(t *testing.T) {
	raw := `{"reply":"这款产品有哪些适用场景？","selection_mode":"multiple","options":[{"label":"早餐","value":"适合早餐","action":"answer"},{"label":"宵夜","value":"适合宵夜","action":"answer"},{"label":"不清楚","value":"我不清楚，问下一个","action":"skip"}],"captured_facts":[],"completed":false}`
	got, err := normalizeSpeechMinerGuideResponse(raw, "继续")
	if err != nil {
		t.Fatal(err)
	}
	if got.SelectionMode != "multiple" || len(got.Options) != 3 || got.Options[2].Action != "skip" {
		t.Fatal(got)
	}
}

func TestNormalizeSpeechMinerGuideResponseDowngradesInvalidMultipleChoice(t *testing.T) {
	raw := `{"reply":"辣度是哪一种？","selection_mode":"multiple","options":[{"label":"微辣","value":"微辣","action":"answer"},{"label":"完成","value":"完成挖掘","action":"complete"}],"captured_facts":[],"completed":false}`
	got, err := normalizeSpeechMinerGuideResponse(raw, "继续")
	if err != nil {
		t.Fatal(err)
	}
	if got.SelectionMode != "single" {
		t.Fatal(got)
	}
}

func TestNormalizeSpeechMinerGuideResponseDefaultsSetsToMultiple(t *testing.T) {
	for _, marker := range []string{"", "unknown", "MULTIPLE"} {
		raw := `{"reply":"他们看重门店哪些方面？可以选多项。","selection_mode":"` + marker + `","options":[{"label":"数量多","value":"数量多"},{"label":"总店开得久","value":"总店开得久"},{"label":"门店地址具体","value":"门店地址具体"},{"label":"不清楚","value":"不清楚","action":"skip"}],"captured_facts":[],"completed":false}`
		got, err := normalizeSpeechMinerGuideResponse(raw, "继续")
		if err != nil || got.SelectionMode != "multiple" || len(got.Options) != 4 || got.Options[3].Action != "skip" {
			t.Fatalf("marker=%q output=%+v err=%v", marker, got, err)
		}
	}
}

func TestNormalizeSpeechMinerGuideResponseRespectsExplicitSingle(t *testing.T) {
	raw := `{"reply":"这一款的固定辣度是哪一种？","selection_mode":"single","options":[{"label":"微辣","value":"微辣"},{"label":"中辣","value":"中辣"},{"label":"很辣","value":"很辣"}],"captured_facts":[],"completed":false}`
	got, err := normalizeSpeechMinerGuideResponse(raw, "继续")
	if err != nil || got.SelectionMode != "single" {
		t.Fatal(got, err)
	}
}
