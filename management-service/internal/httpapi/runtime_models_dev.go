package httpapi

import (
	"net/http"
	"sort"
	"strings"

	"livecompanion/management/internal/agentgateway"
)

func (s *Server) devRuntimeAgentModels(w http.ResponseWriter, r *http.Request) {
	if !strings.EqualFold(strings.TrimSpace(s.env), "development") {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	provider := strings.TrimSpace(r.URL.Query().Get("provider"))
	if provider == "" {
		provider = agentgateway.ProviderQwen
	}
	items, err := agentgateway.NewFromEnv().ListModels(r.Context(), provider)
	if err != nil {
		writeError(w, http.StatusBadGateway, "读取当前账户模型目录失败："+err.Error())
		return
	}
	sort.Slice(items, func(i, j int) bool {
		return strings.ToLower(items[i].ID) < strings.ToLower(items[j].ID)
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"provider": provider,
		"items":    items,
	})
}
