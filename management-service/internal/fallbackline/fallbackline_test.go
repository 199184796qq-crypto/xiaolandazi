package fallbackline

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMain(m *testing.M) {
	path, err := filepath.Abs(filepath.Join("..", "..", "..", "configs", "prompts", "anchor-style", "fact-bound.json"))
	if err == nil {
		os.Setenv("FACT_BOUND_CONFIG", path)
	}
	os.Exit(m.Run())
}

func TestValidateRejectsUnsafeLines(t *testing.T) {
	base := Request{Class: "no_dimension", Question: "能治高血压吗"}
	for _, text := range []string{
		"这个我还没确认，但是包邮的。",
		"这个我还没确认，大概3天能到。",
		"这个我还没确认，以实际为准。",
		"我叫小鹿，这个我还没确认。",
		"这个我还没确认，别弄混了。",
		"好的。",
		"这个我还没确认，\n先不跟你说死。",
	} {
		if Validate(text, base) == nil {
			t.Fatalf("unsafe line accepted: %q", text)
		}
	}
	if err := Validate("这个我还真没问到，先不跟你瞎说哈。", base); err != nil {
		t.Fatalf("safe line rejected: %v", err)
	}
}

func TestValidateRejectsRepeats(t *testing.T) {
	request := Request{Class: "no_dimension", Question: "能治高血压吗", Recent: []string{"这个我还没确认，先不跟你说死。"}}
	if Validate("这个我还没确认，先不跟你说死。", request) == nil {
		t.Fatal("exact repeat accepted")
	}
	if err := Validate("这块我真没底，等我问清楚了再说。", request); err != nil {
		t.Fatalf("different line rejected: %v", err)
	}
}

func TestParseAndPickCandidates(t *testing.T) {
	raw := `{"候选":["这个我还没确认，先不跟你说死。","西藏这块我还没核实，别急哈。","包邮的，放心买。","这个得看情况，3天能到。"]}`
	candidates := ParseCandidates(raw)
	if len(candidates) != 4 {
		t.Fatalf("candidates=%v", candidates)
	}
	request := Request{Class: "partial_value", Question: "西藏包邮吗", Uncovered: []string{"西藏"}}
	line, err := Pick(request, candidates)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(line, "西藏") {
		t.Fatalf("on-topic candidate should win: %s", line)
	}
	if _, err := Pick(request, []string{"包邮的，放心买。"}); err == nil {
		t.Fatal("unsafe candidate accepted")
	}
}

func TestPickPrefersHumanDevices(t *testing.T) {
	request := Request{Class: "no_dimension", Question: "能治高血压吗"}
	line, err := Pick(request, []string{
		"这个我还没确认，先不跟你说死。",
		"这个我还真说不好啊，我这脑子跟漏勺似的，你说我能瞎讲吗？",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(line, "漏勺") {
		t.Fatalf("human line should win: %s", line)
	}
	line, err = Pick(request, []string{
		"亲，这个我还没确认，请您稍等。",
		"这个我暂时说不好，别急哈。",
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(line, "亲") {
		t.Fatalf("customer-service tone should lose: %s", line)
	}
}

func TestPromptCarriesToneAndBans(t *testing.T) {
	_, system, user := Prompt(Request{Class: "identity", Question: "主播叫什么名字"})
	for _, want := range []string{"不要重复", "不要编造名字", "不要解释规则", "这次的语气：", "句式：", "修辞："} {
		if !strings.Contains(system, want) {
			t.Fatalf("system prompt missing %q: %s", want, system)
		}
	}
	if !strings.Contains(user, "主播叫什么名字") {
		t.Fatalf("question missing from payload: %s", user)
	}
}
