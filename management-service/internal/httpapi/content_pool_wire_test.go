package httpapi

import (
	"reflect"
	"testing"

	"livecompanion/management/internal/contentpools"
)

func TestContentPoolWireRestoresOnlyExplicitReferences(t *testing.T) {
	p := contentpools.Pool{Key: "public", Facts: []contentpools.Fact{{ID: "knowledge:13", Text: "抽真空6天，不抽真空3天", Sources: []string{"knowledge:13"}}, {ID: "shared-f7c88a2f02b857e9f4f377fd", Text: "两种包装", Sources: []string{"attributes:4", "attributes:6"}}}, Tasks: []contentpools.Task{{ID: "saved", Point: "包装", Angle: "保持旧正文", Sources: []string{"knowledge:13"}}}}
	w := newContentPoolWire(p)
	if w.pool.Facts[0].ID != "1" || w.pool.Tasks[0].Sources[0] != "1" || p.Facts[0].ID != "knowledge:13" || p.Tasks[0].Sources[0] != "knowledge:13" || len(p.Facts[1].Sources) != 2 {
		t.Fatal("wire conversion mutated saved provenance")
	}
	for _, refs := range []string{`["1","2"]`, `[1,2]`, `["1",2]`} {
		raw := `{"tasks":[{"id":"saved","selling_point":"包装","angle":"条件不改","source_ids":` + refs + `}]}`
		tasks, err := decodePoolTaskResponse(w.response(raw, true), p, p.Tasks)
		if err != nil || len(tasks) != 1 || tasks[0].ID != "saved" || !reflect.DeepEqual(tasks[0].Sources, []string{"knowledge:13", "shared-f7c88a2f02b857e9f4f377fd"}) {
			t.Fatal("explicit alias did not restore source identity", tasks, err)
		}
	}
	for _, refs := range []string{`[0]`, `[3]`, `["other-pool:1"]`, `[]`, `[1,1]`} {
		raw := `{"tasks":[{"selling_point":"包装","angle":"不能猜测引用","source_ids":` + refs + `}]}`
		if _, err := decodePoolTaskResponse(w.response(raw, true), p, nil); err == nil {
			t.Fatal("invalid/cross-pool/duplicate reference accepted", refs)
		}
	}
	old := `{"tasks":[{"id":"saved","selling_point":"包装","angle":"不改文案","source_ids":["knowledge:13","shared-f7c88a2f02b857e9f4f377fd"]}]}`
	restored, err := decodePoolTaskResponse(w.response(w.response(old, false), true), p, p.Tasks)
	if err != nil || len(restored) != 1 || restored[0].Angle != "不改文案" {
		t.Fatal("round trip changed content", restored, err)
	}
}
