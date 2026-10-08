package anchorcycle

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"livecompanion/management/internal/anchorstylescore"
)

func hookFixture(t *testing.T) (*OpeningHookLibrary, map[string]string) {
	t.Helper()
	md, err := os.ReadFile("../../../configs/prompts/anchor-style/opening-hook-library.md")
	if err != nil {
		t.Fatal(err)
	}
	library, err := ParseOpeningHooks(string(md))
	if err != nil {
		t.Fatal(err)
	}
	return library, map[string]string{"福利": "1号链接：新人开盖试吃5斤，不满意退剩余全额退款", "背书": "四川绵阳河清镇39年老油坊，自产自销", "卖点A": "老坊背书", "角度": "产地说明", "目标人群": "日常做饭的家庭", "痛点": "担心不了解来路"}
}

func TestOpeningHookRulesAndSlotNamesComeFromMother(t *testing.T) {
	library, values := hookFixture(t)
	s := fixture(t, 2)
	if err := s.SelectOpeningHook(library, values, "mother-v1"); err != nil || s.PendingOpeningHook.Kind != "利益钩" {
		t.Fatal(err, s.PendingOpeningHook)
	}
	if !strings.Contains(s.PendingOpeningHook.FilledTemplate, values["福利"]) || strings.Contains(s.PendingOpeningHook.FilledTemplate, "{福利}") {
		t.Fatal(s.PendingOpeningHook)
	}
	// Change business policy without changing Go: prefer contrast, no cooldown.
	s.PendingOpeningHook = nil
	library.Rules.FullPreferred = []string{"反差钩"}
	zero := 0
	library.Rules.RecentCount = &zero
	if err := s.SelectOpeningHook(library, values, "mother-v2"); err != nil || s.PendingOpeningHook.Kind != "反差钩" || s.PendingOpeningHook.RecentCount != 0 {
		t.Fatal(err, s.PendingOpeningHook)
	}
	// Arbitrary slot names are legal when the mother declares their binding.
	library.Bindings["来路内容"] = library.Bindings["背书"]
	for i := range library.Hooks {
		library.Hooks[i].Template = strings.ReplaceAll(library.Hooks[i].Template, "{背书}", "{来路内容}")
		for j, key := range library.Hooks[i].Hard {
			if key == "背书" {
				library.Hooks[i].Hard[j] = "来路内容"
			}
		}
	}
	raw, _ := json.Marshal(library)
	if _, err := ParseOpeningHooks("```json\n" + string(raw) + "\n```"); err != nil {
		t.Fatal(err)
	}
}

func TestOpeningHookMissingDataSkipsAndExhaustionDoesNotInvent(t *testing.T) {
	library, values := hookFixture(t)
	s := fixture(t, 2)
	delete(values, "福利")
	if err := s.SelectOpeningHook(library, values, "mother"); err != nil || s.PendingOpeningHook.Kind != "反差钩" {
		t.Fatal(err, s.PendingOpeningHook)
	}
	s.PendingOpeningHook = nil
	s.OpeningCount = 1
	delete(values, "背书")
	delete(values, "目标人群")
	delete(values, "痛点")
	if err := s.SelectOpeningHook(library, values, "mother"); err != nil || s.PendingOpeningHook.Kind != "痛点钩" {
		t.Fatal(err, s.PendingOpeningHook)
	}
	s.PendingOpeningHook = nil
	for _, h := range library.Hooks {
		s.RecentOpeningHookIDs = append(s.RecentOpeningHookIDs, h.ID)
	}
	n := len(s.RecentOpeningHookIDs)
	library.Rules.RecentCount = &n
	if err := s.SelectOpeningHook(library, values, "mother"); err == nil || s.PendingOpeningHook != nil {
		t.Fatal("exhausted library silently reused a hook")
	}
}

func TestOpeningHookRetryReloadAndAdoptionMemory(t *testing.T) {
	library, values := hookFixture(t)
	s := fixture(t, 2)
	if err := s.SelectOpeningHook(library, values, "mother-v1"); err != nil {
		t.Fatal(err)
	}
	chosen := *s.PendingOpeningHook
	if add(t, &s, chosen.FilledTemplate, 50) || len(s.RecentOpeningHookIDs) != 0 {
		t.Fatal("rejected hook consumed memory")
	}
	raw, _ := json.Marshal(s)
	var restored State
	if json.Unmarshal(raw, &restored) != nil {
		t.Fatal("reload failed")
	}
	library.Rules.FullPreferred = []string{"反差钩"}
	if err := restored.SelectOpeningHook(library, map[string]string{}, "mother-v2"); err != nil || restored.PendingOpeningHook.ID != chosen.ID || restored.PendingOpeningHook.Source != "mother-v1" {
		t.Fatal("retry rerolled snapshot", err)
	}
	if !add(t, &restored, chosen.FilledTemplate+"，接着讲本轮卖点。", 90) {
		t.Fatal("valid hook rejected")
	}
	if restored.PendingOpeningHook != nil || len(restored.RecentOpeningHookIDs) != 1 || restored.Accepted[0].OpeningHook.ID != chosen.ID {
		t.Fatal("adoption not recorded")
	}
}

func TestOpeningHookTwentyAdoptionsAvoidRecentThree(t *testing.T) {
	library, values := hookFixture(t)
	s := rollingFixture(t, 2)
	for n := 0; n < 20; n++ {
		s.Phase = "opening"
		if err := s.SelectOpeningHook(library, values, "mother"); err != nil {
			t.Fatal(err)
		}
		chosen := s.PendingOpeningHook
		for _, prior := range s.RecentOpeningHookIDs {
			if prior == chosen.ID {
				t.Fatal("recent hook repeated", n, prior)
			}
		}
		text := chosen.FilledTemplate
		for key, value := range chosen.SoftValues {
			text = strings.ReplaceAll(text, "{"+key+"}", value)
		}
		if !add(t, &s, text, 90) {
			t.Fatal("not adopted", n)
		}
		if len(s.RecentOpeningHookIDs) > 3 {
			t.Fatal("cooldown length wrong")
		}
	}
}

func TestOpeningHookNumbersNormalizeWithoutMandatoryRewrites(t *testing.T) {
	for text, want := range map[string]string{"三十九年": "39年", "五斤": "5斤", "二两": "2两", "两斤": "2斤", "二零二六年": "2026年", "一百零五天": "105天", "129.9元": "129.9元"} {
		if got := normalizeHookQuantity(text); got != want {
			t.Fatalf("%s => %s, want %s", text, got, want)
		}
	}
	library, values := hookFixture(t)
	s := fixture(t, 2)
	if err := s.SelectOpeningHook(library, values, "mother"); err != nil {
		t.Fatal(err)
	}
	if s.openingHookProblem("新来的先看福利，开盖试吃五斤。") != "" {
		t.Fatal("Chinese numbers caused retry")
	}
	if s.openingHookProblem("开盖只能试吃4斤。") == "" {
		t.Fatal("changed hard value passed")
	}
	s.PendingOpeningHook.CheckNumbers = false
	if s.openingHookProblem("没有数字") != "" {
		t.Fatal("configured switch ignored")
	}
}

func TestOpeningHookHardFailureCannotBeResurrectedByBestScore(t *testing.T) {
	library, values := hookFixture(t)
	s := fixture(t, 2)
	s.Config.MaxRetries = 2
	if err := s.SelectOpeningHook(library, values, "mother"); err != nil {
		t.Fatal(err)
	}
	if add(t, &s, "开盖试吃4斤。", 100) {
		t.Fatal("invalid high score adopted")
	}
	if add(t, &s, "开盖试吃5斤，第二稿。", 70) {
		t.Fatal("below score adopted early")
	}
	if !add(t, &s, "开盖试吃五斤，第三稿。", 60) || s.Accepted[0].Selected.Score.OverallScore != 70 {
		t.Fatal("best valid draft not selected")
	}
	if strings.Contains(s.StyleWindow, "4斤") {
		t.Fatal("invalid draft entered memory")
	}
}

func TestOpeningHookForbiddenUsesAnchorBaselineOnly(t *testing.T) {
	library, values := hookFixture(t)
	s := fixture(t, 2)
	if err := s.SelectOpeningHook(library, values, "mother"); err != nil {
		t.Fatal(err)
	}
	var b anchorstylescore.Baseline
	b.Forbidden.Addresses = []string{"宝宝们"}
	text := "宝宝们，开盖试吃5斤。"
	c := candidate(text, 100)
	c.Score = anchorstylescore.Score(text, b, "")
	if ok, _ := s.AddCandidate(c, b); ok || s.Attempts[0].ContentIssue != "opening_hook_forbidden_word" {
		t.Fatal("forbidden draft adopted")
	}
	text = "你们先看，您开盖试吃5斤。"
	c = candidate(text, 100)
	c.Score.Stats = anchorstylescore.Score(text, b, "").Stats
	if ok, err := s.AddCandidate(c, b); !ok || err != nil {
		t.Fatal("legal pronouns banned", err)
	}
}

func TestOpeningHookValidationDoesNotAffectBodyOrClosing(t *testing.T) {
	library, values := hookFixture(t)
	for _, phase := range []string{"body", "closing"} {
		s := fixture(t, 2)
		if err := s.SelectOpeningHook(library, values, "mother"); err != nil {
			t.Fatal(err)
		}
		s.Phase = phase
		if !add(t, &s, "其它环节照常讲自己的内容。", 90) || len(s.RecentOpeningHookIDs) != 0 || s.Accepted[0].OpeningHook != nil {
			t.Fatal("non-opening changed", phase)
		}
	}
}

func TestOpeningHookSchemaErrorsAreExplicit(t *testing.T) {
	library, _ := hookFixture(t)
	for _, mutate := range []func(*OpeningHookLibrary){
		func(l *OpeningHookLibrary) { l.Rules.RecentCount = nil },
		func(l *OpeningHookLibrary) { l.Rules.FullPreferred = []string{"不存在"} },
		func(l *OpeningHookLibrary) { l.Hooks[0].ID = l.Hooks[1].ID },
		func(l *OpeningHookLibrary) { l.Hooks[0].Template += "{未绑定}" },
		func(l *OpeningHookLibrary) { l.Bindings["福利"] = OpeningHookBinding{Source: "自行推断"} },
		func(l *OpeningHookLibrary) {
			l.Bindings["福利"] = OpeningHookBinding{Source: "有效福利", Field: "活动", Pattern: "("}
		},
	} {
		raw, _ := json.Marshal(library)
		var copy OpeningHookLibrary
		_ = json.Unmarshal(raw, &copy)
		mutate(&copy)
		raw, _ = json.Marshal(copy)
		if _, err := ParseOpeningHooks("```json\n" + string(raw) + "\n```"); err == nil {
			t.Fatal("bad schema accepted", string(raw))
		}
	}
}
