package httpapi

import (
	"errors"
	"fmt"
	"strings"

	"livecompanion/management/internal/anchorcycle"
)

const anchorWorkbenchFactsBoundary = "边界：只能使用以上事实；缺少库存、销量、倒计时或现场事件时不得虚构。"

// A rendered fact is a block, not a line: values and expression guidance may
// contain newlines. The renderer's document header and terminal boundary are
// not fact records. Keep continuation text attached to its original record.
func productFactBlocks(facts string) []string {
	var blocks, current []string
	flush := func() {
		if len(current) > 0 {
			blocks = append(blocks, strings.TrimSpace(strings.Join(current, "\n")))
		}
		current = nil
	}
	for _, line := range strings.Split(strings.ReplaceAll(facts, "\r\n", "\n"), "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "- ["):
			flush()
			current = []string{trimmed}
		case trimmed == anchorWorkbenchFactsBoundary:
			flush()
		case len(current) > 0:
			current = append(current, line)
		}
	}
	flush()
	return blocks
}

// Parse explicit program-owned bindings, including multi-link benefits. The
// unlinked fallback is retained for legacy callers; the new initialization
// classifies those records with the model before materializing their bundles.
// Only the first line carries metadata, not examples in multiline guidance.
func scopedPlanProductFact(block string) anchorcycle.ScopedFact {
	if fact, explicit, err := explicitPlanFactScope(block); explicit {
		if err != nil {
			return anchorcycle.ScopedFact{Text: block, Links: []string{"__unavailable_fact_scope__"}}
		}
		return fact
	}
	header, _, _ := strings.Cut(block, "\n")
	links := parseCardLinks(header)
	return anchorcycle.ScopedFact{Text: block, Links: links, Shared: len(links) == 0, Applicable: true}
}

// Check full records, including conditions and guidance, before saving a new
// independent-pool snapshot. A matching source hash alone cannot prove that
// the snapshot actually contains its source facts. Historical versions remain
// immutable and are not filled retroactively by this check.
func validateProductFactSnapshot(facts string, focus *anchorcycle.ProductFocus) error {
	if focus == nil {
		return errors.New("商品事实快照缺失，请重新保存方案")
	}
	remaining := map[string]int{}
	for _, block := range productFactBlocks(facts) {
		remaining[block]++
	}
	for _, fact := range focus.Facts {
		if remaining[fact.Text] == 0 {
			return errors.New("商品事实快照与安全事实不一致，未保存，请重新核对")
		}
		remaining[fact.Text]--
		if err := validateScopeLinks(fact.Links, fact.Shared, focus.Products); err != nil {
			return fmt.Errorf("商品事实快照归属无效：%w", err)
		}
		expected := scopedPlanProductFact(fact.Text)
		if _, explicit, err := explicitPlanFactScope(fact.Text); explicit && (err != nil || expected.Shared != fact.Shared) {
			return errors.New("用户指定的事实归属不能被覆盖，请重新初始化")
		}
		if !fact.Shared && len(fact.Links) == 0 {
			return errors.New("商品事实缺少适用商品，未保存")
		}
		if len(expected.Links) > 0 && (fact.Shared || len(fact.Links) != len(expected.Links)) || (len(fact.Links) > 1 && !fact.Applicable) {
			return errors.New("商品事实快照丢失适用范围，未保存，请重新核对")
		}
		for _, link := range expected.Links {
			if !containsLink(fact.Links, link) {
				return errors.New("商品事实快照链接不一致，未保存，请重新核对")
			}
		}
	}
	for _, count := range remaining {
		if count != 0 {
			return errors.New("商品事实快照遗漏事实或使用条件，未保存，请重试")
		}
	}
	return nil
}
