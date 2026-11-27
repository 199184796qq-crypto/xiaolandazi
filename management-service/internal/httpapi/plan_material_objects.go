package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
	"net/http"
	"regexp"
	"slices"
	"sort"
	"strings"
)

type materialLinkObject struct {
	ID          int64  `json:"id,omitempty"`
	ObjectID    int64  `json:"object_id,omitempty"`
	LinkKey     string `json:"link_key,omitempty"`
	ProductName string `json:"product_name,omitempty"`
	DisplayName string `json:"display_name,omitempty"`
}

func (s *Server) materialLinkObjects(r *http.Request, tenantID, planID int64) ([]materialLinkObject, error) {
	links, err := s.materialProductLinks(r, tenantID, planID)
	if err != nil {
		return nil, err
	}
	catalog, err := s.store.PlanMaterialObjects(r.Context(), tenantID, planID)
	if err != nil {
		return nil, err
	}
	return materialAnalysisLinks(links, catalog.Directory), nil
}

func materialAnalysisLinks(links []materialLinkObject, directory model.MaterialObjectDirectory) []materialLinkObject {
	links = visibleMaterialLinks(links, directory)
	for _, object := range directory.Objects {
		links = append(links, materialLinkObject{ObjectID: object.ID, DisplayName: object.DisplayName})
	}
	return links
}

func (s *Server) materialProductLinks(r *http.Request, tenantID, planID int64) ([]materialLinkObject, error) {
	links, err := s.store.ListLiveAgentPlanProductLinks(r.Context(), tenantID, planID)
	if err != nil {
		return nil, err
	}
	out := []materialLinkObject{}
	for _, it := range links {
		out = append(out, materialLinkObject{ID: it.ID, LinkKey: it.LinkKey, ProductName: it.ProductName, DisplayName: it.FieldLabels["display_name"]})
	}
	return out, nil
}

func visibleMaterialLinks(links []materialLinkObject, directory model.MaterialObjectDirectory) []materialLinkObject {
	links = slices.DeleteFunc(links, func(link materialLinkObject) bool {
		return slices.Contains(directory.RemovedLinkIDs, link.ID)
	})
	for i := range links {
		if name := directory.LinkDisplayNames[links[i].ID]; name != "" {
			links[i].DisplayName = name
		}
	}
	return links
}

func (s *Server) planMaterialObjects(w http.ResponseWriter, r *http.Request) {
	_, planID, tenantID, ok := s.resolvePlanMaterialClaimScope(w, r)
	if !ok {
		return
	}
	links, err := s.materialProductLinks(r, tenantID, planID)
	if err != nil {
		writeError(w, 500, "读取方案链接失败")
		return
	}
	catalog, err := s.store.PlanMaterialObjects(r.Context(), tenantID, planID)
	if err != nil {
		writeError(w, 500, "读取门店关联失败")
		return
	}
	if r.Method == http.MethodGet {
		writeMaterialObjects(w, links, catalog)
		return
	}
	if r.Method == http.MethodPatch {
		var change db.MaterialObjectChange
		if readJSON(w, r, &change) != nil {
			return
		}
		change.Name = strings.TrimSpace(change.Name)
		if !(change.Action == "reorder" && change.Kind == "" || change.Action == "add" && change.Kind == "object" || change.Action == "rename" && (change.Kind == "plan" || change.Kind == "product" || change.Kind == "object") || change.Action == "delete" && (change.Kind == "product" || change.Kind == "object")) {
			writeError(w, 400, "对象操作无效")
			return
		}
		names := make(map[int64]string, len(links))
		var linkNo int64
		for _, link := range links {
			names[link.ID] = link.DisplayName
			if names[link.ID] == "" {
				names[link.ID] = materialTargetLabel(model.MaterialObjectTarget{Kind: "product", LinkID: link.ID, LinkKey: link.LinkKey})
			}
			if change.Kind == "product" && change.ID == link.ID {
				if m := materialSourceNumberedLink.FindStringSubmatch(strings.TrimSpace(link.LinkKey)); m != nil {
					linkNo = materialInt64(m[1])
				}
			}
		}
		err = s.store.ChangePlanMaterialObject(r.Context(), tenantID, planID, change, names, linkNo)
		switch {
		case errors.Is(err, db.ErrMaterialObjectsConflict):
			writeError(w, 409, "对象目录已更新，请刷新后重试")
		case errors.Is(err, db.ErrMaterialObjectMissing):
			writeError(w, 404, "对象已移除，请刷新后重试")
		case errors.Is(err, db.ErrMaterialObjectName):
			writeError(w, 400, "请输入 1 至 50 字的显示名称，名称不能重复")
		case err != nil:
			writeError(w, 500, "保存对象失败，请刷新后重试")
		default:
			if change.Action != "reorder" {
				s.scheduleFactCardRebuild(tenantID, planID)
			}
			s.planMaterialObjectsResponse(w, r, tenantID, planID, links)
		}
		return
	}
	var input model.MaterialObjectCatalog
	if err := readJSON(w, r, &input); err != nil {
		return
	}
	if err := validateMaterialStores(input.Stores, links); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	revision, err := s.store.SavePlanMaterialObjects(r.Context(), tenantID, planID, input.Revision, input.Stores)
	if errors.Is(err, db.ErrMaterialObjectsInUse) {
		writeError(w, 409, "请先重新分配该门店下的属性，再移除门店")
		return
	}
	if errors.Is(err, db.ErrMaterialObjectsConflict) {
		writeError(w, 409, "门店关联已更新，请刷新后重试")
		return
	}
	if err != nil {
		writeError(w, 500, "保存门店关联失败，原关联保留")
		return
	}
	s.scheduleFactCardRebuild(tenantID, planID)
	catalog.Stores, catalog.Revision = input.Stores, revision
	writeMaterialObjects(w, links, catalog)
}

func (s *Server) planMaterialObjectsResponse(w http.ResponseWriter, r *http.Request, tenant, plan int64, links []materialLinkObject) {
	catalog, err := s.store.PlanMaterialObjects(r.Context(), tenant, plan)
	if err != nil {
		writeError(w, 500, "读取对象目录失败，请刷新")
		return
	}
	writeMaterialObjects(w, links, catalog)
}

func writeMaterialObjects(w http.ResponseWriter, links []materialLinkObject, catalog model.MaterialObjectCatalog) {
	visible := visibleMaterialLinks(slices.Clone(links), catalog.Directory)
	writeJSON(w, 200, map[string]any{"links": visible, "binding_links": links, "stores": catalog.Stores, "revision": catalog.Revision, "directory": catalog.Directory})
}
func validateMaterialStores(stores []model.MaterialStoreObject, links []materialLinkObject) error {
	if len(stores) > 100 {
		return errors.New("门店最多100个")
	}
	known := map[int64]bool{}
	for _, l := range links {
		known[l.ID] = true
	}
	names := map[string]bool{}
	for i := range stores {
		s := &stores[i]
		s.Name = strings.TrimSpace(s.Name)
		if s.Name == "" || len([]rune(s.Name)) > 100 || genericMaterialStore(s.Name) {
			return errors.New("请填写具体门店或商家名称")
		}
		if names[s.Name] {
			return errors.New("门店名称重复")
		}
		names[s.Name] = true
		seen := map[int64]bool{}
		ids := []int64{}
		for _, id := range s.LinkIDs {
			if !known[id] {
				return errors.New("关联链接不属于当前方案，请刷新")
			}
			if !seen[id] {
				ids = append(ids, id)
				seen[id] = true
			}
		}
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		s.LinkIDs = ids
	}
	return nil
}
func genericMaterialStore(name string) bool {
	switch strings.TrimSpace(name) {
	case "本直播间", "直播间", "本店", "门店", "线下门店", "全部门店", "商家", "全场":
		return true
	}
	return false
}
func materialTargets(it model.PlanMaterialClaimInput) []model.MaterialObjectTarget {
	if it.ObjectTargets != nil {
		return it.ObjectTargets
	}
	switch it.ScopeKind {
	case "unresolved", "object":
		return []model.MaterialObjectTarget{}
	case "product":
		if it.ProductLinkID > 0 {
			return []model.MaterialObjectTarget{{Kind: "product", LinkNo: it.ProductLinkID}}
		}
		return []model.MaterialObjectTarget{}
	case "store":
		if strings.TrimSpace(it.StoreName) == "" || it.StoreName == "未说明" {
			return []model.MaterialObjectTarget{}
		}
		if !genericMaterialStore(it.StoreName) {
			return []model.MaterialObjectTarget{{Kind: "store", StoreName: strings.TrimSpace(it.StoreName)}}
		}
	case "item", "variant", "voucher", "item_group":
		return []model.MaterialObjectTarget{}
	case "service":
		if it.ScopeName == "" || it.ScopeName == "未说明" {
			return []model.MaterialObjectTarget{}
		}
	}
	return []model.MaterialObjectTarget{{Kind: "plan"}}
}
func normalizeMaterialTargets(it *model.PlanMaterialClaimInput) {
	targets := materialTargets(*it)
	out := []model.MaterialObjectTarget{}
	seen := map[string]bool{}
	global := false
	invalid := false
	for _, t := range targets {
		switch t.Kind {
		case "plan":
			global = true
			t = model.MaterialObjectTarget{Kind: "plan"}
		case "product":
			if t.LinkID <= 0 && t.LinkNo <= 0 {
				invalid = true
				continue
			}
			t.StoreName = ""
			t.ObjectID = 0
		case "object":
			if t.ObjectID <= 0 {
				invalid = true
				continue
			}
			t = model.MaterialObjectTarget{Kind: "object", ObjectID: t.ObjectID}
		case "store":
			t.StoreName = strings.TrimSpace(t.StoreName)
			if t.StoreName == "" || t.StoreName == "未说明" || len([]rune(t.StoreName)) > 100 {
				invalid = true
				continue
			}
			if genericMaterialStore(t.StoreName) {
				global = true
				t = model.MaterialObjectTarget{Kind: "plan"}
			} else {
				t.LinkID = 0
				t.LinkNo = 0
				t.LinkKey = ""
				t.ObjectID = 0
			}
		default:
			invalid = true
			continue
		}
		raw, _ := json.Marshal(t)
		key := string(raw)
		if !seen[key] {
			out = append(out, t)
			seen[key] = true
		}
	}
	if global && len(out) > 1 {
		invalid = true
	}
	if invalid {
		out = []model.MaterialObjectTarget{}
	} else if global {
		out = []model.MaterialObjectTarget{{Kind: "plan"}}
	}
	sort.Slice(out, func(i, j int) bool {
		a, _ := json.Marshal(out[i])
		b, _ := json.Marshal(out[j])
		return string(a) < string(b)
	})
	it.ObjectTargets = out
	it.ScopeName = ""
	it.StoreName = ""
	it.ProductLinkID = 0
	it.ScopeKind = "unresolved"
	if len(out) > 0 {
		t := out[0]
		it.ScopeKind = t.Kind
		it.StoreName = t.StoreName
		if t.LinkID > 0 {
			it.ProductLinkID = t.LinkID
		} else {
			it.ProductLinkID = t.LinkNo
		}
	}
}
func resolveMaterialTargets(it *model.PlanMaterialClaimInput, links []materialLinkObject) {
	normalizeMaterialTargets(it)
	targets := it.ObjectTargets
	for i, t := range targets {
		if t.Kind == "object" {
			if !slices.ContainsFunc(links, func(link materialLinkObject) bool { return link.ObjectID == t.ObjectID }) {
				it.ObjectTargets = []model.MaterialObjectTarget{}
				normalizeMaterialTargets(it)
				it.NeedsConfirmation = true
				return
			}
			continue
		}
		if t.Kind != "product" {
			continue
		}
		matches := []materialLinkObject{}
		for _, l := range links {
			if t.LinkID > 0 && l.ID == t.LinkID {
				matches = append(matches, l)
			} else if t.LinkID == 0 && t.LinkNo > 0 {
				m := regexp.MustCompile(`^第?(\d+)\s*(?:号(?:链接)?)?$`).FindStringSubmatch(strings.TrimSpace(l.LinkKey))
				if m != nil && materialInt64(m[1]) == t.LinkNo {
					matches = append(matches, l)
				}
			}
		}
		if len(matches) != 1 {
			it.ObjectTargets = []model.MaterialObjectTarget{}
			it.ScopeKind = "unresolved"
			it.ProductLinkID = 0
			it.NeedsConfirmation = true
			return
		}
		targets[i] = model.MaterialObjectTarget{Kind: "product", LinkID: matches[0].ID, LinkKey: matches[0].LinkKey}
	}
	it.ObjectTargets = targets
	normalizeMaterialTargets(it)
}
func materialTargetLabel(t model.MaterialObjectTarget) string {
	if t.Kind == "object" {
		return fmt.Sprintf("对象%d", t.ObjectID)
	}
	if t.Kind == "plan" {
		return "全局（本方案全部链接通用）"
	}
	if t.Kind == "store" {
		return "门店：" + t.StoreName
	}
	if t.LinkKey != "" {
		key := strings.TrimSpace(t.LinkKey)
		if regexp.MustCompile(`^\d+$`).MatchString(key) {
			return key + "号链接"
		}
		return key
	}
	if t.LinkNo > 0 {
		return fmt.Sprintf("%d号链接", t.LinkNo)
	}
	return fmt.Sprintf("链接%d", t.LinkID)
}
