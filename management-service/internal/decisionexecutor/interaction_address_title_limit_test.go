package decisionexecutor

import (
	"context"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"livecompanion/management/internal/addressing"
	"livecompanion/management/internal/interactionplan"
	"livecompanion/management/internal/model"
)

func TestV2AddressTitleUsesFinalSpokenName(t *testing.T) {
	for index, tc := range []struct {
		name, raw, spoken, grey string
		canHaveTitle            bool
	}{
		{"shortened_four", "红苕配稀饭", "红苕稀饭", `{"results":[{"index":0,"ok":true,"base":"红苕稀饭","short":"红苕"}]}`, true},
		{"shortened_five", "我爱吃红苕稀饭", "爱吃红苕饭", `{"results":[{"index":0,"ok":true,"base":"爱吃红苕饭","short":"红苕"}]}`, true},
		{"unchanged_five", "好运一直来", "好运一直来", `{"results":[{"index":0,"ok":true,"base":"好运一直来","short":"好运"}]}`, true},
		{"shortened_six", "我爱吃红苕稀饭", "爱吃红苕稀饭", `{"results":[{"index":0,"ok":true,"base":"爱吃红苕稀饭","short":"红苕"}]}`, false},
		{"generic_account_five", "用户3104259755684588", "用户三一零", "", true},
		{"existing_title", "王老板", "王老板", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w, store, _, agent, tts, session, _ := v2Fixture(t, int64(18100+index))
			store.addressingPrefs = model.RoomAddressingPreferences{PreferredTerms: []string{"老乡"}}
			w.nicknameRulesOnce.Do(func() {
				rules := addressing.DefaultRules()
				w.nicknameRulesValue = &rules
			})
			snapshot, _ := interactionplan.Frozen.ForSession(session.TenantID, session.RoomID, session.ID)
			// Exercise the existing random chooser without changing its distribution.
			// Missing all eligible title draws in 64 attempts has probability .4^64.
			const attempts = 64
			for range attempts {
				agent.responses = append(agent.responses, tc.grey)
			}
			seenBare, seenTitle := false, false
			for attempt := range attempts {
				item := &decisionItem{ID: tc.name + strconv.Itoa(attempt), Nicknames: []string{tc.raw}}
				got := w.v2Address(context.Background(), session, item, snapshot)
				switch got {
				case tc.spoken:
					seenBare = true
				case tc.spoken + "老乡":
					if !tc.canHaveTitle {
						t.Fatalf("ineligible spoken name received an extra title: %q", got)
					}
					seenTitle = true
				default:
					t.Fatalf("unexpected spoken address: %q, want %q with an optional eligible title", got, tc.spoken)
				}
				if !reflect.DeepEqual(item.PromptNicknames, []string{tc.spoken}) || item.resolvedAddress != got || item.resolvedAddressingPlan.Candidate != got {
					t.Fatalf("address plan disagrees with spoken address: %+v", item)
				}
			}
			if !seenBare || (tc.canHaveTitle && !seenTitle) {
				t.Fatalf("bare/title choices missing: bare=%v title=%v", seenBare, seenTitle)
			}
			wantModels := 0
			if tc.grey != "" {
				wantModels = attempts
			}
			if agent.calls != wantModels || tts.calls != 0 {
				t.Fatalf("title choice added model/TTS work: models=%d want=%d TTS=%d", agent.calls, wantModels, tts.calls)
			}
		})
	}
}

func TestV2MultiAddressTitleUsesEachFinalSpokenName(t *testing.T) {
	w, store, _, agent, _, session, _ := v2Fixture(t, 18200)
	store.addressingPrefs = model.RoomAddressingPreferences{PreferredTerms: []string{"老乡"}}
	w.nicknameRulesOnce.Do(func() {
		rules := addressing.DefaultRules()
		w.nicknameRulesValue = &rules
	})
	snapshot, _ := interactionplan.Frozen.ForSession(session.TenantID, session.RoomID, session.ID)
	const response = `{"results":[{"index":0,"ok":true,"base":"爱吃红苕饭","short":"红苕"},{"index":1,"ok":true,"base":"爱吃红苕稀饭","short":"红苕"}]}`
	const attempts = 128
	for range attempts {
		agent.responses = append(agent.responses, response)
	}
	seenTitledList := false
	for attempt := range attempts {
		item := &decisionItem{ID: "multi" + strconv.Itoa(attempt), Nicknames: []string{"我爱吃红苕稀饭", "我爱吃红苕稀饭", "王老板"}}
		got := w.v2Address(context.Background(), session, item, snapshot)
		if strings.Contains(got, "爱吃红苕稀饭老乡") || strings.Contains(got, "王老板老乡") {
			t.Fatalf("six-character or existing-title name received a title: %q", got)
		}
		if strings.Contains(got, "爱吃红苕饭老乡") {
			seenTitledList = true
			if got != "爱吃红苕饭老乡、爱吃红苕稀饭、王老板" {
				t.Fatalf("incorrect per-person title application: %q", got)
			}
		}
	}
	if !seenTitledList {
		t.Fatal("eligible shortened name never received a title in the multi-person list")
	}
	if agent.calls != attempts {
		t.Fatalf("grey names were not resolved in one batch per address: %d", agent.calls)
	}
}
