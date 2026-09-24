package policy

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
	"unicode"

	"livecompanion/management/internal/model"
)

// EnsureStableRuleKeys makes machine rule keys a server-side invariant.
// Existing keys win. If the model omits a key, an unchanged rule reuses the
// previous key by title/text; otherwise a deterministic readable key is made.
func EnsureStableRuleKeys(
	layer string,
	rules []model.LivePolicyRule,
	base []model.LivePolicyRule,
) []model.LivePolicyRule {
	result := append([]model.LivePolicyRule(nil), rules...)
	baseByTitle := uniqueBaseRuleKeys(base, func(rule model.LivePolicyRule) string {
		return normalizeKeyMatch(rule.Title)
	})
	baseByText := uniqueBaseRuleKeys(base, func(rule model.LivePolicyRule) string {
		return normalizeKeyMatch(rule.Text)
	})
	used := make(map[string]struct{}, len(result))

	for index := range result {
		rule := result[index]
		key := strings.TrimSpace(rule.Key)
		if key == "" {
			if title := normalizeKeyMatch(rule.Title); title != "" {
				key = baseByTitle[title]
			}
		}
		if key == "" {
			if text := normalizeKeyMatch(rule.Text); text != "" {
				key = baseByText[text]
			}
		}
		if key == "" {
			key = generatedRuleKey(layer, rule)
		}
		if _, exists := used[key]; exists {
			key = nextUniqueRuleKey(generatedRuleKey(layer, rule), used)
		}
		rule.Key = key
		result[index] = rule
		used[key] = struct{}{}
	}
	return result
}

// PrioritizeNewRules puts rules that do not exist in base first. Existing
// rules keep the relative order from base so edits do not make cards jump.
func PrioritizeNewRules(
	rules []model.LivePolicyRule,
	base []model.LivePolicyRule,
) []model.LivePolicyRule {
	if len(rules) == 0 || len(base) == 0 {
		return append([]model.LivePolicyRule(nil), rules...)
	}

	baseKeys := make(map[string]struct{}, len(base))
	currentByKey := make(map[string]model.LivePolicyRule, len(rules))
	result := make([]model.LivePolicyRule, 0, len(rules))

	for _, item := range base {
		key := strings.TrimSpace(item.Key)
		if key != "" {
			baseKeys[key] = struct{}{}
		}
	}
	for _, item := range rules {
		key := strings.TrimSpace(item.Key)
		if key == "" {
			result = append(result, item)
			continue
		}
		if _, existed := baseKeys[key]; !existed {
			result = append(result, item)
			continue
		}
		currentByKey[key] = item
	}
	for _, item := range base {
		key := strings.TrimSpace(item.Key)
		if current, ok := currentByKey[key]; ok {
			result = append(result, current)
		}
	}
	return result
}

// EnsureStableOverrideKeys generates keys only for L3 add operations.
// replace/disable must continue to reference a real, already-known key.
func EnsureStableOverrideKeys(
	overrides []model.LivePolicyOverride,
	base []model.LivePolicyOverride,
) []model.LivePolicyOverride {
	result := append([]model.LivePolicyOverride(nil), overrides...)
	baseByTitle := uniqueBaseOverrideKeys(base, func(item model.LivePolicyOverride) string {
		return normalizeKeyMatch(item.Title)
	})
	baseByText := uniqueBaseOverrideKeys(base, func(item model.LivePolicyOverride) string {
		return normalizeKeyMatch(item.Text)
	})
	used := make(map[string]struct{}, len(result))

	for index := range result {
		item := result[index]
		operation := strings.ToLower(strings.TrimSpace(item.Operation))
		key := strings.TrimSpace(item.Key)
		if key == "" && operation == model.LivePolicyOverrideAdd {
			if title := normalizeKeyMatch(item.Title); title != "" {
				key = baseByTitle[title]
			}
			if key == "" {
				if text := normalizeKeyMatch(item.Text); text != "" {
					key = baseByText[text]
				}
			}
			if key == "" {
				key = generatedRuleKey(model.LivePolicyLayerL3, model.LivePolicyRule{
					Title: item.Title,
					Text:  item.Text,
				})
			}
		}
		if key != "" {
			if _, exists := used[key]; exists && operation == model.LivePolicyOverrideAdd {
				key = nextUniqueRuleKey(
					generatedRuleKey(model.LivePolicyLayerL3, model.LivePolicyRule{
						Title: item.Title,
						Text:  item.Text,
					}),
					used,
				)
			}
			used[key] = struct{}{}
		}
		item.Key = key
		result[index] = item
	}
	return result
}

func uniqueBaseOverrideKeys(
	items []model.LivePolicyOverride,
	match func(model.LivePolicyOverride) string,
) map[string]string {
	result := make(map[string]string)
	duplicates := make(map[string]struct{})
	for _, item := range items {
		key := strings.TrimSpace(item.Key)
		value := match(item)
		if key == "" || value == "" {
			continue
		}
		if existing, exists := result[value]; exists && existing != key {
			duplicates[value] = struct{}{}
			continue
		}
		result[value] = key
	}
	for value := range duplicates {
		delete(result, value)
	}
	return result
}

func uniqueBaseRuleKeys(
	rules []model.LivePolicyRule,
	match func(model.LivePolicyRule) string,
) map[string]string {
	result := make(map[string]string)
	duplicates := make(map[string]struct{})
	for _, rule := range rules {
		key := strings.TrimSpace(rule.Key)
		value := match(rule)
		if key == "" || value == "" {
			continue
		}
		if existing, exists := result[value]; exists && existing != key {
			duplicates[value] = struct{}{}
			continue
		}
		result[value] = key
	}
	for value := range duplicates {
		delete(result, value)
	}
	return result
}

func normalizeKeyMatch(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(value)), " "))
}

func generatedRuleKey(layer string, rule model.LivePolicyRule) string {
	prefix := strings.ToLower(strings.TrimSpace(layer))
	if prefix == "" {
		prefix = "policy"
	}
	seed := normalizeKeyMatch(rule.Title)
	if seed == "" {
		seed = normalizeKeyMatch(rule.Text)
	}
	if seed == "" {
		seed = "rule"
	}
	slug := readableRuleSlug(seed, 40)
	sum := sha256.Sum256([]byte(seed))
	return prefix + "." + slug + "." + hex.EncodeToString(sum[:4])
}

func readableRuleSlug(value string, maxRunes int) string {
	var builder strings.Builder
	lastSeparator := false
	count := 0
	for _, current := range value {
		if unicode.IsLetter(current) || unicode.IsDigit(current) {
			if count >= maxRunes {
				break
			}
			builder.WriteRune(unicode.ToLower(current))
			lastSeparator = false
			count++
			continue
		}
		if builder.Len() > 0 && !lastSeparator {
			builder.WriteByte('-')
			lastSeparator = true
		}
	}
	slug := strings.Trim(builder.String(), "-")
	if slug == "" {
		return "rule"
	}
	return slug
}

func nextUniqueRuleKey(base string, used map[string]struct{}) string {
	if _, exists := used[base]; !exists {
		return base
	}
	for suffix := 2; ; suffix++ {
		candidate := base + "-" + strconv.Itoa(suffix)
		if _, exists := used[candidate]; !exists {
			return candidate
		}
	}
}
