package main

import (
	"context"
	"encoding/json"
	"log"
	"slices"
	"strconv"
	"strings"

	"livecompanion/management/internal/contextword"
	"livecompanion/management/internal/danmakutest"
	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
)

// planIntentContext 是线上意图 payload 需要的方案侧资料：
// 链接目录（商品链接 + 对象目录）、门店、语境词。
// 对齐 answerflow.intentPayload / danmakuLibraryFromSnapshot：
//   - objects = 链接（link_key 数字转“N号链接”，display 取目录或 field_labels）
//   - 对象目录（object_id/display_name）
//   - stores = live_agent_plan_material_objects.stores_json 里的门店名
//   - context_words = live_agent_plan_context_words.items_json 经 contextword.Match
type planIntentContext struct {
	Links  []danmakutest.Link
	Stores []model.MaterialStoreObject
	Words  contextword.Catalog
}

func (l *lab) loadIntentContext(ctx context.Context, tenant, planID int64) planIntentContext {
	out := planIntentContext{Links: []danmakutest.Link{}, Stores: []model.MaterialStoreObject{}, Words: contextword.Catalog{Items: []contextword.Entry{}}}
	var storesRaw, directoryRaw []byte
	err := l.db.QueryRowContext(ctx, `SELECT stores_json,directory_json FROM live_agent_plan_material_objects WHERE tenant_id=? AND plan_id=?`, tenant, planID).
		Scan(&storesRaw, &directoryRaw)
	var directory model.MaterialObjectDirectory
	if err == nil && len(directoryRaw) > 0 {
		_ = json.Unmarshal(directoryRaw, &directory)
	}
	if err == nil && len(storesRaw) > 0 {
		_ = json.Unmarshal(storesRaw, &out.Stores)
	}
	removed := map[int64]bool{}
	for _, id := range directory.RemovedLinkIDs {
		removed[id] = true
	}
	rows, err := l.db.QueryContext(ctx, `SELECT id,link_key,product_name,COALESCE(field_labels_json,JSON_OBJECT())
		FROM live_agent_plan_product_links WHERE tenant_id=? AND plan_id=? ORDER BY id`, tenant, planID)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var id int64
			var linkKey, product string
			var labelsRaw []byte
			if err := rows.Scan(&id, &linkKey, &product, &labelsRaw); err != nil {
				continue
			}
			labels := map[string]string{}
			_ = json.Unmarshal(labelsRaw, &labels)
			name := strings.TrimSpace(linkKey)
			if numeric, err := strconv.ParseInt(name, 10, 64); err == nil {
				name = strconv.FormatInt(numeric, 10) + "号链接"
			}
			display := directory.LinkDisplayNames[id]
			if display == "" {
				display = strings.TrimSpace(labels["display_name"])
			}
			out.Links = append(out.Links, danmakutest.Link{ID: id, Name: name, DisplayName: display, Product: product, MaterialHidden: removed[id]})
		}
	}
	for _, object := range directory.Objects {
		out.Links = append(out.Links, danmakutest.Link{ObjectID: object.ID, Name: object.DisplayName})
	}

	var wordsRevision int64
	var wordsRaw []byte
	if err := l.db.QueryRowContext(ctx, `SELECT revision,items_json FROM live_agent_plan_context_words WHERE tenant_id=? AND plan_id=?`, tenant, planID).
		Scan(&wordsRevision, &wordsRaw); err == nil {
		out.Words.Revision = wordsRevision
		if len(wordsRaw) > 0 {
			_ = json.Unmarshal(wordsRaw, &out.Words.Items)
		}
	}
	return out
}

// intentPayloadAligned 复刻 answerflow.intentPayload 的字段和取值，
// 另提供 objects/attributes 覆盖（只用于本地实验对比，不改变线上字段结构）。
func (l *lab) intentPayloadAligned(cached *cachedPlan, question string, objectOverride, attributeOverride []string) map[string]any {
	links := cached.intentCtx.Links
	stores := cached.intentCtx.Stores
	storeNames := make([]string, 0, len(stores))
	for _, store := range stores {
		storeNames = append(storeNames, store.Name)
	}
	attributes, catalog := cached.attributeDirectory.Names, cached.attributeDirectory.Entries
	if len(attributeOverride) > 0 {
		attributes = []string{}
		catalog = []danmakutest.IntentAttributeEntry{}
		for _, raw := range attributeOverride {
			name := cached.attributeResolver.CanonicalName(raw)
			if slices.Contains(cached.attributeDirectory.Names, name) && !slices.Contains(attributes, name) {
				attributes = append(attributes, name)
			}
		}
		for _, entry := range cached.attributeDirectory.Entries {
			if slices.Contains(attributes, entry.Topic) {
				catalog = append(catalog, entry)
			}
		}
	}
	if len(objectOverride) > 0 {
		links = make([]danmakutest.Link, 0, len(objectOverride))
		for _, name := range objectOverride {
			if name = strings.TrimSpace(name); name != "" {
				links = append(links, danmakutest.Link{Name: name})
			}
		}
	}
	// 注入“全门店”这个全局对象，模型才能把门店类问题归到全局，而不是编一个“门店”对象。
	if cached.globalObjectName != "" {
		found := false
		for _, link := range links {
			if strings.TrimSpace(link.Name) == cached.globalObjectName {
				found = true
				break
			}
		}
		if !found {
			links = append(links, danmakutest.Link{Name: cached.globalObjectName})
		}
	}
	return map[string]any{
		"question":          question,
		"room_intro":        cached.roomIntro,
		"context_words":     contextword.Match(cached.intentCtx.Words.Items, question),
		"objects":           links,
		"stores":            storeNames,
		"attributes":        attributes,
		"attribute_catalog": catalog,
	}
}

func (l *lab) intentAttributeDirectory(ctx context.Context, target plan, facts []fact, intentCtx planIntentContext, globalName string) (danmakutest.IntentAttributeResolver, danmakutest.IntentAttributeDirectory, error) {
	canonical, err := appdb.ReadFactAttributeCatalog(ctx, l.db)
	if err != nil {
		log.Printf("lab intent attribute aliases unavailable plan=%d", target.ID)
	}
	resolver := danmakutest.NewIntentAttributeResolver(intentCtx.Links, intentCtx.Stores, globalName, canonical.CanonicalNames())
	claims, err := appdb.ReadPlanMaterialClaims(ctx, l.db, target.TenantID, target.ID)
	if err != nil {
		return resolver, danmakutest.IntentAttributeDirectory{}, err
	}
	records := make([]danmakutest.Record, 0, len(claims))
	names := make(map[int64]string, len(claims))
	for _, claim := range claims {
		if !claim.SharedSourceNeedsReview {
			records = append(records, danmakutest.Record{Claim: claim})
			names[claim.ID], _ = resolver.ClaimName(claim)
		}
	}
	for i := range facts {
		facts[i].Attribute = names[facts[i].ID]
	}
	directory := resolver.Directory(records)
	log.Printf("lab intent catalog tenant=%d plan=%d facts=%d cataloged=%d filtered_objects=%d attributes=%d",
		target.TenantID, target.ID, len(facts), directory.CatalogedFacts, directory.FilteredObjects, len(directory.Names))
	return resolver, directory, nil
}
