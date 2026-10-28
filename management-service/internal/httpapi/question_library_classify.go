package httpapi

import (
	"context"
	"net/http"
	"strings"

	"livecompanion/management/internal/agentgateway"
)

var questionTypeOptions = []string{"pool_in_out", "fact", "yes_no", "number", "list", "process", "compare", "unknown", "social"}

const questionTypeSystemPrompt = `你是直播问答的问题分类器。把观众问题归到以下类型之一，只输出 JSON：{"type":"类型","reason":"一句话"}。
类型：
- pool_in_out：是否属于某个集合（如某地区能否发货、是否包邮、是否抽真空）
- fact：单一属性事实（价格、规格、成分、保质期、快递公司）
- yes_no：能不能、可不可以（开发票、指定时间送）
- number：多久、几天、多重、几个人份
- list：有哪些口味/规格/包装
- process：怎么退、怎么加热、怎么下单
- compare：哪个好、有什么区别
- unknown：知识库没有、无法回答
- social：夸赞、抱怨、闲聊`

// classifyQuestionLibrary asks the model to suggest a question type.
func (s *Server) classifyQuestionLibrary(w http.ResponseWriter, r *http.Request) {
	_, _, _, ok := s.planAssetScope(w, r)
	if !ok {
		return
	}

	var input struct {
		Question string `json:"question"`
	}
	if err := readJSON(w, r, &input); err != nil || strings.TrimSpace(input.Question) == "" {
		writeError(w, http.StatusBadRequest, "缺少问题")
		return
	}
	gateway := s.agentGateway
	if gateway == nil {
		gateway = agentgateway.NewFromContext(r.Context())
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*1e9)
	defer cancel()
	result, err := gateway.Complete(ctx, agentgateway.Request{
		Stage:          "interaction_question_type",
		Messages:       []agentgateway.Message{{Role: "system", Content: questionTypeSystemPrompt}, {Role: "user", Content: strings.TrimSpace(input.Question)}},
		ResponseFormat: agentgateway.ResponseJSON,
		EnableThinking: false,
		MaxTokens:      60,
		Timeout:        12 * 1e9,
	})
	if err != nil {
		writeError(w, http.StatusBadGateway, "问题分类失败")
		return
	}
	questionType := parseQuestionType(result.Text)
	writeJSON(w, http.StatusOK, map[string]any{"type": questionType, "types": questionTypeOptions})
}

func parseQuestionType(raw string) string {
	raw = strings.ToLower(raw)
	for _, option := range questionTypeOptions {
		if strings.Contains(raw, `"`+option+`"`) || strings.Contains(raw, "\""+option+"\"") {
			return option
		}
	}
	return "fact"
}
