// Package danmakutest implements the test workbench only. It never changes a
// plan, queues audio, or supplies evidence to the formal live executor.
package danmakutest

import (
	"encoding/json"
	"fmt"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/semantic"
	"sort"
	"strconv"
	"strings"
)

type Ask struct {
	Text         string            `json:"text"`
	Subjects     []string          `json:"subjects"`
	Attribute    string            `json:"attribute"`
	AttributeKey string            `json:"attribute_key,omitempty"`
	Keywords     []string          `json:"keywords,omitempty"`
	Conditions   map[string]string `json:"conditions,omitempty"`
}
type Intent struct {
	Meaning       string   `json:"meaning"`
	Category      string   `json:"category"`
	SpeechAct     string   `json:"speech_act"`
	Operation     string   `json:"operation"`
	Corrected     string   `json:"corrected,omitempty"`
	Uncertainties []string `json:"uncertainties"`
	Asks          []Ask    `json:"asks"`
}
type Link struct {
	ID             int64  `json:"-"`
	ObjectID       int64  `json:"-"`
	MaterialHidden bool   `json:"-"`
	Name           string `json:"name"`
	DisplayName    string `json:"display_name,omitempty"`
	Product        string `json:"product"`
}
type Record struct {
	Claim  model.PlanMaterialClaim
	Vector []float32
}
type Hit struct {
	ID              int64   `json:"id"`
	Attribute       string  `json:"attribute"`
	Key             string  `json:"key,omitempty"`
	Content         string  `json:"content"`
	Scope           string  `json:"scope"`
	Score           float64 `json:"score"`
	ConditionStatus string  `json:"condition_status"`
	Conditions      string  `json:"conditions,omitempty"`
	Exceptions      string  `json:"exceptions,omitempty"`
	Original        string  `json:"original,omitempty"`
	Question        string  `json:"question,omitempty"`
	rank            float64
}
type Group struct {
	Ask      Ask    `json:"ask"`
	Hits     []Hit  `json:"hits"`
	Strength string `json:"strength,omitempty"`
	Fallback bool   `json:"fallback,omitempty"`
	Notice   string `json:"notice,omitempty"`
}
type Recall struct {
	Groups  []Group  `json:"groups"`
	Hits    []Hit    `json:"hits"`
	Notices []string `json:"notices"`
}

func NormalizeIntent(in Intent) (Intent, error) {
	in.Meaning = strings.TrimSpace(in.Meaning)
	in.Category = strings.TrimSpace(in.Category)
	in.SpeechAct = strings.TrimSpace(in.SpeechAct)
	in.Operation = strings.TrimSpace(in.Operation)
	if in.Category == "" {
		in.Category = in.SpeechAct
	}
	if in.Meaning == "" || len([]rune(in.Meaning)) > 1600 || len(in.Asks) > 8 || len(in.Uncertainties) > 12 {
		return in, fmt.Errorf("意图结果不完整或诉求过多，请分批测试")
	}
	for i := range in.Asks {
		a := &in.Asks[i]
		a.Text = strings.TrimSpace(a.Text)
		a.Attribute = strings.TrimSpace(a.Attribute)
		a.AttributeKey = strings.TrimSpace(a.AttributeKey)
		a.Keywords = nil
		keywords := []string{}
		for _, keyword := range a.Keywords {
			if keyword = strings.TrimSpace(keyword); keyword != "" && len([]rune(keyword)) <= 20 && !containsString(keywords, keyword) {
				keywords = append(keywords, keyword)
			}
		}
		a.Keywords = keywords
		if len(a.Keywords) > 8 {
			a.Keywords = a.Keywords[:8]
		}
		if a.AttributeKey == "claim" {
			a.AttributeKey = ""
		}
		if a.Text == "" || len([]rune(a.Text)) > 1000 || len(a.Subjects) > 8 || len(a.Conditions) > 16 || len([]rune(a.Attribute)) > 100 {
			return in, fmt.Errorf("诉求结果不完整，请重试")
		}
	}
	return in, nil
}
func Queries(question string, asks []Ask) []string {
	out := []string{}
	seen := map[string]bool{}
	add := func(q string) {
		q = strings.TrimSpace(q)
		if q != "" && !seen[q] {
			out = append(out, q)
			seen[q] = true
		}
	}
	add(question)
	for _, a := range asks {
		// Recall is text-only. Intent attributes and keys are not used here.
		add(a.Text)
		for _, subject := range a.Subjects {
			add(subject)
		}
	}
	return out
}
func containsString(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}

func norm(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	for _, c := range []string{" ", "链接", "第", "市", "省"} {
		s = strings.ReplaceAll(s, c, "")
	}
	return s
}
func targets(c model.PlanMaterialClaim) []model.MaterialObjectTarget {
	if c.ObjectTargets != nil {
		return c.ObjectTargets
	}
	switch c.ScopeKind {
	case "plan":
		return []model.MaterialObjectTarget{{Kind: "plan"}}
	case "product":
		return []model.MaterialObjectTarget{{Kind: "product", LinkID: c.ProductLinkID}}
	case "store":
		if c.StoreName != "" && c.StoreName != "未说明" {
			return []model.MaterialObjectTarget{{Kind: "store", StoreName: c.StoreName}}
		}
	}
	return nil
}

func materialLinkMatches(t model.MaterialObjectTarget, l Link) bool {
	return !l.MaterialHidden && (t.Kind == "product" && t.LinkID > 0 && l.ID == t.LinkID || t.Kind == "object" && t.ObjectID > 0 && l.ObjectID == t.ObjectID)
}
func ResolveSubjects(names []string, links []Link, stores []model.MaterialStoreObject) (map[int64]bool, map[string]bool, bool) {
	ids := map[int64]bool{}
	ss := map[string]bool{}
	unresolved := false
	for _, name := range names {
		matches := 0
		for _, l := range links {
			if norm(name) == norm(l.Name) || l.DisplayName != "" && norm(name) == norm(l.DisplayName) || name == l.Product && l.Product != "" {
				id := l.ID
				if l.ObjectID > 0 {
					id = -l.ObjectID
				}
				ids[id] = true
				matches++
			}
		}
		for _, s := range stores {
			if norm(name) == norm(s.Name) {
				ss[s.Name] = true
				matches++
			}
		}
		if matches != 1 {
			unresolved = true
		}
	}
	if unresolved {
		return map[int64]bool{}, map[string]bool{}, true
	}
	for _, s := range stores {
		for _, id := range s.LinkIDs {
			if ids[id] {
				ss[s.Name] = true
			}
		}
	}
	return ids, ss, false
}
func scope(c model.PlanMaterialClaim, links []Link) string {
	names := []string{}
	for _, t := range targets(c) {
		switch t.Kind {
		case "plan":
			names = append(names, "全局")
		case "store":
			names = append(names, t.StoreName)
		case "product", "object":
			for _, l := range links {
				if materialLinkMatches(t, l) {
					name := l.DisplayName
					if name == "" {
						name = l.Name
					}
					names = append(names, name)
				}
			}
		}
	}
	return strings.Join(names, "、")
}
func allowed(c model.PlanMaterialClaim, ids map[int64]bool, stores map[string]bool, manual bool, allObjects bool) bool {
	if c.ScopeKind == "unresolved" {
		return false
	}
	if allObjects {
		return true
	}
	for _, t := range targets(c) {
		if t.Kind == "plan" || manual || t.Kind == "product" && ids[t.LinkID] || t.Kind == "object" && ids[-t.ObjectID] || t.Kind == "store" && stores[t.StoreName] {
			return true
		}
	}
	return false
}

// Predicate evaluation is deliberately three-valued. Unknown geography or
// missing order information cannot turn a conditional policy into a promise.
func eval(v any, known map[string]string) string {
	switch x := v.(type) {
	case []any:
		status := "适用"
		for _, p := range x {
			s := eval(p, known)
			if s == "不适用" {
				return s
			}
			if s == "待核对" {
				status = s
			}
		}
		return status
	case map[string]any:
		if x["status"] == "unresolved" || x["op"] == "raw" {
			return "待核对"
		}
		if x["op"] == "and" {
			return eval(x["values"], known)
		}
		if x["op"] == "or" {
			return eval(map[string]any{"any": x["values"]}, known)
		}
		if all, ok := x["all"]; ok {
			return eval(all, known)
		}
		if any, ok := x["any"].([]any); ok {
			unknown := false
			for _, p := range any {
				s := eval(p, known)
				if s == "适用" {
					return s
				}
				unknown = unknown || s == "待核对"
			}
			if unknown {
				return "待核对"
			}
			return "不适用"
		}
		field, _ := x["field"].(string)
		op, _ := x["op"].(string)
		value := strings.TrimSpace(known[field])
		if field == "" || value == "" {
			return "待核对"
		}
		vals, _ := x["values"].([]any)
		if len(vals) == 0 {
			if val, ok := x["value"]; ok {
				vals = []any{val}
			}
		}
		if len(vals) == 0 {
			return "待核对"
		}
		match := false
		for _, v := range vals {
			if field == "region" {
				match = match || regionEquivalent(value, fmt.Sprint(v))
			} else if norm(value) == norm(fmt.Sprint(v)) {
				match = true
			}
		}
		switch op {
		case "in", "eq", "=":
			if match {
				return "适用"
			}
			if field == "region" && !province(value) {
				return "待核对"
			}
			return "不适用"
		case "not_in", "ne", "!=":
			if match {
				return "不适用"
			}
			if field == "region" && province(value) && x["universe"] == "china_provinces" {
				return "适用"
			}
			return "待核对"
		case "gte", "gt", "lte", "lt":
			a, e1 := strconv.ParseFloat(value, 64)
			b, e2 := strconv.ParseFloat(fmt.Sprint(vals[0]), 64)
			if e1 != nil || e2 != nil {
				return "待核对"
			}
			pass := op == "gte" && a >= b || op == "gt" && a > b || op == "lte" && a <= b || op == "lt" && a < b
			if pass {
				return "适用"
			}
			return "不适用"
		}
	}
	return "待核对"
}
func province(value string) bool {
	return regionProvince(value) != ""
}
func decode(raw json.RawMessage) any {
	var v any
	if len(raw) == 0 {
		return []any{}
	}
	if json.Unmarshal(raw, &v) != nil {
		return map[string]any{"raw": string(raw)}
	}
	if v == nil {
		return []any{}
	}
	return v
}
func conditionStatus(c model.PlanMaterialClaim, known map[string]string) string {
	s := eval(decode(c.Conditions), known)
	exceptions := decode(c.Exceptions)
	if list, ok := exceptions.([]any); ok {
		for _, p := range list {
			e := eval(p, known)
			if e == "适用" {
				return "不适用"
			}
			if e == "待核对" && s == "适用" {
				s = e
			}
		}
	} else {
		e := eval(exceptions, known)
		if e == "适用" {
			return "不适用"
		}
		if e == "待核对" && s == "适用" {
			s = e
		}
	}
	return s
}
func describe(v any) string {
	switch x := v.(type) {
	case []any:
		out := []string{}
		for _, p := range x {
			if s := describe(p); s != "" {
				out = append(out, s)
			}
		}
		return strings.Join(out, "；")
	case map[string]any:
		if x["op"] == "and" {
			return "同时满足：" + describe(x["values"])
		}
		if x["op"] == "or" {
			return "任一满足：" + describe(x["values"])
		}
		if all, ok := x["all"]; ok {
			return "同时满足：" + describe(all)
		}
		if any, ok := x["any"]; ok {
			return "任一满足：" + describe(any)
		}
		if raw, ok := x["raw"].(string); ok && raw != "" {
			return raw
		}
		labels := map[string]string{"region": "地区", "amount": "金额", "time": "时间", "channel": "渠道", "quantity": "数量", "audience": "人群", "in": "属于", "not_in": "不属于", "eq": "等于", "ne": "不等于", "gte": "不少于", "gt": "大于", "lte": "不超过", "lt": "小于"}
		text := func(v any) string {
			s := fmt.Sprint(v)
			if t, ok := labels[s]; ok {
				return t
			}
			return s
		}
		vs := []string{}
		if vals, ok := x["values"].([]any); ok {
			for _, v := range vals {
				vs = append(vs, fmt.Sprint(v))
			}
		}
		return text(x["field"]) + " " + text(x["op"]) + " " + strings.Join(vs, "、")
	case string:
		return x
	}
	return ""
}
func originalText(c model.PlanMaterialClaim) string {
	for _, value := range []string{c.SourceSentence, c.SourceQuote, c.Content} {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func sourceIdentity(c model.PlanMaterialClaim) string {
	if sentence := strings.TrimSpace(c.SourceSentence); sentence != "" {
		return "sentence:" + sentence
	}
	if quote := strings.TrimSpace(c.SourceQuote); quote != "" {
		return "quote:" + quote
	}
	return ""
}

func explicitSubjects(text string, subjects []string, links []Link, stores []model.MaterialStoreObject) []string {
	if len(subjects) == 0 {
		return nil
	}
	normalized := norm(text)
	out := []string{}
	for _, subject := range subjects {
		matched := strings.Contains(normalized, norm(subject))
		if !matched {
			for _, link := range links {
				if norm(subject) != norm(link.Name) && subject != link.Product {
					continue
				}
				if m := numberedObjectName.FindStringSubmatch(strings.TrimSpace(link.Name)); len(m) == 2 {
					matched = strings.Contains(text, m[1]+"号") || strings.Contains(text, "第"+m[1]+"号")
				}
				if !matched && link.Product != "" {
					matched = strings.Contains(text, link.Product)
				}
				if matched {
					break
				}
			}
		}
		if !matched {
			for _, store := range stores {
				if norm(subject) == norm(store.Name) && strings.Contains(text, store.Name) {
					matched = true
					break
				}
			}
		}
		if matched {
			out = append(out, subject)
		}
	}
	return out
}

func recallStrength(hits []Hit, subjects []string, links []Link, stores []model.MaterialStoreObject) string {
	if len(hits) == 0 {
		return "weak"
	}
	topics := map[string]bool{}
	for _, hit := range hits {
		key := strings.TrimSpace(hit.Key)
		if key == "" {
			key = AttributeName(hit.Attribute, links, stores)
		}
		if key != "" {
			topics[key] = true
		}
	}
	covered := true
	for _, subject := range subjects {
		found := false
		for _, hit := range hits {
			if strings.Contains(norm(hit.Scope), norm(subject)) {
				found = true
				break
			}
		}
		if !found {
			covered = false
			break
		}
	}
	top := hits[0].Score
	if covered && top >= .55 {
		return "strong"
	}
	if covered && top >= .40 && len(topics) >= 2 {
		return "medium"
	}
	return "weak"
}

// AllFactsByObject materializes the complete fact library grouped by object and
// global scope. It does not rank, filter, evaluate conditions or call a model.
func AllFactsByObject(records []Record, links []Link, stores []model.MaterialStoreObject) ([]Group, []Hit) {
	order := []string{"全局"}
	for _, link := range links {
		if link.MaterialHidden {
			continue
		}
		name := strings.TrimSpace(link.DisplayName)
		if name == "" {
			name = strings.TrimSpace(link.Name)
		}
		if name != "" {
			order = append(order, name)
		}
	}
	for _, store := range stores {
		if name := strings.TrimSpace(store.Name); name != "" {
			order = append(order, name)
		}
	}
	byObject := map[string][]Hit{}
	seen := map[string]map[int64]bool{}
	add := func(object string, hit Hit) {
		if seen[object] == nil {
			seen[object] = map[int64]bool{}
		}
		if seen[object][hit.ID] {
			return
		}
		seen[object][hit.ID] = true
		byObject[object] = append(byObject[object], hit)
	}
	for _, record := range records {
		c := record.Claim
		objects := []string{}
		for _, target := range targets(c) {
			switch target.Kind {
			case "plan":
				objects = append(objects, "全局")
			case "product", "object":
				for _, link := range links {
					if materialLinkMatches(target, link) {
						name := strings.TrimSpace(link.DisplayName)
						if name == "" {
							name = link.Name
						}
						objects = append(objects, name)
						break
					}
				}
			case "store":
				if strings.TrimSpace(target.StoreName) != "" {
					objects = append(objects, target.StoreName)
				}
			}
		}
		if len(objects) == 0 {
			if c.ScopeKind == "unresolved" || len(targets(c)) > 0 {
				continue
			}
			objects = []string{"全局"}
		}
		unique := make([]string, 0, len(objects))
		seenObject := map[string]bool{}
		for _, object := range objects {
			if !seenObject[object] {
				seenObject[object] = true
				unique = append(unique, object)
			}
		}
		object := strings.Join(unique, "、")
		content := c.Content
		if c.Destination == "qa" && c.Answer != "" {
			content = c.Answer
		}
		hit := Hit{ID: c.ID, Attribute: c.Topic, Key: c.ClaimKey, Content: content, Scope: scope(c, links), Score: .01, ConditionStatus: "", Conditions: describe(decode(c.Conditions)), Exceptions: describe(decode(c.Exceptions)), Original: originalText(c), Question: c.Question}
		add(object, hit)
	}
	orderSeen := map[string]bool{}
	for _, object := range order {
		orderSeen[object] = true
	}
	extra := []string{}
	for object := range byObject {
		if !orderSeen[object] {
			extra = append(extra, object)
		}
	}
	sort.Strings(extra)
	order = append(order, extra...)

	groups := []Group{}
	all := []Hit{}
	allSeen := map[int64]bool{}
	for _, object := range order {
		hits, ok := byObject[object]
		if !ok || len(hits) == 0 {
			continue
		}
		groups = append(groups, Group{Ask: Ask{Text: object}, Hits: hits, Strength: "strong"})
		for _, hit := range hits {
			if !allSeen[hit.ID] {
				allSeen[hit.ID] = true
				all = append(all, hit)
			}
		}
	}
	return groups, all
}

func Retrieve(records []Record, links []Link, stores []model.MaterialStoreObject, asks []Ask, queries []string, vectors [][]float32, embeddingModel string, manual bool) Recall {
	out := Recall{Groups: []Group{}, Hits: []Hit{}, Notices: []string{}}
	if len(vectors) == 0 {
		return out
	}
	unavailable := 0
	for _, r := range records {
		if r.Claim.Destination != "" && r.Claim.Destination != "fact" && r.Claim.Destination != "qa" {
			continue
		}
		if r.Claim.EmbeddingModel != embeddingModel || len(r.Vector) != len(vectors[0]) {
			unavailable++
		}
	}
	if unavailable > 0 {
		out.Notices = append(out.Notices, fmt.Sprintf("%d条资料的向量不可用或与当前配置不兼容，本次未参与召回。", unavailable))
	}
	byID := map[int64]Record{}
	for _, r := range records {
		byID[r.Claim.ID] = r
	}
	seen := map[int64]bool{}
	budget := 0
	for i, a := range asks {
		g := Group{Ask: a, Hits: []Hit{}}
		subjects := explicitSubjects(a.Text, a.Subjects, links, stores)
		ids, ss, unknown := ResolveSubjects(subjects, links, stores)
		if unknown {
			g.Notice = "对象未能唯一确认，仅召回全局资料。"
		}
		allObjects := len(subjects) == 0 && !manual
		if allObjects && len(links) > 0 {
			g.Notice = "未指定商品，按全部对象召回。"
		}
		queryIndexes := []int{}
		if manual {
			if i < len(vectors) {
				queryIndexes = append(queryIndexes, i)
			}
		} else {
			for _, q := range Queries("", []Ask{a}) {
				for j, x := range queries {
					if x == q {
						queryIndexes = append(queryIndexes, j)
						break
					}
				}
			}
		}
		if len(queryIndexes) == 0 {
			queryIndexes = append(queryIndexes, 0)
		}
		qi := queryIndexes[0]
		candidates := []Hit{}
		for _, r := range records {
			c := r.Claim
			if c.Destination != "" && c.Destination != "fact" && c.Destination != "qa" || len(r.Vector) != len(vectors[qi]) || c.EmbeddingModel != embeddingModel || !allowed(c, ids, ss, manual, allObjects) {
				continue
			}
			score := 0.0
			for _, index := range queryIndexes {
				if index < len(vectors) && len(r.Vector) == len(vectors[index]) {
					if s := semantic.CosineSimilarity(r.Vector, vectors[index]); s > score {
						score = s
					}
				}
			}
			if !manual && score < .35 {
				continue
			}
			rank := score
			content := c.Content
			if c.Destination == "qa" && c.Answer != "" {
				content = c.Answer
			}
			candidates = append(candidates, Hit{c.ID, c.Topic, c.ClaimKey, content, scope(c, links), score, "", describe(decode(c.Conditions)), describe(decode(c.Exceptions)), originalText(c), c.Question, rank})
		}
		sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].rank > candidates[j].rank })
		if !manual && len(subjects) <= 1 && len(candidates) > 0 {
			anchor := candidates[0]
			anchorSource := ""
			if record, ok := byID[anchor.ID]; ok {
				anchorSource = sourceIdentity(record.Claim)
			}
			kept := make([]Hit, 0, len(candidates))
			for _, h := range candidates {
				sameSource := false
				if anchorSource != "" {
					if record, ok := byID[h.ID]; ok {
						sameSource = sourceIdentity(record.Claim) == anchorSource
					}
				}
				if anchorSource == "" || sameSource {
					kept = append(kept, h)
				}
			}
			if len(kept) > 0 {
				candidates = kept
			}
		}
		for _, h := range candidates {
			if len(g.Hits) >= 8 {
				break
			}
			size := len([]rune(h.Content + h.Conditions + h.Exceptions + h.Original))
			if !seen[h.ID] && (len(out.Hits) >= 32 || budget+size > 24000) {
				continue
			}
			if h.Score > 1 {
				h.Score = 1
			}
			g.Hits = append(g.Hits, h)
			if !seen[h.ID] {
				seen[h.ID] = true
				budget += size
				out.Hits = append(out.Hits, h)
			}
		}
		if len(candidates) > len(g.Hits) {
			g.Notice += " 已显示有限相关资料，未截断条件或例外。"
		}
		if !manual {
			for _, base := range append([]Hit{}, g.Hits...) {
				record, ok := byID[base.ID]
				if !ok {
					continue
				}
				sentence := sourceIdentity(record.Claim)
				if sentence == "" {
					continue
				}
				added := 0
				for _, r := range records {
					if len(g.Hits) >= 8 || added >= 6 {
						break
					}
					if sourceIdentity(r.Claim) != sentence || seen[r.Claim.ID] || !allowed(r.Claim, ids, ss, manual, allObjects) {
						continue
					}
					content := r.Claim.Content
					if r.Claim.Destination == "qa" && r.Claim.Answer != "" {
						content = r.Claim.Answer
					}
					size := len([]rune(content + describe(decode(r.Claim.Conditions)) + describe(decode(r.Claim.Exceptions)) + originalText(r.Claim)))
					if len(out.Hits) >= 32 || budget+size > 24000 {
						continue
					}
					h := Hit{ID: r.Claim.ID, Attribute: r.Claim.Topic, Key: r.Claim.ClaimKey, Content: content, Scope: scope(r.Claim, links), Score: base.Score, ConditionStatus: "", Conditions: describe(decode(r.Claim.Conditions)), Exceptions: describe(decode(r.Claim.Exceptions)), Original: originalText(r.Claim), Question: r.Claim.Question, rank: base.rank}
					g.Hits = append(g.Hits, h)
					seen[h.ID] = true
					added++
					budget += size
					out.Hits = append(out.Hits, h)
				}
			}
		}
		if !manual {
			g.Strength = recallStrength(g.Hits, subjects, links, stores)
			if g.Strength == "weak" {
				g.Fallback = true
				g.Notice += " 证据较弱，已按不同属性补充该对象和全局事实。"
				keys := map[string]bool{}
				for _, hit := range g.Hits {
					key := strings.TrimSpace(hit.Key)
					if key == "" {
						key = AttributeName(hit.Attribute, links, stores)
					}
					keys[key] = true
				}
				for _, r := range records {
					if len(g.Hits) >= 32 || len(out.Hits) >= 32 {
						break
					}
					c := r.Claim
					key := strings.TrimSpace(c.ClaimKey)
					if key == "" {
						key = AttributeName(c.Topic, links, stores)
					}
					if seen[c.ID] || keys[key] || c.Destination == "qa" || !allowed(c, ids, ss, manual, allObjects) {
						continue
					}
					size := len([]rune(c.Content + describe(decode(c.Conditions)) + describe(decode(c.Exceptions)) + originalText(c)))
					if budget+size > 24000 {
						continue
					}
					h := Hit{ID: c.ID, Attribute: c.Topic, Key: c.ClaimKey, Content: c.Content, Scope: scope(c, links), Score: .01, ConditionStatus: "", Conditions: describe(decode(c.Conditions)), Exceptions: describe(decode(c.Exceptions)), Original: originalText(c), Question: c.Question, rank: .01}
					g.Hits = append(g.Hits, h)
					seen[c.ID] = true
					keys[key] = true
					budget += size
					out.Hits = append(out.Hits, h)
				}
			}
		} else {
			g.Strength = recallStrength(g.Hits, subjects, links, stores)
		}
		out.Groups = append(out.Groups, g)
	}
	return out
}
