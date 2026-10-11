package contentguard

import "testing"

func TestContentFirstControlRemovalKeepsFacts(t *testing.T) {
	got, edit := StripLeadingProductControl("直接说1号链接900克，87.6元。", "直接说", []string{"1号链接"})
	if got != "1号链接900克，87.6元。" || edit == nil || edit.Removed != "直接说" {
		t.Fatal(got, edit)
	}
	for _, text := range []string{"直接说你喜欢什么口味？", "直接说2号链接450克。", "我就直接说，份量很实在。"} {
		got, edit := StripLeadingProductControl(text, "直接说", []string{"1号链接"})
		if got != text || edit != nil {
			t.Fatal("audience speech or unauthorized product changed", got)
		}
	}
}

func TestRequestControlDeletionPreservesAudienceSpeech(t *testing.T) {
	labels := []string{"本轮收口", "这轮收口"}
	cases := []struct {
		text, want string
		edits      int
	}{
		{"哥哥姐姐们，咱们今天这轮收口，重点就在这个1号链接。900g一整只。", "哥哥姐姐们，重点就在这个1号链接。900g一整只。", 1},
		{"900g一整只。第一句直接说售后保障。支持7天无理由退货。", "900g一整只。支持7天无理由退货。", 1},
		{"这件衣服收口设计方便活动。先看清规格再拍。", "这件衣服收口设计方便活动。先看清规格再拍。", 0},
		{"这个步骤要先说清楚。", "这个步骤要先说清楚。", 0},
	}
	for _, c := range cases {
		got, edits, e := StripRequestInstructions(c.text, []string{"第一句直接说售后保障"}, labels)
		if e != nil || got != c.want || len(edits) != c.edits {
			t.Fatalf("got=%q edits=%v err=%v", got, edits, e)
		}
	}
	if _, _, e := StripRequestInstructions("本轮收口。", nil, labels); e == nil {
		t.Fatal("empty body accepted")
	}
}
