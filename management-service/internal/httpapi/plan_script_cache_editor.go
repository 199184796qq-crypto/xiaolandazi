package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/scriptchain"
)

type planScriptCacheInput struct {
	GroupID    string               `json:"group_id"`
	EntryID    string               `json:"entry_id"`
	Sample     string               `json:"sample"`
	Basis      string               `json:"basis,omitempty"`
	Text       string               `json:"text,omitempty"`
	Mode       scriptGenerationMode `json:"mode,omitempty"`
	SpecPrompt string               `json:"spec_prompt,omitempty"`
}

type planScriptCacheRequest struct {
	input    planScriptCacheInput
	tenantID int64
	planID   int64
}

type planScriptCacheContext struct {
	scriptTextMaterial
	basis string
}

type planScriptCacheError struct {
	status  int
	message string
}

func (e *planScriptCacheError) Error() string { return e.message }

func planScriptCacheUnit(input planScriptCacheInput) volcengineScriptUnit {
	return volcengineScriptUnit{
		Text:       input.Sample,
		Key:        scriptchain.ContentHash(input.Sample),
		Pool:       scriptEntryPoolID(recordingScriptTab{TabID: input.GroupID}, 0, input.EntryID),
		SpecPrompt: input.SpecPrompt,
	}
}

func newPlanScriptCacheContext(tenant, plan int64, input planScriptCacheInput, facts json.RawMessage, redLines, speechRequirements, persona string) planScriptCacheContext {
	snapshot := newScriptRewriteSnapshot(redLines, facts)
	v := withScriptRewriteSnapshot(model.LiveAgentPlanVersion{TenantID: tenant, PlanID: plan}, snapshot)
	unit := planScriptCacheUnit(input)
	basis, _ := json.Marshal([]string{scriptRewriteFactsKey(v), unit.poolID(), unit.Key, strings.TrimSpace(speechRequirements), strings.TrimSpace(persona)})
	hash := sha256.Sum256(basis)
	redLines, _ = backgroundRewriteRedLines(snapshot)
	return planScriptCacheContext{
		scriptTextMaterial: scriptTextMaterial{
			version: v, unit: unit, facts: facts, redLines: redLines, speechRequirements: speechRequirements, persona: persona,
		},
		basis: hex.EncodeToString(hash[:]),
	}
}

func (s *Server) readPlanScriptCacheRequest(w http.ResponseWriter, r *http.Request) (planScriptCacheRequest, bool) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return planScriptCacheRequest{}, false
	}
	plan, ok := liveAgentPlanPathID(w, r)
	if !ok {
		return planScriptCacheRequest{}, false
	}
	tenant, ok := s.resolveLiveAgentPlanTenant(w, r, actor, requestTenantID(r), true)
	if !ok {
		return planScriptCacheRequest{}, false
	}
	var input planScriptCacheInput
	if err := readJSONWithLimit(w, r, &input, 128<<10); err != nil ||
		!recordingScriptTabIDPattern.MatchString(input.GroupID) || !recordingScriptTabIDPattern.MatchString(input.EntryID) {
		writeError(w, http.StatusBadRequest, "需要提供有效的话术组和话术条目")
		return planScriptCacheRequest{}, false
	}
	input.Sample = singleParagraphRecordingScriptEntry(input.Sample)
	if n := utf8.RuneCountInString(input.Sample); n < recordingScriptMinTextRunes || n > recordingScriptMaxTextRunes {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("母版话术需在 %d 至 %d 字符之间", recordingScriptMinTextRunes, recordingScriptMaxTextRunes))
		return planScriptCacheRequest{}, false
	}
	input.SpecPrompt = strings.TrimSpace(input.SpecPrompt)
	if input.Mode == scriptGenerationSpecified &&
		(input.SpecPrompt == "" || utf8.RuneCountInString(input.SpecPrompt) > recordingScriptMaxPromptRunes) {
		writeError(w, http.StatusBadRequest, "请填写 1 到 4000 字的指定提示词")
		return planScriptCacheRequest{}, false
	}
	if _, err := s.store.GetLiveAgentPlan(r.Context(), tenant, plan); err != nil {
		writeError(w, http.StatusNotFound, "方案不存在或无权查看")
		return planScriptCacheRequest{}, false
	}
	return planScriptCacheRequest{input: input, tenantID: tenant, planID: plan}, true
}

func (s *Server) loadPlanScriptCacheContext(ctx context.Context, request planScriptCacheRequest) (planScriptCacheContext, error) {
	input, tenant, plan := request.input, request.tenantID, request.planID
	if input.Mode == scriptGenerationSpecified {
		facts, err := s.resolveScriptPieceFacts(ctx, tenant, plan, input.SpecPrompt)
		if err != nil {
			return planScriptCacheContext{}, err
		}
		sections, err := s.store.GetLiveAgentPlanInteractionSections(ctx, tenant, plan)
		if err != nil {
			return planScriptCacheContext{}, &planScriptCacheError{http.StatusServiceUnavailable, "读取红线禁止失败，请稍后重试"}
		}
		return newPlanScriptCacheContext(tenant, plan, input, facts, sections.RedLinesMD, "", ""), nil
	}
	facts, err := s.rewriteFactsJSON(ctx, tenant, plan)
	if err != nil {
		log.Printf("script cache editor facts unavailable tenant=%d plan=%d: %v", tenant, plan, err)
		return planScriptCacheContext{}, &planScriptCacheError{http.StatusServiceUnavailable, "读取原子事实失败，请稍后重试"}
	}
	if len(facts) == 0 || string(facts) == "[]" {
		return planScriptCacheContext{}, &planScriptCacheError{http.StatusConflict, "请先在资料导入中确认入库，再生成缓存话术"}
	}
	sections, err := s.store.GetLiveAgentPlanInteractionSections(ctx, tenant, plan)
	if err != nil {
		return planScriptCacheContext{}, &planScriptCacheError{http.StatusServiceUnavailable, "读取红线禁止失败，请稍后重试"}
	}
	doc, err := s.store.GetLiveAgentPlanPromptDocument(ctx, tenant, plan)
	if err != nil {
		return planScriptCacheContext{}, &planScriptCacheError{http.StatusServiceUnavailable, "读取话术要求失败，请稍后重试"}
	}
	return newPlanScriptCacheContext(tenant, plan, input, facts, sections.RedLinesMD, doc.Sections.SpeechRequirementsMD, doc.Sections.PersonaMD), nil
}

func (s *Server) previewPlanScriptCache(w http.ResponseWriter, r *http.Request) {
	s.previewPlanScriptText(w, r, scriptGenerationDefault)
}

// Older page sessions can still send a request to the original polish URL.
func (s *Server) previewPlanScriptPolish(w http.ResponseWriter, r *http.Request) {
	s.previewPlanScriptText(w, r, scriptGenerationPolish)
}

func (s *Server) previewPlanScriptText(w http.ResponseWriter, r *http.Request, mode scriptGenerationMode) {
	request, ok := s.readPlanScriptCacheRequest(w, r)
	if !ok {
		return
	}
	if mode == scriptGenerationDefault {
		mode = request.input.Mode
	}
	material := scriptTextMaterial{
		version: model.LiveAgentPlanVersion{TenantID: request.tenantID, PlanID: request.planID},
		unit:    planScriptCacheUnit(request.input),
	}
	s.writePlanScriptTextPreview(w, r, mode, material, func() (planScriptCacheContext, error) {
		return s.loadPlanScriptCacheContext(r.Context(), request)
	})
}

func (s *Server) writePlanScriptTextPreview(w http.ResponseWriter, r *http.Request, mode scriptGenerationMode, material scriptTextMaterial, load func() (planScriptCacheContext, error)) {
	load = sync.OnceValues(load)
	var stream *anchorPreviewStream
	var started bool
	tenant, plan := material.version.TenantID, material.version.PlanID
	ctx, cancel := context.WithTimeout(r.Context(), 270*time.Second)
	defer cancel()
	ctx = withExchangeScope(ctx, appdb.ModelExchangeScope{TenantID: tenant, PlanID: plan})
	result, err := s.generateScriptText(ctx, mode, scriptTextGenerationInput{
		material: material,
		loadMaterial: func() (scriptTextMaterial, error) {
			cfg, err := load()
			return cfg.scriptTextMaterial, err
		},
		progress: func(message string) {
			if !started {
				started = true
				stream = openAnchorPreviewStream(w, r)
			}
			stream.send("progress", map[string]string{"message": message})
		},
	})
	if stream != nil {
		w = stream.resultWriter()
	}
	if err == nil {
		err = ctx.Err()
	}
	if err != nil {
		scriptPreviewError(w, err, tenant, plan, material.unit.poolID())
		return
	}
	if mode == scriptGenerationSpecified {
		writeJSON(w, http.StatusOK, map[string]string{"text": result.Script})
		return
	}
	// The editor owns the save basis, even for a strategy that writes from the sample alone.
	cfg, err := load()
	if err != nil {
		scriptPreviewError(w, err, tenant, plan, material.unit.poolID())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"text": result.Script, "basis": cfg.basis})
}

func scriptPreviewError(w http.ResponseWriter, err error, tenant, plan int64, piece string) {
	var loadError *planScriptCacheError
	var templateError *scriptPieceTemplateError
	switch {
	case errors.As(err, &loadError):
		writeError(w, loadError.status, loadError.message)
	case errors.As(err, &templateError):
		writeError(w, http.StatusUnprocessableEntity, templateError.Error())
	case errors.Is(err, errUnknownScriptGenerationMode):
		writeError(w, http.StatusBadRequest, errUnknownScriptGenerationMode.Error())
	case errors.Is(err, errScriptPolishUnavailable):
		writeError(w, http.StatusNotImplemented, errScriptPolishUnavailable.Error())
	case errors.Is(err, errScriptAnalysisFailed):
		log.Printf("script cache editor analysis failed tenant=%d plan=%d piece=%s: %v", tenant, plan, piece, err)
		writeError(w, http.StatusBadGateway, errScriptAnalysisFailed.Error())
	case errors.Is(err, errScriptAnalysisNoChain):
		writeError(w, http.StatusBadGateway, errScriptAnalysisNoChain.Error())
	default:
		log.Printf("script cache editor rewrite failed tenant=%d plan=%d piece=%s: %v", tenant, plan, piece, err)
		writeError(w, http.StatusBadGateway, "仿写话术失败，请重试")
	}
}

func (s *Server) savePlanScriptCache(w http.ResponseWriter, r *http.Request) {
	request, ok := s.readPlanScriptCacheRequest(w, r)
	if !ok {
		return
	}
	if request.input.Mode == scriptGenerationSpecified {
		writeError(w, http.StatusBadRequest, "指定提示词的预览由方案编辑器保留")
		return
	}
	cfg, err := s.loadPlanScriptCacheContext(r.Context(), request)
	if err != nil {
		scriptPreviewError(w, err, request.tenantID, request.planID, "")
		return
	}
	input := request.input
	if input.Basis != cfg.basis {
		writeError(w, http.StatusConflict, "母版或方案资料已更新，请重新生成后再保存")
		return
	}
	body := stripBracketed(scriptchain.NormalizeScript(input.Text))
	if body == "" {
		writeError(w, http.StatusBadRequest, "缓存话术没有可用正文")
		return
	}
	p0, p1, p2, err := loadRiskAnnotationMothers()
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "读取话术红线规则失败，请稍后重试")
		return
	}
	hits, err := annotateScript(body, p0, p1, p2)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "检查缓存话术失败，请稍后重试")
		return
	}
	if len(hits) > 0 {
		writeError(w, http.StatusBadRequest, "缓存话术命中红线，请修改后再保存。命中内容："+hits[0])
		return
	}
	if r.Context().Err() != nil {
		return
	}
	if err := saveScriptSharedText(recordingCacheMediaRoot(), cfg.version, cfg.unit, body); err != nil {
		log.Printf("script cache editor save failed plan=%d piece=%s: %v", cfg.version.PlanID, cfg.unit.poolID(), err)
		writeError(w, http.StatusInternalServerError, "保存缓存话术失败，请稍后重试")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"text": body})
}
