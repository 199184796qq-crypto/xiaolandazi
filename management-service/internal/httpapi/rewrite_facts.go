package httpapi

import (
	"context"
	"encoding/json"
	"log"
	"slices"
	"strings"

	"livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
)

// The writing model gets reviewed values and restrictions. Evidence stays on
// the source claim and continues to be used by the separate answer pipeline.
type rewriteFact struct {
	Object    string          `json:"对象"`
	Attribute string          `json:"属性"`
	Value     json.RawMessage `json:"值"`
	Polarity  string          `json:"极性,omitempty"`
	Condition string          `json:"条件,omitempty"`
	Exception string          `json:"例外,omitempty"`
}

func (s *Server) buildRewriteFactsPack(ctx context.Context, tenant, plan int64) (json.RawMessage, error) {
	claims, catalog, links, err := s.store.PlanRewriteFactsSnapshot(ctx, tenant, plan)
	if err != nil {
		return nil, err
	}
	facts, skipped := projectRewriteFacts(claims, catalog, links)
	log.Printf("rewrite facts projected tenant=%d plan=%d count=%d omitted=%d", tenant, plan, len(facts), skipped)
	return json.Marshal(facts)
}

type rewriteObject struct {
	kind string
	id   int64
	name string
}

type rewriteCandidate struct {
	claim      model.PlanMaterialClaim
	objects    []rewriteObject
	attribute  string
	value      json.RawMessage
	conditions []rewritePredicate
	exceptions []rewritePredicate
}

func rewriteCandidates(claims []model.PlanMaterialClaim, catalog model.MaterialObjectCatalog, links []db.QALink) []rewriteCandidate {
	linkNames := make(map[int64]string, len(links))
	for _, link := range links {
		if slices.Contains(catalog.Directory.RemovedLinkIDs, link.ID) {
			continue
		}
		name := strings.TrimSpace(catalog.Directory.LinkDisplayNames[link.ID])
		if name == "" {
			name = strings.TrimSpace(link.FieldLabels["display_name"])
		}
		if name == "" {
			name = materialTargetLabel(model.MaterialObjectTarget{Kind: "product", LinkID: link.ID, LinkKey: link.LinkKey})
		}
		linkNames[link.ID] = name
	}
	objectNames := make(map[int64]string, len(catalog.Directory.Objects))
	for _, object := range catalog.Directory.Objects {
		objectNames[object.ID] = strings.TrimSpace(object.DisplayName)
	}
	global := strings.TrimSpace(catalog.Directory.GlobalDisplayName)
	if global == "" {
		global = "全店通用"
	}
	out := make([]rewriteCandidate, 0, len(claims))
	for _, c := range claims {
		if c.Status != "" && c.Status != "active" && c.Status != "confirmed" ||
			c.Destination != "" && c.Destination != "fact" && c.Destination != "material" ||
			c.NeedsConfirmation || c.NeedsSplit || c.SourceIssue != "" || c.PolarityIssue != "" || c.ConditionIssue != "" || len(c.P0Hits) > 0 {
			continue
		}
		objects := rewriteClaimObjects(c, objectNames, linkNames, global)
		if len(objects) == 0 {
			continue
		}
		attribute := strings.TrimSpace(c.Attribute)
		topic := strings.TrimSpace(c.Topic)
		if attribute == "" {
			attribute = topic
		} else if topic != "" && !strings.Contains(attribute, topic) && !strings.Contains(topic, attribute) {
			attribute = topic + "·" + attribute
		}
		value := strings.TrimSpace(c.Value)
		if value == "" {
			value = strings.TrimSpace(c.Content)
		}
		if attribute == "" || value == "" {
			continue
		}
		conditions, conditionsOK := decodeRewritePredicates(c.Conditions)
		exceptions, exceptionsOK := decodeRewritePredicates(c.Exceptions)
		if !conditionsOK || !exceptionsOK || c.FactType == "conditional" && len(conditions)+len(exceptions) == 0 {
			continue
		}
		content := strings.TrimSpace(c.Content)
		// Keep a reviewed qualifier when the short value is just part of the
		// reviewed content and the entire qualifier is attested by its quote.
		if c.Destination == "fact" && len(conditions)+len(exceptions) == 0 && !strings.HasPrefix(value, "[") &&
			strings.Contains(content, value) && strings.Contains(c.SourceQuote, content) {
			value = content
		}
		out = append(out, rewriteCandidate{c, objects, attribute, encodeRewriteValue(attribute, value), conditions, exceptions})
	}
	return out
}

func rewriteClaimObjects(c model.PlanMaterialClaim, objectNames, linkNames map[int64]string, global string) []rewriteObject {
	if c.ScopeKind == "unresolved" {
		return nil
	}
	targets := c.ObjectTargets
	if targets == nil {
		switch c.ScopeKind {
		case "plan":
			targets = []model.MaterialObjectTarget{{Kind: "plan"}}
		case "product":
			targets = []model.MaterialObjectTarget{{Kind: "product", LinkID: c.ProductLinkID}}
		case "store":
			targets = []model.MaterialObjectTarget{{Kind: "store", StoreName: c.StoreName}}
		}
	}
	objects := make([]rewriteObject, 0, len(targets))
	for _, t := range targets {
		var name string
		var id int64
		switch t.Kind {
		case "plan":
			if len(targets) != 1 {
				return nil
			}
			name = global
		case "object":
			id, name = t.ObjectID, objectNames[t.ObjectID]
		case "product":
			id, name = t.LinkID, linkNames[t.LinkID]
		case "store":
			name = strings.TrimSpace(t.StoreName)
			if genericMaterialStore(name) || name == "未说明" {
				name = ""
			}
		}
		if name != "" {
			o := rewriteObject{kind: t.Kind, id: id, name: name}
			if !slices.Contains(objects, o) {
				objects = append(objects, o)
			}
		}
	}
	slices.SortFunc(objects, func(a, b rewriteObject) int {
		return strings.Compare(a.name, b.name)
	})
	return objects
}

func encodeRewriteValue(attribute, value string) json.RawMessage {
	var list []string
	ordered := false
	if strings.HasPrefix(value, "[") {
		_ = json.Unmarshal([]byte(value), &list)
	} else if (strings.Contains(attribute, "流程") || strings.Contains(attribute, "工序") || strings.Contains(attribute, "工艺")) &&
		!strings.ContainsAny(value, "()（）[]【】《》\"“”") {
		// Keep the sequence in one value, in order; all its steps retain the
		// same scope and restrictions. Do not split other free-form facts.
		list = strings.FieldsFunc(value, func(r rune) bool { return strings.ContainsRune("，；;", r) })
		ordered = true
	}
	if len(list) > 0 {
		values := make([]string, 0, len(list))
		for _, item := range list {
			if item = strings.TrimSpace(item); item != "" && (ordered || !slices.Contains(values, item)) {
				values = append(values, item)
			}
		}
		if len(values) == 1 {
			value = values[0]
		} else if len(values) > 1 {
			raw, _ := json.Marshal(values)
			return raw
		}
	}
	// Commas can belong to names, measurements, sequences or conditional
	// clauses. Keep free-form values intact unless the source typed a list.
	raw, _ := json.Marshal(value)
	return raw
}

func projectRewriteFacts(claims []model.PlanMaterialClaim, catalog model.MaterialObjectCatalog, links []db.QALink) ([]rewriteFact, int) {
	return projectRewriteFactsByObject(claims, catalog, links, false)
}

func projectRewriteFactsByObject(claims []model.PlanMaterialClaim, catalog model.MaterialObjectCatalog, links []db.QALink, separate bool) ([]rewriteFact, int) {
	candidates := rewriteCandidates(claims, catalog, links)
	type factKey struct{ attribute, value, polarity, condition, exception string }
	type entry struct {
		fact    rewriteFact
		objects []rewriteObject
	}
	type evidenceKey struct {
		factKey
		key, sentence, quote string
	}
	entries := []entry{}
	index := map[factKey]int{}
	scopedEvidence := map[evidenceKey]bool{}
	skipped := len(claims) - len(candidates)
	type rendered struct {
		candidate rewriteCandidate
		fact      factKey
		evidence  evidenceKey
	}
	rows := make([]rendered, 0, len(candidates))
	for _, c := range candidates {
		condition, ok := renderRewritePredicates(c.conditions, c, candidates)
		exception, exOK := renderRewritePredicates(c.exceptions, c, candidates)
		if !ok || !exOK {
			skipped++
			continue
		}
		polarity := ""
		if c.claim.Polarity == "negative" || c.claim.Polarity == "" && c.claim.FactType == "negative" {
			polarity = "否定"
		}
		key := factKey{c.attribute, string(c.value), polarity, condition, exception}
		evidenceFact := key
		evidenceFact.attribute = "" // The stable key and same evidence can disambiguate differing display labels.
		evidence := evidenceKey{evidenceFact, c.claim.ClaimKey, c.claim.SourceSentence, c.claim.SourceQuote}
		if c.objects[0].kind != "plan" && evidence.key != "" && evidence.sentence != "" && evidence.quote != "" {
			scopedEvidence[evidence] = true
		}
		rows = append(rows, rendered{c, key, evidence})
	}
	for _, row := range rows {
		c, key := row.candidate, row.fact
		if c.objects[0].kind == "plan" && scopedEvidence[row.evidence] {
			continue
		}
		i, exists := index[key]
		if !exists {
			i = len(entries)
			index[key] = i
			entries = append(entries, entry{fact: rewriteFact{
				Attribute: key.attribute, Value: c.value, Polarity: key.polarity,
				Condition: key.condition, Exception: key.exception,
			}})
		}
		for _, object := range c.objects {
			if !slices.Contains(entries[i].objects, object) {
				entries[i].objects = append(entries[i].objects, object)
			}
		}
	}
	facts := make([]rewriteFact, 0, len(entries))
	for _, entry := range entries {
		names := make([]string, 0, len(entry.objects))
		for _, o := range entry.objects {
			if o.kind == "plan" {
				names = []string{o.name}
				break
			}
			if !slices.Contains(names, o.name) {
				names = append(names, o.name)
			}
		}
		slices.Sort(names)
		if separate {
			for _, name := range names {
				fact := entry.fact
				fact.Object = name
				facts = append(facts, fact)
			}
			continue
		}
		entry.fact.Object = strings.Join(names, "、")
		facts = append(facts, entry.fact)
	}
	return facts, skipped
}
