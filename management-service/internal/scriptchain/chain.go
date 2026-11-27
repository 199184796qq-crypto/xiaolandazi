// Package scriptchain 负责话术改写链路里的"逻辑链"：
// 把原始话术交给分析模型拆成若干条逻辑链，缓存起来，生成时随机取一条。
package scriptchain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/rand"
	"slices"
	"strings"
)

// Chain 是分析模型给出的一条逻辑链。
type Chain struct {
	Name   string   `json:"名称"`
	Nodes  []string `json:"段序"`
	Reason string   `json:"理由"`
}

// Analysis 是分析模型对一条原始话术的完整返回。
type Analysis struct {
	Opening string  `json:"开场方式"`
	Chains  []Chain `json:"逻辑链"`
	Closing string  `json:"收口逻辑"`
	Style   string  `json:"语言风格"`
}

const minChainNodes = 3

func DefaultRequirement(target int) string {
	return RenderRequirement(Analysis{
		Opening: "从观众正在关心的购买需求切入",
		Closing: "概括选择依据，邀请观众按需下单",
		Style:   "短句、口语、自然衔接",
	}, Chain{Name: "通用顺序", Nodes: []string{"购买需求", "商品介绍", "商品信息", "下单引导"}}, target)
}

// AnalysisPrompt 是分析阶段的系统提示词，用低思考调用。
const AnalysisPrompt = `你是直播话术分析师。产出是「写作蓝图」，不是复述样板。
禁止出现样板里的任何原句，禁止出现任何具体数字、价格、地名、品名、链接号。

只输出一个 JSON 对象，不要解释、不要 Markdown 代码块：
{
  "开场方式": "样板用的开法类型 + 这种开法起什么作用（一句话）",
  "逻辑链": [
    {"名称": "路线名", "段序": ["段名1","段名2","段名3"], "理由": "为什么这条路线成立、适合什么场合"},
    ... 正好 10 条 ...
  ],
  "收口逻辑": "落点类型和顺序（一句话）",
  "语言风格": "句长、称呼类型、口头禅类型、重复强调方式（一句话）"
}

要求：
1. 只写类型和逻辑，不要写任何具体场景、画面、例句。
2. 不要写内容要点，段名本身就是内容大纲。
3. 每条路线尽量给出至少 3 个不同的段名，每个段名 2 到 4 个字，由你根据这条样板自己定。
4. 全部中文，只输出 JSON。`

// ContentHash 判断话术片内容有没有变过，变了就重新分析。
func ContentHash(sample string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(sample)))
	return hex.EncodeToString(sum[:])
}

// ParseAnalysis 解析分析模型返回的 JSON，容忍外层包裹。
func ParseAnalysis(raw string) (Analysis, error) {
	var a Analysis
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start < 0 || end <= start {
		return a, errors.New("分析结果不是 JSON 对象")
	}
	if err := json.Unmarshal([]byte(raw[start:end+1]), &a); err != nil {
		return a, err
	}
	if len(a.Chains) == 0 {
		return a, errors.New("分析结果没有逻辑链")
	}
	a.Chains = completeChains(a.Chains)
	if len(a.Chains) == 0 {
		return a, errors.New("所有路线合计不足 3 个不同的段落")
	}
	return a, nil
}

// completeChains preserves each route and its internal order. For short routes,
// borrow an ordered group of new nodes from the next routes in the same reply.
func completeChains(chains []Chain) []Chain {
	for i := range chains {
		chains[i].Nodes = cleanNodes(chains[i].Nodes)
	}
	usable := make([]Chain, 0, len(chains))
	for i, chain := range chains {
		if len(chain.Nodes) == 0 {
			continue
		}
		if len(chain.Nodes) < minChainNodes {
			chain.Nodes = slices.Clone(chain.Nodes)
			for offset := 1; offset < len(chains) && len(chain.Nodes) < minChainNodes; offset++ {
				donor := chains[(i+offset)%len(chains)]
				for _, node := range donor.Nodes {
					if !slices.Contains(chain.Nodes, node) {
						chain.Nodes = append(chain.Nodes, node)
					}
					if len(chain.Nodes) == minChainNodes {
						break
					}
				}
			}
		}
		if len(chain.Nodes) >= minChainNodes {
			usable = append(usable, chain)
		}
	}
	return usable
}

func cleanNodes(nodes []string) []string {
	out := make([]string, 0, len(nodes))
	for _, n := range nodes {
		if n = strings.TrimSpace(n); n != "" && !slices.Contains(out, n) {
			out = append(out, n)
		}
	}
	return out
}

// Pick 在避开 recent 的前提下随机取一条链，返回下标；都避不开时忽略 recent。
func Pick(n int, recent []int, r *rand.Rand) int {
	if n <= 0 {
		return -1
	}
	blocked := make(map[int]bool, len(recent))
	for _, v := range recent {
		if v >= 0 && v < n {
			blocked[v] = true
		}
	}
	candidates := make([]int, 0, n)
	for i := 0; i < n; i++ {
		if !blocked[i] {
			candidates = append(candidates, i)
		}
	}
	if len(candidates) == 0 {
		for i := 0; i < n; i++ {
			candidates = append(candidates, i)
		}
	}
	if r == nil {
		return candidates[0]
	}
	return candidates[r.Intn(len(candidates))]
}

// TargetLength 在 [400,1000] 之间取一个目标字数。
func TargetLength(r *rand.Rand) int {
	const lo, hi = 400, 1000
	if r == nil {
		return (lo + hi) / 2
	}
	return lo + r.Intn(hi-lo+1)
}

// RenderRequirement 拼出写作阶段的「写作要求」段落。
// RenderRequirement 只拼接分析模型给的结构与字数参数，不写任何额外要求。
// 客户自己的补充写在方案「话术要求」里，由调用方原样插入。
func RenderRequirement(a Analysis, c Chain, target int) string {
	var b strings.Builder
	b.WriteString("【写作要求】\n")
	b.WriteString("开场方式：" + strings.TrimSpace(a.Opening) + "\n")
	b.WriteString("本条逻辑链：" + c.Name + "\n")
	b.WriteString("段落顺序：" + strings.Join(c.Nodes, " → ") + "\n")
	b.WriteString("收口逻辑：" + strings.TrimSpace(a.Closing) + "\n")
	b.WriteString("语言风格：" + strings.TrimSpace(a.Style) + "\n")
	b.WriteString("目标字数：" + itoa(target) + "\n")
	return b.String()
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	var buf [12]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}

// NormalizeScript 把正文压成单段：去换行与行首尾空白、折叠空白、
// 只保留英文数字内部的空格，中文之间和中文标点两侧的空格全部去掉。
func NormalizeScript(text string) string {
	lines := strings.FieldsFunc(text, func(r rune) bool {
		return r == '\r' || r == '\n' || r == '\v' || r == '\f' || r == '\u0085' || r == '\u2028' || r == '\u2029'
	})
	var b strings.Builder
	for _, line := range lines {
		b.WriteString(strings.TrimSpace(line))
	}
	return collapseSpaces(b.String())
}

func collapseSpaces(s string) string {
	out := make([]rune, 0, len(s))
	pending := false
	for _, r := range s {
		switch r {
		case ' ', '\t', '\u3000', '\u00a0':
			pending = true
			continue
		}
		if pending {
			if len(out) > 0 && isASCIIAlnum(out[len(out)-1]) && isASCIIAlnum(r) {
				out = append(out, ' ')
			}
			pending = false
		}
		out = append(out, r)
	}
	return string(out)
}

func isASCIIAlnum(r rune) bool {
	return r < 128 && (r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9')
}
