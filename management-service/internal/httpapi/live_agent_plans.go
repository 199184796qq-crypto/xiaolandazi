package httpapi

import (
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
)

func liveAgentPlanPathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	value, err := strconv.ParseInt(strings.TrimSpace(r.PathValue("planID")), 10, 64)
	if err != nil || value <= 0 {
		writeError(w, http.StatusBadRequest, "直播智能体方案 ID 无效")
		return 0, false
	}
	return value, true
}

func requestTenantID(r *http.Request) int64 {
	value, _ := strconv.ParseInt(strings.TrimSpace(r.URL.Query().Get("tenant_id")), 10, 64)
	return value
}

func (s *Server) resolveLiveAgentPlanTenant(
	w http.ResponseWriter,
	r *http.Request,
	actor model.Actor,
	requestedTenantID int64,
	write bool,
) (int64, bool) {
	if actor.TenantID != nil {
		tenantID := *actor.TenantID
		if requestedTenantID > 0 && requestedTenantID != tenantID {
			writeError(w, http.StatusForbidden, "不能访问其他终端的直播智能体方案")
			return 0, false
		}
		return tenantID, true
	}
	if actor.IsPlatformAdmin() {
		if requestedTenantID <= 0 {
			writeError(w, http.StatusBadRequest, "请选择要测试的终端 tenant_id")
			return 0, false
		}
		return requestedTenantID, true
	}
	if !actor.IsInternalStaff() {
		writeError(w, http.StatusForbidden, "当前账号没有直播智能体方案权限")
		return 0, false
	}
	access, err := s.staffAccessForActor(r, actor)
	if err != nil {
		writeError(w, http.StatusForbidden, "读取内部权限失败")
		return 0, false
	}
	allowed := staffHasPermission(access, "liveops.configure")
	if !write {
		allowed = allowed || staffHasPermission(access, "liveops.view_all")
	}
	if !allowed {
		writeError(w, http.StatusForbidden, "当前岗位没有直播智能体方案权限")
		return 0, false
	}
	if requestedTenantID <= 0 {
		writeError(w, http.StatusBadRequest, "请选择终端 tenant_id")
		return 0, false
	}
	return requestedTenantID, true
}

func (s *Server) liveAgentPlanList(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	tenantID, ok := s.resolveLiveAgentPlanTenant(w, r, actor, requestTenantID(r), false)
	if !ok {
		return
	}
	items, err := s.store.ListLiveAgentPlans(r.Context(), tenantID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取直播智能体方案失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) liveAgentPlanCreate(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	var input model.CreateLiveAgentPlanInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	input.Name = strings.TrimSpace(input.Name)
	input.Description = strings.TrimSpace(input.Description)
	if input.Name == "" || utf8.RuneCountInString(input.Name) > 160 {
		writeError(w, http.StatusBadRequest, "方案名称必须为 1 到 160 字")
		return
	}
	if utf8.RuneCountInString(input.Description) > 2000 {
		writeError(w, http.StatusBadRequest, "方案说明不能超过 2000 字")
		return
	}
	tenantID, ok := s.resolveLiveAgentPlanTenant(w, r, actor, input.TenantID, true)
	if !ok {
		return
	}
	item, err := s.store.CreateLiveAgentPlan(r.Context(), tenantID, actor.UserID, input)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "创建直播智能体方案失败")
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) liveAgentPlanUpdate(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	planID, ok := liveAgentPlanPathID(w, r)
	if !ok {
		return
	}
	var input model.CreateLiveAgentPlanInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	input.Name = strings.TrimSpace(input.Name)
	input.Description = strings.TrimSpace(input.Description)
	if input.Name == "" || utf8.RuneCountInString(input.Name) > 160 {
		writeError(w, http.StatusBadRequest, "方案名称必须为 1 到 160 字")
		return
	}
	if utf8.RuneCountInString(input.Description) > 2000 {
		writeError(w, http.StatusBadRequest, "方案说明不能超过 2000 字")
		return
	}
	tenantID, ok := s.resolveLiveAgentPlanTenant(w, r, actor, input.TenantID, true)
	if !ok {
		return
	}
	item, err := s.store.UpdateLiveAgentPlan(r.Context(), tenantID, planID, input)
	if errors.Is(err, appdb.ErrLiveAgentPlanNotFound) {
		writeError(w, http.StatusNotFound, "直播智能体方案不存在或已归档")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "保存直播智能体方案失败")
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) liveAgentPlanGet(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	planID, ok := liveAgentPlanPathID(w, r)
	if !ok {
		return
	}
	tenantID, ok := s.resolveLiveAgentPlanTenant(w, r, actor, requestTenantID(r), false)
	if !ok {
		return
	}
	item, err := s.store.GetLiveAgentPlan(r.Context(), tenantID, planID)
	if errors.Is(err, appdb.ErrLiveAgentPlanNotFound) {
		writeError(w, http.StatusNotFound, "直播智能体方案不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取直播智能体方案失败")
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) liveAgentPlanArchive(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	planID, ok := liveAgentPlanPathID(w, r)
	if !ok {
		return
	}
	tenantID, ok := s.resolveLiveAgentPlanTenant(w, r, actor, requestTenantID(r), true)
	if !ok {
		return
	}
	if err := s.store.ArchiveLiveAgentPlan(r.Context(), tenantID, planID); errors.Is(err, appdb.ErrLiveAgentPlanNotFound) {
		writeError(w, http.StatusNotFound, "直播智能体方案不存在或已经归档")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "归档直播智能体方案失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) liveAgentPlanCurrentForRoom(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	roomID, ok := pathID(w, r)
	if !ok {
		return
	}
	tenantID, ok := s.tenantForRoom(w, r, actor, roomID)
	if !ok {
		return
	}
	item, err := s.store.GetLiveAgentPlanForRoom(r.Context(), tenantID, roomID)
	if errors.Is(err, appdb.ErrLiveAgentPlanNotFound) {
		writeJSON(w, http.StatusOK, map[string]any{"plan": nil})
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取当前直播智能体方案失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"plan": item})
}

func (s *Server) liveAgentPlansForRoom(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	roomID, ok := pathID(w, r)
	if !ok {
		return
	}
	tenantID, ok := s.tenantForRoom(w, r, actor, roomID)
	if !ok {
		return
	}
	items, err := s.store.ListLiveAgentPlansForRoom(r.Context(), tenantID, roomID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取直播间已绑定方案失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) liveAgentPlanBindRoom(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	planID, ok := liveAgentPlanPathID(w, r)
	if !ok {
		return
	}
	var input model.BindLiveAgentPlanRoomInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	if input.RoomID <= 0 {
		writeError(w, http.StatusBadRequest, "直播间 ID 无效")
		return
	}
	tenantID, ok := s.resolveLiveAgentPlanTenant(w, r, actor, input.TenantID, true)
	if !ok {
		return
	}
	if _, err := s.getCoreRoomState(r.Context(), tenantID, input.RoomID); err != nil {
		writeError(w, http.StatusNotFound, "直播间不存在或不属于当前终端")
		return
	}
	item, err := s.store.BindRoomToLiveAgentPlan(r.Context(), tenantID, planID, input.RoomID, actor.UserID)
	if errors.Is(err, appdb.ErrLiveAgentPlanNotFound) {
		writeError(w, http.StatusNotFound, "直播智能体方案不存在或已经归档")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "绑定直播间失败")
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) liveAgentPlanUnbindRoom(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	planID, ok := liveAgentPlanPathID(w, r)
	if !ok {
		return
	}
	roomID, err := strconv.ParseInt(strings.TrimSpace(r.PathValue("roomID")), 10, 64)
	if err != nil || roomID <= 0 {
		writeError(w, http.StatusBadRequest, "直播间 ID 无效")
		return
	}
	tenantID, ok := s.resolveLiveAgentPlanTenant(w, r, actor, requestTenantID(r), true)
	if !ok {
		return
	}
	if selected, selectedErr := s.store.GetLiveAgentPlanForRoom(r.Context(), tenantID, roomID); selectedErr == nil && selected.ID == planID {
		runtimeState, runtimeErr := s.getCoreAgentState(r.Context(), tenantID, roomID)
		if runtimeErr != nil {
			writeError(w, http.StatusBadGateway, "无法确认当前直播方案运行状态")
			return
		}
		if runtimeState.State == "working" || runtimeState.State == "paused" {
			writeError(w, http.StatusConflict, "这个方案当前正在直播间使用，请先热切换到其它已绑定方案，或停止AI后再取消绑定")
			return
		}
		if runtimeState.PlanID == planID {
			if _, syncErr := s.setCoreAgentPlan(r.Context(), tenantID, roomID, 0, ""); syncErr != nil {
				writeError(w, http.StatusBadGateway, "清理直播间当前方案失败")
				return
			}
		}
	} else if selectedErr != nil && !errors.Is(selectedErr, appdb.ErrLiveAgentPlanNotFound) {
		writeError(w, http.StatusInternalServerError, "读取直播间当前方案失败")
		return
	}
	if err := s.store.UnbindRoomFromLiveAgentPlan(r.Context(), tenantID, planID, roomID); errors.Is(err, appdb.ErrLiveAgentPlanNotFound) {
		writeError(w, http.StatusNotFound, "没有找到有效绑定")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "解除直播间绑定失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) liveAgentPlanUpsertTerm(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	planID, ok := liveAgentPlanPathID(w, r)
	if !ok {
		return
	}
	var input model.UpsertLiveAgentPlanTermInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	input.CanonicalText = strings.TrimSpace(input.CanonicalText)
	input.ObservedText = strings.TrimSpace(input.ObservedText)
	input.TermType = strings.TrimSpace(input.TermType)
	input.Note = strings.TrimSpace(input.Note)
	if input.CanonicalText == "" || utf8.RuneCountInString(input.CanonicalText) > 255 {
		writeError(w, http.StatusBadRequest, "正确术语必须为 1 到 255 字")
		return
	}
	if utf8.RuneCountInString(input.ObservedText) > 255 || utf8.RuneCountInString(input.Note) > 512 {
		writeError(w, http.StatusBadRequest, "纠错内容过长")
		return
	}
	tenantID, ok := s.resolveLiveAgentPlanTenant(w, r, actor, input.TenantID, true)
	if !ok {
		return
	}
	item, err := s.store.UpsertLiveAgentPlanTerm(r.Context(), tenantID, planID, actor.UserID, input)
	if errors.Is(err, appdb.ErrLiveAgentPlanNotFound) {
		writeError(w, http.StatusNotFound, "直播智能体方案不存在或已经归档")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "保存人工纠错失败")
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func normalizePlanText(text string, terms []model.LiveAgentPlanTerm) model.NormalizeLiveAgentPlanTextOutput {
	output := model.NormalizeLiveAgentPlanTextOutput{
		OriginalText:   text,
		NormalizedText: text,
		Applied:        []model.LiveAgentPlanCorrectionHit{},
		HotTerms:       []string{},
	}
	type replacement struct {
		termID    int64
		observed  string
		canonical string
	}
	replacements := make([]replacement, 0)
	for _, term := range terms {
		if term.Status != "active" || strings.TrimSpace(term.CanonicalText) == "" {
			continue
		}
		output.HotTerms = append(output.HotTerms, term.CanonicalText)
		for _, variant := range term.Variants {
			if strings.TrimSpace(variant.VariantText) == "" || variant.VariantText == term.CanonicalText {
				continue
			}
			replacements = append(replacements, replacement{
				termID: term.ID, observed: variant.VariantText, canonical: term.CanonicalText,
			})
		}
	}
	sort.SliceStable(replacements, func(i, j int) bool {
		return utf8.RuneCountInString(replacements[i].observed) > utf8.RuneCountInString(replacements[j].observed)
	})
	for _, item := range replacements {
		if !strings.Contains(output.NormalizedText, item.observed) {
			continue
		}
		output.NormalizedText = strings.ReplaceAll(output.NormalizedText, item.observed, item.canonical)
		output.Applied = append(output.Applied, model.LiveAgentPlanCorrectionHit{
			ObservedText: item.observed, CanonicalText: item.canonical, TermID: item.termID,
		})
	}
	return output
}

func (s *Server) liveAgentPlanNormalizeText(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	planID, ok := liveAgentPlanPathID(w, r)
	if !ok {
		return
	}
	var input model.NormalizeLiveAgentPlanTextInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	input.Text = strings.TrimSpace(input.Text)
	if input.Text == "" || utf8.RuneCountInString(input.Text) > 6000 {
		writeError(w, http.StatusBadRequest, "测试文字必须为 1 到 6000 字")
		return
	}
	tenantID, ok := s.resolveLiveAgentPlanTenant(w, r, actor, input.TenantID, false)
	if !ok {
		return
	}
	plan, err := s.store.GetLiveAgentPlan(r.Context(), tenantID, planID)
	if errors.Is(err, appdb.ErrLiveAgentPlanNotFound) {
		writeError(w, http.StatusNotFound, "直播智能体方案不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取直播智能体方案失败")
		return
	}
	if plan.Status != "active" {
		writeError(w, http.StatusConflict, "直播智能体方案已归档，不再参与新的转写纠错")
		return
	}
	writeJSON(w, http.StatusOK, normalizePlanText(input.Text, plan.Terms))
}
