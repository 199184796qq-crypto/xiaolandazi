package httpapi

import (
	"context"
	"errors"
	"livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
	"net/http"
	"strings"
)

type materialImportChoice struct {
	Item   model.PlanMaterialClaimInput `json:"item"`
	Action string                       `json:"action"`
}

// Every existing library match needs an explicit operator decision. No inferred
// replaces_id or provider output may stand in for a keep/replace choice.
func materialImportChanges(choices []materialImportChoice, existing []model.PlanMaterialClaimInput, ids []int64, stores []model.MaterialStoreObject) ([]model.PlanMaterialClaimInput, []int64, int, error) {
	candidates := []model.PlanMaterialClaimInput{}
	remove := map[int64]bool{}
	decisions := map[int64]string{}
	clients := map[string]bool{}
	kept := 0
	for i, c := range choices {
		if c.Item.ClientID == "" || clients[c.Item.ClientID] {
			return nil, nil, 0, errors.New("草稿标识重复，请重新分析")
		}
		clients[c.Item.ClientID] = true
		for j := 0; j < i; j++ {
			if db.MaterialClaimDuplicate(c.Item, choices[j].Item) {
				return nil, nil, 0, errors.New("请先处理本次草稿中的重复属性")
			}
		}
		matches := []int{}
		for j, old := range existing {
			if db.MaterialClaimCollision(c.Item, old, stores) {
				matches = append(matches, j)
			}
		}
		if len(matches) == 0 {
			if c.Action != "append" {
				return nil, nil, 0, errors.New("资料匹配已变化，请重新核对入库选择")
			}
			candidates = append(candidates, c.Item)
			continue
		}
		if c.Action != "keep" && c.Action != "replace" {
			return nil, nil, 0, errors.New("库中已有该属性，请手动选择替换或保留库中")
		}
		for _, j := range matches {
			if before := decisions[ids[j]]; before != "" && before != c.Action {
				return nil, nil, 0, errors.New("同一组库中属性的选择不一致，请重新核对")
			}
			decisions[ids[j]] = c.Action
		}
		if c.Action == "keep" {
			kept++
			continue
		}
		for _, j := range matches {
			remove[ids[j]] = true
		}
		candidates = append(candidates, c.Item)
	}
	removed := []int64{}
	for id := range remove {
		removed = append(removed, id)
	}
	// A replacement only consumes the explicitly matched owners. Preserve all
	// other owners as a separate row in the same atomic adoption.
	for j, old := range existing {
		if !remove[ids[j]] {
			continue
		}
		owners := map[string]bool{}
		for _, c := range choices {
			if c.Action == "replace" && db.MaterialClaimCollision(c.Item, old, stores) {
				for _, t := range c.Item.ObjectTargets {
					owners[db.MaterialOwnerKey(t)] = true
				}
			}
		}
		if rest, ok := materialOwnersRemaining(old, owners); ok {
			rest.ClientID = "__preserved_owner"
			candidates = append(candidates, rest)
		}
	}
	return candidates, removed, kept, nil
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
		writeError(w, 409, "库中资料已变化，请重新打开入库确认并选择")
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
	oldInputs := []model.PlanMaterialClaimInput{}
	ids := []int64{}
	for _, old := range existing {
		it := db.MaterialClaimInput(old)
		resolveMaterialTargets(&it, links)
		oldInputs = append(oldInputs, it)
		ids = append(ids, old.ID)
	}
	for i := range input.Choices {
		it := normalizeMaterialClaim(input.Choices[i].Item, tenantID, planID)
		it.ReplacesID = 0
		resolveMaterialTargets(&it, links)
		if err = validateMaterialClaim(it); err != nil {
			writeError(w, 400, err.Error())
			return
		}
		input.Choices[i].Item = it
	}
	candidates, remove, kept, err := materialImportChanges(input.Choices, oldInputs, ids, catalog.Stores)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	var vectors [][]float32
	modelName := ""
	if len(candidates) > 0 {
		embedder, enabled := s.semanticMetrics.(interface {
			Enabled() bool
			Model() string
			EmbedTexts(context.Context, []string) ([][]float32, error)
		})
		if !enabled || !embedder.Enabled() {
			writeError(w, 400, "服务端向量模型未启用")
			return
		}
		texts := []string{}
		for _, it := range candidates {
			texts = append(texts, materialDocumentText(it))
		}
		vectors, err = embedMaterialTexts(r.Context(), embedder, texts)
		if err != nil || db.ValidateMaterialVectors(vectors, len(candidates)) != nil {
			writeError(w, 502, "向量化失败，整批未采用，原草稿与库中资料保留")
			return
		}
		modelName = embedder.Model()
	}
	added, err := s.store.ImportPlanMaterialLibrary(r.Context(), tenantID, planID, actor.UserID, input.LibraryRevision, input.ObjectRevision, candidates, remove, modelName, vectors)
	if errors.Is(err, db.ErrMaterialClaimConflict) {
		writeError(w, 409, "资料或归属已变化，整批未采用，请重新核对")
		return
	}
	if err != nil {
		writeError(w, 500, "入库失败，整批未采用，原资料保留")
		return
	}
	accepted := []string{}
	for _, c := range input.Choices {
		accepted = append(accepted, c.Item.ClientID)
	}
	// Preserve internal residuals without counting them as newly adopted drafts.
	added = 0
	for _, c := range input.Choices {
		if c.Action != "keep" {
			added++
		}
	}
	writeJSON(w, 200, map[string]any{"ok": true, "added": added, "kept": kept, "removed": len(remove), "accepted_client_ids": accepted, "embedding_model": modelName})
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
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "removed": removed})
}
