package anchorstylescore

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"
)

// Normalize only a private JSON copy. Models sometimes return one alias as a
// string, or an alias -> full-address explanation instead of full -> aliases.
// These lexical fields must not turn explanation text into counted vocabulary.
func normalizeAudienceAliases(audience map[string]json.RawMessage) error {
	raw, present := audience["称呼别名"]
	if !present || string(raw) == "null" {
		return nil
	}
	var entries map[string]json.RawMessage
	if err := json.Unmarshal(raw, &entries); err != nil || entries == nil {
		return fmt.Errorf("称呼别名必须是对象，值须为字符串或字符串数组")
	}
	var candidates, other []string
	var main string
	_ = json.Unmarshal(audience["候选称呼"], &candidates)
	_ = json.Unmarshal(audience["辅助称呼"], &other)
	_ = json.Unmarshal(audience["主力称呼"], &main)
	candidates = clean(append(append(candidates, other...), main))
	keys := make([]string, 0, len(entries))
	for key := range entries {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	normalized := map[string][]string{}
	for _, key := range keys {
		var values []string
		var single string
		value := entries[key]
		if string(value) != "null" && json.Unmarshal(value, &values) != nil {
			if err := json.Unmarshal(value, &single); err != nil {
				return fmt.Errorf("称呼别名[%s]须为字符串或字符串数组", key)
			}
			single = strings.TrimSpace(single)
			full := single
			explanation := false
			for _, suffix := range []string{"的简称", "的缩写", "的简写", "的缩略称呼"} {
				if strings.HasSuffix(single, suffix) {
					full = strings.TrimSpace(strings.TrimSuffix(single, suffix))
					explanation = true
					break
				}
			}
			// Reverse only explicit full-address references from the supplied
			// vocabulary. Do not infer relationships from general prose.
			if containsExact(candidates, full) && (explanation || (main != key && utf8.RuneCountInString(full) > utf8.RuneCountInString(key))) {
				normalized[full] = clean(append(normalized[full], key))
				continue
			}
			values = []string{single}
		}
		normalized[key] = clean(append(normalized[key], values...))
	}
	audience["称呼别名"], _ = json.Marshal(normalized)
	return nil
}
