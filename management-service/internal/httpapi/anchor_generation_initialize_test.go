package httpapi

import (
	"errors"
	"strings"
	"testing"
)

func TestAnchorGenerationInitializationNoticeKeepsIndependentSuccesses(t *testing.T) {
	steps := []anchorGenerationInitializationStep{
		anchorGenerationStep("anchor_style", "主播风格", "主播风格已关联保存", errors.New("未绑定主播风格")),
		anchorGenerationStep("scorecard", "评分基准", "评分基准已关联保存", errors.New("等待主播风格")),
		anchorGenerationStep("fact_package", "事实包", "事实包已独立刷新", nil),
		anchorGenerationStep("selling_point_wheel", "卖点角度", "已生成 4 个卖点、12 个角度组合", nil),
	}
	initialized, partial, notice := anchorGenerationInitializationNotice(steps)
	if initialized {
		t.Fatal("partial success must not report full initialization")
	}
	if !partial {
		t.Fatal("independent successes must be preserved as a partial result")
	}
	if !strings.Contains(notice, "成功 2 项，失败 2 项") {
		t.Fatalf("unexpected notice: %s", notice)
	}
	if steps[2].Status != "success" || steps[3].Status != "success" {
		t.Fatalf("later steps must remain successful: %#v", steps)
	}
}

func TestAnchorGenerationInitializationNoticeAllSuccess(t *testing.T) {
	steps := []anchorGenerationInitializationStep{
		anchorGenerationStep("anchor_style", "主播风格", "ok", nil),
		anchorGenerationStep("scorecard", "评分基准", "ok", nil),
		anchorGenerationStep("fact_package", "事实包", "ok", nil),
		anchorGenerationStep("selling_point_wheel", "卖点角度", "ok", nil),
	}
	initialized, partial, notice := anchorGenerationInitializationNotice(steps)
	if !initialized || partial || notice != "四项初始化均已完成。" {
		t.Fatalf("unexpected summary: initialized=%v partial=%v notice=%q", initialized, partial, notice)
	}
}

// 方案不绑定主播风格是合法配置：风格与评分基准两项跳过，其余照常成功，
// 整体必须判为「已完成」，不能显示成失败。
func TestAnchorGenerationInitializationNoticeTreatsSkippedStyleAsComplete(t *testing.T) {
	steps := []anchorGenerationInitializationStep{
		anchorGenerationSkippedStep("anchor_style", "主播风格", "本方案没有绑定主播风格"),
		anchorGenerationSkippedStep("scorecard", "评分基准", "没有主播风格就没有评分基准"),
		anchorGenerationStep("fact_package", "事实包", "事实包已独立刷新", nil),
		anchorGenerationStep("selling_point_wheel", "卖点角度", "已生成 4 个卖点、12 个角度组合", nil),
	}
	initialized, partial, notice := anchorGenerationInitializationNotice(steps)
	if !initialized || partial {
		t.Fatalf("skipped style must still count as complete: initialized=%v partial=%v", initialized, partial)
	}
	if !strings.Contains(notice, "按当前方案跳过") || strings.Contains(notice, "失败") {
		t.Fatalf("skipped steps must not be reported as failures: %s", notice)
	}
	if steps[0].Status != "skipped" || steps[1].Status != "skipped" {
		t.Fatalf("style steps must be marked skipped: %#v", steps)
	}
}

// 真失败（读库错误等）仍要如实报失败。
func TestAnchorGenerationInitializationNoticeStillReportsRealFailure(t *testing.T) {
	steps := []anchorGenerationInitializationStep{
		anchorGenerationSkippedStep("anchor_style", "主播风格", "未绑定"),
		anchorGenerationSkippedStep("scorecard", "评分基准", "无基准"),
		anchorGenerationStep("fact_package", "事实包", "ok", errors.New("读取事实失败")),
		anchorGenerationStep("selling_point_wheel", "卖点角度", "ok", nil),
	}
	initialized, partial, notice := anchorGenerationInitializationNotice(steps)
	if initialized || !partial {
		t.Fatalf("real failure must not be hidden: initialized=%v partial=%v", initialized, partial)
	}
	if !strings.Contains(notice, "失败 1 项") {
		t.Fatalf("unexpected notice: %s", notice)
	}
}
