package httpapi

import (
	"strings"
	"testing"
)

// 验证品相关剥离：数字/链接号/价格/商品名剥掉，序号与风格词保留。
func TestStripAnchorStyleProductContent(t *testing.T) {
	cases := []struct {
		name         string
		before       string
		productNames []string
		wantGone     []string
		wantKeep     []string
	}{
		{
			name:         "硬参数剥离且序号不误伤",
			before:       "1. 会提及重量（9斤2两）、价格（69块9）、时间（5年、80多万单）、库存（最后6桶）。2. 反复强调1号链接、2号链接的黄菜籽油。",
			productNames: []string{"黄菜籽油", "菜籽油"},
			wantGone:     []string{"9斤2两", "69块9", "5年", "80多万单", "6桶", "1号链接", "2号链接", "黄菜籽油"},
			wantKeep:     []string{"1.", "2.", "重量", "价格", "时间", "库存"},
		},
		{
			name:         "口语语气词保留",
			before:       "语气词：哟、啊、嘛；称呼：哥哥姐姐们、叔叔阿姨们、老乡。",
			productNames: []string{"菜籽油"},
			wantGone:     []string{"菜籽油"},
			wantKeep:     []string{"哟", "啊", "嘛", "哥哥姐姐们", "叔叔阿姨们", "老乡"},
		},
		{
			name:         "候选词表剥离品相关词",
			before:       "",
			productNames: []string{"菜籽油", "鸭子"},
			wantGone:     nil,
			wantKeep:     nil,
		},
	}

	for _, c := range cases {
		after, _ := stripAnchorStyleProductContent(c.before, anchorStyleWorkbenchLexicon{}, c.productNames)
		for _, g := range c.wantGone {
			if strings.Contains(after, g) {
				t.Errorf("[%s] 剥离后仍残留品相关: %q\n结果: %s", c.name, g, after)
			}
		}
		for _, k := range c.wantKeep {
			if !strings.Contains(after, k) {
				t.Errorf("[%s] 误伤了风格词: %q\n结果: %s", c.name, k, after)
			}
		}
	}

	// 候选词表剥离：含链接号(正则)或商品名(词表)的候选词应被剔除，风格词保留
	lex := anchorStyleWorkbenchLexicon{
		Catchphrases: []string{"哥哥姐姐们", "1号链接", "黄菜籽油"},
	}
	_, lex2 := stripAnchorStyleProductContent("", lex, []string{"黄菜籽油", "菜籽油"})
	if len(lex2.Catchphrases) != 1 || lex2.Catchphrases[0] != "哥哥姐姐们" {
		t.Fatalf("候选词剥离错误: %v", lex2.Catchphrases)
	}
}
