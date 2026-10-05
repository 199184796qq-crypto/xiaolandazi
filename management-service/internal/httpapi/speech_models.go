package httpapi

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"livecompanion/management/internal/agentgateway"
)

func (s *Server) speechGateway() *agentgateway.Gateway {
	return agentgateway.NewFromEnv().WithSpeechModels(s.store)
}

func speechGenerationGateway(gateways []anchorStyleCompleter) anchorStyleCompleter {
	if len(gateways) > 0 && gateways[0] != nil {
		return gateways[0]
	}
	return agentgateway.NewFromEnv()
}

func (s *Server) systemSpeechModelsGet(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if _, ok := s.requireAgentPromptAdmin(w, r); !ok {
		return
	}
	config, err := s.store.SpeechModels(r.Context())
	if err != nil {
		writeError(w, 500, "读取主播模型配置失败")
		return
	}
	writeJSON(w, 200, map[string]any{"config": config, "key_storage_ready": len(os.Getenv("MODEL_CONFIG_ENCRYPTION_KEY")) >= 32})
}

func (s *Server) systemSpeechModelsSave(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	actor, ok := s.requireAgentPromptAdmin(w, r)
	if !ok {
		return
	}
	var input agentgateway.SpeechModelsInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, 400, "模型配置格式错误")
		return
	}
	if err := s.store.SaveSpeechModels(r.Context(), input, actor.UserID); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	config, err := s.store.SpeechModels(r.Context())
	if err != nil {
		writeError(w, 500, "已保存，但读取失败，请刷新")
		return
	}
	writeJSON(w, 200, map[string]any{"config": config})
}

type speechModelTestRequest struct {
	Profile  agentgateway.SpeechModelInput `json:"profile"`
	Mode     string                        `json:"mode"`
	Question string                        `json:"question"`
}

func (s *Server) speechProfileWithKey(ctx context.Context, input agentgateway.SpeechModelInput) (agentgateway.SpeechModel, error) {
	p := input.SpeechModel
	p.APIKey = strings.TrimSpace(input.NewAPIKey)
	if len(p.APIKey) > 4096 {
		return p, errors.New("API 密钥过长")
	}
	if p.APIKey != "" {
		return p, nil
	}
	if !agentgateway.ValidSpeechProfileID(p.ID) {
		return p, errors.New("请输入 API 密钥，或先保存模型配置")
	}
	stored, err := s.store.ResolveSpeechModel(ctx, p.ID)
	if err != nil || stored == nil {
		return p, errors.New("请输入 API 密钥，或先保存模型配置")
	}
	if stored.BaseURL != p.BaseURL || stored.Protocol != p.Protocol {
		return p, errors.New("API 地址或协议改变后，请重新填写密钥")
	}
	p.APIKey = stored.APIKey
	return p, nil
}

func (s *Server) systemSpeechModelsList(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if _, ok := s.requireAgentPromptAdmin(w, r); !ok {
		return
	}
	var input struct {
		Profile agentgateway.SpeechModelInput `json:"profile"`
	}
	if readJSON(w, r, &input) != nil {
		writeError(w, 400, "模型连接格式错误")
		return
	}
	if err := agentgateway.ValidateSpeechConnection(input.Profile.SpeechModel); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	p, err := s.speechProfileWithKey(r.Context(), input.Profile)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	list, err := agentgateway.ListSpeechEndpointModels(r.Context(), p)
	if err != nil {
		writeError(w, 502, err.Error())
		return
	}
	writeJSON(w, 200, list)
}

func (s *Server) systemSpeechModelsTest(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if _, ok := s.requireAgentPromptAdmin(w, r); !ok {
		return
	}
	var input speechModelTestRequest
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, 400, "测试请求格式错误")
		return
	}
	p := input.Profile.SpeechModel
	if err := agentgateway.ValidateSpeechModel(p); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	p, err := s.speechProfileWithKey(r.Context(), input.Profile)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	if input.Mode != "connectivity" && input.Mode != "direct" && input.Mode != "prompt" {
		writeError(w, 400, "请选择连通性、直接提问或提示词测试")
		return
	}
	question := strings.TrimSpace(input.Question)
	maxTokens := p.MaxTokens
	timeout := time.Duration(p.TimeoutMS) * time.Millisecond
	if input.Mode == "connectivity" {
		question = "只回复 OK"
		maxTokens = 64
		if timeout > 20*time.Second {
			timeout = 20 * time.Second
		}
	}
	if question == "" || utf8.RuneCountInString(question) > 30000 {
		writeError(w, 400, "测试问题或提示词须为 1—30000 字")
		return
	}
	messages := []agentgateway.Message{}
	if input.Mode == "prompt" && strings.TrimSpace(p.SystemPrompt) != "" {
		messages = append(messages, agentgateway.Message{Role: "system", Content: p.SystemPrompt})
	}
	messages = append(messages, agentgateway.Message{Role: "user", Content: question})
	result, err := agentgateway.CompleteSpeechEndpoint(r.Context(), p, agentgateway.Request{Model: p.Model, Messages: messages, MaxTokens: maxTokens, Timeout: timeout})
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"text": result.Text, "model": p.Model, "protocol": p.Protocol, "latency_ms": result.LatencyMS, "input_tokens": result.InputTokens, "output_tokens": result.OutputTokens, "total_tokens": result.TotalTokens})
}
