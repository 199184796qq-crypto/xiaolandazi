package atomicfacts

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"livecompanion/management/internal/contentstrategy"
)

// CompleteFunc is the model call used to split facts by attribute.
type CompleteFunc func(context.Context, string) (string, error)

type modelAtomicFact struct {
	SourceFactID string   `json:"source_fact_id"`
	Text         string   `json:"text"`
	Attribute    string   `json:"attribute"`
	Conditions   []string `json:"conditions"`
}

type modelAtomicResult struct {
	Facts []modelAtomicFact `json:"facts"`
}

// BuildWithModel asks the model to split the full fact package into one
// attribute per fact. The model may only cut the original text; the program
// rejects paraphrases, invented numbers and missing source facts.
func BuildWithModel(ctx context.Context, raw string, complete CompleteFunc) (string, []Fact, error) {
	source := contentstrategy.Facts(raw)
	if len(source) == 0 {
		return "", nil, fmt.Errorf("no facts")
	}
	if len(source) <= 1 || complete == nil {
		text, facts := Build(raw)
		return text, facts, nil
	}
	type sourceFact struct {
		id, prefix, body string
	}
	byID := make(map[string]sourceFact, len(source))
	for _, fact := range source {
		prefix, body := splitPrefix(fact.Text)
		byID[fact.ID] = sourceFact{id: fact.ID, prefix: prefix, body: body}
	}
	lines := make([]string, 0, len(source))
	rendered := make([]string, 0, len(source)+8)
	for start := 0; start < len(source); start += 12 {
		end := start + 12
		if end > len(source) {
			end = len(source)
		}
		var batch strings.Builder
		for _, fact := range source[start:end] {
			item := byID[fact.ID]
			batch.WriteString(fact.ID)
			batch.WriteString(" | ")
			batch.WriteString(item.body)
			batch.WriteString("\n")
		}
		response, err := complete(ctx, atomicSplitPrompt+"\n\n【待拆事实】\n"+batch.String())
		if err != nil {
			return "", nil, err
		}
		var result modelAtomicResult
		startJSON := strings.Index(response, "{")
		endJSON := strings.LastIndex(response, "}")
		if startJSON < 0 || endJSON <= startJSON || json.Unmarshal([]byte(response[startJSON:endJSON+1]), &result) != nil {
			return "", nil, fmt.Errorf("atomic fact JSON invalid")
		}
		for _, item := range result.Facts {
			sourceFact, ok := byID[strings.TrimSpace(item.SourceFactID)]
			if !ok {
				continue
			}
			text := strings.TrimSpace(item.Text)
			if text == "" || !quoteFromSource(sourceFact.body, text) {
				continue
			}
			line := text
			if sourceFact.prefix != "" {
				line = sourceFact.prefix + " " + line
			}
			rendered = append(rendered, line)
			lines = append(lines, sourceFact.id)
		}
	}
	if len(rendered) == 0 {
		return "", nil, fmt.Errorf("model split produced no usable facts")
	}
	// A model may omit a source fact; append the original so nothing disappears.
	covered := make(map[string]bool, len(lines))
	for _, id := range lines {
		covered[id] = true
	}
	for _, fact := range source {
		if !covered[fact.ID] {
			rendered = append(rendered, fact.Text)
		}
	}
	renderedText := strings.Join(rendered, "\n")
	parsed := contentstrategy.Facts(renderedText)
	out := make([]Fact, 0, len(parsed))
	for _, fact := range parsed {
		out = append(out, Fact{ID: fact.ID, Text: fact.Text})
	}
	return renderedText, out, nil
}

func quoteFromSource(source, text string) bool {
	source = normalizeAtomic(source)
	text = normalizeAtomic(text)
	if source == "" || text == "" {
		return false
	}
	return strings.Contains(source, text)
}

func normalizeAtomic(value string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(value) {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			continue
		}
		switch r {
		case '，', ',', '。', '.', '；', ';', '：', ':', '！', '!', '？', '?', '、':
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

const atomicSplitPrompt = `你是直播事实原子化器。把每条事实拆成“一个属性一条”，只做切分，不回答问题。

强行规则：
1. 只能使用原文里的连续文字，不能改写、补充、删条件、换数字。
2. 一条事实可以拆成多条；每条 text 必须是原文的连续片段。
3. 商品卡、福利卡可以按字段拆，但每条都保留 source_fact_id。
4. 条件、例外、适用范围必须跟着对应的 text 走，不能丢。
5. 无法拆的事实原样返回一条。
6. 只输出 JSON，不要 Markdown。

输出：
{"facts":[{"source_fact_id":"F001","text":"原文连续片段","attribute":"属性名","conditions":["条件"]}]}`
