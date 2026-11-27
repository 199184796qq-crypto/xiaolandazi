package httpapi

import (
	"bytes"
	"encoding/json"
	"slices"
	"strings"
)

var rewriteOperatorLabels = map[string]string{
	"in": "属于", "not_in": "不属于", "eq": "为", "ne": "不等于",
	"gte": "不少于", "gt": "大于", "lte": "不超过", "lt": "小于", "between": "介于",
}

type rewritePredicate struct {
	field, op, raw, universe string
	values                   []string
	children                 []rewritePredicate
}

func decodeRewritePredicates(raw json.RawMessage) ([]rewritePredicate, bool) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, true
	}
	var v any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if dec.Decode(&v) != nil {
		return nil, false
	}
	switch v.(type) {
	case nil:
		return nil, true
	case map[string]any:
		if len(v.(map[string]any)) == 0 {
			return nil, true
		}
	}
	return rewritePredicateList(v)
}

func rewritePredicateList(v any) ([]rewritePredicate, bool) {
	items, ok := v.([]any)
	if !ok {
		items = []any{v}
	}
	out := make([]rewritePredicate, 0, len(items))
	for _, item := range items {
		switch item := item.(type) {
		case string:
			raw := strings.TrimSpace(item)
			if raw != "" {
				out = append(out, rewritePredicate{raw: raw})
			}
		case map[string]any:
			p := rewritePredicate{field: rewriteField(rewriteScalar(item["field"])), op: rewriteOperator(rewriteScalar(item["op"])),
				raw: rewriteScalar(item["raw"]), universe: rewriteScalar(item["universe"])}
			if p.field == "" {
				p.field = rewriteField(rewriteScalar(item["key"]))
			}
			if p.op == "and" || p.op == "or" || item["all"] != nil || item["any"] != nil {
				children := item["values"]
				if item["all"] != nil && item["any"] != nil {
					return nil, false
				}
				if item["all"] != nil {
					p.op, children = "and", item["all"]
				}
				if item["any"] != nil {
					p.op, children = "or", item["any"]
				}
				p.children, ok = rewritePredicateList(children)
				if !ok || len(p.children) == 0 {
					return nil, false
				}
			} else {
				values := item["values"]
				if values == nil {
					values = item["value"]
				}
				switch values := values.(type) {
				case []any:
					for _, x := range values {
						if text := rewriteScalar(x); text != "" && !slices.Contains(p.values, text) {
							p.values = append(p.values, text)
						}
					}
				default:
					if text := rewriteScalar(values); text != "" {
						p.values = []string{text}
					}
				}
				if rewriteScalar(item["status"]) == "unresolved" {
					p.op = "raw"
				}
			}
			if len(p.children) == 0 && len(p.values) == 0 && p.raw == "" {
				return nil, false
			}
			out = append(out, p)
		default:
			return nil, false
		}
	}
	return out, true
}

func rewriteScalar(v any) string {
	switch v := v.(type) {
	case string:
		return strings.TrimSpace(v)
	case json.Number:
		return v.String()
	case bool:
		if v {
			return "是"
		}
		return "否"
	default:
		return ""
	}
}

func rewriteField(field string) string {
	switch field {
	case "region", "地区", "收货地区":
		return "收货地区"
	case "包装状态", "包装方式":
		return "包装方式"
	case "amount":
		return "金额"
	case "time":
		return "时间"
	case "channel":
		return "渠道"
	case "quantity":
		return "数量"
	case "audience":
		return "人群"
	default:
		return field
	}
}

func rewriteOperator(op string) string {
	switch op {
	case "属于", "属于其中":
		return "in"
	case "不属于", "不属于其中":
		return "not_in"
	case "等于", "=":
		return "eq"
	case "不等于", "!=":
		return "ne"
	case "不少于", "不早于", ">=":
		return "gte"
	case "大于", ">":
		return "gt"
	case "不超过", "<=":
		return "lte"
	case "小于", "<":
		return "lt"
	default:
		return op
	}
}

func renderRewritePredicates(ps []rewritePredicate, c rewriteCandidate, candidates []rewriteCandidate) (string, bool) {
	parts := make([]string, 0, len(ps))
	for _, p := range ps {
		text, ok := renderRewritePredicate(p, c, candidates)
		if !ok {
			return "", false
		}
		if text != "" && !slices.Contains(parts, text) {
			parts = append(parts, text)
		}
	}
	slices.Sort(parts)
	return strings.Join(parts, "；"), true
}

func renderRewritePredicate(p rewritePredicate, c rewriteCandidate, candidates []rewriteCandidate) (string, bool) {
	if len(p.children) > 0 {
		text, ok := renderRewritePredicates(p.children, c, candidates)
		if p.op == "or" {
			return "任一满足（" + text + "）", ok
		}
		return "同时满足（" + text + "）", ok
	}
	if p.field != "" && (p.op == "eq" || p.op == "in") && len(p.values) == 1 &&
		rewriteRelativeValue(p.values[0]) {
		peer, ok := rewriteComplementPeer(p, c, candidates)
		if !ok {
			return "", false
		}
		p.op, p.values = "not_in", peer.values
	}
	if len(p.values) == 0 || p.field == "" {
		return p.raw, p.raw != ""
	}
	op, ok := rewriteOperatorLabels[p.op]
	if !ok {
		if p.raw != "" {
			return p.raw, true
		}
		return "", false
	}
	values := slices.Clone(p.values)
	if p.op == "in" || p.op == "not_in" {
		slices.Sort(values)
	}
	join := "、"
	if p.op == "between" && len(values) == 2 {
		join = "至"
	}
	text := p.field + op + strings.Join(values, join)
	if p.op == "not_in" && p.universe != "" {
		universe := p.universe
		if universe == "china_provinces" {
			universe = "全国省份"
		}
		text += "（" + universe + "范围内）"
	}
	return text, true
}

func rewriteRelativeValue(value string) bool {
	switch value {
	case "其他", "其它", "其余", "其他地区", "其它地区", "其余地区", "其他地方", "其它地方", "其余地方":
		return true
	default:
		return false
	}
}

func rewriteComplementPeer(p rewritePredicate, c rewriteCandidate, candidates []rewriteCandidate) (rewritePredicate, bool) {
	sentence := strings.TrimSpace(c.claim.SourceSentence)
	if sentence == "" {
		return rewritePredicate{}, false
	}
	var peer rewritePredicate
	for _, other := range candidates {
		if other.claim.SourceSentence != sentence || other.attribute != c.attribute ||
			!slices.Equal(other.objects, c.objects) || len(other.conditions) != 1 || len(other.exceptions) != 0 {
			continue
		}
		// The condition's exact text must be supported by this same source
		// sentence; unrelated claims from the plan cannot define "other".
		test := other.conditions[0]
		if test.field != p.field || test.op != "in" && test.op != "eq" || len(test.values) == 0 ||
			test.raw == "" || !strings.Contains(sentence, test.raw) || slices.ContainsFunc(test.values, rewriteRelativeValue) {
			continue
		}
		if len(peer.values) != 0 && !slices.Equal(peer.values, test.values) {
			return rewritePredicate{}, false
		}
		peer = test
	}
	return peer, len(peer.values) > 0
}
