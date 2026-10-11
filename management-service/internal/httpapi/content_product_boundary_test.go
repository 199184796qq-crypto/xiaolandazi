package httpapi

import (
	"encoding/json"
	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/contentpools"
	"strings"
	"testing"
)

func TestMinimumTaskIncludesProductBoundaryWithoutServicesOrSecondLevel(t *testing.T) {
	set := taskFactPoolFixture()
	set.Sources.Rows = append(set.Sources.Rows,
		contentpools.SourceRow{ID: "usage", Area: "knowledge", Fields: map[string]string{"分类": "product"}},
		contentpools.SourceRow{ID: "second-level", Area: "lecture", Fields: map[string]string{"分类": "product"}},
	)
	set.Pools[0].Facts = append(set.Pools[0].Facts,
		contentpools.Fact{ID: "usage", Text: "已经加工好的成品，开袋直接吃", Sources: []string{"usage"}, Products: []int64{7}},
		contentpools.Fact{ID: "raw-material", Text: "二级原料资料", Sources: []string{"second-level"}, Products: []int64{7}},
	)
	set.SourceSHA = set.Sources.SHA()
	tasks, _, e := contentPoolRuntime(set, anchorcycle.ProductPolicy{})
	if e != nil {
		t.Fatal(e)
	}
	wire, _ := json.Marshal(tasks[0].FactScope.ProductBoundary)
	if !strings.Contains(string(wire), "开袋直接吃") || strings.Contains(string(wire), "京东物流") || strings.Contains(string(wire), "二级原料") {
		t.Fatal(string(wire))
	}
	// Another category is handled by source roles, not duck-specific word checks.
	set.Pools[0].Facts[len(set.Pools[0].Facts)-2].Text = "布料成衣，按标签手洗，不可高温熨烫"
	tasks, _, e = contentPoolRuntime(set, anchorcycle.ProductPolicy{})
	wire, _ = json.Marshal(tasks[0].FactScope.ProductBoundary)
	if e != nil || !strings.Contains(string(wire), "不可高温熨烫") {
		t.Fatal(string(wire), e)
	}
	s := cycleTestState(t)
	s.Phase = "closing"
	c := anchorcycle.Candidate{Text: "哥哥姐姐们，咱们今天这轮收口，重点就在这个1号链接。900g一整只。"}
	if !cleanCycleOpeningInstruction(&s, &c, "") || strings.Contains(c.OutputText, "收口") || !strings.Contains(c.Text, "收口") {
		t.Fatal(c)
	}
}
