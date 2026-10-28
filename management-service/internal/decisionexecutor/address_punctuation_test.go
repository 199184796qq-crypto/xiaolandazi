package decisionexecutor

import "testing"

func TestEnsureAddressPunctuation(t *testing.T) {
	cases := []struct{ text, address, want string }{
		{"可爱噜健康一生老乡们来了", "可爱噜健康一生", "可爱噜健康一生，老乡们来了"},
		{"小陈，你们来啦", "小陈", "小陈，你们来啦"},
		{"欢迎小陈", "小陈", "欢迎小陈"},
		{"", "小陈", ""},
	}
	for _, tc := range cases {
		if got := ensureAddressPunctuation(tc.text, tc.address); got != tc.want {
			t.Fatalf("punctuate(%q,%q)=%q want %q", tc.text, tc.address, got, tc.want)
		}
	}
}
