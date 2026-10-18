package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"livecompanion/management/internal/contentpools"
	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
)

func benefitScopeProducts(b model.LiveAgentPlanBenefit, products []model.LiveAgentPlanProductLink, overrides map[int64]appdb.BenefitScope) []int64 {
	ids := []int64{}
	if scope, ok := overrides[b.ID]; ok {
		if !scope.All {
			for _, p := range products {
				if p.Status == "active" && contentpools.Contains(scope.Products, p.ID) {
					ids = append(ids, p.ID)
				}
			}
			return contentpools.IDs(ids)
		}
		for _, p := range products {
			if p.Status == "active" {
				ids = append(ids, p.ID)
			}
		}
		return contentpools.IDs(ids)
	}
	for _, p := range products {
		if p.Status == "active" && (b.LinkKey == "" || b.LinkKey == p.LinkKey) {
			ids = append(ids, p.ID)
		}
	}
	return contentpools.IDs(ids)
}
func applyContentBenefitScopes(benefits []model.LiveAgentPlanBenefit, links []model.LiveAgentPlanProductLink, scopes map[int64]appdb.BenefitScope) []model.LiveAgentPlanBenefit {
	copy := append([]model.LiveAgentPlanBenefit(nil), benefits...)
	for i, b := range copy {
		if _, ok := scopes[b.ID]; !ok {
			continue
		}
		ids := benefitScopeProducts(b, links, scopes)
		keys := []string{}
		for _, p := range links {
			if contentpools.Contains(ids, p.ID) {
				keys = append(keys, p.LinkKey)
			}
		}
		copy[i].LinkKey = strings.Join(keys, "、")
		copy[i].ProductName = ""
	}
	return copy
}
func activeContentBenefit(b model.LiveAgentPlanBenefit, now time.Time) bool {
	return b.Status == "active" && (b.ValidityMode == "permanent" || ((b.StartsAt == nil || !now.Before(*b.StartsAt)) && (b.EndsAt == nil || now.Before(*b.EndsAt))))
}

// No model call. Existing per-entity revision tables retain the detailed edit
// history, while a source snapshot makes net differences survive page reloads.
func (s *Server) captureContentSources(ctx context.Context, tenant, plan int64) (contentpools.Sources, error) {
	for attempt := 0; attempt < 2; attempt++ {
		rev, err := s.store.ContentSourceRevision(ctx, tenant, plan)
		if err != nil {
			return contentpools.Sources{}, err
		}
		links, err := s.store.ListLiveAgentPlanProductLinks(ctx, tenant, plan)
		if err != nil {
			return contentpools.Sources{}, err
		}
		facts, err := s.store.ListLiveAgentPlanFacts(ctx, tenant, plan)
		if err != nil {
			return contentpools.Sources{}, err
		}
		benefits, err := s.store.ListLiveAgentPlanBenefits(ctx, tenant, plan)
		if err != nil {
			return contentpools.Sources{}, err
		}
		scopes, err := s.store.ListBenefitScopes(ctx, tenant, plan)
		if err != nil {
			return contentpools.Sources{}, err
		}
		out := contentpools.Sources{Revision: rev, ObservedAt: time.Now().UTC().UnixMilli(), Products: []contentpools.Product{}, Rows: []contentpools.SourceRow{}}
		out.Corrections, err = s.store.ListContentCorrections(ctx, tenant, plan)
		if err != nil {
			return out, err
		}
		for _, p := range links {
			if p.Status != "active" {
				continue
			}
			out.Products = append(out.Products, contentpools.Product{ID: p.ID, Link: p.LinkKey, Name: p.ProductName, Roles: p.RoomRoles})
			roles := append([]string{}, p.RoomRoles...)
			sort.Strings(roles)
			out.Rows = append(out.Rows, contentpools.SourceRow{ID: fmt.Sprintf("products:%d", p.ID), Area: "products", Label: p.LinkKey + "号 · " + p.ProductName, Products: []int64{p.ID}, Fields: map[string]string{"商品名称": p.ProductName, "链接": p.LinkKey, p.SourceFieldLabel("spec", "规格"): p.Spec, p.SourceFieldLabel("daily_price", "日常价"): p.DailyPrice, p.SourceFieldLabel("quantity", "数量"): p.Quantity, p.SourceFieldLabel("audience", "适用人群"): p.Audience, "定位": strings.Join(roles, "、")}})
			for _, a := range p.Attributes {
				if a.Status != "active" {
					continue
				}
				out.Rows = append(out.Rows, contentpools.SourceRow{ID: fmt.Sprintf("attributes:%d", a.ID), Area: "products", Label: p.LinkKey + "号 · " + a.Label, Products: []int64{p.ID}, Fields: map[string]string{"属性": a.Label, "属性编号": a.Code, "内容": a.Value, "单位": a.Unit}})
			}
		}
		for _, f := range facts {
			if f.SourceType == "story_lecture" {
				continue
			}
			if f.Status != "active" {
				continue
			}
			out.Rows = append(out.Rows, contentpools.SourceRow{ID: fmt.Sprintf("knowledge:%d", f.ID), Area: "knowledge", Label: f.Key, Products: []int64{}, Fields: map[string]string{"名称": f.Key, "分类": f.Category, "事实": f.Value, "安全表达": f.SafeRewrite, "禁说": f.ForbiddenWording}})
		}
		now := time.Now().UTC()
		for _, b := range benefits {
			if b.Status == "disabled" {
				continue
			}
			start, end := "", ""
			if b.StartsAt != nil {
				start = b.StartsAt.Format(time.RFC3339)
			}
			if b.EndsAt != nil {
				end = b.EndsAt.Format(time.RFC3339)
			}
			ids := benefitScopeProducts(b, links, scopes)
			scopeText := []string{}
			for _, p := range out.Products {
				if contentpools.Contains(ids, p.ID) {
					scopeText = append(scopeText, p.Link)
				}
			}
			out.Rows = append(out.Rows, contentpools.SourceRow{ID: fmt.Sprintf("benefits:%d", b.ID), Area: "benefits", Label: b.Key, Products: ids, Fields: map[string]string{"名称": b.Key, "活动价": b.ActivityPrice, "赠品": b.Gift, "活动": b.Activity, "开始": start, "结束": end, "有效方式": b.ValidityMode, "状态": b.Status, "当前有效": fmt.Sprint(activeContentBenefit(b, now)), "适用链接": strings.Join(scopeText, "、")}})
		}
		wheel, err := s.store.GetActiveLiveSellingPointWheel(ctx, tenant, plan)
		if err != nil && !errors.Is(err, appdb.ErrLiveSellingPointWheelNotFound) {
			return out, err
		}
		if wheel.ID > 0 {
			var w anchorStyleSellingPointWheel
			if json.Unmarshal([]byte(wheel.EffectiveWheelJSON()), &w) == nil {
				lines := []string{}
				for _, p := range w.Items {
					lines = append(lines, p.Name+"："+strings.Join(p.Angles, "；"))
				}
				out.Rows = append(out.Rows, contentpools.SourceRow{ID: "wheel:active", Area: "miner", Label: "卖点手动编辑", Products: []int64{}, Fields: map[string]string{"内容": strings.Join(lines, "\n")}})
			}
		}
		sort.Slice(out.Products, func(i, j int) bool { return out.Products[i].ID < out.Products[j].ID })
		sort.Slice(out.Rows, func(i, j int) bool { return out.Rows[i].ID < out.Rows[j].ID })
		endRev, err := s.store.ContentSourceRevision(ctx, tenant, plan)
		if err != nil {
			return out, err
		}
		if endRev == rev {
			return out, nil
		}
	}
	return contentpools.Sources{}, appdb.ErrPoolSourceChanged
}

type contentFactHeader struct {
	key, body, link  string
	explicit, shared bool
}

// Scope is program-owned metadata. Read the same JSON link prefix as the
// runtime before looking up the category/name; quoted delimiters are not keys.
func parseContentFactHeader(line string) (contentFactHeader, error) {
	var header contentFactHeader
	scope, explicit, err := explicitPlanFactScope(line)
	if err != nil {
		return header, err
	}
	header.explicit, header.shared = explicit, scope.Shared
	rest := strings.TrimPrefix(line, "- [")
	if explicit && scope.Shared {
		rest = strings.TrimPrefix(line, explicitCommonFactPrefix)
	} else if explicit {
		rest = strings.TrimPrefix(line, explicitProductFactPrefix)
		decoder := json.NewDecoder(strings.NewReader(rest))
		if err := decoder.Decode(&header.link); err != nil {
			return header, errors.New("无法读取事实归属商品")
		}
		rest = strings.TrimPrefix(rest[decoder.InputOffset():], "；")
	}
	end := strings.Index(rest, "]")
	if end < 0 || strings.TrimSpace(rest[:end]) == "" {
		return header, errors.New("事实标题缺失或未闭合")
	}
	header.key, header.body = rest[:end], strings.TrimSpace(rest[end+1:])
	return header, nil
}

func contentFactsFromSafety(source contentpools.Sources, safe string) ([]contentpools.Fact, error) {
	products := map[string]int64{}
	all := []int64{}
	rows := map[string]contentpools.SourceRow{}
	for _, p := range source.Products {
		products[p.Link] = p.ID
		all = append(all, p.ID)
	}
	for _, r := range source.Rows {
		rows[r.ID] = r
	}
	// A duplicate display key is ambiguous, never permission to use the last row.
	knowledge := map[string][]string{}
	benefits := map[string]contentpools.SourceRow{}
	for _, r := range source.Rows {
		if r.Area == "knowledge" {
			key := r.Fields["分类"] + "/" + r.Fields["名称"]
			knowledge[key] = append(knowledge[key], r.ID)
		}
		if r.Area == "benefits" && r.Fields["当前有效"] == "true" {
			benefits[r.Fields["名称"]] = r
		}
	}
	out := []contentpools.Fact{}
	for i, line := range strings.Split(safe, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "- [") {
			continue
		}
		header, err := parseContentFactHeader(line)
		if err != nil {
			return nil, fmt.Errorf("事实第%d行归属或标题无效：%w", i+1, err)
		}
		if !header.explicit && header.key == "商品卡" {
			id := products[safeProductField(line, "链接")]
			if id == 0 {
				continue
			}
			row := rows[fmt.Sprintf("products:%d", id)]
			for _, part := range contentCardFields(header.body, source, id) {
				key, value, ok := strings.Cut(part, "=")
				if !ok || value == "" {
					continue
				}
				origin := row.ID
				private := key == "链接" || key == "商品" || key == "规格" || key == "日常价" || key == "数量" || key == "适用"
				for _, a := range source.Rows {
					if a.Area == "products" && len(a.Products) == 1 && a.Products[0] == id && a.Fields["属性"] == key {
						origin = a.ID
						private = private || strings.Contains(a.Fields["属性编号"], "price") || strings.Contains(key, "价") || strings.Contains(key, "规格")
					}
				}
				out = append(out, contentpools.Fact{ID: contentFactFieldID(origin, key), Text: "- [商品事实] " + part, Sources: []string{origin}, Products: []int64{id}, Owner: id, Private: private})
			}
			continue
		}
		if !header.explicit && header.key == "有效福利" {
			r := benefits[safeProductField(line, "福利")]
			if r.ID == "" {
				continue
			}
			out = append(out, contentpools.Fact{ID: r.ID, Text: line, Sources: []string{r.ID}, Products: r.Products, Private: true})
			continue
		}
		ids := knowledge[header.key]
		if len(ids) == 0 || (len(ids) == 1 && ids[0] == "") {
			return nil, fmt.Errorf("事实第%d行「%s」无法关联当前快照来源，请核对事实类型和名称", i+1, header.key)
		}
		if len(ids) != 1 {
			return nil, fmt.Errorf("事实第%d行「%s」对应多个快照来源，请先区分同名事实", i+1, header.key)
		}
		applicable := append([]int64{}, all...)
		var owner int64
		if header.explicit && !header.shared {
			owner = products[header.link]
			if owner == 0 {
				return nil, fmt.Errorf("事实第%d行「%s」的归属商品「%s」不在当前快照中", i+1, header.key, header.link)
			}
			applicable = []int64{owner}
		}
		// Explicit common facts bypass model reclassification; explicit product
		// facts must also survive equal-text merging without widening their scope.
		out = append(out, contentpools.Fact{ID: ids[0], Text: line, Sources: []string{ids[0]}, Products: applicable, Owner: owner, Private: header.explicit})
	}
	return out, nil
}

// Keep semicolon-separated conditions attached to their actual field. Field
// identity follows the attribute record ID, never its display position.
func contentFactFieldID(origin, key string) string {
	if strings.HasPrefix(origin, "attributes:") {
		return origin
	}
	return origin + ":field:" + contentpools.Digest(key)[:12]
}
func contentCardFields(body string, source contentpools.Sources, product int64) []string {
	known := map[string]bool{"链接": true, "商品": true, "规格": true, "日常价": true, "数量": true, "适用": true}
	for _, r := range source.Rows {
		if r.Area == "products" && len(r.Products) == 1 && r.Products[0] == product && r.Fields["属性"] != "" {
			known[r.Fields["属性"]] = true
		}
	}
	fields := []string{}
	for _, part := range strings.Split(body, "；") {
		key, _, ok := strings.Cut(part, "=")
		if ok && known[key] {
			fields = append(fields, part)
		} else if len(fields) > 0 {
			fields[len(fields)-1] += "；" + part
		}
	}
	return fields
}
