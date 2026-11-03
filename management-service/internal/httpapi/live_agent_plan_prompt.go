package httpapi

import (
	"errors"
	"net/http"
	"strings"
	"sync"
	"unicode/utf8"

	"livecompanion/management/internal/db"
	"livecompanion/management/internal/planprompt"
)

func (s *Server) liveAgentPlanPromptGet(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	planID, ok := liveAgentPlanPathID(w, r)
	if !ok {
		return
	}
	tenantID, ok := s.resolveLiveAgentPlanTenant(w, r, actor, 0, false)
	if !ok {
		return
	}
	document, err := s.store.GetLiveAgentPlanPromptDocument(r.Context(), tenantID, planID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "方案提示词读取失败")
		return
	}
	planning, err := resolvePlanRewritePlanning(document)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	document.RewritePlanningMD = &planning
	writeJSON(w, http.StatusOK, document)
}

func (s *Server) liveAgentPlanPromptPut(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	planID, ok := liveAgentPlanPathID(w, r)
	if !ok {
		return
	}
	tenantID, ok := s.resolveLiveAgentPlanTenant(w, r, actor, 0, true)
	if !ok {
		return
	}
	if !s.allowPlanSave(w, r, tenantID, planID) {
		return
	}
	var input struct {
		PromptMD          string               `json:"prompt_md"`
		Sections          *planprompt.Sections `json:"sections"`
		RewritePromptMD   *string              `json:"rewrite_prompt_md"`
		RewritePlanningMD *string              `json:"rewrite_planning_md"`
	}
	if readJSON(w, r, &input) != nil {
		writeError(w, http.StatusBadRequest, "方案提示词请求格式错误")
		return
	}
	if input.Sections == nil {
		// A legacy client has one original field, not four. Preserve that document
		// without guessing which page each of its headings belongs to.
		prompt := stripPlanPromptHints(input.PromptMD)
		if utf8.RuneCountInString(prompt) > planprompt.MaxPromptRunes {
			writeError(w, http.StatusBadRequest, "方案提示词不能超过20000字")
			return
		}
		if _, err := s.store.SaveLiveAgentPlanPrompt(r.Context(), tenantID, planID, actor.UserID, prompt); err != nil {
			writeError(w, http.StatusInternalServerError, "方案提示词保存失败")
			return
		}
		document, err := s.store.GetLiveAgentPlanPromptDocument(r.Context(), tenantID, planID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "方案提示词读取失败")
			return
		}
		writeJSON(w, http.StatusOK, document)
		return
	}
	sections := *input.Sections
	if err := sections.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	planning := input.RewritePlanningMD
	if planning == nil {
		previous, err := s.store.GetLiveAgentPlanPromptDocument(r.Context(), tenantID, planID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "方案提示词读取失败，当前内容未保存")
			return
		}
		// Older clients may submit the former editable full prompt. Extract only
		// a recognized envelope; omitted fields preserve the current planning.
		if input.RewritePromptMD != nil {
			previous.RewritePromptMD = *input.RewritePromptMD
			previous.RewritePlanningMD = nil
			if value, ok := planprompt.LegacyRewritePlanning(*input.RewritePromptMD, previous.Sections); ok {
				previous.RewritePlanningMD = &value
			} else if value, ok := planprompt.LegacyRewritePlanning(*input.RewritePromptMD, sections); ok {
				previous.RewritePlanningMD = &value
			}
			if *input.RewritePromptMD == "" {
				previous.RewritePlanningMD = input.RewritePromptMD
			}
		}
		value, err := resolvePlanRewritePlanning(previous)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		planning = &value
	}
	if utf8.RuneCountInString(*planning) > planprompt.MaxPromptRunes {
		writeError(w, http.StatusBadRequest, "话术规划不能超过20000字，请缩短内容")
		return
	}
	if utf8.RuneCountInString(planprompt.CompileRewritePlanning(sections, *planning)) > planprompt.MaxPromptRunes {
		writeError(w, http.StatusBadRequest, "合成后的话术修改元提示词不能超过20000字，请缩短话术规划或方案资料")
		return
	}
	document, err := s.store.SaveLiveAgentPlanPromptPlanning(r.Context(), tenantID, planID, actor.UserID, sections, *planning)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "方案提示词保存失败")
		return
	}
	writeJSON(w, http.StatusOK, document)
}

func resolvePlanRewritePlanning(document db.LiveAgentPlanPromptDocument) (string, error) {
	if document.RewritePlanningMD != nil {
		return *document.RewritePlanningMD, nil
	}
	if document.RewritePromptMD != "" {
		if planning, ok := planprompt.LegacyRewritePlanning(document.RewritePromptMD, document.Sections); ok {
			return planning, nil
		}
		return "", errors.New("旧版话术修改元提示词无法安全拆出话术规划，原内容已保留，请先核对旧版提示词")
	}
	return defaultPlanRewritePlanning()
}

func defaultPlanRewritePlanning() (string, error) {
	master, err := loadPlanRewriteMaster()
	if err != nil {
		return "", errors.New("话术规划模板读取失败，当前内容已保留")
	}
	planning, ok := planprompt.LegacyRewritePlanning(master, planprompt.Sections{
		PersonaMD: "{{主播人设}}", FactsMD: "{{事实依据}}", RedLinesMD: "{{红线禁止}}",
	})
	if !ok || !strings.HasPrefix(master, planprompt.RewriteIntro) || strings.TrimSpace(planning) == "" {
		return "", errors.New("话术规划模板格式不正确，当前内容已保留")
	}
	return planning, nil
}

// stripPlanPromptHints 仅用于旧单字段客户端。四分区原文原样保存，提示行
// 只在编译派生副本时过滤，不能从可编辑原文中删除。
func stripPlanPromptHints(text string) string {
	if !strings.Contains(text, "〔提示〕") {
		return text
	}
	lines := strings.Split(text, "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "〔提示〕") {
			continue
		}
		kept = append(kept, line)
	}
	out := strings.Join(kept, "\n")
	for strings.Contains(out, "\n\n\n") {
		out = strings.ReplaceAll(out, "\n\n\n", "\n\n")
	}
	return strings.TrimSpace(out)
}

// planRewriteMaster 读取改写元提示词母版；进程内只读一次，改母版后重启生效。
var planRewriteMasterOnce sync.Once
var planRewriteMasterMD string
var planRewriteMasterErr error

func loadPlanRewriteMaster() (string, error) {
	planRewriteMasterOnce.Do(func() {
		md, _, err := loadReplaceablePromptAsset("PLAN_REWRITE_MASTER_PATH", "configs/prompts/anchor-style/rewrite-master.md", "话术改写母版")
		planRewriteMasterMD, planRewriteMasterErr = md, err
	})
	return planRewriteMasterMD, planRewriteMasterErr
}

// renderPlanRewritePrompt 用母版和方案内容生成方案专属的改写元提示词。
//
// 样本话术保留占位符：真正改写的每一条由调用方在请求时填进 {{样本话术}}，
// 这样同一个方案所有片段共用同一份指令，只有样本不同。
func renderPlanRewritePrompt(sections planprompt.Sections) (string, error) {
	planning, err := defaultPlanRewritePlanning()
	if err != nil {
		return "", err
	}
	return planprompt.CompileRewritePlanning(sections, planning), nil
}
