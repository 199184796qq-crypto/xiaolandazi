package decisionexecutor

import "testing"

func TestSafeNicknameAddressHardFilter(t *testing.T) {
	cases := []struct{ in, want string }{
		{"张三", "张三"},                 // 正常昵称原样保留
		{"小明", "小明"},                 // 正常昵称
		{"12345678", "这位朋友"},          // 纯数字 → 降级
		{"用户13800138000", "这位朋友"},    // 长数字串 → 降级
		{"加微信123", "这位朋友"},          // 引流词 → 降级
		{"扫码加好友", "这位朋友"},          // 引流词 → 降级
		{"滚", "这位朋友"},               // 侮辱词 → 降级
		{"张**", "张"},                   // 掩码昵称 → 星号前前缀
		{"", ""},                        // 空 → 空
		{"不给你讲", "不给你讲"},           // 命令式（程序不覆盖，交给模型语义判断）
	}
	for _, c := range cases {
		if got := safeNicknameAddress(c.in); got != c.want {
			t.Errorf("safeNicknameAddress(%q) = %q, want %q", c.in, got, c.want)
		}
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
