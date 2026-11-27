package danmakutest

import (
	"sort"
	"strings"

	"livecompanion/management/internal/model"
)

func ObjectLabel(name string, links []Link, stores []model.MaterialStoreObject) (string, string) {
	name = strings.TrimSpace(name)
	if name == "全局" || name == "" {
		return "全店通用", "不区分链接，适用于本直播间所有商品"
	}
	if strings.Contains(name, "、") {
		return name + "共用", "同时适用于" + name
	}
	for _, store := range stores {
		if strings.TrimSpace(store.Name) == name {
			return "线下实体门店", "线下实体门店信息，不是线上链接"
		}
	}
	return name, "仅适用于" + name
}

func CompactEntries(hits []Hit) []map[string]any {
	index := map[string]int{}
	entries := []map[string]any{}
	for _, hit := range hits {
		text := strings.TrimSpace(hit.Original)
		if text == "" {
			text = strings.TrimSpace(hit.Content)
		}
		if text == "" {
			continue
		}
		if hit.Conditions != "" && !strings.Contains(text, hit.Conditions) {
			text += "；条件：" + hit.Conditions
		}
		if hit.Exceptions != "" && !strings.Contains(text, hit.Exceptions) {
			text += "；例外：" + hit.Exceptions
		}
		if i, ok := index[text]; ok {
			ids := entries[i]["ids"].([]int64)
			ids = append(ids, hit.ID)
			sort.Slice(ids, func(a, b int) bool { return ids[a] < ids[b] })
			entries[i]["ids"] = ids
			continue
		}
		index[text] = len(entries)
		entries = append(entries, map[string]any{"ids": []int64{hit.ID}, "text": text})
	}
	return entries
}

// BuildFactCard is deterministic: no model call and no ranking. It is the
// persisted asset read by the danmaku answer pipeline.
func BuildFactCard(records []Record, links []Link, stores []model.MaterialStoreObject) map[string]any {
	groups, _ := AllFactsByObject(records, links, stores)
	factsByObject := []map[string]any{}
	for _, group := range groups {
		object, meaning := ObjectLabel(group.Ask.Text, links, stores)
		factsByObject = append(factsByObject, map[string]any{"object": object, "meaning": meaning, "facts": CompactEntries(group.Hits)})
	}
	return map[string]any{"facts_by_object": factsByObject}
}
