package orderbell

import "testing"

// Model labels are deliberately crossed with the same viewer statements.
// A different classification must never change the program's order evidence.
func TestOriginalConfirmationOverridesModelLabel(t *testing.T) {
	cases := []struct {
		text string
		want bool
	}{
		{"已拍五香", true},
		{"我拍了五香，多久发", true},
		{"五香味刚付完钱了，麻烦安排发货。", true},
		{"五香味这单刚结完账，支持一下主播。", true},
		{"刚付完款", true},
		{"已结账", true},
		{"已经结账", true},
		{"我结账了", true},
		{"已 买", true},
		{"我拍了不知道多久到", true},
		{"我买了不要今天发", true},
		{"", false},
		{"我想买五香", false},
		{"还没付款", false},
		{"已拍?", false},
		{"买了会不会不好吃", false},
		{"买了有破损我要投诉", false},
		{"昨天拍了", false},
		{"我朋友买了", false},
		{"已拍照", false},
		{"我还没付完钱", false},
		{"未结账", false},
		{"结完账了吗", false},
		{"结完账了又退款了", false},
		{"你付完钱了", false},
		{"朋友结完账", false},
		{"昨天付完款", false},
		{"如果结账了", false},
		{"付完钱就发货", false},
		{"结完账再说", false},
		{"已 买 了 有 破 损", false},
	}
	for _, typ := range []string{TypeOrder, TypeUrging, TypeComplai, "闲聊", "提问", "主动互动", ""} {
		for _, c := range cases {
			t.Run(typ+"/"+c.text, func(t *testing.T) {
				if got := Detect(typ, c.text); got != c.want {
					t.Fatalf("original=%q label=%q got=%t want=%t", c.text, typ, got, c.want)
				}
			})
		}
	}
}

func TestUnconfirmedLabelDoesNotConsumeConfirmedOrderRing(t *testing.T) {
	g := NewGuard()
	if d := g.Decide(TypeOrder, "我还没付款", "same-task"); d.IsOrder || d.Ring {
		t.Fatalf("unconfirmed intent consumed a ring: %+v", d)
	}
	if d := g.Decide(TypeUrging, "我已付款", "same-task"); !d.IsOrder || !d.Ring {
		t.Fatalf("confirmed original must still ring: %+v", d)
	}
}

func TestConfirmedProgramTemplateKeepsExistingRingGuard(t *testing.T) {
	g := NewGuard()
	if d := g.Decide(TypeOrder, "", "count-template"); d.IsOrder {
		t.Fatal("a label must not act as template proof")
	}
	if d := g.DecideConfirmed("", "count-template"); !d.IsOrder || !d.Ring {
		t.Fatalf("validated count template lost its ring: %+v", d)
	}
	if d := g.DecideConfirmed("", "count-template"); !d.IsOrder || d.Ring {
		t.Fatalf("validated template bypassed dedupe: %+v", d)
	}
	if d := g.Decide(TypeUrging, "已拍", "other-task"); !d.IsOrder || d.Ring {
		t.Fatalf("semantic and template paths must share the ring gap: %+v", d)
	}
}
