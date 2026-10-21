package decisionexecutor

import (
	"context"
	"strings"
	"testing"

	"livecompanion/management/internal/model"
)

func TestClassifyInteraction(t *testing.T) {
	cases := []struct {
		name string
		kind string
		want interactionTier
	}{
		{"回答问题", "reply_question", tierFactAnswer},
		{"投诉", "complaint", tierFactAnswer},
		{"回应关注", "reply_follow", tierAcknowledge},
		{"回应点赞", "reply_like", tierAcknowledge},
		{"点名欢迎", "welcome_named", tierAcknowledge},
		{"打包欢迎", "welcome_batch", tierAcknowledge},
		{"下单回应", "conversion_signal", tierAcknowledge},
		{"暖场", "warmup", tierAcknowledge},
		{"回复弹幕", "reply_chat", tierChat},
		{"空 kind 兜底", "", tierFactAnswer},
		{"未知 kind 兜底", "unknown_kind", tierFactAnswer},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := classifyInteraction(decisionItem{MissionKind: tc.kind})
			if got != tc.want {
				t.Fatalf("classifyInteraction(%q) = %d，期望 %d", tc.kind, got, tc.want)
			}
		})
	}
}

func TestClassifyChat(t *testing.T) {
	// 有内容的弹幕（骨架阶段）归第一档；无内容归第二档。
	if got := classifyChat(decisionItem{SampleQuestions: []string{"这个怎么发货"}}); got != tierFactAnswer {
		t.Fatalf("有内容弹幕应归第一档，实际 %d", got)
	}
	if got := classifyChat(decisionItem{}); got != tierAcknowledge {
		t.Fatalf("无内容弹幕应归第二档，实际 %d", got)
	}
}

// TestDispatchLightInteraction 验证按 mission_kind 分派到三个独立策略：
// 关注/点赞 → likeFollowStrategy，欢迎 → welcomeStrategy，下单 → orderStrategy。
func TestDispatchLightInteraction(t *testing.T) {
	w := &Worker{}
	ctx := context.Background()
	session := model.LiveRuntimeSession{}

	cases := []struct {
		name    string
		kind    string
		wantSub string
	}{
		{"关注走关注策略", "reply_follow", "关注"},
		{"点赞走关注策略", "reply_like", "点赞"},
		{"点名欢迎走欢迎策略", "welcome_named", "欢迎"},
		{"打包欢迎走欢迎策略", "welcome_batch", "欢迎"},
		{"暖场走欢迎策略", "warmup", "欢迎"},
		{"下单走下单策略", "conversion_signal", "安排"},
		{"未知轻互动兜底走关注策略", "unknown_light", "关注"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			item := decisionItem{MissionKind: tc.kind}
			got := w.dispatchLightInteraction(ctx, session, item)
			if strings.TrimSpace(got) == "" {
				t.Fatalf("分派返回空文字")
			}
			if tc.wantSub != "" && !strings.Contains(got, tc.wantSub) {
				t.Fatalf("期望输出包含 %q，实际：%q", tc.wantSub, got)
			}
		})
	}
}

func TestLightweightInteractionText(t *testing.T) {
	w := &Worker{}
	ctx := context.Background()
	session := model.LiveRuntimeSession{}
	cases := []struct {
		name     string
		kind     string
		question string
		wantSub  string
	}{
		{"欢迎", "welcome_named", "", "欢迎"},
		{"点赞关注", "reply_follow", "", "关注"},
		{"下单", "conversion_signal", "", "安排"},
		{"暖场", "warmup", "", "欢迎"},
		{"投诉", "complaint", "", "处理"},
		{"闲聊带原话", "reply_chat", "主播今天几点下播", "几点下播"},
		{"商品提问带原话", "reply_question", "这个油多少钱", "多少钱"},
		{"未知场景带原话", "unknown_kind", "随便问问", "随便问问"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			item := decisionItem{MissionKind: tc.kind, SampleQuestions: []string{tc.question}}
			got := w.lightweightInteractionText(ctx, session, item)
			if strings.TrimSpace(got) == "" {
				t.Fatalf("轻量策略机返回空文字")
			}
			if tc.wantSub != "" && !strings.Contains(got, tc.wantSub) {
				t.Fatalf("期望输出包含 %q，实际：%q", tc.wantSub, got)
			}
		})
	}
}

// TestLightInteractionStrategies 验证三个独立策略各自的行为差异。
func TestLightInteractionStrategies(t *testing.T) {
	ctx := context.Background()
	session := model.LiveRuntimeSession{}

	t.Run("关注策略点昵称带感谢", func(t *testing.T) {
		s := likeFollowStrategy{}
		got := s.Respond(ctx, session, decisionItem{MissionKind: "reply_follow", Nicknames: []string{"阿芳"}})
		if !strings.Contains(got, "阿芳") || !strings.Contains(got, "关注") {
			t.Fatalf("关注策略应点昵称+感谢，实际：%q", got)
		}
	})

	t.Run("欢迎策略点昵称带欢迎", func(t *testing.T) {
		s := welcomeStrategy{}
		got := s.Respond(ctx, session, decisionItem{MissionKind: "welcome_named", Nicknames: []string{"小陈"}})
		if !strings.Contains(got, "小陈") || !strings.Contains(got, "欢迎") {
			t.Fatalf("欢迎策略应点昵称+欢迎，实际：%q", got)
		}
	})

	t.Run("下单策略点昵称带承接", func(t *testing.T) {
		s := orderStrategy{}
		got := s.Respond(ctx, session, decisionItem{MissionKind: "conversion_signal", Nicknames: []string{"刘老板"}})
		if !strings.Contains(got, "刘老板") || !strings.Contains(got, "安排") {
			t.Fatalf("下单策略应点昵称+承接，实际：%q", got)
		}
	})

	t.Run("无昵称关注用群称", func(t *testing.T) {
		s := likeFollowStrategy{}
		got := s.Respond(ctx, session, decisionItem{MissionKind: "reply_follow"})
		if !strings.Contains(got, "关注") {
			t.Fatalf("无昵称关注应给群称感谢，实际：%q", got)
		}
	})
}

// TestInteractionStrategyInterface 验证「轻互动策略」是统一接口：
// 三个策略都实现 InteractionStrategy，换实现时调用点零改动。
func TestInteractionStrategyInterface(t *testing.T) {
	ctx := context.Background()
	session := model.LiveRuntimeSession{}
	item := decisionItem{MissionKind: "reply_follow", Nicknames: []string{"小陈"}}

	var strategies = []InteractionStrategy{
		likeFollowStrategy{},
		welcomeStrategy{},
		orderStrategy{},
	}
	for _, s := range strategies {
		if got := s.Respond(ctx, session, item); strings.TrimSpace(got) == "" {
			t.Fatalf("策略 %T 返回空文字", s)
		}
	}

	// 通过接口统一调用，模拟将来换模型实现。
	fake := fakeInteractionStrategy{}
	var r InteractionStrategy = fake
	if got := r.Respond(ctx, session, item); got != "FAKE:谢谢小陈" {
		t.Fatalf("接口调用 fake 实现失败，实际：%q", got)
	}
}

// fakeInteractionStrategy 模拟将来的模型实现，用于验证接口可替换性。
type fakeInteractionStrategy struct{}

func (fakeInteractionStrategy) Respond(ctx context.Context, session model.LiveRuntimeSession, item decisionItem) string {
	if len(item.Nicknames) > 0 {
		return "FAKE:谢谢" + item.Nicknames[0]
	}
	return "FAKE:感谢支持"
}

// TestResolveInteractionAddressingMultiPersonWelcome 验证「多人欢迎」时称呼策略输出的完整名单：
//   - resolvedAddressingPlan.SelectedNames 单个=1 个、多个=2~3 个，名单不丢、不压平；
//   - addressingGuidance 里名单用顿号串好（模型策略用）；
//   - resolvedAddress 是单个便捷值（骨架模板用），不代表完整名单。
func TestResolveInteractionAddressingMultiPersonWelcome(t *testing.T) {
	ctx := context.Background()
	session := model.LiveRuntimeSession{TenantID: 7, RoomID: 11}
	worker := New(readyVoiceStore(), nil, &fakeAgent{}, nil)

	t.Run("4个人欢迎保留完整名单", func(t *testing.T) {
		// 4 个昵称，预设 PromptNicknames 使 planNicknameAddresses 直接复用（不走模型）。
		item := decisionItem{
			MissionKind:       "welcome_batch",
			MissionEventCount: 4,
			Nicknames:         []string{"小陈", "阿芳", "老周", "刘老板"},
			PromptNicknames:   []string{"小陈", "阿芳", "老周", "刘老板"},
		}
		address, guidance := worker.resolveInteractionAddressing(ctx, session, &item)

		plan := item.resolvedAddressingPlan
		if len(plan.SelectedNames) < 2 || len(plan.SelectedNames) > 3 {
			t.Fatalf("多人欢迎应保留 2~3 个点名名单，实际 %d 个：%v", len(plan.SelectedNames), plan.SelectedNames)
		}
		// 名单必须完整保留前几个（不能只剩 1 个）。
		if plan.SelectedNames[0] != "小陈" || plan.SelectedNames[1] != "阿芳" {
			t.Fatalf("名单丢失，实际：%v", plan.SelectedNames)
		}
		// guidance 里名单用顿号串好。
		if !strings.Contains(guidance, "小陈") || !strings.Contains(guidance, "阿芳") {
			t.Fatalf("guidance 缺少名单，实际：%s", guidance)
		}
		// resolvedAddress 是单个便捷值，不等于完整名单。
		if address == "" {
			t.Fatalf("resolvedAddress 不应为空")
		}
		// Mode 应是多人模式之一（MIXED/SAMPLE）。
		if plan.Mode != "MIXED" && plan.Mode != "SAMPLE" {
			t.Fatalf("多人欢迎 Mode 应为 MIXED/SAMPLE，实际：%s", plan.Mode)
		}
	})

	t.Run("单个人欢迎只输出1个", func(t *testing.T) {
		item := decisionItem{
			MissionKind:       "welcome_named",
			MissionEventCount: 1,
			Nicknames:         []string{"小陈"},
			PromptNicknames:   []string{"小陈"},
		}
		_, guidance := worker.resolveInteractionAddressing(ctx, session, &item)

		plan := item.resolvedAddressingPlan
		if len(plan.SelectedNames) != 1 {
			t.Fatalf("单人欢迎应恰好 1 个名单，实际 %d 个：%v", len(plan.SelectedNames), plan.SelectedNames)
		}
		if plan.SelectedNames[0] != "小陈" {
			t.Fatalf("单人名单错误，实际：%v", plan.SelectedNames)
		}
		_ = guidance
	})
}

func TestPickTopBenefits(t *testing.T) {
	t.Run("价格对比优先于优惠券", func(t *testing.T) {
		// 3 条以上才触发分桶：价格对比桶应排在优惠桶之前。
		benefits := []welcomeBenefit{
			{Type: "活动优惠", Content: "领30元券每桌减30"},
			{Type: "团购", Content: "10人餐团购价337原价547省210"},
			{Type: "其他", Content: "环境宽敞"},
		}
		got := pickTopBenefits(benefits)
		if len(got) != 2 {
			t.Fatalf("期望 2 条，实际 %d", len(got))
		}
		// 价格对比应在第 1 位（优先桶）。
		if !benefitHasPriceContrast(got[0].Content) {
			t.Fatalf("第 1 条应是价格对比，实际 %q", got[0].Content)
		}
	})

	t.Run("一堆福利跨桶轮换只取2个", func(t *testing.T) {
		benefits := []welcomeBenefit{
			{Content: "地址：成都武侯区"},
			{Content: "领30元券"},
			{Content: "10人餐团购价337原价547省210"},
			{Content: "6人餐团购价187原价245省58"},
			{Content: "赠品：小礼品一份"},
		}
		got := pickTopBenefits(benefits)
		if len(got) != 2 {
			t.Fatalf("期望 2 条，实际 %d：%+v", len(got), got)
		}
		// 跨桶轮换：第 1 条来自价格桶（优先），第 2 条应来自优惠桶，
		// 而不是价格桶占满 2 个名额（否则优惠券永远排不上）。
		if !benefitHasPriceContrast(got[0].Content) {
			t.Fatalf("第 1 条应是价格对比，实际 %q", got[0].Content)
		}
		if !benefitHasPromoKeyword(got[1].Content) {
			t.Fatalf("第 2 条应来自优惠桶（跨桶轮换），实际 %q", got[1].Content)
		}
	})

	t.Run("少于等于2个原样返回", func(t *testing.T) {
		benefits := []welcomeBenefit{{Content: "10人餐团购价337原价547省210"}}
		got := pickTopBenefits(benefits)
		if len(got) != 1 {
			t.Fatalf("期望 1 条，实际 %d", len(got))
		}
	})

	t.Run("兜底随机：只有非价格非优惠时随机取不固定", func(t *testing.T) {
		// 3 条纯兜底福利，取 2 条。随机取样意味着结果不应永远等于前 2 个。
		benefits := []welcomeBenefit{
			{Content: "口味：麻辣鲜香"},
			{Content: "环境：宽敞明亮"},
			{Content: "服务：热情周到"},
		}
		saw := map[string]bool{}
		for i := 0; i < 50; i++ {
			got := pickTopBenefits(benefits)
			if len(got) != 2 {
				t.Fatalf("期望 2 条，实际 %d", len(got))
			}
			saw[got[0].Content] = true
		}
		// 50 次里至少出现 2 种不同的首条，说明兜底确实随机（而非固定第 1 个）。
		if len(saw) < 2 {
			t.Fatalf("兜底应是随机取样，但 50 次首条始终固定：%v", saw)
		}
	})
}
