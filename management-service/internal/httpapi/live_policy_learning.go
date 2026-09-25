package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/policy"
)

type livePolicyLearningModelOutput struct {
	AbsorbRecommended bool   `json:"absorb_recommended"`
	TargetLayer       string `json:"target_layer"`
	Reason            string `json:"reason"`
	Confidence        int    `json:"confidence"`
	RuleTitle         string `json:"rule_title"`
	RuleText          string `json:"rule_text"`
	ExecutionMode     string `json:"execution_mode"`
}

type livePolicyLearningEvidenceProposal struct {
	AbsorbRecommended bool     `json:"absorb_recommended"`
	TargetLayer       string   `json:"target_layer"`
	Reason            string   `json:"reason"`
	Confidence        int      `json:"confidence"`
	RuleTitle         string   `json:"rule_title"`
	RuleText          string   `json:"rule_text"`
	ExecutionMode     string   `json:"execution_mode"`
	PromotionLevel    string   `json:"promotion_level"`
	RegressionCases   []string `json:"regression_cases"`
}

type livePolicyLearningEvidenceModelOutput struct {
	Summary   string                               `json:"summary"`
	Proposals []livePolicyLearningEvidenceProposal `json:"proposals"`
}

func normalizeLearningEvidenceType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "manual_feedback", "bad_answer", "corrected_answer", "screenshot_review", "post_live_review", "gold_sample":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return "manual_feedback"
	}
}

func normalizeLearningPromotionLevel(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "stable":
		return "stable"
	case "guardrail":
		return "guardrail"
	default:
		return "candidate"
	}
}

func normalizeLearningRegressionCases(items []string) []string {
	return normalizeUniqueStrings(items, 8)
}

func normalizeLivePolicyLearningHistory(
	items []model.LivePolicyLearningHistoryItem,
) []model.LivePolicyLearningHistoryItem {
	if len(items) > 16 {
		items = items[len(items)-16:]
	}
	result := make([]model.LivePolicyLearningHistoryItem, 0, len(items))
	for _, item := range items {
		role := strings.ToLower(strings.TrimSpace(item.Role))
		if role == "assistant" {
			role = "agent"
		}
		if role != "user" && role != "agent" {
			continue
		}
		text := strings.TrimSpace(item.Text)
		if text == "" {
			continue
		}
		if utf8.RuneCountInString(text) > 2000 {
			text = string([]rune(text)[:2000])
		}
		result = append(result, model.LivePolicyLearningHistoryItem{
			Role: role,
			Text: text,
		})
	}
	return result
}

func buildLivePolicyLearningPrompt(input model.CreateLivePolicyLearningCandidateInput) (string, error) {
	historyRaw, err := json.Marshal(input.History)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(fmt.Sprintf(`
你是直播策略“调教学习归因器”。你的任务不是再次回答观众，而是从一次人工反馈证据中提取可复用的学习，并判断它应该沉淀到规则层、行业层还是用户层。证据可能是满意回复，也可能是错误回答、截图复盘或人工纠正。

【三层定义】
规则层：跨行业、跨商户、跨商品、跨直播间仍成立的“判断方法与表达原则”。例如真实性判断、先理解意图再柔性转译、信息不足时如何热情承接。规则层不是禁止清单。
行业层：同一行业内多数商户都适用，但换行业后不一定适用的专业知识、常见问法、行业销售节奏、行业表达习惯和行业边界。
用户层：只属于具体商户、直播间、商品、活动、主播个人风格、口头习惯、当地经营策略或具体事实的数据与表达。

【判断规则】
1. 用“换行业、换商户、换商品、换主播后还成立吗”判断层级。
2. 只要依赖具体商品事实、活动、价格、库存、发货时间、门店、主播称呼偏好等，优先用户层，不能为了复用而硬升到规则层或行业层。
3. 某行业普遍规律才进入行业层；单一客户经验不能冒充行业规则。
4. 真正跨行业的判断与表达方法才进入规则层。
5. 如果这次只是一次偶然改词，没有可复用规律，absorb_recommended=false；仍给出最接近的层级和原因，供人工判断。
6. rule_text 要写成“以后遇到同类情况如何判断、如何表达”的规则，不要简单复制最终答案或错误答案。规则层和行业层尤其要去掉具体客户、价格、库存、活动等一次性事实。
7. 默认 execution_mode=intent。只有学习本身明确要求“一字不改/固定原话/100%%原话”时才可 verbatim。
8. observed_reply 是当时真实生成或播出的回答，可能是错误样本；final_reply 可能为空。不要把错误回答里的事实或错误理解吸收到规则里。
9. 如果证据反映的是纯运行时故障，例如音频丢块、播放器失败，不要伪造成语言策略规则。
10. 返回严格 JSON，不要 Markdown。reason 等面向人的文字只能使用“规则层 / 行业层 / 用户层”称呼，不要输出内部层级编码。

JSON：
{
  "absorb_recommended": true,
  "target_layer": "L1|L2|L3",
  "reason": "为什么属于这一层，说明为什么不是另外两层",
  "confidence": 0-100,
  "rule_title": "简短规则名",
  "rule_text": "可直接保存为规则的完整正文",
  "execution_mode": "intent|verbatim"
}

证据类型：%s
证据来源：%s
当前来源层：%s
当前行业：%s
当前直播间ID：%d
原始问题：
%s

当时真实回答（可能是错误样本）：
%s

最终满意回复/人工改写（可能为空）：
%s

用户最后的调教/确认：
%s

多轮调教历史：
%s
`,
		input.EvidenceType,
		input.SourceRef,
		input.SourceLayer,
		input.IndustryCode,
		input.RoomID,
		input.Question,
		input.ObservedReply,
		input.FinalReply,
		input.Feedback,
		string(historyRaw),
	)), nil
}

func normalizeLivePolicyLearningModelOutput(
	output livePolicyLearningModelOutput,
	input model.CreateLivePolicyLearningCandidateInput,
) livePolicyLearningModelOutput {
	output.TargetLayer = strings.ToUpper(strings.TrimSpace(output.TargetLayer))
	if output.TargetLayer != model.LivePolicyLayerL1 &&
		output.TargetLayer != model.LivePolicyLayerL2 &&
		output.TargetLayer != model.LivePolicyLayerL3 {
		output.TargetLayer = strings.ToUpper(strings.TrimSpace(input.SourceLayer))
		if output.TargetLayer != model.LivePolicyLayerL1 &&
			output.TargetLayer != model.LivePolicyLayerL2 &&
			output.TargetLayer != model.LivePolicyLayerL3 {
			if input.RoomID > 0 {
				output.TargetLayer = model.LivePolicyLayerL3
			} else {
				output.TargetLayer = model.LivePolicyLayerL1
			}
		}
	}
	output.Reason = strings.TrimSpace(output.Reason)
	output.RuleTitle = strings.TrimSpace(output.RuleTitle)
	output.RuleText = strings.TrimSpace(output.RuleText)
	output.ExecutionMode = strings.ToLower(strings.TrimSpace(output.ExecutionMode))
	if output.ExecutionMode != model.LivePolicyModeVerbatim {
		output.ExecutionMode = model.LivePolicyModeIntent
	}
	if output.Confidence < 0 {
		output.Confidence = 0
	}
	if output.Confidence > 100 {
		output.Confidence = 100
	}
	if output.RuleTitle == "" {
		output.RuleTitle = "调教沉淀规则"
	}
	if output.RuleText == "" {
		output.RuleText = strings.TrimSpace(input.Feedback)
		if output.RuleText == "" {
			output.RuleText = strings.TrimSpace(input.FinalReply)
		}
	}
	if output.Reason == "" {
		output.Reason = "根据调教内容的适用范围判断。"
	}
	return output
}

func callPolicyLearningModel(
	ctx context.Context,
	prompt string,
	provider string,
	modelName string,
) (livePolicyLearningModelOutput, string, string, int64, error) {
	result, err := agentgateway.NewFromEnv().Complete(ctx, agentgateway.Request{
		Provider: provider,
		Model:    modelName,
		Messages: []agentgateway.Message{
			{Role: "system", Content: prompt},
			{Role: "user", Content: "请分析这次调教并返回 JSON。"},
		},
		MaxTokens:      1200,
		EnableThinking: false,
		ResponseFormat: agentgateway.ResponseJSON,
		Timeout:        30 * time.Second,
	})
	if err != nil {
		return livePolicyLearningModelOutput{}, "", "", 0, err
	}
	raw := stripPolicyJSONFence(result.Text)
	var output livePolicyLearningModelOutput
	if err := json.Unmarshal([]byte(raw), &output); err != nil {
		return livePolicyLearningModelOutput{}, result.Provider, result.Model, result.LatencyMS, fmt.Errorf("decode policy learning output: %w", err)
	}
	return output, result.Provider, result.Model, result.LatencyMS, nil
}

func (s *Server) livePolicyLearningCreateCandidate(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	var input model.CreateLivePolicyLearningCandidateInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "调教学习内容格式错误")
		return
	}
	input.SourceLayer = strings.ToUpper(strings.TrimSpace(input.SourceLayer))
	input.IndustryCode = strings.ToLower(strings.TrimSpace(input.IndustryCode))
	input.EvidenceType = normalizeLearningEvidenceType(input.EvidenceType)
	input.SourceRef = strings.TrimSpace(input.SourceRef)
	input.ObservedReply = strings.TrimSpace(input.ObservedReply)
	input.LearningProvider = strings.TrimSpace(input.LearningProvider)
	input.LearningModel = strings.TrimSpace(input.LearningModel)
	input.Question = strings.TrimSpace(input.Question)
	input.FinalReply = strings.TrimSpace(input.FinalReply)
	input.Feedback = strings.TrimSpace(input.Feedback)
	input.History = normalizeLivePolicyLearningHistory(input.History)

	if input.SourceLayer != model.LivePolicyLayerL1 &&
		input.SourceLayer != model.LivePolicyLayerL2 &&
		input.SourceLayer != model.LivePolicyLayerL3 {
		writeError(w, http.StatusBadRequest, "请选择调教来源层")
		return
	}
	if input.Question == "" {
		writeError(w, http.StatusBadRequest, "原始问题不能为空")
		return
	}
	if input.FinalReply == "" && input.Feedback == "" && input.ObservedReply == "" {
		writeError(w, http.StatusBadRequest, "请至少提供真实回答、人工反馈或最终满意回复中的一项")
		return
	}
	if utf8.RuneCountInString(input.Question) > 4000 ||
		utf8.RuneCountInString(input.ObservedReply) > 6000 ||
		utf8.RuneCountInString(input.FinalReply) > 6000 ||
		utf8.RuneCountInString(input.Feedback) > 4000 ||
		utf8.RuneCountInString(input.SourceRef) > 512 {
		writeError(w, http.StatusBadRequest, "单次调教学习内容过长")
		return
	}

	var tenantID *int64
	if actor.Role == "customer" {
		if actor.TenantID == nil || input.RoomID <= 0 {
			writeError(w, http.StatusBadRequest, "客户调教学习必须指定自己的直播间")
			return
		}
		value := *actor.TenantID
		if _, err := s.getCoreRoomState(r.Context(), value, input.RoomID); err != nil {
			writeError(w, http.StatusNotFound, "直播间不存在或不属于当前终端")
			return
		}
		tenantID = &value
		input.SourceLayer = model.LivePolicyLayerL3
	} else {
		policyActor, viewOK := s.requireLivePolicyView(w, r)
		if !viewOK {
			return
		}
		actor = policyActor
	}

	prompt, err := buildLivePolicyLearningPrompt(input)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "准备调教学习上下文失败")
		return
	}
	analysis, modelProvider, modelName, latencyMS, err := callPolicyLearningModel(
		r.Context(), prompt, input.LearningProvider, input.LearningModel,
	)
	if err != nil {
		writeError(w, http.StatusBadGateway, "调教学习分析暂时不可用，请稍后重试")
		return
	}
	analysis = normalizeLivePolicyLearningModelOutput(analysis, input)
	if analysis.TargetLayer == model.LivePolicyLayerL3 && input.RoomID <= 0 {
		analysis.Reason += " 当前没有绑定具体直播间，采纳到用户层前需要指定直播间。"
	}
	item, err := s.store.CreateLivePolicyLearningCandidate(
		r.Context(),
		actor.UserID,
		tenantID,
		input,
		analysis.TargetLayer,
		analysis.Reason,
		analysis.RuleTitle,
		analysis.RuleText,
		analysis.ExecutionMode,
		modelProvider,
		modelName,
		analysis.AbsorbRecommended,
		analysis.Confidence,
		latencyMS,
		map[string]any{
			"promotion_level":  "candidate",
			"regression_cases": []string{},
		},
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "保存调教学习候选失败")
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func buildLivePolicyLearningEvidencePrompt(input model.CreateLivePolicyLearningEvidenceInput) (string, error) {
	historyRaw, err := json.Marshal(input.History)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(fmt.Sprintf(`
你是直播系统的“学习 Agent”。你不负责重新回答观众，而是把一次真实反馈证据提炼成可复用、可验证、可人工审核的学习候选。

【目标】
从一份证据中提炼 0 到 4 条彼此独立的候选规律。不要为了凑数量强行拆分。
每条候选都必须判断应进入规则层、行业层还是用户层，并给出回归测试问题。
所有候选都只是候选，最终是否发布由人工批准。

【三层定义】
规则层：跨行业、跨商户、跨商品、跨直播间仍成立的判断方法、真实性原则、意图理解原则、表达与质量评审原则。
行业层：同一行业多数商户都适用，但换行业后不一定成立的专业知识、常见问法、行业表达与销售节奏。
用户层：具体直播间、商户、商品、活动、主播风格、地域说法、当地经营策略或具体事实。

【必须分开】
1. 风格规律、商品事实、合规边界、运行时故障不能混成一条。
2. observed_reply 是真实生成/播出的内容，可能是错误样本，绝不能因为它出现过就当成正确事实。
3. corrected_reply 如果存在，是强证据；feedback 是用户对本次结果的直接判断，优先级高。
4. 一次商品事实不能升级成跨行业规则；地域口语通常优先用户层或行业层语义样本。
5. 如果问题是纯播放器、TTS丢块、音频延迟等运行时问题，没有可学习的语言规律，就返回 proposals=[]，在 summary 里说明应进入运行时问题而不是策略层。
6. “意图理解错误仍被评审放行”这类问题可以拆成两个独立候选：回答侧的理解原则、评审侧的质量原则。
7. 不要把一个词写死成唯一含义；要写成结合上下文判断的可泛化规则。
8. execution_mode 默认 intent；只有用户明确要求固定原话时才 verbatim。
9. promotion_level 只表示学习成熟度：candidate=单次证据候选；stable=用户明确确认或有重复证据；guardrail=真实性/合规/用户明确硬性原则。即使是 guardrail 也必须人工批准后才能生效。
10. regression_cases 给 2 到 6 个短测试输入，既要覆盖正例，也至少包含一个容易过拟合的反例。
11. 只返回严格 JSON，不要 Markdown。

JSON：
{
  "summary": "这次证据学到了什么；如果不适合策略学习，说明原因",
  "proposals": [
    {
      "absorb_recommended": true,
      "target_layer": "L1|L2|L3",
      "reason": "为什么属于这一层",
      "confidence": 0-100,
      "rule_title": "简短规则名",
      "rule_text": "以后遇到同类情况如何判断、如何表达或如何评审",
      "execution_mode": "intent|verbatim",
      "promotion_level": "candidate|stable|guardrail",
      "regression_cases": ["测试问题1", "测试问题2"]
    }
  ]
}

证据类型：%s
证据来源：%s
来源层：%s
行业：%s
直播间ID：%d

原始问题/场景：
%s

当时真实回答（可能错误）：
%s

人工修正后的回复（可能为空）：
%s

用户反馈：
%s

相关历史：
%s
`,
		input.EvidenceType,
		input.SourceRef,
		input.SourceLayer,
		input.IndustryCode,
		input.RoomID,
		input.Question,
		input.ObservedReply,
		input.CorrectedReply,
		input.Feedback,
		string(historyRaw),
	)), nil
}

func callPolicyLearningEvidenceModel(
	ctx context.Context,
	prompt string,
	provider string,
	modelName string,
) (livePolicyLearningEvidenceModelOutput, string, string, int64, error) {
	result, err := agentgateway.NewFromEnv().Complete(ctx, agentgateway.Request{
		Provider: provider,
		Model:    modelName,
		Messages: []agentgateway.Message{
			{Role: "system", Content: prompt},
			{Role: "user", Content: "请提炼本次证据中的学习候选并返回 JSON。"},
		},
		MaxTokens:      2400,
		EnableThinking: false,
		ResponseFormat: agentgateway.ResponseJSON,
		Timeout:        40 * time.Second,
	})
	if err != nil {
		return livePolicyLearningEvidenceModelOutput{}, "", "", 0, err
	}
	raw := stripPolicyJSONFence(result.Text)
	var output livePolicyLearningEvidenceModelOutput
	if err := json.Unmarshal([]byte(raw), &output); err != nil {
		return livePolicyLearningEvidenceModelOutput{}, result.Provider, result.Model, result.LatencyMS, fmt.Errorf("decode learning evidence output: %w", err)
	}
	output.Summary = strings.TrimSpace(output.Summary)
	if len(output.Proposals) > 4 {
		output.Proposals = output.Proposals[:4]
	}
	return output, result.Provider, result.Model, result.LatencyMS, nil
}

func normalizeLivePolicyLearningEvidenceProposal(
	proposal livePolicyLearningEvidenceProposal,
	input model.CreateLivePolicyLearningEvidenceInput,
) livePolicyLearningEvidenceProposal {
	legacyInput := model.CreateLivePolicyLearningCandidateInput{
		SourceLayer: input.SourceLayer,
		RoomID:      input.RoomID,
		FinalReply:  input.CorrectedReply,
		Feedback:    input.Feedback,
	}
	normalized := normalizeLivePolicyLearningModelOutput(livePolicyLearningModelOutput{
		AbsorbRecommended: proposal.AbsorbRecommended,
		TargetLayer:       proposal.TargetLayer,
		Reason:            proposal.Reason,
		Confidence:        proposal.Confidence,
		RuleTitle:         proposal.RuleTitle,
		RuleText:          proposal.RuleText,
		ExecutionMode:     proposal.ExecutionMode,
	}, legacyInput)
	proposal.AbsorbRecommended = normalized.AbsorbRecommended
	proposal.TargetLayer = normalized.TargetLayer
	proposal.Reason = normalized.Reason
	proposal.Confidence = normalized.Confidence
	proposal.RuleTitle = normalized.RuleTitle
	proposal.RuleText = normalized.RuleText
	proposal.ExecutionMode = normalized.ExecutionMode
	proposal.PromotionLevel = normalizeLearningPromotionLevel(proposal.PromotionLevel)
	proposal.RegressionCases = normalizeLearningRegressionCases(proposal.RegressionCases)
	return proposal
}

func (s *Server) livePolicyLearningCreateEvidence(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	var input model.CreateLivePolicyLearningEvidenceInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "学习证据格式错误")
		return
	}

	input.EvidenceType = normalizeLearningEvidenceType(input.EvidenceType)
	input.SourceRef = strings.TrimSpace(input.SourceRef)
	input.SourceLayer = strings.ToUpper(strings.TrimSpace(input.SourceLayer))
	input.IndustryCode = strings.ToLower(strings.TrimSpace(input.IndustryCode))
	input.Question = strings.TrimSpace(input.Question)
	input.ObservedReply = strings.TrimSpace(input.ObservedReply)
	input.CorrectedReply = strings.TrimSpace(input.CorrectedReply)
	input.Feedback = strings.TrimSpace(input.Feedback)
	input.LearningProvider = strings.TrimSpace(input.LearningProvider)
	input.LearningModel = strings.TrimSpace(input.LearningModel)
	input.History = normalizeLivePolicyLearningHistory(input.History)

	if input.SourceLayer == "" {
		if input.RoomID > 0 {
			input.SourceLayer = model.LivePolicyLayerL3
		} else {
			input.SourceLayer = model.LivePolicyLayerL1
		}
	}
	if input.SourceLayer != model.LivePolicyLayerL1 &&
		input.SourceLayer != model.LivePolicyLayerL2 &&
		input.SourceLayer != model.LivePolicyLayerL3 {
		writeError(w, http.StatusBadRequest, "来源层必须是规则层、行业层或用户层")
		return
	}
	if input.Question == "" && input.ObservedReply == "" &&
		input.CorrectedReply == "" && input.Feedback == "" {
		writeError(w, http.StatusBadRequest, "学习证据不能为空")
		return
	}
	if utf8.RuneCountInString(input.Question) > 4000 ||
		utf8.RuneCountInString(input.ObservedReply) > 6000 ||
		utf8.RuneCountInString(input.CorrectedReply) > 6000 ||
		utf8.RuneCountInString(input.Feedback) > 4000 ||
		utf8.RuneCountInString(input.SourceRef) > 512 {
		writeError(w, http.StatusBadRequest, "单次学习证据过长")
		return
	}

	var tenantID *int64
	if actor.Role == "customer" {
		if actor.TenantID == nil || input.RoomID <= 0 {
			writeError(w, http.StatusBadRequest, "客户学习必须指定自己的直播间")
			return
		}
		value := *actor.TenantID
		if _, err := s.getCoreRoomState(r.Context(), value, input.RoomID); err != nil {
			writeError(w, http.StatusNotFound, "直播间不存在或不属于当前终端")
			return
		}
		tenantID = &value
		input.SourceLayer = model.LivePolicyLayerL3
	} else {
		policyActor, viewOK := s.requireLivePolicyView(w, r)
		if !viewOK {
			return
		}
		actor = policyActor
	}

	prompt, err := buildLivePolicyLearningEvidencePrompt(input)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "准备学习证据失败")
		return
	}
	analysis, modelProvider, modelName, latencyMS, err := callPolicyLearningEvidenceModel(
		r.Context(), prompt, input.LearningProvider, input.LearningModel,
	)
	if err != nil {
		writeError(w, http.StatusBadGateway, "学习 Agent 暂时不可用，请稍后重试")
		return
	}

	items := make([]model.LivePolicyLearningCandidate, 0, len(analysis.Proposals))
	for index, proposal := range analysis.Proposals {
		proposal = normalizeLivePolicyLearningEvidenceProposal(proposal, input)
		if strings.TrimSpace(proposal.RuleText) == "" {
			continue
		}
		if proposal.TargetLayer == model.LivePolicyLayerL3 && input.RoomID <= 0 {
			proposal.Reason += " 当前没有绑定具体直播间，采纳到用户层前需要指定直播间。"
		}
		candidateInput := model.CreateLivePolicyLearningCandidateInput{
			SourceLayer:      input.SourceLayer,
			IndustryCode:     input.IndustryCode,
			RoomID:           input.RoomID,
			Question:         input.Question,
			ObservedReply:    input.ObservedReply,
			FinalReply:       input.CorrectedReply,
			Feedback:         input.Feedback,
			History:          input.History,
			EvidenceType:     input.EvidenceType,
			SourceRef:        input.SourceRef,
			LearningProvider: input.LearningProvider,
			LearningModel:    input.LearningModel,
		}
		item, createErr := s.store.CreateLivePolicyLearningCandidate(
			r.Context(),
			actor.UserID,
			tenantID,
			candidateInput,
			proposal.TargetLayer,
			proposal.Reason,
			proposal.RuleTitle,
			proposal.RuleText,
			proposal.ExecutionMode,
			modelProvider,
			modelName,
			proposal.AbsorbRecommended,
			proposal.Confidence,
			latencyMS,
			map[string]any{
				"summary":          analysis.Summary,
				"promotion_level":  proposal.PromotionLevel,
				"regression_cases": proposal.RegressionCases,
				"proposal_index":   index,
			},
		)
		if createErr != nil {
			writeError(w, http.StatusInternalServerError, "保存学习候选失败")
			return
		}
		items = append(items, item)
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"summary":        analysis.Summary,
		"items":          items,
		"model_provider": modelProvider,
		"model":          modelName,
		"latency_ms":     latencyMS,
	})
}

func (s *Server) livePolicyLearningListCandidates(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireLivePolicyView(w, r); !ok {
		return
	}
	status := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("status")))
	if status == "" {
		status = model.LivePolicyLearningStatusPending
	}
	switch status {
	case "all",
		model.LivePolicyLearningStatusPending,
		model.LivePolicyLearningStatusAdopted,
		model.LivePolicyLearningStatusRejected:
	default:
		writeError(w, http.StatusBadRequest, "无效的学习状态")
		return
	}
	items, err := s.store.ListLivePolicyLearningCandidates(r.Context(), status, 100)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取调教学习候选失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) livePolicyLearningRejectCandidate(w http.ResponseWriter, r *http.Request) {
	candidateID, ok := namedPathID(w, r, "candidateID", "学习候选")
	if !ok {
		return
	}
	candidate, err := s.store.GetLivePolicyLearningCandidate(r.Context(), candidateID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "学习候选不存在")
			return
		}
		writeError(w, http.StatusInternalServerError, "读取学习候选失败")
		return
	}
	requiredLayer := model.LivePolicyLayerL2
	if candidate.RecommendedLayer == model.LivePolicyLayerL1 {
		requiredLayer = model.LivePolicyLayerL1
	}
	actor, ok := s.requireLivePolicyLayerManage(w, r, requiredLayer)
	if !ok {
		return
	}
	var input model.RejectLivePolicyLearningCandidateInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "拒绝原因格式错误")
		return
	}
	item, err := s.store.RejectLivePolicyLearningCandidate(
		r.Context(),
		candidateID,
		actor.UserID,
		input.ReviewNote,
	)
	if err != nil {
		writeError(w, http.StatusConflict, "该学习候选已被处理")
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func preferredLearningPolicyVersion(ctx model.LivePolicyContext) *model.LivePolicyVersion {
	for index := range ctx.Versions {
		if ctx.Versions[index].LifecycleStatus == "draft" {
			value := ctx.Versions[index]
			return &value
		}
	}
	if ctx.Active != nil {
		value := *ctx.Active
		return &value
	}
	return nil
}

func (s *Server) adoptLearningToAdminLayer(
	w http.ResponseWriter,
	r *http.Request,
	candidate model.LivePolicyLearningCandidate,
	input model.AdoptLivePolicyLearningCandidateInput,
	targetLayer string,
) (*model.LivePolicyVersion, model.Actor, bool) {
	actor, ok := s.requireLivePolicyLayerManage(w, r, targetLayer)
	if !ok {
		return nil, model.Actor{}, false
	}
	industryCode := strings.ToLower(strings.TrimSpace(input.IndustryCode))
	if industryCode == "" {
		industryCode = strings.ToLower(strings.TrimSpace(candidate.IndustryCode))
	}
	if targetLayer == model.LivePolicyLayerL2 && industryCode == "" {
		industryCode = "general"
	}
	if _, err := s.store.EnsureLivePolicyScope(
		r.Context(), targetLayer, industryCode, 0, 0, actor.UserID, "",
	); err != nil {
		writeError(w, http.StatusInternalServerError, "初始化目标策略层失败")
		return nil, model.Actor{}, false
	}
	contextValue, err := s.store.GetLivePolicyContext(
		r.Context(), targetLayer, industryCode, 0, 0,
	)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusInternalServerError, "读取目标策略层失败")
		return nil, model.Actor{}, false
	}
	baseRules := []model.LivePolicyRule{}
	if err == nil {
		if base := preferredLearningPolicyVersion(contextValue); base != nil {
			baseRules = base.Rules
		}
	}
	mode := candidate.ExecutionMode
	if mode != model.LivePolicyModeVerbatim {
		mode = model.LivePolicyModeIntent
	}
	newRule := model.LivePolicyRule{
		Title:         candidate.RuleTitle,
		Text:          candidate.RuleText,
		ExecutionMode: mode,
		Enabled:       true,
	}
	if mode == model.LivePolicyModeVerbatim {
		newRule.FixedText = candidate.RuleText
	}
	rules := append(append([]model.LivePolicyRule{}, baseRules...), newRule)
	rules = policy.PrioritizeNewRules(
		policy.EnsureStableRuleKeys(targetLayer, rules, baseRules),
		baseRules,
	)
	var l1 *model.LivePolicyVersion
	if targetLayer == model.LivePolicyLayerL2 {
		l1, _ = s.store.GetActiveLivePolicyVersion(r.Context(), model.LivePolicyLayerL1, "", 0, 0)
	}
	draftInput := model.CreateLivePolicyDraftInput{
		Layer:        targetLayer,
		IndustryCode: industryCode,
		SourceText:   "调教学习候选 #" + fmt.Sprint(candidate.ID) + "：" + candidate.Feedback,
		Rules:        rules,
		Note:         "由调教学习中心采纳，原建议层：" + candidate.RecommendedLayer,
	}
	draftInput.Conflicts = policy.ValidateDraft(targetLayer, rules, nil, l1)
	item, err := s.store.CreateLivePolicyDraft(r.Context(), actor.UserID, draftInput)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "生成目标层草稿失败")
		return nil, model.Actor{}, false
	}
	return &item, actor, true
}

func (s *Server) adoptLearningToL3(
	w http.ResponseWriter,
	r *http.Request,
	candidate model.LivePolicyLearningCandidate,
	input model.AdoptLivePolicyLearningCandidateInput,
) (*model.LivePolicyVersion, model.Actor, bool) {
	roomID := input.RoomID
	if roomID <= 0 && candidate.RoomID != nil {
		roomID = *candidate.RoomID
	}
	if roomID <= 0 {
		writeError(w, http.StatusBadRequest, "采纳到用户层必须指定直播间")
		return nil, model.Actor{}, false
	}
	actor, tenantID, roomID, ok := s.requireCustomerPolicyRoomID(w, r, roomID)
	if !ok {
		return nil, model.Actor{}, false
	}
	if _, err := s.store.EnsureLivePolicyScope(
		r.Context(), model.LivePolicyLayerL3, "", tenantID, roomID, actor.UserID, "",
	); err != nil {
		writeError(w, http.StatusInternalServerError, "初始化直播间用户层失败")
		return nil, model.Actor{}, false
	}
	contextValue, err := s.store.GetLivePolicyContext(
		r.Context(), model.LivePolicyLayerL3, "", tenantID, roomID,
	)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusInternalServerError, "读取直播间用户层失败")
		return nil, model.Actor{}, false
	}
	baseOverrides := []model.LivePolicyOverride{}
	if err == nil {
		if base := preferredLearningPolicyVersion(contextValue); base != nil {
			baseOverrides = base.Overrides
		}
	}
	mode := candidate.ExecutionMode
	if mode != model.LivePolicyModeVerbatim {
		mode = model.LivePolicyModeIntent
	}
	override := model.LivePolicyOverride{
		Operation:     model.LivePolicyOverrideAdd,
		Title:         candidate.RuleTitle,
		Text:          candidate.RuleText,
		ExecutionMode: mode,
	}
	if mode == model.LivePolicyModeVerbatim {
		override.FixedText = candidate.RuleText
	}
	overrides := append(append([]model.LivePolicyOverride{}, baseOverrides...), override)
	overrides = policy.EnsureStableOverrideKeys(overrides, baseOverrides)
	_, l1, _, _, err := s.store.LoadLivePolicyLayers(r.Context(), tenantID, roomID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取直播间上层策略失败")
		return nil, model.Actor{}, false
	}
	draftInput := model.CreateLivePolicyDraftInput{
		Layer:      model.LivePolicyLayerL3,
		TenantID:   tenantID,
		RoomID:     roomID,
		SourceText: "调教学习候选 #" + fmt.Sprint(candidate.ID) + "：" + candidate.Feedback,
		Overrides:  overrides,
		Note:       "由调教学习中心采纳",
	}
	draftInput.Conflicts = policy.ValidateDraft(model.LivePolicyLayerL3, nil, overrides, l1)
	item, err := s.store.CreateLivePolicyDraft(r.Context(), actor.UserID, draftInput)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "生成直播间用户层草稿失败")
		return nil, model.Actor{}, false
	}
	if actor.IsInternalStaff() {
		_ = s.store.RegisterLiveSupportConfigVersion(
			r.Context(),
			item.ID,
			tenantID,
			roomID,
			actor.UserID,
			model.LiveSupportCapabilityL3Policy,
		)
	}
	return &item, actor, true
}

func (s *Server) livePolicyLearningAdoptCandidate(w http.ResponseWriter, r *http.Request) {
	candidateID, ok := namedPathID(w, r, "candidateID", "学习候选")
	if !ok {
		return
	}
	candidate, err := s.store.GetLivePolicyLearningCandidate(r.Context(), candidateID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "学习候选不存在")
			return
		}
		writeError(w, http.StatusInternalServerError, "读取学习候选失败")
		return
	}
	if candidate.Status != model.LivePolicyLearningStatusPending {
		writeError(w, http.StatusConflict, "该学习候选已被处理")
		return
	}
	var input model.AdoptLivePolicyLearningCandidateInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "采纳参数格式错误")
		return
	}
	targetLayer := strings.ToUpper(strings.TrimSpace(input.TargetLayer))
	if targetLayer == "" {
		targetLayer = candidate.RecommendedLayer
	}
	if targetLayer != model.LivePolicyLayerL1 &&
		targetLayer != model.LivePolicyLayerL2 &&
		targetLayer != model.LivePolicyLayerL3 {
		writeError(w, http.StatusBadRequest, "目标层必须是规则层、行业层或用户层")
		return
	}
	if strings.TrimSpace(candidate.RuleText) == "" {
		writeError(w, http.StatusBadRequest, "学习候选没有可采纳的规则正文")
		return
	}

	var version *model.LivePolicyVersion
	var actor model.Actor
	var adopted bool
	if targetLayer == model.LivePolicyLayerL3 {
		version, actor, adopted = s.adoptLearningToL3(w, r, candidate, input)
	} else {
		version, actor, adopted = s.adoptLearningToAdminLayer(w, r, candidate, input, targetLayer)
	}
	if !adopted || version == nil {
		return
	}
	item, err := s.store.MarkLivePolicyLearningCandidateAdopted(
		r.Context(),
		candidate.ID,
		actor.UserID,
		version.ID,
		targetLayer,
		input.ReviewNote,
	)
	if err != nil {
		writeError(w, http.StatusConflict, "草稿已生成，但学习候选状态更新失败，请刷新确认")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"candidate": item,
		"draft":     version,
	})
}
