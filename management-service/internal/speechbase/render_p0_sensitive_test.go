package speechbase

import (
	"encoding/json"
	"strings"
	"testing"

	"livecompanion/management/internal/interactionpolicy"
)

// TestP0ViewOmitsPoliticalReligiousSensitiveWords 防止政治/宗教敏感词再次进入
// 「有效P0」视图。这些词作为规则清单塞进喂给模型的 system prompt 时，会触发
// 阿里 dashscope 的输入内容安全审查（HTTP 400 data_inspection_failed），导致
// 正文生成反复失败而无声（session311 根因）。规则本身不得以字面敏感词形式
// 出现在输入里。
func TestP0ViewOmitsPoliticalReligiousSensitiveWords(t *testing.T) {
	md, err := interactionpolicy.Read("compliance/L1/p0-hard-block.md")
	if err != nil {
		t.Fatal(err)
	}
	view := GenerationP0View(string(md), "ordinary_food")
	raw, _ := json.Marshal(view)
	sensitive := []string{
		"辱华", "辱国", "反党", "反政府", "反华", "台独", "港独", "藏独", "疆独",
		"分裂国家", "颠覆国家", "法轮功", "邪教", "汉奸", "卖国", "叛国", "打倒",
		"推翻政权", "污蔑领袖", "诋毁政府", "攻击国家",
		"宗教", "信仰", "传教", "洗脑", "信教", "皈依", "上帝", "佛祖", "菩萨",
		"耶稣", "基督", "真主", "安拉", "伊斯兰", "佛教", "基督教", "道教",
		"天主教", "清真", "阿门", "阿弥陀佛", "诵经", "祈祷", "超度", "开光",
		"符咒", "算命", "风水", "跳大神", "拜神", "烧香拜佛",
	}
	for _, w := range sensitive {
		if strings.Contains(string(raw), w) {
			t.Errorf("有效P0视图不应包含敏感词 %q", w)
		}
	}
}
