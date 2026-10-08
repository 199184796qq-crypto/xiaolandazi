package httpapi

import (
	"livecompanion/management/internal/anchorstylescore"
	"testing"
)

func TestScoreFeedbackTemplateLoadedAndErrorsDoNotLoseScore(t *testing.T) {
	t.Setenv("ANCHOR_STYLE_SCORE_FEEDBACK_PROMPT_PATH", "")
	md, _, err := loadAnchorStyleScoreFeedbackPrompt()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = anchorstylescore.ParseFeedbackTemplate(md); err != nil {
		t.Fatal(err)
	}
	f := buildAnchorStyleScoreFeedback(anchorstylescore.Result{OverallScore: 100}, anchorstylescore.Baseline{})
	if f.Status != "no_issue" || f.Prompt != "" {
		t.Fatalf("unexpected feedback: %+v", f)
	}
	t.Setenv("ANCHOR_STYLE_SCORE_FEEDBACK_PROMPT_PATH", t.TempDir()+"/missing.md")
	f = buildAnchorStyleScoreFeedback(anchorstylescore.Result{OverallScore: 100}, anchorstylescore.Baseline{})
	if f.Status != "template_error" || f.Error == "" || f.Prompt != "" {
		t.Fatalf("template failure not explicit: %+v", f)
	}
}
