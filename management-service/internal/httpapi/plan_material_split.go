package httpapi

import (
	"context"
	"errors"
	"livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
	"net/http"
	"strconv"
)

// Splitting replaces exactly the reviewed source, without matching or removing
// other records that happen to have the same attribute.
func materialSplitItems(existing []model.PlanMaterialClaim, id int64, revision string, items []model.PlanMaterialClaimInput, links []materialLinkObject, tenantID, planID int64) ([]model.PlanMaterialClaimInput, error) {
	if err := db.CheckMaterialClaimExpectations(existing, []db.MaterialClaimExpectation{{ID: id, Revision: revision}}, id); err != nil {
		return nil, err
	}
	if len(items) == 0 || len(items) > 500 {
		return nil, errors.New("请拆分为1至500条资料")
	}
	var original model.PlanMaterialClaimInput
	for _, old := range existing {
		if old.ID == id {
			original = db.MaterialClaimInput(old)
			resolveMaterialTargets(&original, links)
		}
	}
	out := make([]model.PlanMaterialClaimInput, 0, len(items))
	clients := map[string]bool{}
	for _, input := range items {
		if input.ClientID == "" || clients[input.ClientID] {
			return nil, errors.New("拆分条目标识重复，请重新打开拆分")
		}
		clients[input.ClientID] = true
		// Ownership and source rules belong to the reviewed source. A model
		// preview or stale client cannot redirect or drop them during splitting.
		input.ObjectTargets = original.ObjectTargets
		input.ScopeKind, input.StoreName, input.ProductLinkID, input.ScopeName = original.ScopeKind, original.StoreName, original.ProductLinkID, original.ScopeName
		input.Conditions, input.Exceptions = original.Conditions, original.Exceptions
		input.SourceQuote, input.SourceSentence = original.SourceQuote, original.SourceSentence
		input.ReplacesID = 0
		input.NeedsConfirmation, input.NeedsSplit, input.AtomicityReason = false, false, ""
		item := normalizeMaterialClaim(input, tenantID, planID)
		resolveMaterialTargets(&item, links)
		if err := validateMaterialClaim(item); err != nil {
			return nil, err
		}
		for _, before := range out {
			if db.MaterialClaimDuplicate(item, before) {
				return nil, errors.New("拆分内容有重复行，请保留一条后保存")
			}
		}
		out = append(out, item)
	}
	return out, nil
}

func (s *Server) planMaterialClaimSplit(w http.ResponseWriter, r *http.Request) {
	actor, planID, tenantID, ok := s.resolvePlanMaterialClaimScope(w, r)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("claimID"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, 400, "资料 ID 不正确")
		return
	}
	var input struct {
		Revision string                         `json:"revision"`
		Items    []model.PlanMaterialClaimInput `json:"items"`
	}
	if err = readJSON(w, r, &input); err != nil {
		writeError(w, 400, "请求格式不正确")
		return
	}
	existing, err := s.store.ListPlanMaterialClaims(r.Context(), tenantID, planID)
	if err != nil {
		writeError(w, 500, "读取原资料失败，未开始保存")
		return
	}
	links, err := s.materialLinkObjects(r, tenantID, planID)
	if err != nil {
		writeError(w, 500, "读取方案对象失败，原资料保留")
		return
	}
	items, err := materialSplitItems(existing, id, input.Revision, input.Items, links, tenantID, planID)
	if errors.Is(err, db.ErrMaterialClaimConflict) {
		writeError(w, 409, "原资料已变化，请刷新后重新拆分")
		return
	}
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	catalog, err := s.store.PlanMaterialObjects(r.Context(), tenantID, planID)
	if err != nil {
		writeError(w, 500, "读取归属失败，原资料保留")
		return
	}
	embedder, enabled := s.semanticMetrics.(interface {
		Enabled() bool
		Model() string
		EmbedTexts(context.Context, []string) ([][]float32, error)
	})
	if !enabled || !embedder.Enabled() {
		writeError(w, 400, "服务端向量模型未启用，原资料保留")
		return
	}
	texts := make([]string, len(items))
	for i, it := range items {
		texts[i] = materialDocumentText(it)
	}
	vectors, err := embedMaterialTexts(r.Context(), embedder, texts)
	if err != nil || db.ValidateMaterialVectors(vectors, len(items)) != nil {
		writeError(w, 502, "拆分保存失败，原资料保留，请重试")
		return
	}
	added, err := s.store.ImportPlanMaterialLibrary(r.Context(), tenantID, planID, actor.UserID, db.MaterialLibraryRevision(existing), catalog.Revision, items, []int64{id}, embedder.Model(), vectors)
	if errors.Is(err, db.ErrMaterialClaimConflict) {
		writeError(w, 409, "资料或归属已变化，本次未保存，请刷新后重新拆分")
		return
	}
	if err != nil {
		writeError(w, 500, "拆分保存失败，原资料保留，请重试")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "added": added, "removed": 1})
}
