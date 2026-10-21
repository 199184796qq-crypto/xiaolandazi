package decisionexecutor

import (
	"context"
	"strings"
	"testing"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/contentguard"
	"livecompanion/management/internal/model"
)

const ttsP0Fixture = "```json\n" + `{
  "schema_version": 1,
  "layer": "L1",
  "severity": "P0",
  "policy_version": "tts-test-v1",
  "status": "active",
  "execution": {"default_scope": "all", "max_rewrites": 3, "max_consecutive_skips": 3,
    "rewrite_scope": "matched_sentence_or_paragraph", "recheck_scope": "whole_candidate",
    "on_exhausted": "skip_current_selling_point_angle", "count_rejected_as_adopted": false,
    "allow_user_override": false},
  "normalization": {"unicode_form": "NFKC", "case_fold": false, "remove_zero_width": true,
    "ignore_spaces_for_match": true, "keep_original_offsets": true, "aliases": {}},
  "rules": [
    {"id": "r1", "severity": "P0", "category": "绝对词与极限词", "scope": "all", "condition": "always",
     "terms": ["最好", "最佳", "第一", "绝对", "顶级"], "patterns": [], "note": "test"},
    {"id": "r2", "severity": "P0", "category": "医疗与保健功效", "scope": "all", "condition": "always",
     "terms": ["治疗", "根治"], "patterns": [], "note": "test"}
  ]
}` + "\n```"

type ttsP0FakeAgent struct {
	replies []string
	calls   int
}

func (f *ttsP0FakeAgent) Complete(context.Context, agentgateway.Request) (agentgateway.Response, error) {
	index := f.calls
	f.calls++
	if index >= len(f.replies) {
		index = len(f.replies) - 1
	}
	if index < 0 {
		return agentgateway.Response{}, nil
	}
	return agentgateway.Response{Text: f.replies[index]}, nil
}

func ttsP0TestPolicy(t *testing.T) *contentguard.Policy {
	t.Helper()
	policy, err := contentguard.Compile(ttsP0Fixture)
	if err != nil {
		t.Fatal(err)
	}
	return policy
}

func TestSplitTTSSentencesPreservesPunctuationAndNewlines(t *testing.T) {
	text := "第一句。第二句！第三句？\n第四句；第五句"
	got := splitTTSSentences(text)
	want := []string{"第一句。", "第二句！", "第三句？", "\n", "第四句；", "第五句"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("split = %#v, want %#v", got, want)
	}
	if joined := strings.Join(got, ""); joined != text {
		t.Fatalf("rejoin changed text: %q", joined)
	}
}

func TestTTSP0CandidatesAndPicking(t *testing.T) {
	policy := ttsP0TestPolicy(t)
	candidates := ttsP0RewriteCandidates("```json\n{\"备选\":[\"这是最好的\",\"这是很好的\"]}\n```")
	if len(candidates) != 2 {
		t.Fatalf("candidates=%#v", candidates)
	}
	if got := ttsP0PickCandidate(candidates, policy); got != "这是很好的" {
		t.Fatalf("picked=%q", got)
	}
	if got := ttsP0PickCandidate([]string{"这是最好的"}, policy); got != "" {
		t.Fatalf("all-hit must drop, got %q", got)
	}
}

func TestTTSP0KeepPunctuation(t *testing.T) {
	if got := ttsP0KeepSentencePunctuation("这是最好的。", "这是很好的"); got != "这是很好的。" {
		t.Fatalf("got %q", got)
	}
	if got := ttsP0KeepSentencePunctuation("这是最好的！", "这是很好的？"); got != "这是很好的？" {
		t.Fatalf("existing punctuation changed: %q", got)
	}
}

func TestRewriteOneTTSSentencePicksCleanCandidate(t *testing.T) {
	policy := ttsP0TestPolicy(t)
	agent := &ttsP0FakeAgent{replies: []string{`{"备选":["这是最好的","这是很好的"]}`}}
	worker := &Worker{agent: agent}
	got := worker.rewriteOneTTSSentence(context.Background(), modelSessionForTest(), "这是最好的。", policy.Scan("这是最好的。", "all").Hits, policy)
	if got != "这是很好的。" {
		t.Fatalf("got %q", got)
	}
}

func TestRewriteOneTTSSentenceDropsWhenBothHit(t *testing.T) {
	policy := ttsP0TestPolicy(t)
	agent := &ttsP0FakeAgent{replies: []string{`{"备选":["这是最好的","这是最佳的"]}`}}
	worker := &Worker{agent: agent}
	got := worker.rewriteOneTTSSentence(context.Background(), modelSessionForTest(), "这是最好的。", policy.Scan("这是最好的。", "all").Hits, policy)
	if got != "" {
		t.Fatalf("expected drop, got %q", got)
	}
}

func TestRewriteTTSP0SentencesRewritesOnlyHitSentences(t *testing.T) {
	agent := &ttsP0FakeAgent{replies: []string{
		`{"备选":["这是很好的","这是最好的"]}`,
		`{"备选":["绝对第一","咱们家这个不错"]}`,
	}}
	worker := &Worker{agent: agent}
	got, rewritten, dropped := worker.rewriteTTSP0Sentences(
		context.Background(),
		model.LiveRuntimeSession{},
		"这是最好的。收到后尽快吃。绝对第一。",
	)
	if rewritten != 2 || dropped != 0 {
		t.Fatalf("rewritten=%d dropped=%d", rewritten, dropped)
	}
	if got != "这是很好的。收到后尽快吃。咱们家这个不错。" {
		t.Fatalf("got %q", got)
	}
}

// 20 条真实口播风格的句子：只验证分句与 P0 命中，不调用模型。
func TestTTSP0SentenceMatrix20(t *testing.T) {
	policy := ttsP0TestPolicy(t)
	cases := []struct {
		sentence string
		hit      bool
	}{
		{"这是最好的鸭子。", true},
		{"这是很好的鸭子。", false},
		{"绝对不踩雷。", true},
		{"收到后尽快吃。", false},
		{"全网第一。", true},
		{"咱们家是第一名。", true},
		{"这个价格很实在。", false},
		{"能治疗胃病。", true},
		{"这个可以根治。", true},
		{"口感很香。", false},
		{"这是最佳选择。", true},
		{"今天现货。", false},
		{"保证不亏。", false},
		{"没有谁比这更香。", false},
		{"这是顶级工艺。", true},
		{"分量很足。", false},
		{"放心拍。", false},
		{"绝对第一。", true},
		{"这是很好的选择。", false},
		{"非常香。", false},
	}
	if len(cases) != 20 {
		t.Fatalf("want 20 cases, got %d", len(cases))
	}
	for _, tc := range cases {
		parts := splitTTSSentences(tc.sentence)
		if len(parts) != 1 {
			t.Fatalf("split %q -> %#v", tc.sentence, parts)
		}
		got := len(policy.Scan(parts[0], "all").Hits) > 0
		if got != tc.hit {
			t.Fatalf("%q hit=%v, want %v", tc.sentence, got, tc.hit)
		}
	}
}

func modelSessionForTest() model.LiveRuntimeSession {
	return model.LiveRuntimeSession{}
}
