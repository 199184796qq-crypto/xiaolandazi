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

// PartitionFacts labels relevance without discarding any recalled evidence.
// Exact attribute matches are main facts; all other candidates remain available
// as peripheral facts, including separately stored prerequisites and conflicts.
// Neither tier implies greater truth or applicability. An unknown attribute
// leaves the main tier empty and all candidates in the peripheral tier.
func PartitionFacts(group Group, links []Link, stores []model.MaterialStoreObject) (main, peripheral []Hit, resolved bool) {
	focus := AttributeName(group.Ask.Attribute, links, stores)
	main, peripheral = []Hit{}, []Hit{}
	for _, hit := range group.Hits {
		if focus != "" && hit.ConditionStatus != "不适用" && AttributeName(hit.Attribute, links, stores) == focus {
			main = append(main, hit)
		} else {
			peripheral = append(peripheral, hit)
		}
	}
	return main, peripheral, len(main) > 0
}
