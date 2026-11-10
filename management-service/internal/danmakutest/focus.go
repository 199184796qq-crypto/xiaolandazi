package danmakutest

import (
	"regexp"
	"sort"
	"strings"
	"unicode"

	"livecompanion/management/internal/model"
)

var numberedObjectName = regexp.MustCompile(`^第?([0-9]+)\s*号?(?:链接)?$`)

// AttributeName removes only prefixes belonging to actual plan objects. It
// does not equate different attributes (price, store price, fees, etc.) by
// substring, nor does it require the model to maintain database keys.
func AttributeName(name string, links []Link, stores []model.MaterialStoreObject) string {
	prefixes := []string{}
	for _, link := range links {
		label := strings.TrimSpace(link.Name)
		if m := numberedObjectName.FindStringSubmatch(label); len(m) == 2 {
			for _, p := range []string{"第" + m[1] + "号链接", m[1] + "号链接", "第" + m[1] + "号", m[1] + "号", m[1] + "链接", label + "：", label + ":"} {
				prefixes = append(prefixes, p)
			}
		} else if label != "" {
			prefixes = append(prefixes, label)
		}
	}
	for _, store := range stores {
		if strings.TrimSpace(store.Name) != "" {
			prefixes = append(prefixes, strings.TrimSpace(store.Name))
		}
	}
	sort.SliceStable(prefixes, func(i, j int) bool { return len(prefixes[i]) > len(prefixes[j]) })
	name = strings.TrimSpace(name)
	for _, prefix := range prefixes {
		if rest, ok := strings.CutPrefix(name, prefix); ok && strings.TrimSpace(rest) != "" {
			name = strings.TrimLeftFunc(rest, func(r rune) bool { return unicode.IsSpace(r) || strings.ContainsRune("：:·・-—_", r) })
			break
		}
	}
	return strings.ToLower(strings.Join(strings.Fields(name), ""))
}

// FocusFacts leaves the broad recall list intact for inspection. Only the
// current attribute and all of its complete conditional variants enter the
// answer facts when an exact, program-normalized name is available. Unknown
// vocabulary keeps the bounded candidates, explicitly marked unresolved, so
// this precision fix does not silently turn semantic matches into no facts.
func FocusFacts(group Group, links []Link, stores []model.MaterialStoreObject) ([]Hit, bool) {
	focus := AttributeName(group.Ask.Attribute, links, stores)
	selected := []Hit{}
	if focus != "" {
		for _, hit := range group.Hits {
			if AttributeName(hit.Attribute, links, stores) == focus {
				selected = append(selected, hit)
			}
		}
	}
	if len(selected) > 0 {
		return selected, true
	}
	return group.Hits, false
}
