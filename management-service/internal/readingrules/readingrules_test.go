package readingrules

import "testing"

func TestParseAndApplyBreak(t *testing.T) {
	rules := Parse("南充特产绸都杨鸭子 => 南充特产|绸都杨鸭子\n")
	if len(rules) != 1 || rules[0].Kind != "break" {
		t.Fatalf("rules=%#v", rules)
	}
	plain, ssml := Apply("咱卖的是南充特产绸都杨鸭子", rules)
	if plain != "咱卖的是南充特产，绸都杨鸭子" {
		t.Fatalf("plain=%q", plain)
	}
	want := `咱卖的是南充特产<break time="200ms"/>绸都杨鸭子`
	if ssml != want {
		t.Fatalf("ssml=%q want %q", ssml, want)
	}
}

func TestParsePronounce(t *testing.T) {
	rules := Parse("绸都 => chou2 du1")
	if len(rules) != 1 || rules[0].Kind != "pronounce" {
		t.Fatalf("rules=%#v", rules)
	}
	plain, ssml := Apply("绸都特产", rules)
	if plain != "绸都特产" {
		t.Fatalf("plain=%q", plain)
	}
	want := `<phoneme alphabet="py" ph="chou2 du1">绸都</phoneme>特产`
	if ssml != want {
		t.Fatalf("ssml=%q want %q", ssml, want)
	}
}

func TestParseSkipsMalformed(t *testing.T) {
	rules := Parse("随便一行\n南充 => 南充特产|绸都\n绸都 => chou2\n# 注释")
	if len(rules) != 1 || rules[0].Source != "南充" {
		t.Fatalf("rules=%#v", rules)
	}
}
