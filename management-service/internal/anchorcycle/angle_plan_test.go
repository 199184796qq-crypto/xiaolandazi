package anchorcycle

import "testing"

func TestAnglePlanRequiresStructureAndBindsToTaskAndFacts(t *testing.T) {
	for _, raw := range []string{`{}`, `{"audience_concern":"选择","communication_goal":"区分","explanation_action":"讲明","hook_intents":[]}`, `{"audience_concern":"选择","communication_goal":"区分","explanation_action":"讲明","hook_intents":[""]}`} {
		if _, err := ParseAnglePlan(raw); err == nil {
			t.Fatal("missing control structure accepted", raw)
		}
	}
	p, err := ParseAnglePlan(`{"audience_concern":"规格怎么选","communication_goal":"区分整只半只","explanation_action":"解释选择方法","hook_intents":["从先尝味还是分享进入","从桌上有没有其他菜进入"],"compatible_extra":"允许"}`)
	if err != nil {
		t.Fatal(err)
	}
	s := fixture(t, 2)
	s.AnglePlanMotherSHA = "v1"
	p.SourceSHA = s.AnglePlanKey()
	s.AnglePlans = map[string]*AnglePlan{p.SourceSHA: p}
	if s.CurrentAnglePlan() != p {
		t.Fatal("cache missing")
	}
	s.Facts += "资料更新"
	if s.CurrentAnglePlan() != nil {
		t.Fatal("facts changed but old plan used")
	}
	s.Facts = ""
	s.AnglePlanMotherSHA = "v2"
	if s.CurrentAnglePlan() != nil {
		t.Fatal("mother changed but old plan used")
	}
}
