package httpapi

import (
	"strings"
	"testing"
)

func validMinerConfirmation() speechMinerConfirmInput {
	return speechMinerConfirmInput{Confirmed: true, SessionID: "session-123456", Facts: []speechMinerFactDraft{{Category: "product", Key: "保存方式", Value: "常温保存", SourceQuote: "常温保存"}}, History: []speechMinerMessage{{Role: "agent", Text: "怎么保存？"}, {Role: "user", Text: "本品常温保存"}}}
}

func TestMinerConfirmationRequiresHumanEvidenceAndExplicitConfirmation(t *testing.T) {
	items, err := validateSpeechMinerConfirmation(validMinerConfirmation())
	if err != nil || len(items) != 1 || items[0].Value != "常温保存" {
		t.Fatal(items, err)
	}
	for _, tc := range []struct {
		name   string
		change func(*speechMinerConfirmInput)
	}{
		{"not confirmed", func(i *speechMinerConfirmInput) { i.Confirmed = false }},
		{"agent evidence", func(i *speechMinerConfirmInput) { i.History[1].Role = "agent" }},
		{"invented evidence", func(i *speechMinerConfirmInput) { i.Facts[0].SourceQuote = "冷藏" }},
		{"no category", func(i *speechMinerConfirmInput) { i.Facts[0].Category = "" }},
		{"duplicate fact", func(i *speechMinerConfirmInput) { i.Facts = append(i.Facts, i.Facts[0]) }},
		{"invalid session", func(i *speechMinerConfirmInput) { i.SessionID = "../../other" }},
		{"oversize conversation", func(i *speechMinerConfirmInput) { i.History[0].Text = strings.Repeat("字", 120001) }},
		{"no facts", func(i *speechMinerConfirmInput) { i.Facts = nil }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			i := validMinerConfirmation()
			tc.change(&i)
			if _, err := validateSpeechMinerConfirmation(i); err == nil {
				t.Fatal("unsafe confirmation accepted")
			}
		})
	}
}

func TestMinerControlEventsNeverCreateFacts(t *testing.T) {
	for _, input := range []speechMinerGuideInput{{Start: true}, {Action: "skip"}, {Action: "complete"}} {
		output := speechMinerGuideResponse{Reply: "核对后确认", Completed: true, Options: []speechMinerOption{{Label: "继续", Value: "继续"}}, CapturedFacts: validMinerConfirmation().Facts}
		applySpeechMinerTurnBoundary(&output, input)
		if len(output.CapturedFacts) != 0 {
			t.Fatal("control event generated facts", input)
		}
		if input.Start && output.Completed {
			t.Fatal("start event completed")
		}
		if input.Action == "complete" && (!output.Completed || len(output.Options) != 0) {
			t.Fatal("complete did not end questions")
		}
	}
	output := speechMinerGuideResponse{CapturedFacts: validMinerConfirmation().Facts}
	applySpeechMinerTurnBoundary(&output, speechMinerGuideInput{Action: "answer"})
	if len(output.CapturedFacts) != 1 {
		t.Fatal("real answer was lost")
	}
}

func TestMinerModesAndActionsRejectUnknownInputs(t *testing.T) {
	for _, mode := range []string{"", "fact_gaps", "new_selling_point", "audience_reverse"} {
		if _, err := normalizeSpeechMinerMode(mode); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := normalizeSpeechMinerMode("publish"); err == nil {
		t.Fatal("invalid mode accepted")
	}
	for _, action := range []string{"", "answer", "skip", "complete"} {
		if _, err := normalizeSpeechMinerAction(action); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := normalizeSpeechMinerAction("save"); err == nil {
		t.Fatal("save cannot be a conversation action")
	}
}
