package decisionexecutor

import "testing"

func TestInteractionExpressionVariantRotates(t *testing.T) {
	first := interactionExpressionVariant(0)
	if first == "" {
		t.Fatal("变体不应为空")
	}
	for i := 1; i < len(interactionExpressionVariants); i++ {
		if interactionExpressionVariant(i) == "" {
			t.Fatalf("第 %d 个变体不应为空", i)
		}
	}
	// 轮换一圈后回到第一个
	if interactionExpressionVariant(len(interactionExpressionVariants)) != first {
		t.Fatal("轮换一圈后应回到第一个变体")
	}
	// 负数回退到 0
	if interactionExpressionVariant(-5) != first {
		t.Fatal("负数应回退到第一个变体")
	}
}
