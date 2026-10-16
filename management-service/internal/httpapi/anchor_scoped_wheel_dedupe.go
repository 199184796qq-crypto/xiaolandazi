package httpapi

import (
	"encoding/json"
	"strings"
)

// Deduplicate literal content before the existing contract validator. Do not
// conflate near-synonyms, prices or conditions. Each product is a separate call;
// its similar content must never erase another product's pool.
func dedupeScopedWheelJSON(raw string, singleProduct bool) string {
	var top map[string]json.RawMessage
	if json.Unmarshal([]byte(stripPolicyJSONFence(raw)), &top) != nil {
		return raw
	}
	var items []map[string]json.RawMessage
	if json.Unmarshal(top["卖点列表"], &items) != nil {
		return raw
	}
	clean := []map[string]json.RawMessage{}
	byPoint := map[string]int{}
	seenAngles := map[string]bool{}
	identity := func(text string) string { return strings.Join(strings.Fields(text), " ") }
	for _, item := range items {
		// Preserve invalid/unknown fields for the original validator; never hide
		// protocol errors by discarding a duplicate item with such fields.
		for key := range item {
			switch key {
			case "卖点名", "角度", "目标人群", "痛点", "tier", "归属":
			default:
				return raw
			}
		}
		// A duplicate must not conceal a malformed optional field.
		encoded, _ := json.Marshal(item)
		var typed anchorStyleSellingPointWheelItem
		if json.Unmarshal(encoded, &typed) != nil {
			return raw
		}
		var name, scope string
		var angles []string
		if json.Unmarshal(item["卖点名"], &name) != nil || json.Unmarshal(item["归属"], &scope) != nil || json.Unmarshal(item["角度"], &angles) != nil || len(angles) == 0 {
			return raw
		}
		if scope != "商品" && scope != "公共" {
			return raw
		}
		key := scope + ":" + identity(name)
		i, exists := byPoint[key]
		if !exists {
			i = len(clean)
			clean = append(clean, item)
			byPoint[key] = i
			clean[i]["角度"] = json.RawMessage(`[]`)
		}
		var merged []string
		_ = json.Unmarshal(clean[i]["角度"], &merged)
		for _, angle := range angles {
			angleKey := key + ":" + identity(angle)
			if scope == "公共" || singleProduct {
				angleKey = scope + ":" + identity(angle)
			}
			if strings.TrimSpace(angle) == "" || seenAngles[angleKey] {
				continue
			}
			seenAngles[angleKey] = true
			merged = append(merged, angle)
		}
		clean[i]["角度"], _ = json.Marshal(merged)
	}
	retained := clean[:0]
	for _, item := range clean {
		var angles []string
		_ = json.Unmarshal(item["角度"], &angles)
		if len(angles) > 0 {
			retained = append(retained, item)
		}
	}
	top["卖点列表"], _ = json.Marshal(retained)
	encoded, e := json.Marshal(top)
	if e != nil {
		return raw
	}
	return string(encoded)
}
