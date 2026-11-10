package httpapi

import (
	"context"
	"errors"
	"livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
	"net/http"
)

func (s *Server) planMaterialDuplicates(w http.ResponseWriter, r *http.Request) {
	actor, planID, tenantID, ok := s.resolvePlanMaterialClaimScope(w, r)
	if !ok {
		return
	}
	var input struct {
		KeepID    int64                         `json:"keep_id"`
		Expected  []db.MaterialClaimExpectation `json:"expected"`
		Item      model.PlanMaterialClaimInput  `json:"item"`
		ObjectKey string                        `json:"object_key"`
	}
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, 400, "请求格式不正确")
		return
	}
	existing, err := s.store.ListPlanMaterialClaims(r.Context(), tenantID, planID)
	if err != nil {
		writeError(w, 500, "读取重复资料失败")
		return
	}
	if err = db.CheckMaterialClaimExpectations(existing, input.Expected, input.KeepID); err != nil {
		writeError(w, 409, "资料已变化，请刷新后重新比较")
		return
	}
	links, err := s.materialLinkObjects(r, tenantID, planID)
	if err != nil {
		writeError(w, 500, "读取方案对象失败")
		return
	}
	item := normalizeMaterialClaim(input.Item, tenantID, planID)
	// Retention compares the actual submitted value, without trimming it or
	// manufacturing a missing value from generated prose.
	item.Value = input.Item.Value
	item.ReplacesID = 0
	resolveMaterialTargets(&item, links)
	if err = validateMaterialClaim(item); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	residuals, split, scopeErr := materialDuplicateObjectPlan(existing, input.Expected, item, input.ObjectKey, links)
	if scopeErr != nil {
		writeError(w, 400, scopeErr.Error())
		return
	}
	if split {
		catalog, e := s.store.PlanMaterialObjects(r.Context(), tenantID, planID)
		if e != nil {
			writeError(w, 500, "读取方案对象失败")
			return
		}
		candidates := append(residuals, item)
		embedder, enabled := s.semanticMetrics.(interface {
			Enabled() bool
			Model() string
			EmbedTexts(context.Context, []string) ([][]float32, error)
		})
		if !enabled || !embedder.Enabled() {
			writeError(w, 400, "服务端向量模型未启用，原资料已保留")
			return
		}
		texts := []string{}
		for _, it := range candidates {
			texts = append(texts, materialDocumentText(it))
		}
		vectors, e := embedMaterialTexts(r.Context(), embedder, texts)
		if e != nil || db.ValidateMaterialVectors(vectors, len(candidates)) != nil {
			writeError(w, 502, "向量化失败，所有对象的原资料保留")
			return
		}
		remove := []int64{}
		for _, e := range input.Expected {
			remove = append(remove, e.ID)
		}
		_, err = s.store.ImportPlanMaterialLibrary(r.Context(), tenantID, planID, actor.UserID, db.MaterialLibraryRevision(existing), catalog.Revision, candidates, remove, embedder.Model(), vectors)
		if errors.Is(err, db.ErrMaterialClaimConflict) {
			writeError(w, 409, "资料已变化，所有对象的原资料保留，请刷新")
			return
		}
		if err != nil {
			writeError(w, 500, "处理失败，所有对象的原资料保留")
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "kept_id": 0, "removed": len(input.Expected) - 1, "preserved_objects": len(residuals)})
		return
	}
	var old model.PlanMaterialClaimInput
	for _, it := range existing {
		if it.ID == input.KeepID {
			old = db.MaterialClaimInput(it)
			resolveMaterialTargets(&old, links)
		}
	}
	var vector []float32
	embeddingModel := ""
	if materialDocumentText(item) != materialDocumentText(old) {
		embedder, enabled := s.semanticMetrics.(interface {
			Enabled() bool
			Model() string
			EmbedTexts(context.Context, []string) ([][]float32, error)
		})
		if !enabled || !embedder.Enabled() {
			writeError(w, 400, "服务端向量模型未启用，原资料已保留")
			return
		}
		vectors, e := embedMaterialTexts(r.Context(), embedder, []string{materialDocumentText(item)})
		if e != nil || db.ValidateMaterialVectors(vectors, 1) != nil {
			writeError(w, 502, "向量化失败，原资料全部保留，请重试")
			return
		}
		vector = vectors[0]
		embeddingModel = embedder.Model()
	}
	err = s.store.ResolvePlanMaterialDuplicates(r.Context(), tenantID, planID, input.KeepID, input.Expected, item, embeddingModel, vector)
	if errors.Is(err, db.ErrMaterialClaimConflict) {
		writeError(w, 409, "资料已变化，本次未采用，请刷新后重新比较")
		return
	}
	if err != nil {
		writeError(w, 500, "处理失败，原资料全部保留")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "kept_id": input.KeepID, "removed": len(input.Expected) - 1})
}

// Validate every compared record against the one explicit object, including
// legacy clients. Global inheritance and store relations never authorize deletion.
func materialDuplicateObjectPlan(rows []model.PlanMaterialClaim, expected []db.MaterialClaimExpectation, item model.PlanMaterialClaimInput, owner string, links []materialLinkObject) ([]model.PlanMaterialClaimInput, bool, error) {
	if owner == "" || len(item.ObjectTargets) != 1 || db.MaterialOwnerKey(item.ObjectTargets[0]) != owner {
		return nil, false, errors.New("请刷新后按同一对象比较重复属性")
	}
	residuals := []model.PlanMaterialClaimInput{}
	for _, e := range expected {
		found := false
		for _, row := range rows {
			if row.ID != e.ID {
				continue
			}
			old := db.MaterialClaimInput(row)
			resolveMaterialTargets(&old, links)
			if !db.MaterialClaimDuplicate(item, old) {
				return nil, false, errors.New("只能保留同一对象下属性值和原文都相同的重复条目")
			}
			if rest, ok := materialOwnersRemaining(old, map[string]bool{owner: true}); ok {
				residuals = append(residuals, rest)
			}
			found = true
			break
		}
		if !found {
			return nil, false, db.ErrMaterialClaimConflict
		}
	}
	return residuals, len(residuals) > 0, nil
}
