package httpapi

import "testing"

func TestScriptHasTimeDrift(t *testing.T) {
	if hits := scriptHasTimeDrift("这会儿刚下班，晚饭还没吃"); len(hits) == 0 {
		t.Fatal("应检出“刚下班”")
	}
	if hits := scriptHasTimeDrift("这会儿鸭子刚出锅，趁热撕着吃"); len(hits) > 0 {
		t.Fatalf("中性说法不该报警: %v", hits)
	}
	if hits := scriptHasTimeDrift("下午三点直播间见"); len(hits) != 1 {
		t.Fatalf("应只命中一个时段词: %v", hits)
	}
}