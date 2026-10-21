package decisionexecutor

import "testing"

func TestSafeNicknameAddressHardFilter(t *testing.T) {
	cases := []struct{ in, want string }{
		{"张三", "张三"},              // 正常昵称原样保留
		{"小明", "小明"},              // 正常昵称
		{"12345678", "这位朋友"},      // 纯数字 → 降级
		{"用户13800138000", "这位朋友"}, // 长数字串 → 降级
		{"加微信123", "这位朋友"},        // 引流词 → 降级
		{"扫码加好友", "这位朋友"},         // 引流词 → 降级
		{"滚", "这位朋友"},             // 侮辱词 → 降级
		{"张**", "张"},              // 掩码昵称 → 星号前前缀
		{"", ""},                  // 空 → 空
		{"不给你讲", "不给你讲"},          // 命令式（程序不覆盖，交给模型语义判断）
		{"爸爸", "这位朋友"},            // 亲属/占有关系词不能当称呼
		{"叫我爸爸", "这位朋友"},          // “叫我爸爸”是语义越界，不是称谓偏好
		{"老王", "老王"},              // 正常称呼保留
	}
	for _, c := range cases {
		if got := safeNicknameAddress(c.in); got != c.want {
			t.Errorf("safeNicknameAddress(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestNeutralAndPairedAddressTerms(t *testing.T) {
	for _, term := range []string{"哥姐", "哥哥姐姐", "叔叔阿姨", "兄弟姐妹", "朋友", "老乡"} {
		found := false
		for _, candidate := range neutralAddressTerms {
			if candidate == term {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("neutral address terms missing %q", term)
		}
	}
	cases := map[string]string{
		"哥":     "哥姐",
		"哥哥":    "哥哥姐姐",
		"姐":     "哥姐",
		"叔叔":    "叔叔阿姨",
		"阿姨":    "叔叔阿姨",
		"兄弟":    "兄弟姐妹",
		"哥姐":    "哥姐",
		"王哥":    "王哥",
		"哥哥姐姐们": "哥哥姐姐们",
	}
	for in, want := range cases {
		if got := normalizePairedAddressTerm(in); got != want {
			t.Fatalf("normalizePairedAddressTerm(%q)=%q want %q", in, got, want)
		}
	}
	pool := normalizeAddressPool([]string{"哥", "哥姐", "朋友", "叔叔"})
	if len(pool) != 3 || pool[0] != "哥姐" || pool[1] != "朋友" || pool[2] != "叔叔阿姨" {
		t.Fatalf("pool=%#v", pool)
	}
}

func TestSemanticAddressTermsBlockedEverywhere(t *testing.T) {
	for _, term := range []string{"爸爸", "妈妈", "老公", "老婆", "主人", "叫我爸爸"} {
		if !containsSemanticAddressTerm(term) {
			t.Fatalf("%q should be blocked", term)
		}
	}
	if containsSemanticAddressTerm("老王") || containsSemanticAddressTerm("哥姐") {
		t.Fatal("normal address terms must not be blocked")
	}
	cleaned := cleanAddressingTerms([]string{"爸爸", "朋友", "老王", "妈妈"})
	if len(cleaned) != 2 || cleaned[0] != "朋友" || cleaned[1] != "老王" {
		t.Fatalf("semantic terms not removed: %#v", cleaned)
	}
	if !addressingTermBlocked("爸爸", nil) {
		t.Fatal("semantic address term must be blocked")
	}
	if addressingTermBlocked("老王", nil) {
		t.Fatal("normal address term must not be blocked")
	}
}

func TestPromptNicknamesNeverEmpty(t *testing.T) {
	// 昵称为空 → 兜底"这位朋友"，保证所有互动都有可播称呼
	if got := (decisionItem{}).promptNicknames(); len(got) != 1 || got[0] != "这位朋友" {
		t.Fatalf("空昵称应兜底'这位朋友', got %v", got)
	}
	// 全是硬违规昵称 → 过滤后仍兜底
	item := decisionItem{Nicknames: []string{"12345678", "加微信123"}}
	if got := item.promptNicknames(); len(got) != 1 || got[0] != "这位朋友" {
		t.Fatalf("全违规昵称应兜底'这位朋友', got %v", got)
	}
	// 正常 + 违规混合 → 保留正常，违规降级代称（对应两个不同观众）
	item = decisionItem{Nicknames: []string{"张三", "12345678"}}
	if got := item.promptNicknames(); len(got) != 2 || got[0] != "张三" || got[1] != "这位朋友" {
		t.Fatalf("混合昵称应得 ['张三' '这位朋友'], got %v", got)
	}
	// 超过 3 个昵称 → 只保留前 3 个，其余交给群体称呼
	item = decisionItem{Nicknames: []string{"张三", "李四", "王五", "赵六", "钱七"}}
	if got := item.promptNicknames(); len(got) != 3 || got[0] != "张三" || got[1] != "李四" || got[2] != "王五" {
		t.Fatalf("超3个昵称应只保留前3个, got %v", got)
	}
}
