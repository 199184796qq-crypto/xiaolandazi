package orderbell

import (
	"testing"
	"time"
)

func TestDetect(t *testing.T) {
	if !Detect("下单意向", "") {
		t.Fatal("下单意向应算下单")
	}
	// 催单里夹着「我拍了」，也算真下单。
	if !Detect("催单", "我拍了五香，多久发") {
		t.Fatal("催单里带下单证据应算下单")
	}
	for _, c := range []struct{ typ, text string }{
		{"催单", "什么时候发货"},
		{"投诉", "等了三天没发"},
		{"提问", "多少钱"},
		{"闲聊", "主播声音好听"},
		{"", ""},
	} {
		if Detect(c.typ, c.text) {
			t.Fatalf("%s/%s 不该算下单", c.typ, c.text)
		}
	}
	if Detect("提问", "我想买五香") {
		t.Fatal("想买不是已下单")
	}
	if Detect("提问", "我拍了照片") {
		t.Fatal("拍照不是下单")
	}
}

func TestFlavorAndPhrase(t *testing.T) {
	cases := map[string]string{
		"拍了五香":   "五香",
		"我要两斤麻辣的": "麻辣",
		"拍了":     "",
		"你好":     "",
	}
	for text, want := range cases {
		if got := Flavor(text); got != want {
			t.Fatalf("%s 口味=%q 期望 %q", text, got, want)
		}
	}
	if got := Phrase("五香", ""); got != "收到，五香，感谢支持。" {
		t.Fatal(got)
	}
	if got := Phrase("", ""); got != "收到订单，感谢支持。" {
		t.Fatal(got)
	}
}

func TestGuardDedupeAndRingGap(t *testing.T) {
	g := NewGuard()
	now := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	g.now = func() time.Time { return now }

	first := g.Decide("下单意向", "拍了五香", "u1")
	if !first.IsOrder || !first.Ring {
		t.Fatalf("首次应打铃: %+v", first)
	}

	// 同一用户同一口味，5 分钟内不再打铃。
	now = now.Add(2 * time.Second)
	second := g.Decide("下单意向", "拍了五香", "u1")
	if !second.IsOrder || second.Ring {
		t.Fatalf("重复下单不该再打铃: %+v", second)
	}

	// 不同用户，但距离上次打铃不到 8 秒，不打铃。
	now = now.Add(time.Second)
	third := g.Decide("下单意向", "我也拍了五香", "u2")
	if third.Ring {
		t.Fatalf("全局间隔内不该打铃: %+v", third)
	}

	// 过了全局间隔，可以再打。
	now = now.Add(12 * time.Second)
	fourth := g.Decide("下单意向", "拍了五香", "u3")
	if !fourth.Ring {
		t.Fatalf("过了间隔应打铃: %+v", fourth)
	}

	// 催单不打铃。
	if d := g.Decide("催单", "我的五香多久发", "u9"); d.IsOrder {
		t.Fatalf("催单不该算下单: %+v", d)
	}
}
