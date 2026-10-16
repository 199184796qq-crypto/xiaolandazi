package model

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

var productFieldDefaultLabels = map[string]string{"spec": "规格", "daily_price": "销售价", "quantity": "数量", "audience": "适用"}

// Internal field keys and product IDs remain stable when a customer renames a
// field. Labels travel with the product snapshot and its versioned facts.
func (p LiveAgentPlanProductLink) FieldLabel(key, fallback string) string {
	if label := strings.TrimSpace(p.FieldLabels[key]); label != "" {
		return label
	}
	return fallback
}

func MergeProductFieldLabels(current, patch map[string]string) (map[string]string, error) {
	next := make(map[string]string, len(current)+len(patch))
	for key, value := range current {
		next[key] = value
	}
	for key, raw := range patch {
		fallback, ok := productFieldDefaultLabels[key]
		if !ok {
			return nil, fmt.Errorf("不支持修改这个商品字段的名称")
		}
		label := strings.TrimSpace(raw)
		if label == "" || utf8.RuneCountInString(label) > 32 || strings.ContainsFunc(label, unicode.IsControl) {
			return nil, fmt.Errorf("字段名称须为1至32字，不能包含换行或控制字符")
		}
		if label == fallback {
			delete(next, key)
		} else {
			next[key] = label
		}
	}
	return next, nil
}

// Source maps use the stable key suffix for renamed fields, so two customer
// labels cannot overwrite each other or a reserved product-name/link entry.
func (p LiveAgentPlanProductLink) SourceFieldLabel(key, fallback string) string {
	if strings.TrimSpace(p.FieldLabels[key]) == "" {
		return fallback
	}
	return p.FieldLabel(key, fallback) + "（" + key + "）"
}
