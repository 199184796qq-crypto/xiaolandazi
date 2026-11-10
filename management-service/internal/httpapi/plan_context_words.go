package httpapi

import (
	"encoding/json"
	"errors"
	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/contextword"
	"livecompanion/management/internal/db"
	"livecompanion/management/internal/interactionpolicy"
	"net/http"
	"strings"
)

func (s *Server) planContextWords(w http.ResponseWriter, r *http.Request) {
	actor, plan, tenant, ok := s.resolvePlanMaterialClaimScope(w, r)
	if !ok {
		return
	}
	if r.Method == http.MethodGet {
		out, err := s.store.PlanContextWords(r.Context(), tenant, plan)
		if err != nil {
			writeError(w, 500, "读取语境词失败")
			return
		}
		writeJSON(w, 200, out)
		return
	}
	var input contextword.Catalog
	if err := readJSON(w, r, &input); err != nil {
		return
	}
	items, err := contextword.Normalize(input.Items)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	input.Items = items
	out, err := s.store.SavePlanContextWords(r.Context(), tenant, plan, actor.UserID, input)
	if errors.Is(err, db.ErrContextWordsConflict) {
		writeError(w, 409, err.Error())
		return
	}
	if err != nil {
		writeError(w, 500, "保存失败，原语境词保留")
		return
	}
	writeJSON(w, 200, out)
}
func (s *Server) organizeContextWords(w http.ResponseWriter, r *http.Request) {
	_, _, _, ok := s.resolvePlanMaterialClaimScope(w, r)
	if !ok {
		return
	}
	var input struct {
		Text string `json:"text"`
	}
	if err := readJSON(w, r, &input); err != nil {
		return
	}
	input.Text = strings.TrimSpace(input.Text)
	if input.Text == "" || len([]rune(input.Text)) > 8000 {
		writeError(w, 400, "请输入1至8000字的词义说明")
		return
	}
	settings, err := s.store.AnchorGenerationSettings(r.Context())
	if err != nil {
		writeError(w, 500, "读取审核模型失败")
		return
	}
	profile := settings.Cycle.TextProfileID
	if profile == "" {
		profile = settings.SafetyProfileID
	}
	if profile == "" {
		profile = settings.SellingPointProfileID
	}
	if profile == "" {
		writeError(w, 400, "请先配置审核模型")
		return
	}
	prompt, err := interactionpolicy.Read("context-words-organize.md")
	if err != nil {
		writeError(w, 500, "读取语境词整理规则失败")
		return
	}
	result, _, err := s.completeAnchorStyleWorkbench(r.Context(), profile, "context_words_organize", agentgateway.ResponseJSON, []agentgateway.Message{{Role: "system", Content: string(prompt)}, {Role: "user", Content: input.Text}}, 6000, false)
	if err != nil {
		writeError(w, 502, "整理失败，输入已保留，请重试")
		return
	}
	if strings.EqualFold(result.FinishReason, "length") {
		writeError(w, 502, "整理结果被截断，请分批整理")
		return
	}
	items, questions, err := parseContextWordSuggestions(result.Text)
	if err != nil {
		writeError(w, 502, "整理结果不完整，请手工补充或重试")
		return
	}
	writeJSON(w, 200, map[string]any{"items": items, "questions": questions})
}
func parseContextWordSuggestions(text string) ([]contextword.Entry, []string, error) {
	raw, err := parseLooseJSONValue(text)
	if err != nil {
		return nil, nil, err
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return nil, nil, err
	}
	var out struct {
		Items     []contextword.Entry `json:"items"`
		Questions []string            `json:"questions"`
	}
	if err = json.Unmarshal(data, &out); err != nil {
		return nil, nil, err
	}
	if len(out.Items) > 50 {
		return nil, nil, errors.New("请分批整理")
	}
	for i := range out.Items {
		out.Items[i].ID = ""
		out.Items[i].Enabled = true
	}
	items, err := contextword.Normalize(out.Items)
	if len(items) == 0 && len(out.Questions) == 0 {
		err = errors.New("无可确认词义")
	}
	return items, out.Questions, err
}
