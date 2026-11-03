package httpapi

import "testing"

func TestOpeningSimilarityDetectsSamePattern(t *testing.T) {
	same := []string{
		"朋友们，欢迎来到绸都杨鸭子直播间",
		"朋友们，欢迎来到咱们绸都杨鸭子",
		"朋友们好，欢迎进到绸都杨鸭子",
	}
	diff := "线下门店一只算上运费要一百多"
	for _, s := range same {
		if got := openingSimilarity(same[0], s); got < 0.5 {
			t.Fatalf("同样开场应判为相似，实际 %.2f: %s", got, s)
		}
	}
	if got := openingSimilarity(same[0], diff); got >= 0.5 {
		t.Fatalf("不同开场不该判为相似: %.2f", got)
	}
}