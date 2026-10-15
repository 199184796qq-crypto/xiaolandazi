package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"livecompanion/management/internal/agentgateway"
	appdb "livecompanion/management/internal/db"
)

type speechMinerMessage struct {
	Role string `json:"role"`
	Text string `json:"text"`
}

type speechMinerOption struct {
	Label  string `json:"label"`
	Value  string `json:"value"`
	Action string `json:"action"`
}

type speechMinerFactDraft struct {
	Category    string `json:"category"`
	Key         string `json:"key"`
	Value       string `json:"value"`
	SourceQuote string `json:"source_quote"`
}

type speechMinerGuideInput struct {
	TenantID      int64                  `json:"tenant_id,omitempty"`
	Message       string                 `json:"message"`
	History       []speechMinerMessage   `json:"history"`
	DraftFacts    []speechMinerFactDraft `json:"draft_facts"`
	Mode          string                 `json:"mode,omitempty"`
	Start         bool                   `json:"start,omitempty"`
	Action        string                 `json:"action,omitempty"`
	FocusGap      string                 `json:"focus_gap,omitempty"`
	ProductLinkID int64                  `json:"product_link_id,omitempty"`
}

func normalizeSpeechMinerAction(action string) (string, error) {
	if action == "" || action == "answer" {
		return "answer", nil
	}
	if action == "skip" || action == "complete" {
		return action, nil
	}
	return "", errors.New("挖掘操作无效")
}

func applySpeechMinerTurnBoundary(output *speechMinerGuideResponse, input speechMinerGuideInput) {
	if input.Start || input.Action == "skip" || input.Action == "complete" {
		output.CapturedFacts = []speechMinerFactDraft{}
	}
	if input.Start {
		output.Completed = false
	}
	if !input.Start && input.Action == "complete" {
		output.Completed = true
		output.Options = nil
		output.SelectionMode = "single"
	}
}

func normalizeSpeechMinerMode(mode string) (string, error) {
	if mode == "" || mode == "fact_gaps" {
		return "fact_gaps", nil
	}
	if mode == "new_selling_point" {
		return mode, nil
	}
	if mode == "audience_reverse" {
		return mode, nil
	}
	return "", errors.New("挖掘模式无效")
}

type speechMinerGuideResponse struct {
	Reply         string                 `json:"reply"`
	SelectionMode string                 `json:"selection_mode"`
	Options       []speechMinerOption    `json:"options"`
	CapturedFacts []speechMinerFactDraft `json:"captured_facts"`
	Completed     bool                   `json:"completed"`
	Provider      string                 `json:"provider,omitempty"`
	Model         string                 `json:"model,omitempty"`
	LatencyMS     int64                  `json:"latency_ms,omitempty"`
}

func normalizeSpeechMinerGuideResponse(raw, latestMessage string) (speechMinerGuideResponse, error) {
	catalog, _, err := loadFactCategoryCatalog()
	if err != nil {
		return speechMinerGuideResponse{}, err
	}
	return normalizeSpeechMinerGuideResponseWithCategories(raw, latestMessage, catalog)
}

func normalizeSpeechMinerGuideResponseWithCategories(raw, latestMessage string, catalog factCategoryCatalog) (speechMinerGuideResponse, error) {
	var output speechMinerGuideResponse
	if err := json.Unmarshal([]byte(stripPolicyJSONFence(raw)), &output); err != nil {
		return output, errors.New("话术挖掘机返回格式无效")
	}
	output.Reply = strings.TrimSpace(output.Reply)
	if output.Reply == "" || utf8.RuneCountInString(output.Reply) > 1200 {
		return output, errors.New("话术挖掘机回复为空或过长")
	}
	output.SelectionMode = strings.ToLower(strings.TrimSpace(output.SelectionMode))
	// The model must explicitly choose single for mutually exclusive answers.
	// An omitted/unknown marker must not silently turn a set into one-click answers.
	if output.SelectionMode != "single" {
		output.SelectionMode = "multiple"
	}
	options := make([]speechMinerOption, 0, min(5, len(output.Options)))
	seenOptions := map[string]bool{}
	for _, option := range output.Options {
		option.Label = strings.TrimSpace(option.Label)
		option.Value = strings.TrimSpace(option.Value)
		option.Action = strings.ToLower(strings.TrimSpace(option.Action))
		if option.Action != "skip" && option.Action != "complete" {
			option.Action = "answer"
		}
		if option.Label == "" || option.Value == "" || utf8.RuneCountInString(option.Label) > 80 || utf8.RuneCountInString(option.Value) > 500 || seenOptions[option.Value] {
			continue
		}
		seenOptions[option.Value] = true
		options = append(options, option)
		if len(options) == 5 {
			break
		}
	}
	output.Options = options
	answerCount := 0
	for _, option := range output.Options {
		if option.Action == "answer" {
			answerCount++
		}
	}
	if answerCount < 2 {
		output.SelectionMode = "single"
	}
	facts := make([]speechMinerFactDraft, 0, min(5, len(output.CapturedFacts)))
	seenFacts := map[string]bool{}
	for _, fact := range output.CapturedFacts {
		fact.Category = strings.TrimSpace(fact.Category)
		fact.Key = strings.TrimSpace(fact.Key)
		fact.Value = strings.TrimSpace(fact.Value)
		fact.SourceQuote = strings.TrimSpace(fact.SourceQuote)
		if !catalog.contains(fact.Category) || fact.Key == "" || fact.Value == "" || fact.SourceQuote == "" {
			continue
		}
		if !strings.Contains(latestMessage, fact.SourceQuote) || utf8.RuneCountInString(fact.Key) > 255 || utf8.RuneCountInString(fact.Value) > 4000 || utf8.RuneCountInString(fact.SourceQuote) > 1000 {
			continue
		}
		identity := fact.Category + "\x00" + fact.Key
		if seenFacts[identity] {
			continue
		}
		seenFacts[identity] = true
		facts = append(facts, fact)
		if len(facts) == 5 {
			break
		}
	}
	output.CapturedFacts = facts
	if output.Completed {
		output.SelectionMode = "single"
		output.Options = nil
	}
	return output, nil
}

func (s *Server) speechMinerGuide(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	planID, ok := liveAgentPlanPathID(w, r)
	if !ok {
		return
	}
	var input speechMinerGuideInput
	if readJSON(w, r, &input) != nil {
		writeError(w, http.StatusBadRequest, "话术挖掘请求格式错误")
		return
	}
	input.Message = strings.TrimSpace(input.Message)
	mode, err := normalizeSpeechMinerMode(input.Mode)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	input.Mode = mode
	input.Action, err = normalizeSpeechMinerAction(input.Action)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	if (!input.Start && input.Message == "") || utf8.RuneCountInString(input.Message) > 3000 || len(input.History) > 20 || len(input.DraftFacts) > 12 || (input.Start && (input.Message != "" || len(input.History) != 0 || len(input.DraftFacts) != 0)) {
		writeError(w, http.StatusBadRequest, "话术挖掘内容为空或过长")
		return
	}
	tenantID, ok := s.resolveLiveAgentPlanTenant(w, r, actor, input.TenantID, false)
	if !ok {
		return
	}
	isSupplement := strings.HasSuffix(r.URL.Path, "/product-supplement/chat")
	if input.ProductLinkID < 0 || (!isSupplement && input.ProductLinkID != 0) {
		writeError(w, http.StatusBadRequest, "商品链接选择无效")
		return
	}
	p0, p1, p2, err := loadRiskAnnotationMothers()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	source, err := s.loadSupplementFactPackage(r.Context(), tenantID, planID, input.ProductLinkID, p0, p1, p2)
	if err != nil {
		supplementScopeError(w, err)
		return
	}
	wheelValue := any(map[string]any{})
	wheel, wheelErr := s.store.GetActiveLiveSellingPointWheel(r.Context(), tenantID, planID)
	if wheelErr == nil {
		var parsed any
		if json.Unmarshal([]byte(wheel.EffectiveWheelJSON()), &parsed) == nil {
			wheelValue = parsed
		}
	} else if !errors.Is(wheelErr, appdb.ErrLiveSellingPointWheelNotFound) {
		writeError(w, http.StatusInternalServerError, "读取卖点轮盘失败")
		return
	}
	prompt, _, err := loadSpeechMinerGuidePrompt()
	stage := "speech_miner_guide"
	chatUnavailable := "话术挖掘机暂时没有响应，请稍后重试"
	if isSupplement {
		prompt, _, err = loadReplaceablePromptAsset("PRODUCT_SUPPLEMENT_CHAT_PROMPT_PATH", "configs/prompts/anchor-style/product-supplement-chat.md", "商品资料补充对话母版")
		stage = "product_supplement_chat"
		chatUnavailable = "资料助手暂时没有响应，请稍后重试"
		wheelValue = nil
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	settings, err := s.store.AnchorGenerationSettings(r.Context())
	if err != nil || settings.SellingPointProfileID == "" {
		writeError(w, http.StatusConflict, "请先在后台配置卖点挖掘模型")
		return
	}
	categories, _, err := s.tenantFactCategoryCatalog(r.Context(), tenantID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// First supplement turn runs the mandatory fact-gap checklist so the
	// assistant opens with the highest-priority missing facts (price first)
	// instead of waiting passively. When the user picked one specific gap from
	// the list, only that gap is injected and no full checklist run happens.
	// Failures degrade to the passive mode.
	gapChecklist := ""
	if isSupplement {
		if focus := strings.TrimSpace(input.FocusGap); focus != "" {
			gapChecklist = `{"gaps":[` + focus + `]}`
		} else if len(input.History) == 0 {
			gapCtx, gapCancel := context.WithTimeout(r.Context(), 60*time.Second)
			if gaps, gapErr := s.runFactGapCheck(gapCtx, tenantID, planID, source.Facts, p0, source.Target); gapErr == nil {
				gapChecklist = gaps
			}
			gapCancel()
		}
	}
	requestPayload := speechMinerGuidePayload(input, categories, source.Facts, wheelValue, p0, gapChecklist)
	if isSupplement {
		requestPayload["target_product"] = source.Target
	}
	payload, _ := json.Marshal(requestPayload)
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	ctx = withExchangeScope(ctx, appdb.ModelExchangeScope{TenantID: tenantID, PlanID: planID})
	result, _, err := s.completeAnchorStyleWorkbench(ctx, settings.SellingPointProfileID, stage, agentgateway.ResponseJSON, []agentgateway.Message{{Role: "system", Content: prompt}, {Role: "user", Content: string(payload)}}, 2400, false)
	if err != nil {
		writeError(w, http.StatusBadGateway, chatUnavailable)
		return
	}
	output, err := normalizeSpeechMinerGuideResponseWithCategories(result.Text, input.Message, categories)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	// A control/start event can never create facts or authorize persistence.
	applySpeechMinerTurnBoundary(&output, input)
	output.Provider = result.Provider
	output.Model = result.Model
	output.LatencyMS = result.LatencyMS
	writeJSON(w, http.StatusOK, output)
}

func speechMinerGuidePayload(input speechMinerGuideInput, categories factCategoryCatalog, facts string, wheel any, p0, gapChecklist string) map[string]any {
	payload := map[string]any{
		"effective_p0":              p0,
		"available_fact_categories": categories.Categories,
		"mode":                      input.Mode,
		"action":                    input.Action,
		"turn_kind":                 map[bool]string{true: "start", false: "answer"}[input.Start],
		"fact_package":              facts,
		"current_wheel":             wheel,
		"draft_facts":               input.DraftFacts,
		"history":                   input.History,
		"latest_message":            input.Message,
	}
	if gapChecklist != "" {
		payload["gap_checklist"] = gapChecklist
	}
	return payload
}
