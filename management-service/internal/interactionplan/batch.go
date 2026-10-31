package interactionplan

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/interactionpolicy"
)

const MaxBatchItems = 10

// These opaque per-request labels never contain a user, event or task identity.
type BatchItem struct {
	ID string `json:"item_id"`
	Input
}
type ItemResult struct {
	ID     string
	Intent Intent
	Body   string
	Err    error
}
type BatchResult struct {
	Items  []ItemResult
	Issues []string
}

func ValidateBatch(items []BatchItem) error {
	if len(items) < 1 || len(items) > MaxBatchItems {
		return errors.New("互动批次须包含1至10人")
	}
	seen := map[string]bool{}
	for _, item := range items {
		validID := false
		for i := 1; i <= MaxBatchItems; i++ {
			if item.ID == fmt.Sprintf("U%02d", i) {
				validID = true
			}
		}
		if !validID || seen[item.ID] {
			return errors.New("互动批次编号无效或重复")
		}
		seen[item.ID] = true
		in := item.Input
		if utf8.RuneCountInString(in.EngagementToken) > 160 {
			return errors.New("互动口令上下文超出范围")
		}
		if (strings.TrimSpace(in.Text) == "" && (in.Kind == "" || in.Kind == "semantic" || in.Kind == "urgent" || in.Kind == "dialogue")) || utf8.RuneCountInString(in.Text) > 2400 || len(in.Recent) > 5 || len(in.Context) > 5 || utf8.RuneCountInString(in.CurrentMainline) > 8000 || utf8.RuneCountInString(in.Original) > 2400 || utf8.RuneCountInString(in.Clarification) > 2400 || utf8.RuneCountInString(in.Correction) > 8000 {
			return errors.New("互动输入超出范围")
		}
		for _, turn := range in.Recent {
			if utf8.RuneCountInString(turn.Question) > 2400 || utf8.RuneCountInString(turn.Answer) > 2400 {
				return errors.New("互动上下文超出范围")
			}
		}
		for _, m := range in.Context {
			if (m.Role != "user" && m.Role != "assistant") || utf8.RuneCountInString(m.Text) > 2400 {
				return errors.New("互动上下文格式错误")
			}
		}
	}
	return nil
}

func GenerateBatch(ctx context.Context, s Snapshot, items []BatchItem, complete Complete) (BatchResult, error) {
	if err := ValidateBatch(items); err != nil {
		return BatchResult{}, err
	}
	if complete == nil {
		return BatchResult{}, errors.New("互动模型未配置")
	}
	if strings.TrimSpace(s.Prompt) == "" || strings.TrimSpace(s.P0) == "" || strings.TrimSpace(s.P0Markdown) == "" {
		return BatchResult{}, errors.New("互动方案或完整P0不可用")
	}
	if err := ctx.Err(); err != nil {
		return BatchResult{}, err
	}
	md, err := interactionpolicy.Read("interaction-v2-batch.md")
	if err != nil {
		return BatchResult{}, err
	}
	// Shared plan and scene are sent once; each person's histories stay inside its item.
	inputs := append([]BatchItem(nil), items...)
	mainline := inputs[0].CurrentMainline
	engagement := inputs[0].EngagementToken
	for i := range inputs {
		if inputs[i].CurrentMainline != "" && inputs[i].CurrentMainline != mainline {
			return BatchResult{}, errors.New("互动批次现场不一致")
		}
		inputs[i].CurrentMainline = ""
		if inputs[i].EngagementToken != engagement {
			return BatchResult{}, errors.New("互动批次口令现场不一致")
		}
		inputs[i].EngagementToken = ""
	}
	raw, err := json.Marshal(struct {
		Items      []BatchItem `json:"items"`
		Mainline   string      `json:"current_mainline,omitempty"`
		Engagement string      `json:"engagement_token,omitempty"`
	}{inputs, mainline, engagement})
	if err != nil {
		return BatchResult{}, err
	}
	req := agentgateway.Request{Stage: "interaction_purpose_resolution", ResponseFormat: agentgateway.ResponseText, MaxTokens: 900 * len(items), Timeout: 27 * time.Second, EnableThinking: false, CacheSystemPrompt: true,
		Messages: []agentgateway.Message{{Role: "system", Content: s.P0 + "\n\n本场冻结的方案提示词：\n" + stripFormatSection(s.Prompt) + "\n\n" + string(md)}, {Role: "user", Content: string(raw)}}}
	response, err := complete(ctx, req)
	if err != nil {
		return BatchResult{}, err
	}
	if err := ctx.Err(); err != nil {
		return BatchResult{}, err
	}
	return parseBatch(s, items, response.Text), nil
}

// Parse blocks without relying on return order. Bad/missing blocks are explicit
// failures; duplicate labels invalidate that person, never overwrite a reply.
func parseBatch(s Snapshot, items []BatchItem, raw string) BatchResult {
	out := BatchResult{Items: make([]ItemResult, len(items))}
	indices := map[string]int{}
	for i, item := range items {
		indices[item.ID] = i
		out.Items[i] = ItemResult{ID: item.ID, Err: errors.New("模型遗漏该人结果")}
	}
	if len(raw) > 128<<10 {
		out.Issues = append(out.Issues, "互动批次返回超出范围")
		return out
	}
	type block struct {
		fields  map[string]string
		invalid bool
		closed  bool
	}
	blocks := map[string]*block{}
	var active *block
	activeID := ""
	legacy := len(items) == 1 && !strings.Contains(raw, "编号：") && !strings.Contains(raw, "编号:")
	if legacy {
		activeID = items[0].ID
		active = &block{fields: map[string]string{}, closed: true}
		blocks[activeID] = active
	}
	for _, line := range strings.Split(strings.TrimSpace(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "```") {
			continue
		}
		at := strings.IndexAny(line, "：:")
		if at <= 0 {
			if active != nil {
				active.invalid = true
			}
			continue
		}
		sep := 1
		if strings.HasPrefix(line[at:], "：") {
			sep = len("：")
		}
		key, value := strings.TrimSpace(line[:at]), strings.TrimSpace(line[at+sep:])
		if key == "编号" {
			if active != nil && !active.closed {
				active.invalid = true
			}
			activeID = value
			if _, ok := indices[value]; !ok {
				active = nil
				out.Issues = append(out.Issues, "模型返回未知编号")
				continue
			}
			if old := blocks[value]; old != nil {
				old.invalid = true
				active = old
				out.Issues = append(out.Issues, "模型返回重复编号")
				continue
			}
			active = &block{fields: map[string]string{}}
			blocks[value] = active
			continue
		}
		if key == "结束" {
			if active == nil || value != activeID {
				if active != nil {
					active.invalid = true
				}
				out.Issues = append(out.Issues, "互动结果结束编号不符")
			} else {
				active.closed = true
			}
			active = nil
			continue
		}
		if active == nil {
			continue
		}
		if _, exists := active.fields[key]; exists {
			active.invalid = true
		}
		active.fields[key] = value
	}
	for id, b := range blocks {
		i := indices[id]
		if b.invalid || !b.closed {
			out.Items[i].Err = errors.New("互动结果编号、字段重复或分段不完整")
			continue
		}
		if !legacy && (strings.TrimSpace(b.fields["类型"]) == "" || strings.TrimSpace(b.fields["歧义"]) == "") {
			out.Items[i].Err = errors.New("互动结果缺少类型或歧义判断")
			continue
		}
		intent, body, err := parseSingleContent(s, items[i].Input, b.fields)
		out.Items[i] = ItemResult{ID: id, Intent: intent, Body: body, Err: err}
	}
	return out
}
