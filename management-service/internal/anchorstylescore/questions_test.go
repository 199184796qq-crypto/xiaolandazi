package anchorstylescore

import "testing"

func TestSpokenQuestionsAndPunctuationDeduplicate(t *testing.T) {
	for _, tc := range []struct {
		text  string
		count int
	}{
		{"都买好了不？都拍到了不，划算不。对不对嘛。听好了没。", 5},
		{"活动嘛，低芥酸嘛，你看嘛。不得不说，这个不贵，说不定还有，没得了。", 0},
		{"这个行不行啊，那个好不好哟？听到了么。", 3},
	} {
		if got := CountQuestions(tc.text); got != tc.count {
			t.Fatalf("%q=%d want%d", tc.text, got, tc.count)
		}
	}
}

func TestForbiddenWordsArePlanSpecificSeparateAndHighestPriority(t *testing.T) {
	b := sampleBaseline()
	b.Forbidden.Addresses = []string{"你们", "您"}
	r := Score("我来给你们算账，您看一下，你们听好了。", b, "价格")
	if len(r.Stats.Forbidden) != 2 || r.Stats.Forbidden[0].Count != 2 || r.Stats.Forbidden[1].Count != 1 {
		t.Fatal(r.Stats.Forbidden)
	}
	// 评分侧纠偏草稿（BuildFeedback）已随回灌下线删除，这里只校验禁用词统计本身。
	report := AnalyzeMacro1000("您看。", []string{"您看。"}, 1, b, rollingStyle)
	if !report.ObservationOnly || len(report.Issues) == 0 || report.Issues[0].Key != "forbidden_word" {
		t.Fatal(report)
	}
	b.Forbidden.Addresses = nil
	if len(Measure("你们和您", b).Forbidden) != 0 {
		t.Fatal("globally hardcoded forbidden words")
	}
}
