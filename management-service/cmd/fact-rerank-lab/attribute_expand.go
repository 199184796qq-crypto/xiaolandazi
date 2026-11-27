package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"

	"livecompanion/management/internal/semantic"
)

// ensureAttributeVectors 加载方案级“属性概念向量”：
// 优先读 live_agent_plan_attribute_vectors，文本 hash 不一致或缺失时才在内存里补算。
func (l *lab) ensureAttributeVectors(ctx context.Context, cached *cachedPlan) error {
	l.mu.Lock()
	if cached.attributeVectorsLoaded && len(cached.attributeVectors) > 0 {
		l.mu.Unlock()
		return nil
	}
	l.mu.Unlock()
	conceptText := map[string][]string{}
	for _, item := range cached.facts {
		name := item.attributeName()
		if name == "" {
			continue
		}
		if len(conceptText[name]) >= 3 {
			continue
		}
		value := strings.TrimSpace(item.Content)
		if value == "" {
			value = strings.TrimSpace(item.Quote)
		}
		if value == "" {
			continue
		}
		conceptText[name] = append(conceptText[name], value)
	}
	names := make([]string, 0, len(conceptText))
	for name := range conceptText {
		names = append(names, name)
	}
	sort.Strings(names)
	type storedVector struct {
		hash   string
		vector []float32
	}
	stored := map[string]storedVector{}
	rows, err := l.db.QueryContext(ctx, `SELECT attribute_name,concept_text_hash,embedding_json FROM live_agent_plan_attribute_vectors WHERE tenant_id=? AND plan_id=?`, cached.plan.TenantID, cached.plan.ID)
	if err == nil {
		for rows.Next() {
			var name, hash, raw string
			if rows.Scan(&name, &hash, &raw) != nil {
				continue
			}
			var vector []float32
			if json.Unmarshal([]byte(raw), &vector) == nil && len(vector) > 0 {
				stored[name] = storedVector{hash: hash, vector: vector}
			}
		}
		rows.Close()
	}
	l.mu.Lock()
	if cached.attributeVectors == nil {
		cached.attributeVectors = map[string][]float32{}
	}
	missing := []string{}
	missingTexts := []string{}
	for _, name := range names {
		text := name
		if len(conceptText[name]) > 0 {
			text += "：" + strings.Join(conceptText[name], "；")
		}
		hash := sha256Hex(text)
		if item, ok := stored[name]; ok && item.hash == hash && len(item.vector) > 0 {
			cached.attributeVectors[name] = item.vector
			continue
		}
		if len(cached.attributeVectors[name]) > 0 {
			continue
		}
		missing = append(missing, name)
		missingTexts = append(missingTexts, text)
	}
	l.mu.Unlock()
	if len(missing) == 0 {
		l.mu.Lock()
		cached.attributeVectorsLoaded = true
		l.mu.Unlock()
		return nil
	}
	log.Printf("attr-vectors fallback compute plan=%d missing=%d", cached.plan.ID, len(missing))
	vectors, err := l.embed(ctx, missingTexts)
	if err != nil || len(vectors) != len(missing) {
		return fmt.Errorf("属性向量生成失败: %w", err)
	}
	l.mu.Lock()
	for index, name := range missing {
		cached.attributeVectors[name] = vectors[index]
	}
	cached.attributeVectorsLoaded = true
	l.mu.Unlock()
	return nil
}

func sha256Hex(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

// expandAttributesFromQuery 用问题本身找最相近的属性，最多补 3 个。
// 这是通用机制，不维护“好吃→口感”之类的业务词表。
func (l *lab) expandAttributesFromQuery(ctx context.Context, cached *cachedPlan, query string, current []string) ([]string, []string) {
	query = strings.TrimSpace(query)
	if query == "" {
		return current, nil
	}
	// 属性匹配时先去掉对象名和口语泛词，避免“1号链接的商品好吃吗”里的对象/泛词稀释属性语义。
	queryForAttr := query
	for _, name := range cached.objectNames {
		if name != "" {
			queryForAttr = strings.ReplaceAll(queryForAttr, name, " ")
		}
	}
	for _, word := range []string{"商品", "东西", "这个", "那个", "这款", "哪款", "怎么样", "好不好", "帮我", "看看", "请问", "的", "吗", "呢", "啊", "呀", "是"} {
		queryForAttr = strings.ReplaceAll(queryForAttr, word, " ")
	}
	queryForAttr = strings.TrimSpace(strings.Join(strings.Fields(queryForAttr), " "))
	if queryForAttr == "" {
		queryForAttr = query
	}
	if err := l.ensureAttributeVectors(ctx, cached); err != nil {
		return current, nil
	}
	queryVectors, err := l.embed(ctx, []string{queryForAttr})
	if err != nil || len(queryVectors) != 1 {
		return current, nil
	}
	existing := map[string]bool{}
	for _, attribute := range current {
		existing[attribute] = true
	}
	type scored struct {
		name  string
		score float64
	}
	all := []scored{}
	l.mu.Lock()
	for name, vector := range cached.attributeVectors {
		if existing[name] {
			continue
		}
		if len(vector) != len(queryVectors[0]) {
			log.Printf("attr-expand dimension mismatch name=%s vector=%d query=%d", name, len(vector), len(queryVectors[0]))
			continue
		}
		score := semantic.CosineSimilarity(queryVectors[0], vector)
		all = append(all, scored{name: name, score: score})
	}
	l.mu.Unlock()
	sort.SliceStable(all, func(i, j int) bool { return all[i].score > all[j].score })
	debugTop := []string{}
	for i := 0; i < len(all) && i < 8; i++ {
		debugTop = append(debugTop, fmt.Sprintf("%s(%.3f)", all[i].name, all[i].score))
	}
	log.Printf("attr-expand query=%q top=%v", queryForAttr, debugTop)
	const attributeExpandThreshold = 0.45
	ranked := []scored{}
	for _, item := range all {
		if item.score >= attributeExpandThreshold {
			ranked = append(ranked, item)
		}
	}
	extra := []string{}
	notes := []string{}
	for _, item := range ranked {
		if len(extra) >= 3 {
			break
		}
		extra = append(extra, item.name)
		notes = append(notes, fmt.Sprintf("%s(%.2f)", item.name, item.score))
	}
	if len(extra) == 0 {
		return current, nil
	}
	return append(append([]string{}, current...), extra...), notes
}
