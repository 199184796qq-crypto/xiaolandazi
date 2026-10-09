package contentpools

import (
	"reflect"
	"testing"
)

func editFixture() Set {
	source := Sources{Products: []Product{{ID: 1, Link: "1"}, {ID: 2, Link: "2"}}, Rows: []SourceRow{{ID: "facts:1", Fields: map[string]string{"内容": "现卤现炸"}}}}
	pool := func(key string, owner int64, products []int64, id string) Pool {
		return Pool{Key: key, Owner: owner, Products: products, Fingerprint: key, Facts: []Fact{{ID: "f", Sources: []string{"facts:1"}, Products: products}}, Tasks: []Task{{ID: id, Point: "新鲜", Angle: id + "角度", PainPoint: "担心存货", TargetAudience: "买鸭子的人", Sources: []string{"f"}}}}
	}
	return Set{ID: 7, Sources: source, SourceSHA: source.SHA(), SafeFacts: "现卤现炸", Pools: []Pool{pool("common", 0, []int64{1, 2}, "shared"), pool("product:1", 1, []int64{1}, "a"), pool("product:2", 2, []int64{2}, "b")}}
}

func TestEditContentPoolScopeAndImmutableSnapshot(t *testing.T) {
	original := editFixture()
	before := Digest(original)
	edit := PoolEdit{ExpectedSetID: 7, PoolKey: "product:1", TaskIDs: []string{"a"}, Tasks: []TaskEdit{{ID: "a", Point: "塑价", Angle: "按只报价"}, {Point: "塑价", Angle: "换一个计算角度"}}}
	next, err := ApplyEdit(original, edit)
	if err != nil {
		t.Fatal(err)
	}
	if Digest(original) != before || !reflect.DeepEqual(original.Pools[0], next.Pools[0]) || !reflect.DeepEqual(original.Pools[2], next.Pools[2]) {
		t.Fatal("editing one product changed the original/common/other product snapshot")
	}
	if len(next.Pools[1].Tasks) != 2 || next.Pools[1].Tasks[0].Sources[0] != "f" || next.Pools[1].Tasks[1].Sources[0] != "f" || next.Pools[1].Tasks[1].ID == "" || next.Pools[1].Fingerprint != original.Pools[1].Fingerprint {
		t.Fatal("task identity, inheritance or future reuse lost", next.Pools[1])
	}
	for _, bad := range []PoolEdit{
		{ExpectedSetID: 6, PoolKey: "product:1", TaskIDs: []string{"a"}, Tasks: []TaskEdit{}},
		{ExpectedSetID: 7, PoolKey: "product:1", TaskIDs: []string{"b"}, Tasks: []TaskEdit{}},
		{ExpectedSetID: 7, PoolKey: "product:1", TaskIDs: []string{"a"}, Tasks: []TaskEdit{{ID: "b", Point: "错池", Angle: "角度"}}},
		{ExpectedSetID: 7, PoolKey: "product:1", TaskIDs: []string{"a"}, Tasks: []TaskEdit{{ID: "a", Point: "空内容", Angle: " "}}},
		{ExpectedSetID: 7, PoolKey: "product:1", TaskIDs: []string{"a", "a"}, Tasks: []TaskEdit{}},
		{ExpectedSetID: 7, PoolKey: "common", TaskIDs: []string{"shared"}},
	} {
		if _, err := ApplyEdit(original, bad); err == nil {
			t.Fatal("invalid edit accepted", bad)
		}
	}
	deleted, err := ApplyEdit(original, PoolEdit{ExpectedSetID: 7, PoolKey: "product:1", TaskIDs: []string{"a"}, Tasks: []TaskEdit{}})
	if err != nil || len(deleted.Pools[1].Tasks) != 0 || len(deleted.Pools[0].Tasks) != 1 {
		t.Fatal("delete affected other scope", err)
	}
}
