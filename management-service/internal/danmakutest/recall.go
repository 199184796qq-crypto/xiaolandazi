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
	Text       string            `json:"text"`
	Subjects   []string          `json:"subjects"`
	Attribute  string            `json:"attribute"`
	Conditions map[string]string `json:"conditions,omitempty"`
}
type Intent struct {
	Meaning       string   `json:"meaning"`
	SpeechAct     string   `json:"speech_act"`
	Operation     string   `json:"operation"`
	Corrected     string   `json:"corrected,omitempty"`
	Uncertainties []string `json:"uncertainties"`
	Asks          []Ask    `json:"asks"`
}
type Link struct {
	ID      int64  `json:"-"`
	Name    string `json:"name"`
	Product string `json:"product"`
}
type Record struct {
	Claim  model.PlanMaterialClaim
	Vector []float32
}
type Hit struct {
	ID              int64   `json:"id"`
	Attribute       string  `json:"attribute"`
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
	Ask    Ask    `json:"ask"`
	Hits   []Hit  `json:"hits"`
	Notice string `json:"notice,omitempty"`
}
type Recall struct {
	Groups  []Group  `json:"groups"`
	Hits    []Hit    `json:"hits"`
	Notices []string `json:"notices"`
}

func NormalizeIntent(in Intent) (Intent, error) {
	in.Meaning = strings.TrimSpace(in.Meaning)
	if in.Meaning == "" || len([]rune(in.Meaning)) > 1600 || len(in.Asks) > 8 || len(in.Uncertainties) > 12 {
		return in, fmt.Errorf("意图结果不完整或诉求过多，请分批测试")
	}
	for i := range in.Asks {
		a := &in.Asks[i]
		a.Text = strings.TrimSpace(a.Text)
		a.Attribute = strings.TrimSpace(a.Attribute)
		if a.Text == "" || len([]rune(a.Text)) > 1000 || len(a.Subjects) > 8 || len(a.Conditions) > 16 || len([]rune(a.Attribute)) > 100 {
			return in, fmt.Errorf("诉求结果不完整，请重试")
		}
	}
	return in, nil
}
func Queries(question string, asks []Ask) []string {
	out := []string{question}
	seen := map[string]bool{question: true}
	for _, a := range asks {
		q := strings.TrimSpace(strings.Join(a.Subjects, "、") + " " + a.Text + " " + a.Attribute)
		if q != "" && !seen[q] {
			out = append(out, q)
			seen[q] = true
		}
	}
	return out
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
func ResolveSubjects(names []string, links []Link, stores []model.MaterialStoreObject) (map[int64]bool, map[string]bool, bool) {
	ids := map[int64]bool{}
	ss := map[string]bool{}
	unresolved := false
	for _, name := range names {
		matches := 0
		for _, l := range links {
			if norm(name) == norm(l.Name) || name == l.Product && l.Product != "" {
				ids[l.ID] = true
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
		case "product":
			for _, l := range links {
				if l.ID == t.LinkID {
					names = append(names, l.Name)
				}
			}
		}
	}
	return strings.Join(names, "、")
}
func allowed(c model.PlanMaterialClaim, ids map[int64]bool, stores map[string]bool, manual bool) bool {
	for _, t := range targets(c) {
		if t.Kind == "plan" || manual || t.Kind == "product" && ids[t.LinkID] || t.Kind == "store" && stores[t.StoreName] {
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
			if norm(value) == norm(fmt.Sprint(v)) {
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
	for _, p := range strings.Fields("北京 天津 上海 重庆 河北 山西 辽宁 吉林 黑龙江 江苏 浙江 安徽 福建 江西 山东 河南 湖北 湖南 广东 海南 四川 贵州 云南 陕西 甘肃 青海 台湾 内蒙古 广西 西藏 宁夏 新疆 香港 澳门") {
		if norm(value) == p {
			return true
		}
	}
	return false
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
	seen := map[int64]bool{}
	budget := 0
	for i, a := range asks {
		g := Group{Ask: a, Hits: []Hit{}}
		ids, ss, unknown := ResolveSubjects(a.Subjects, links, stores)
		if unknown {
			g.Notice = "对象未能唯一确认，仅召回全局资料。"
		}
		if len(a.Subjects) == 0 && !manual && len(links) > 0 {
			g.Notice = "尚未明确商品对象，商品专属资料不作为依据。"
		}
		qi := 0
		q := Queries("", []Ask{a})
		for j, x := range queries {
			if len(q) > 1 && x == q[1] || len(q) == 1 && x == q[0] {
				qi = j
				break
			}
		}
		if manual && i < len(vectors) {
			qi = i
		}
		candidates := []Hit{}
		for _, r := range records {
			c := r.Claim
			if c.Destination != "" && c.Destination != "fact" && c.Destination != "qa" || len(r.Vector) != len(vectors[qi]) || c.EmbeddingModel != embeddingModel || !allowed(c, ids, ss, manual) {
				continue
			}
			score := semantic.CosineSimilarity(r.Vector, vectors[qi])
			exact := a.Attribute != "" && norm(a.Attribute) == norm(c.Topic)
			if !manual && score < .3 && !exact {
				continue
			}
			rank := score
			if exact {
				rank += .18
			}
			content := c.Content
			if c.Destination == "qa" && c.Answer != "" {
				content = c.Answer
			}
			candidates = append(candidates, Hit{c.ID, c.Topic, content, scope(c, links), score, conditionStatus(c, a.Conditions), describe(decode(c.Conditions)), describe(decode(c.Exceptions)), c.SourceQuote, c.Question, rank})
		}
		sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].rank > candidates[j].rank })
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
		out.Groups = append(out.Groups, g)
	}
	return out
}
