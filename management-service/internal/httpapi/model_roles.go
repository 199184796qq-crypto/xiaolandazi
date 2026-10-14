package httpapi

import (
	"context"
	"crypto/subtle"
	"livecompanion/management/internal/agentgateway"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type pinnedSpeechProfileKey struct{}

func withPinnedSpeechProfile(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, pinnedSpeechProfileKey{}, id)
}

func (s *Server) modelResolverMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.store != nil {
			r = r.WithContext(agentgateway.WithModelResolver(r.Context(), s.store))
			// Path/query values select a room only after actor ownership is checked.
			// Administrative model-pool endpoints never accept a customer room scope.
			parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
			if len(parts) > 3 && parts[0] == "api" && parts[1] == "v1" && parts[2] == "rooms" {
				room, _ := strconv.ParseInt(parts[3], 10, 64)
				if actor, err := s.auth.Resolve(r); err == nil && actor.TenantID != nil && room > 0 {
					r = r.WithContext(agentgateway.WithRoomModelScope(r.Context(), agentgateway.RoomModelScope{TenantID: *actor.TenantID, RoomID: room}))
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

// Core transcribes and segments recordings; the management gateway owns the
// selected analysis connection and credentials. Only text crosses this route.
func (s *Server) internalModelAnalysis(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if s.purposeInternalToken == "" || subtle.ConstantTimeCompare([]byte(token), []byte(s.purposeInternalToken)) != 1 {
		writeError(w, 401, "invalid internal token")
		return
	}
	var input struct {
		Model          string `json:"model"`
		Stream         bool   `json:"stream"`
		EnableThinking bool   `json:"enable_thinking"`
		Messages       []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
		MaxTokens int `json:"max_tokens"`
	}
	if readJSON(w, r, &input) != nil || input.Stream || len(input.Messages) == 0 || len(input.Messages) > 30 || input.MaxTokens < 1 || input.MaxTokens > 32000 {
		writeError(w, 400, "invalid analysis request")
		return
	}
	messages := make([]agentgateway.Message, 0, len(input.Messages))
	for _, m := range input.Messages {
		if m.Role != "system" && m.Role != "user" && m.Role != "assistant" {
			writeError(w, 400, "invalid message role")
			return
		}
		messages = append(messages, agentgateway.Message{Role: m.Role, Content: m.Content})
	}
	result, err := s.speechGateway().Complete(r.Context(), agentgateway.Request{Stage: "speech_analysis", Messages: messages, MaxTokens: input.MaxTokens, EnableThinking: false, ResponseFormat: agentgateway.ResponseText, Timeout: 180 * time.Second})
	if err != nil {
		writeError(w, 502, "系统智能体分析请求失败")
		return
	}
	writeJSON(w, 200, map[string]any{"model": result.Model, "choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": result.Text}}}, "usage": map[string]any{"prompt_tokens": result.InputTokens, "completion_tokens": result.OutputTokens, "total_tokens": result.TotalTokens}})
}
