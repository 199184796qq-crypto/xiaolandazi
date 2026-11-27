package danmakutest

import (
	"slices"
	"sort"
	"strings"

	"livecompanion/management/internal/model"
)

const intentAttributeLimit = 150

type IntentAttributeEntry struct {
	Key   string `json:"key"`
	Topic string `json:"topic"`
}

type IntentAttributeDirectory struct {
	Names           []string
	Entries         []IntentAttributeEntry
	CatalogedFacts  int
	FilteredObjects int
}

// IntentAttributeResolver applies the same naming rules to intent, recall and
// the local lab. Older claims can still use an attribute stored in Topic.
type IntentAttributeResolver struct {
	links      []Link
	stores     []model.MaterialStoreObject
	aliases    map[string]string
	objectKeys map[string]bool
}

func NewIntentAttributeResolver(links []Link, stores []model.MaterialStoreObject, globalName string, aliases map[string]string) IntentAttributeResolver {
	r := IntentAttributeResolver{links: links, stores: stores, aliases: aliases, objectKeys: map[string]bool{}}
	add := func(name string) {
		if key := IntentAttributeKey(name); key != "" {
			r.objectKeys[key] = true
		}
	}
	for _, link := range links {
		add(link.Name)
		add(link.DisplayName)
		add(link.Product)
		if match := numberedObjectName.FindStringSubmatch(IntentAttributeKey(link.Name)); match != nil {
			for _, suffix := range []string{"", "号", "链接", "号链接"} {
				add(match[1] + suffix)
				add("第" + match[1] + suffix)
			}
		}
	}
	for _, store := range stores {
		add(store.Name)
	}
	add(globalName)
	return r
}

func IntentAttributeKey(text string) string {
	return strings.ToLower(strings.Join(strings.Fields(text), ""))
}

func (r IntentAttributeResolver) IsObject(name string) bool {
	return r.objectKeys[IntentAttributeKey(name)]
}

func (r IntentAttributeResolver) CanonicalName(name string) string {
	name = strings.TrimSpace(name)
	if r.IsObject(name) {
		return ""
	}
	key := IntentAttributeKey(name)
	switch key {
	case "品名", "商品名称":
		// These claims describe the product's name, not general product information.
		name = "商品名称"
	case "不邮寄":
		// Older records used the fact's negative value as the shipping attribute.
		name = "配送"
	default:
		if canonical := r.aliases[key]; canonical != "" {
			name = canonical
		}
		if name == "直播价" || name == "直播价格" || name == "直播间价格" {
			name = "价格"
		}
	}
	if r.IsObject(name) {
		return ""
	}
	return name
}

func (r IntentAttributeResolver) ClaimName(claim model.PlanMaterialClaim) (name string, filteredObject bool) {
	name = strings.TrimSpace(claim.Attribute)
	if name == "" && !r.IsObject(claim.Topic) {
		name = AttributeName(claim.Topic, r.links, r.stores)
	} else if name == "品种特征" && strings.TrimSpace(claim.ClaimKey) == "品种来源" {
		// Use the known source label for claims specifically about the variety's origin.
		name = "品种来源"
	}
	if name == "" && r.IsObject(claim.Topic) {
		return "", true
	}
	canonical := r.CanonicalName(name)
	return canonical, name != "" && canonical == ""
}

func (r IntentAttributeResolver) Directory(records []Record) IntentAttributeDirectory {
	out := IntentAttributeDirectory{Names: []string{}, Entries: []IntentAttributeEntry{}}
	seenNames := map[string]bool{}
	seenEntries := map[IntentAttributeEntry]bool{}
	for _, record := range records {
		claim := record.Claim
		if claim.ScopeKind == "unresolved" || (claim.Destination != "" && claim.Destination != "fact" && claim.Destination != "qa") {
			continue
		}
		name, filtered := r.ClaimName(claim)
		if filtered {
			out.FilteredObjects++
		}
		if name == "" {
			continue
		}
		if !seenNames[name] {
			if len(out.Names) == intentAttributeLimit {
				continue
			}
			out.Names = append(out.Names, name)
			seenNames[name] = true
		}
		key := strings.TrimSpace(claim.ClaimKey)
		if key == "" || key == "claim" {
			continue
		}
		out.CatalogedFacts++
		entry := IntentAttributeEntry{Key: key, Topic: name}
		if !seenEntries[entry] {
			out.Entries = append(out.Entries, entry)
			seenEntries[entry] = true
		}
	}
	sort.Strings(out.Names)
	return out
}

func (d IntentAttributeDirectory) CountUnknown(attributes []string) int {
	unknown := 0
	for _, attribute := range attributes {
		name := strings.TrimSpace(attribute)
		if name != "" && !slices.Contains(d.Names, name) {
			unknown++
		}
	}
	return unknown
}
