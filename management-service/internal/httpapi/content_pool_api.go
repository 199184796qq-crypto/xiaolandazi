package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"livecompanion/management/internal/contentpools"
	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
)

type contentPoolStatus struct {
	Notices           []contentpools.Notice        `json:"notices,omitempty"`
	Corrections       []contentpools.Correction    `json:"corrections,omitempty"`
	State             string                       `json:"state"`
	Changes           []contentpools.Change        `json:"changes"`
	Job               appdb.PoolJob                `json:"job"`
	Products          []contentpools.Product       `json:"products"`
	Benefits          []appdb.BenefitScope         `json:"benefits"`
	Pools             []map[string]any             `json:"pools"`
	SetID             int64                        `json:"set_id"`
	History           []appdb.ContentChangeHistory `json:"history,omitempty"`
	SavedSetID        int64                        `json:"saved_set_id"`
	SourceRows        []contentpools.SourceRow     `json:"source_rows,omitempty"`
	CurrentSourceRows []contentpools.SourceRow     `json:"current_source_rows,omitempty"`
}

func contentPoolSetID(data map[string]any) int64 {
	ref := asSupportMap(data["content_pools"])
	raw, _ := json.Marshal(ref["set_id"])
	var id int64
	_ = json.Unmarshal(raw, &id)
	return id
}
func contentPoolDraftState(set contentpools.Set, source contentpools.Sources, savedID int64) string {
	// Notices live outside the executable pool. They do not veto saving the
	// already approved current-source draft; unfinished items have optional retry.
	if set.ID == 0 || set.SourceSHA != source.SHA() {
		return "needs_update"
	}
	if set.ID == savedID {
		return "synced"
	}
	return "ready"
}
func (s *Server) contentPoolStatus(ctx context.Context, tenant, plan, room int64, preview bool) (contentPoolStatus, error) {
	out := contentPoolStatus{State: "needs_update", Changes: []contentpools.Change{}, Products: []contentpools.Product{}, Benefits: []appdb.BenefitScope{}, Pools: []map[string]any{}}
	source, err := s.captureContentSources(ctx, tenant, plan)
	if err != nil {
		return out, err
	}
	out.Products = source.Products
	out.Corrections = source.Corrections
	baseline, err := s.store.ObserveContentSources(ctx, tenant, plan, source)
	if err != nil {
		return out, err
	}
	versions, versionErr := s.store.ListLiveAgentPlanVersionsForPlan(ctx, tenant, plan)
	var version model.LiveAgentPlanVersion
	if len(versions) > 0 {
		version = versions[0]
	}

	if versionErr == nil && version.PlanID == plan {
		out.SavedSetID = contentPoolSetID(version.GenerationContext)
		if out.SavedSetID > 0 {
			saved, e := s.store.GetContentPoolSet(ctx, tenant, plan, out.SavedSetID)
			if e == nil {
				baseline = saved.Sources
			}
			// 容错：版本引用的内容池 set 可能已被清理/重建删除，忽略即可，
			// 不应阻断状态读取（否则保存按钮会被误判为「资料状态没读到」而置灰）。
		}
	}
	out.Changes = contentpools.Changes(baseline, source)
	set, err := s.store.LatestContentPoolSet(ctx, tenant, plan)
	if err != nil {
		return out, err
	}
	out.SetID = set.ID
	out.Notices = set.Notices
	out.State = contentPoolDraftState(set, source, out.SavedSetID)
	if out.SavedSetID == 0 {
		out.Changes = append(out.Changes, contentpools.Change{ID: "legacy-snapshot", Area: "miner", Label: "商品专属池与公共池尚未保存", Before: "现有方案", After: "点击更新受影响内容，核对后保存智能体"})
	}
	job, err := s.store.LatestPoolJob(ctx, tenant, plan)
	if err != nil {
		return out, err
	}
	out.Job = job
	if preview {
		out.SourceRows = set.Sources.Rows
		out.CurrentSourceRows = source.Rows
		out.History, err = s.store.ContentChangeHistory(ctx, tenant, plan)
		if err != nil {
			return out, err
		}
	}
	if job.Status == "running" {
		out.State = "updating"
	} else if job.Status == "failed" && (set.ID == 0 || set.SourceSHA != source.SHA()) {
		out.State = "failed"
	}
	for _, p := range set.Pools {
		row := map[string]any{"key": p.Key, "label": p.Label, "owner": p.Owner, "products": p.Products, "task_count": len(p.Tasks), "reused": p.Reused, "tasks": p.Tasks}
		if preview {
			row["tasks"] = p.Tasks
			row["facts"] = p.Facts
		}
		out.Pools = append(out.Pools, row)
	}
	benefits, err := s.store.ListLiveAgentPlanBenefits(ctx, tenant, plan)
	if err != nil {
		return out, err
	}
	scopes, err := s.store.ListBenefitScopes(ctx, tenant, plan)
	if err != nil {
		return out, err
	}
	for _, b := range benefits {
		scope, ok := scopes[b.ID]
		if !ok {
			scope = appdb.BenefitScope{BenefitID: b.ID, All: b.LinkKey == "", Products: []int64{}}
			for _, p := range source.Products {
				if scope.All || b.LinkKey == p.Link {
					scope.Products = append(scope.Products, p.ID)
				}
			}
		}
		out.Benefits = append(out.Benefits, scope)
	}
	return out, nil
}
func (s *Server) contentPoolAccess(w http.ResponseWriter, r *http.Request, edit bool) (tenant, plan, room, actorID int64, ok bool) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return 0, 0, 0, 0, false
	}
	plan, ok = liveAgentPlanPathID(w, r)
	if !ok {
		return 0, 0, 0, 0, false
	}
	tenant, ok = s.resolveLiveAgentPlanTenant(w, r, actor, requestTenantID(r), edit)
	if !ok {
		return 0, 0, 0, 0, false
	}
	room = 0

	if _, err := s.store.GetLiveAgentPlan(r.Context(), tenant, plan); err != nil {
		writeError(w, 404, "当前方案不存在")
		return 0, 0, 0, 0, false
	}
	return tenant, plan, room, actor.UserID, true
}
func (s *Server) contentPoolGet(w http.ResponseWriter, r *http.Request) {
	tenant, plan, room, _, ok := s.contentPoolAccess(w, r, false)
	if !ok {
		return
	}
	status, err := s.contentPoolStatus(r.Context(), tenant, plan, room, r.URL.Query().Get("preview") == "1")
	if err != nil {
		writeError(w, 500, "读取方案待更新内容失败")
		return
	}
	writeJSON(w, 200, status)
}
func (s *Server) contentPoolUpdate(w http.ResponseWriter, r *http.Request) {
	tenant, plan, room, actor, ok := s.contentPoolAccess(w, r, true)
	if !ok {
		return
	}
	// force=1 时跳过「指纹未变即复用」的所有增量逻辑，强制全量重新生成内容池。
	force := r.URL.Query().Get("force") == "1"
	source, err := s.captureContentSources(r.Context(), tenant, plan)
	if err != nil {
		writeError(w, 409, err.Error())
		return
	}
	if len(source.Products) == 0 {
		writeError(w, 409, "请先采纳商品链接")
		return
	}
	set, err := s.store.LatestContentPoolSet(r.Context(), tenant, plan)
	if err != nil {
		writeError(w, 500, "读取内容版本失败")
		return
	}
	if !force && set.ID > 0 && set.SourceSHA == source.SHA() && set.GenerationProtocol == contentpools.GenerationProtocol && !set.HasUnfinishedContent() {
		status, e := s.contentPoolStatus(r.Context(), tenant, plan, room, false)
		if e != nil {
			writeError(w, 500, "读取内容状态失败")
			return
		}
		writeJSON(w, 200, status)
		return
	}
	job, err := s.store.ClaimPoolJob(r.Context(), tenant, plan, actor, source)
	if errors.Is(err, appdb.ErrPoolJobRunning) {
		writeError(w, 409, err.Error())
		return
	}
	if err != nil {
		writeError(w, 409, "资料变化或任务创建失败，请刷新重试")
		return
	}
	go s.runContentPoolJob(tenant, plan, actor, job, source, force)
	writeJSON(w, http.StatusAccepted, map[string]any{"state": "updating", "job": job})
}

func (s *Server) contentPoolEdit(w http.ResponseWriter, r *http.Request) {
	tenant, plan, room, actorID, ok := s.contentPoolAccess(w, r, true)
	if !ok {
		return
	}
	actor, ok := s.resolveActor(w, r)
	if !ok || !s.requireLiveSupportPlanScope(w, r, actor, tenant, plan, true) {
		return
	}
	var input contentpools.PoolEdit
	if readJSON(w, r, &input) != nil {
		writeError(w, 400, "卖点角度编辑格式错误")
		return
	}
	source, err := s.captureContentSources(r.Context(), tenant, plan)
	if err != nil {
		writeError(w, 409, err.Error())
		return
	}
	if _, err = s.store.EditContentPool(r.Context(), tenant, plan, actorID, source, input); err != nil {
		writeError(w, 409, err.Error())
		return
	}
	status, err := s.contentPoolStatus(r.Context(), tenant, plan, room, false)
	if err != nil {
		writeError(w, 500, "修改已保存，请刷新读取内容")
		return
	}
	writeJSON(w, 200, status)
}
func (s *Server) contentBenefitScopeSave(w http.ResponseWriter, r *http.Request) {
	tenant, plan, _, _, ok := s.contentPoolAccess(w, r, true)
	if !ok {
		return
	}
	id, _ := strconv.ParseInt(r.PathValue("benefitID"), 10, 64)
	if id <= 0 {
		writeError(w, 400, "福利编号无效")
		return
	}
	var input struct {
		All                  bool    `json:"all"`
		Products             []int64 `json:"products"`
		ExpectedVersion      int64   `json:"expected_version"`
		ExpectedScopeVersion int64   `json:"expected_scope_version"`
	}
	if readJSON(w, r, &input) != nil {
		writeError(w, 400, "福利适用范围格式错误")
		return
	}
	if err := s.store.SaveBenefitScope(r.Context(), tenant, plan, id, input.ExpectedVersion, appdb.BenefitScope{BenefitID: id, All: input.All, Products: input.Products, VersionNo: input.ExpectedScopeVersion}); err != nil {
		writeError(w, 409, err.Error())
		return
	}
	writeJSON(w, 200, appdb.BenefitScope{BenefitID: id, All: input.All, Products: contentpools.IDs(input.Products), VersionNo: input.ExpectedScopeVersion + 1})
}

func (s *Server) readyContentPoolSet(ctx context.Context, tenant, plan int64) (contentpools.Set, error) {
	source, err := s.captureContentSources(ctx, tenant, plan)
	if err != nil {
		return contentpools.Set{}, err
	}
	set, err := s.store.LatestContentPoolSet(ctx, tenant, plan)
	if err != nil {
		return set, err
	}
	if set.ID == 0 || set.SourceSHA != source.SHA() {
		return set, errors.New("资料有待更新内容，请在上方点击“更新受影响内容”，完成后保存智能体")
	}
	set.Sources.Revision = source.Revision
	return set, contentpools.ValidateSet(set)
}
func contentPoolRef(set contentpools.Set) map[string]any {
	return map[string]any{"set_id": set.ID, "source_sha": set.SourceSHA, "source_revision": set.Sources.Revision}
}

// latestSavedContentPoolSet 直接返回方案最近一次已保存的内容池，跳过「来源快照 SHA 比对」与
// ValidateSet 全量核对。保存方案不再把「事实引用了快照之外/资料有待更新」这类完整性问题
// 卡在保存入口——按新生成的内容原样落库标记可用，核对留给内容池自身的生成/更新链路。
func (s *Server) latestSavedContentPoolSet(ctx context.Context, tenant, plan int64) (contentpools.Set, bool, error) {
	set, err := s.store.LatestContentPoolSet(ctx, tenant, plan)
	if err != nil {
		return contentpools.Set{}, false, err
	}
	if set.ID == 0 {
		// 方案还没有任何内容池：落到旧的非内容池事实核对路径，由调用方兜底。
		return contentpools.Set{}, false, nil
	}
	return set, true, nil
}
