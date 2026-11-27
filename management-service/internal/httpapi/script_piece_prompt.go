package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"regexp"
	"strings"
	"time"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/planprompt"
	"livecompanion/management/internal/scriptchain"
)

type directScriptPromptKey struct{}

var (
	scriptPieceBodyOnly = regexp.MustCompile(`(?:^|[\s，。；、：:])(?:请)?(?:最终|最后)?(?:只|仅)(?:需)?(?:输出|返回)` +
		`(?:改写后(?:的)?)?(?:完整(?:的)?)?(?:纯文本(?:的)?)?(?:口播(?:话术|正文)|话术|正文|纯文本)(?:[\s，。；、：:！]|$)`)
	scriptPieceOutputNotes = regexp.MustCompile(`(?m)^[\t \p{Zs}]*(?:` +
		`(?:【)?(?:事实修正清单|修正清单|修正点|待确认(?:信息|事项)?|改写说明|改动说明|说明|备注|解释|注)(?:】|[\t ]*[：:]|[\t ]*$)|` +
		`(?:以下是|下面是)[^\n。]{0,30}(?:话术|正文)[\t ]*(?:[：:]|$)|#{1,6}[\t ]+|>[\t ]+|(?:---+|\*\*\*+)[\t ]*$)`)
)

func scriptConfiguredPiecePrompts(v model.LiveAgentPlanVersion) map[string]recordingScriptEntry {
	script, ok := recordingScriptFromContext(v.GenerationContext)
	if !ok || script.Version != 2 {
		return nil
	}
	entries := make(map[string]recordingScriptEntry)
	for index, tab := range script.Tabs {
		for _, entry := range tab.Entries {
			entries[scriptEntryPoolID(tab, index, entry.EntryID)] = entry
		}
	}
	return entries
}

func scriptPiecePromptHash(unit volcengineScriptUnit) string {
	if strings.TrimSpace(unit.SpecPrompt) == "" {
		return ""
	}
	key, _ := json.Marshal([]string{strings.TrimSpace(unit.SpecPrompt), unit.Text, unit.SpecPromptAt})
	return scriptchain.ContentHash(string(key))
}

func scriptPieceVersionHash(unit volcengineScriptUnit) string {
	prompt := scriptPiecePromptHash(unit)
	if !strings.HasPrefix(strings.TrimSpace(unit.SpecPrompt), scriptFactLabel) {
		return prompt
	}
	if unit.SpecFactsKey == "" {
		return ""
	}
	return scriptchain.ContentHash("scoped:" + prompt + ":" + unit.SpecFactsKey)
}

func renderSpecifiedScriptPrompt(material scriptTextMaterial, target int) (string, error) {
	template, err := parseScriptPieceTemplate(material.unit.SpecPrompt)
	if err != nil {
		return "", err
	}
	body := "【原始话术】\n" + material.unit.Text + "\n\n【本片专属提示词】\n" + material.unit.SpecPrompt
	if template.useSlot {
		body = strings.Replace(template.body, scriptOriginalSlot, material.unit.Text, 1)
	}
	if len(template.objects) > 0 {
		if len(material.facts) == 0 {
			return "", &scriptPieceTemplateError{"未读取到提示词所引用的已确认事实"}
		}
		body = "【已确认事实】\n" + string(material.facts) + "\n\n" + body
	}
	return fmt.Sprintf("%s\n\n【红线禁止】\n%s\n\n按本片提示词改写原始话术，遵守红线，目标约 %d 字，400 至 1000 字，只写一段。\n\n%s",
		body, strings.TrimSpace(material.redLines), target, planprompt.RewriteOutputContract), nil
}

func parseSpecifiedScriptResponse(raw, prompt string) (scriptRewriteOutput, bool, error) {
	allowsPlain := scriptPieceBodyOnly.MatchString(prompt)
	if allowsPlain {
		raw = strings.ReplaceAll(strings.ReplaceAll(raw, "\r\n", "\n"), "\r", "\n")
		raw = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(raw), "\ufeff"))
		if scriptPieceOutputNotes.MatchString(raw) || strings.Contains(raw, "```") || strings.Contains(raw, "~~~") || strings.Contains(raw, "**") {
			return scriptRewriteOutput{}, false, errors.New("指定改写结果混入了说明或 Markdown 格式")
		}
	}
	parsed, err := parseScriptRewriteResponse(joinRewriteBodyLines(raw))
	if err == nil || !allowsPlain {
		return parsed, false, err
	}
	// The legacy parser preserves plain paragraphs and identifies incomplete
	// structured responses. Only a verbatim body is eligible for this fallback.
	plain, plainErr := parseScriptRewriteOutput(raw)
	if plainErr != nil || plain.Script != raw || plain.Skeleton != "" || plain.Context != "" || json.Valid([]byte(raw)) {
		return parsed, false, err
	}
	return plain, true, nil
}

func (s *Server) generateSpecifiedScriptText(ctx context.Context, input scriptTextGenerationInput) (scriptRewriteOutput, error) {
	material := input.material
	if input.loadMaterial != nil {
		var err error
		material, err = input.loadMaterial()
		if err != nil {
			return scriptRewriteOutput{}, err
		}
	}
	if strings.TrimSpace(material.unit.SpecPrompt) == "" {
		return scriptRewriteOutput{}, errUnknownScriptGenerationMode
	}
	prompt, err := renderSpecifiedScriptPrompt(material, 400+rand.Intn(601))
	if err != nil {
		return scriptRewriteOutput{}, err
	}
	if input.progress != nil {
		input.progress("正在按本片提示词生成…")
	}
	request := agentgateway.Request{
		Stage: "script_rewrite", ResponseFormat: agentgateway.ResponseText, MaxTokens: 4000, Timeout: 180 * time.Second,
		Messages: []agentgateway.Message{{Role: "user", Content: prompt}},
	}
	if input.session > 0 {
		request.EnableThinking = true
		request.ReasoningEffort = "high"
		request.MaxTokens = 0
		request.UseProviderOutputLimit = true
	}
	reportScriptManualProgress(ctx, scriptManualWriting)
	result, err := s.completePlayedScriptModel(context.WithValue(ctx, directScriptPromptKey{}, true), request)
	if err != nil {
		return scriptRewriteOutput{}, err
	}
	parsed, plain, err := parseSpecifiedScriptResponse(result.Text, material.unit.SpecPrompt)
	if err != nil {
		return scriptRewriteOutput{}, fmt.Errorf("改写输出格式错误: %w", err)
	}
	spoken := singleParagraphRecordingScriptEntry(stripBracketed(scriptchain.NormalizeScript(parsed.Script)))
	if spoken == "" {
		return scriptRewriteOutput{}, errors.New("指定改写没有可用正文")
	}
	if strings.Join(strings.Fields(spoken), "") == strings.Join(strings.Fields(material.unit.Text), "") {
		return scriptRewriteOutput{}, errors.New("指定话术与原文相同")
	}
	p0, p1, p2, err := loadRiskAnnotationMothers()
	if err != nil {
		return scriptRewriteOutput{}, err
	}
	reportScriptManualProgress(ctx, scriptManualReviewing)
	if hits, err := annotateScript(spoken, p0, p1, p2); err != nil || len(hits) > 0 {
		return scriptRewriteOutput{}, errSpecifiedScriptReviewRejected
	}
	if plain && input.session == 0 {
		parsed.Script = strings.TrimSpace(stripBracketed(parsed.Script))
	} else {
		parsed.Script = spoken
	}
	return parsed, nil
}
