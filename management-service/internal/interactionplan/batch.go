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
	"livecompanion/management/internal/planprompt"
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
	Parse  ParseDiagnostics
}
type BatchResult struct {
	Items               []ItemResult
	Issues              []string
	Groups              int
	SpeechGroups        int
	ClarificationGroups int
	UnmappedGroups      int
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
	if result, handled, err := generateBehaviorBatch(ctx, s, items); handled {
		return result, err
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
	for i := range inputs {
		if inputs[i].Kind != "time_report" {
			inputs[i].ReportClock, inputs[i].ReportPeriod = "", ""
		}
	}
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
	contract := planprompt.BatchInteractionOutputContract
	if len(items) == 1 {
		contract = planprompt.InteractionOutputContract
	}
	// 完整P0留在程序快照中，由解析后的Validate及送播前校验使用，
	// 不在欢迎或其它互动的正文生成请求里额外叠加。
	req := agentgateway.Request{Stage: "interaction_purpose_resolution", ResponseFormat: agentgateway.ResponseText, MaxTokens: 900 * len(items), Timeout: 27 * time.Second, EnableThinking: false, CacheSystemPrompt: true,
		Messages: []agentgateway.Message{{Role: "system", Content: "本场冻结的方案提示词：\n" + stripFormatSection(s.Prompt) + "\n\n" + stripFormatSection(string(md)) + "\n\n" + contract}, {Role: "user", Content: string(raw)}}}
	response, err := complete(ctx, req)
	if err != nil {
		return BatchResult{}, err
	}
	if err := ctx.Err(); err != nil {
		return BatchResult{}, err
	}
	parsed := parseBatch(s, items, response.Text, response.FinishReason)
	logParseDiagnostics(ctx, s.Scope, parsed)
	return parsed, nil
}
