package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strings"

	"livecompanion/management/internal/agentgateway"
)

// anchorStyleDimension 分步提取的一个维度。模型逐维回答、程序合并，
// 避免一次性输出导致格式乱、维度串、品相关混入。
type anchorStyleDimension struct {
	Title  string // 维度标题
	Prompt string // 该维度的提取要求
}

// anchorStyleDimensions 风格提取的维度，顺序即输出顺序。
// 只提「怎么说」的腔调，不提炼任何话术/算账/催单/情绪策略。
// 情绪、热情、亲和等气质由语气词、称呼、长短句、口头禅自然带出，不单独成维。
var anchorStyleDimensions = []anchorStyleDimension{
	{"语气词", "提取样本实际出现的语气词、位置和功能，区分句末助词、普通实词和问句收尾。保留高频特征，不要求每句都加。"},
	{"称呼与自指", "提取主播实际称呼观众、简称及自称。称呼是叫观众，自指是叫自己。主力称呼可高频分布，但不等于每段固定开头。"},
	{"句式与节奏", "描述长短句搭配、问答式推进、重复强调、停顿位置和承接方式，用抽象结构表达，不摘口播照抄。"},
	{"方言与口头禅", "只保留真实出现、可独立复用的语言词。没有出现的方言不能因地域印象补入。"},
}

// anchorStyleSpeechSections 前四个维度归入【口语风格】。
var anchorStyleSpeechSections = []string{"语气词", "称呼与自指", "句式与节奏", "方言与口头禅"}

// 单维回答的格式约束：content 必须是一段连贯纯文字。
const anchorStyleContentFormat = "只返回 JSON：{\"content\":\"该维度的描述\"}。content 必须是一段连贯的纯文字（不超过250字），禁止编号、列表、小标题、【】标签和换行，不要重复维度名，不要分点。"

// 清理单维回答用正则。
var (
	reAnchorStyleTagLabel = regexp.MustCompile(`【[^】]*】`)
	reAnchorStyleListMark = regexp.MustCompile(`^\s*(?:[-*•]\s*|\d+\s*[.、．)）]\s*|[①②③④⑤⑥⑦⑧⑨⑩]\s*)`)
)

// cleanAnchorStyleDimensionContent 把模型单维回答压成一段连贯文字：
// 去掉维度名重复、【】标签、编号与列表符号，多行合并。
func cleanAnchorStyleDimensionContent(title, content string) string {
	text := strings.TrimSpace(content)
	for _, prefix := range []string{title + "：", title + ":"} {
		if strings.HasPrefix(text, prefix) {
			text = strings.TrimSpace(strings.TrimPrefix(text, prefix))
		}
	}
	text = reAnchorStyleTagLabel.ReplaceAllString(text, "")
	parts := make([]string, 0, 8)
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		line = reAnchorStyleListMark.ReplaceAllString(line, "")
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts = append(parts, line)
	}
	return strings.Join(parts, "")
}

// extractAnchorStyleByDimensions 逐维度问模型、逐维回答，程序合并成固定格式的元提示词。
// 每个维度一次调用，模型只答一个维度，输出聚焦、不串维度。
func (s *Server) extractAnchorStyleByDimensions(ctx context.Context, analysis, sample, profileID string) (string, agentgateway.Response, error) {
	results := make(map[string]string, len(anchorStyleDimensions))
	var agg agentgateway.Response
	for _, dim := range anchorStyleDimensions {
		data, _ := json.Marshal(map[string]string{"scene": "带货直播", "sample_text": sample})
		system := analysis + "\n\n本次只提取【" + dim.Title + "】这一个维度，不要输出其它维度，也不要输出商品、价格、规格、功效、销量、库存、售后、催单、逼单、制造紧迫、从众引导等具体内容。\n" + dim.Prompt + "\n\n" + anchorStyleContentFormat
		resp, _, err := s.completeAnchorStyleWorkbench(ctx, profileID, "style_analysis_"+dim.Title, agentgateway.ResponseJSON, []agentgateway.Message{{Role: "system", Content: system}, {Role: "user", Content: string(data)}}, 1200, true)
		if err != nil {
			return "", agg, err
		}
		agg.LatencyMS += resp.LatencyMS
		if agg.Provider == "" {
			agg.Provider, agg.Model = resp.Provider, resp.Model
		}
		var out struct {
			Content string `json:"content"`
		}
		if json.Unmarshal([]byte(stripPolicyJSONFence(resp.Text)), &out) == nil && strings.TrimSpace(out.Content) != "" {
			results[dim.Title] = cleanAnchorStyleDimensionContent(dim.Title, out.Content)
		}
	}
	if len(results) == 0 {
		return "", agg, errors.New("分步提取未返回有效维度")
	}
	return mergeAnchorStyleSections(results), agg, nil
}

// mergeAnchorStyleSections 把 4 个纯腔调维度合并成【口语风格】单章节固定格式（扁平、不嵌套）。
func mergeAnchorStyleSections(results map[string]string) string {
	var b strings.Builder

	b.WriteString("【口语风格】\n")
	for _, dim := range anchorStyleSpeechSections {
		if v := strings.TrimSpace(results[dim]); v != "" {
			b.WriteString("- **" + dim + "**：" + v + "\n")
		}
	}

	return b.String()
}
