package httpapi

import (
	"errors"
	"fmt"
	"livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
	"net/http"
	"strings"
)

type materialImportChoice struct {
	Item   model.PlanMaterialClaimInput `json:"item"`
	Action string                       `json:"action"`
}

func materialImportItems(choices []materialImportChoice) ([]model.PlanMaterialClaimInput, error) {
	items := make([]model.PlanMaterialClaimInput, 0, len(choices))
	clients := make(map[string]bool, len(choices))
	for _, choice := range choices {
		if choice.Action != "append" {
			return nil, errors.New("入库方式已更新，请刷新页面后重试")
		}
		item := choice.Item
		if item.ClientID == "" || clients[item.ClientID] {
			return nil, errors.New("草稿标识重复，请重新分析")
		}
		for _, prior := range items {
			if db.MaterialClaimDuplicate(item, prior) {
				return nil, errors.New("请先处理本次草稿中的重复属性")
			}
		}
		clients[item.ClientID] = true
		items = append(items, item)
	}
	return items, nil
}

func materialOwnersRemaining(it model.PlanMaterialClaimInput, removed map[string]bool) (model.PlanMaterialClaimInput, bool) {
	targets := []model.MaterialObjectTarget{}
	for _, t := range it.ObjectTargets {
		if !removed[db.MaterialOwnerKey(t)] {
			targets = append(targets, t)
		}
	}
	it.ObjectTargets = targets
	normalizeMaterialTargets(&it)
	return it, len(targets) > 0
}

func (s *Server) planMaterialCommitImport(w http.ResponseWriter, r *http.Request) {
	actor, planID, tenantID, ok := s.resolvePlanMaterialClaimScope(w, r)
	if !ok {
		return
	}
	var input struct {
		LibraryRevision string                 `json:"library_revision"`
		ObjectRevision  int64                  `json:"object_revision"`
		Choices         []materialImportChoice `json:"choices"`
	}
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, 400, "请求格式不正确")
		return
	}
	if len(input.Choices) == 0 || len(input.Choices) > 500 {
		writeError(w, 400, "请选择1至500条草稿")
		return
	}
	existing, err := s.store.ListPlanMaterialClaims(r.Context(), tenantID, planID)
	if err != nil {
		writeError(w, 500, "读取语义库失败")
		return
	}
	if len(input.LibraryRevision) != 64 || db.MaterialLibraryRevision(existing) != input.LibraryRevision {
		writeError(w, 409, "库中资料已变化，请重新打开入库确认")
		return
	}
	links, err := s.materialLinkObjects(r, tenantID, planID)
	if err != nil {
		writeError(w, 500, "读取方案对象失败")
		return
	}
	catalog, err := s.store.PlanMaterialObjects(r.Context(), tenantID, planID)
	if err != nil {
		writeError(w, 500, "读取门店关联失败")
		return
	}
	if catalog.Revision != input.ObjectRevision {
		writeError(w, 409, "门店关联已变化，请重新核对")
		return
	}
	sources := map[string]string{}
	for i := range input.Choices {
		candidate := input.Choices[i].Item
		if err := s.checkMaterialImportSource(r.Context(), tenantID, planID, &candidate, sources); err != nil {
			writeError(w, 400, fmt.Sprintf("第%d条资料：%s", i+1, err))
			return
		}
		it := normalizeMaterialClaim(candidate, tenantID, planID)
		it.ReplacesID = 0
		resolveMaterialTargets(&it, links)
		if err = validateMaterialClaim(it); err != nil {
			writeError(w, 400, err.Error())
			return
		}
		input.Choices[i].Item = it
	}
	candidates, err := materialImportItems(input.Choices)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	embeddingModel, vectors := s.materialClaimEmbedding(r.Context(), candidates)
	added, err := s.store.ImportPlanMaterialLibrary(r.Context(), tenantID, planID, actor.UserID, input.LibraryRevision, input.ObjectRevision, candidates, nil, embeddingModel, vectors)
	if errors.Is(err, db.ErrMaterialClaimConflict) {
		writeError(w, 409, "资料或归属已变化，整批未采用，请重新核对")
		return
	}
	if err != nil {
		writeError(w, 500, "入库失败，整批未采用，原资料保留")
		return
	}
	accepted := make([]string, 0, len(candidates))
	for _, item := range candidates {
		accepted = append(accepted, item.ClientID)
	}
	s.scheduleFactCardRebuild(tenantID, planID)
	writeJSON(w, 200, map[string]any{"ok": true, "added": added, "kept": 0, "removed": 0, "accepted_client_ids": accepted, "embedding_model": ""})
}

func (s *Server) planMaterialClear(w http.ResponseWriter, r *http.Request) {
	_, planID, tenantID, ok := s.resolvePlanMaterialClaimScope(w, r)
	if !ok {
		return
	}
	var input struct {
		LibraryRevision string `json:"library_revision"`
	}
	if err := readJSON(w, r, &input); err != nil || len(strings.TrimSpace(input.LibraryRevision)) != 64 {
		writeError(w, 400, "请先刷新库中资料后确认清除")
		return
	}
	removed, err := s.store.ClearPlanMaterialLibrary(r.Context(), tenantID, planID, input.LibraryRevision)
	if errors.Is(err, db.ErrMaterialClaimConflict) {
		writeError(w, 409, "库中资料已变化，未清除，请刷新后重新确认")
		return
	}
	if err != nil {
		writeError(w, 500, "清除失败，原资料保留")
		return
	}
	s.scheduleFactCardRebuild(tenantID, planID)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "removed": removed})
}
