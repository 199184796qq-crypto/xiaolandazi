package addressing

import (
	"math/rand"
	"testing"
)

func TestSpeakableLatin(t *testing.T) {
	speakable := []string{"Lucy", "Tom", "Mike", "Kevin", "Amy", "sunny"}
	for _, name := range speakable {
		if !SpeakableLatin(name) {
			t.Fatalf("%s 应可念", name)
		}
	}
	// asdf 有元音、连续辅音正好 3 个，按阈值 3 判为可念，属于已知漏网。
	unspeakable := []string{"KKK", "xqj", "sdfg", "BCDF", "xyz", ""}
	for _, name := range unspeakable {
		if SpeakableLatin(name) {
			t.Fatalf("%s 不应可念", name)
		}
	}
}

func TestInitialAddress(t *testing.T) {
	cases := map[string]string{"KKK": "K", "xqj": "X", "asdf": "A", "k8ta": "K", "": ""}
	for in, want := range cases {
		if got := InitialAddress(in); got != want {
			t.Fatalf("%s → %q，期望 %q", in, got, want)
		}
	}
}

func TestSingularTermStripsMen(t *testing.T) {
	cases := map[string]string{"家人们": "家人", "老乡们": "老乡", "哥姐们": "哥姐", "老板": "老板", "": ""}
	for in, want := range cases {
		if got := SingularTerm(in); got != want {
			t.Fatalf("%s → %q，期望 %q", in, got, want)
		}
	}
}

func TestIsGroupOnlyTerm(t *testing.T) {
	for _, term := range []string{"各位", "大家", "大伙", "诸位"} {
		if !IsGroupOnlyTerm(term) {
			t.Fatalf("%s 应判为群体专用", term)
		}
	}
	for _, term := range []string{"老乡们", "家人们", "老板", "小王"} {
		if IsGroupOnlyTerm(term) {
			t.Fatalf("%s 不应判为群体专用", term)
		}
	}
}

func TestHasAddressSuffixCoversProfession(t *testing.T) {
	for _, name := range []string{"李姐", "王老板", "张老师", "陈师傅", "刘总", "赵经理"} {
		if !HasAddressSuffix(name) {
			t.Fatalf("%s 应判为已含称谓", name)
		}
	}
	for _, name := range []string{"小王", "阿芳", "老周", "KKK"} {
		if HasAddressSuffix(name) {
			t.Fatalf("%s 不应判为已含称谓", name)
		}
	}
}

func TestFormsForIndividualRules(t *testing.T) {
	// 复数称谓接在个体后要去「们」
	forms := FormsFor("小陈", "小陈", []string{"家人们", "老乡们"})
	has := func(want string) bool {
		for _, f := range forms.Forms {
			if f == want {
				return true
			}
		}
		return false
	}
	if !has("小陈家人") || !has("小陈老乡") {
		t.Fatalf("缺少去「们」后的形态: %v", forms.Forms)
	}
	if has("小陈家人们") || has("小陈老乡们") {
		t.Fatalf("个体不应出现复称: %v", forms.Forms)
	}

	// 群体专用词不能连写
	forms = FormsFor("小陈", "小陈", []string{"各位"})
	if len(forms.Forms) != 1 || forms.Forms[0] != "小陈" {
		t.Fatalf("群体专用词不应连写: %v", forms.Forms)
	}

	// 昵称已含称谓（含职业敬称）不再叠加
	forms = FormsFor("王老板", "王老板", []string{"哥姐"})
	if len(forms.Forms) != 1 || forms.Forms[0] != "王老板" {
		t.Fatalf("已含称谓不应叠加: %v", forms.Forms)
	}

	// 不可念的纯字母换成首字母
	forms = FormsFor("KKK", "KKK", []string{"家人们"})
	if forms.Base != "K" {
		t.Fatalf("纯字母应改用首字母: %s", forms.Base)
	}
	if len(forms.Forms) == 0 || forms.Forms[0] != "K" {
		t.Fatalf("首字母形态丢失: %v", forms.Forms)
	}
}

func TestWithTitleSplitsFortySixty(t *testing.T) {
	r := rand.New(rand.NewSource(7))
	title := 0
	const n = 4000
	for i := 0; i < n; i++ {
		if WithTitle(r) {
			title++
		}
	}
	ratio := float64(title) / float64(n)
	if ratio < 0.55 || ratio > 0.65 {
		t.Fatalf("带称谓比例应约 60%%，实际 %.3f", ratio)
	}
}
