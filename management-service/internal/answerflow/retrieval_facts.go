package answerflow

import (
	"context"
	"crypto/sha256"
	"fmt"
	"math"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode"

	"livecompanion/management/internal/danmakutest"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/semantic"
)

const (
	factRecallLimit    = 12
	factEvidenceBudget = 5000
)

type retrievalFact struct {
	hit    danmakutest.Hit
	claim  model.PlanMaterialClaim
	vector []float32
	owners []string
	text   string
}

type factRetrieval struct {
	config         RetrievalConfig
	facts          []retrievalFact
	objects        []danmakutest.Link
	subjectAliases map[string]string
	attributes     []string
	terms          []string
	conceptText    map[string]string
	regionCoverage map[[2]string][]string
}

func newFactRetrieval(lib Library, config RetrievalConfig) factRetrieval {
	r := factRetrieval{
		config: config, subjectAliases: map[string]string{}, conceptText: map[string]string{},
	}
	r.config.GlobalName = strings.TrimSpace(config.GlobalName)
	r.regionCoverage = map[[2]string][]string{}
	addObject := func(name string, originals ...string) {
		name = strings.TrimSpace(name)
		if name == "" {
			return
		}
		r.objects = append(r.objects, danmakutest.Link{Name: name, DisplayName: name})
		r.terms = append(r.terms, name)
		for _, alias := range append(originals, name) {
			key := subjectKey(alias)
			if key == "" {
				continue
			}
			if previous, ok := r.subjectAliases[key]; ok && previous != name {
				r.subjectAliases[key] = ""
			} else if !ok {
				r.subjectAliases[key] = name
			}
		}
	}
	for _, link := range lib.Links {
		if link.MaterialHidden {
			continue
		}
		name := link.DisplayName
		if name == "" {
			name = link.Name
		}
		aliases := []string{link.Name, link.DisplayName, link.Product}
		if number := linkNumber(link.Name); number != "" {
			aliases = append(aliases, number, number+"号", "第"+number+"号", number+"链接")
		}
		addObject(name, aliases...)
		if link.Product != "" && strings.TrimSpace(name) != "" {
			r.objects[len(r.objects)-1].Product = link.Product
			r.terms = append(r.terms, link.Product)
		}
	}
	for _, store := range lib.Stores {
		addObject(store.Name)
	}
	addObject(r.config.GlobalName)

	records := map[int64]danmakutest.Record{}
	for _, record := range lib.Records {
		if record.Claim.ScopeKind == "unresolved" {
			continue
		}
		switch record.Claim.Destination {
		case "", "fact", "qa":
			if record.Claim.ID > 0 {
				records[record.Claim.ID] = record
			}
		}
	}
	groups, _ := danmakutest.AllFactsByObject(lib.Records, lib.Links, lib.Stores)
	seen := map[int64]bool{}
	for _, group := range groups {
		for _, hit := range group.Hits {
			record, ok := records[hit.ID]
			if !ok || seen[hit.ID] || strings.TrimSpace(hit.Content) == "" {
				continue
			}
			seen[hit.ID] = true
			attribute := danmakutest.AttributeName(hit.Attribute, lib.Links, lib.Stores)
			r.terms = append(r.terms, hit.Attribute, attribute)
			hit.Attribute = attribute
			hit.Scope = group.Ask.Text
			owners := strings.Split(group.Ask.Text, "、")
			for _, owner := range owners {
				key := [2]string{owner, attribute}
				r.regionCoverage[key] = append(r.regionCoverage[key], danmakutest.ExplicitEvidenceRegions(record.Claim)...)
			}
			r.facts = append(r.facts, retrievalFact{
				hit: hit, claim: record.Claim, vector: record.Vector,
				owners: owners, text: evidenceText(hit),
			})
		}
	}
	sort.SliceStable(r.facts, func(i, j int) bool { return r.facts[i].hit.ID < r.facts[j].hit.ID })
	values := map[string][]string{}
	for _, fact := range r.facts {
		name := fact.hit.Attribute
		if name == "" {
			continue
		}
		if len(values[name]) < 3 {
			value := strings.TrimSpace(fact.claim.Content)
			if value == "" {
				value = strings.TrimSpace(fact.claim.SourceQuote)
			}
			if value != "" {
				values[name] = append(values[name], value)
			}
		}
	}
	for name, examples := range values {
		r.attributes = append(r.attributes, name)
		r.conceptText[name] = name + "：" + strings.Join(examples, "；")
	}
	slices.Sort(r.attributes)
	for alias, canonical := range config.AttributeAliases {
		if slices.Contains(r.attributes, canonical) {
			r.terms = append(r.terms, alias)
		}
	}
	return r
}

func keyOf(text string) string {
	return strings.ToLower(strings.Join(strings.Fields(text), ""))
}

func subjectKey(text string) string {
	return strings.TrimPrefix(strings.TrimSuffix(keyOf(text), "链接"), "第")
}

func linkNumber(name string) string {
	number := strings.TrimSuffix(subjectKey(name), "号")
	if number == "" || strings.ContainsFunc(number, func(r rune) bool { return r < '0' || r > '9' }) {
		return ""
	}
	return number
}

func (r factRetrieval) normalizeSubject(subject string) string {
	key := subjectKey(subject)
	if value, found := r.subjectAliases[key]; found {
		return value
	}
	global := subjectKey(r.config.GlobalName)
	if global != "" && len([]rune(key)) >= 2 &&
		(strings.Contains(global, key) || strings.Contains(key, global)) {
		return r.config.GlobalName
	}
	return ""
}

func (r factRetrieval) attributeNamed(question string, attributes []string) bool {
	question = keyOf(question)
	for _, attribute := range attributes {
		if strings.Contains(question, keyOf(attribute)) {
			return true
		}
		for alias, canonical := range r.config.AttributeAliases {
			if canonical == attribute && strings.Contains(question, alias) {
				return true
			}
		}
	}
	return false
}

func evidenceText(hit danmakutest.Hit) string {
	body := strings.TrimSpace(hit.Content)
	if hit.Attribute != "" && !strings.Contains(body, hit.Attribute) {
		body = hit.Attribute + "：" + body
	}
	if hit.Conditions != "" && !strings.Contains(body, hit.Conditions) {
		body += "。条件：" + hit.Conditions
	}
	if hit.Exceptions != "" && !strings.Contains(body, hit.Exceptions) {
		body += "。例外：" + hit.Exceptions
	}
	return body
}

type retrievalVectors struct {
	model    string
	byText   map[string][]float32
	concepts map[string][]float32
}

func (s Service) embedRetrievalQueries(ctx context.Context, scope Scope, texts []string) (Embeddings, error) {
	if len(texts) == 0 || s.Embed == nil {
		return Embeddings{}, nil
	}
	if err := s.check(ctx, scope, BeforeEmbedding); err != nil {
		return Embeddings{}, err
	}
	response, err := s.Embed(ctx, texts)
	if ctx.Err() != nil {
		return Embeddings{}, ctx.Err()
	}
	if err != nil || len(response.Vectors) != len(texts) {
		return Embeddings{}, nil
	}
	return response, nil
}

func (r factRetrieval) conceptQuery(query string) string {
	replacements := make([]string, 0, len(r.objects)*2+24)
	for _, object := range r.objects {
		replacements = append(replacements, object.Name, " ")
	}
	for _, word := range []string{"商品", "东西", "这个", "那个", "这款", "哪款", "怎么样", "好不好", "请问", "的", "吗", "呢", "啊"} {
		replacements = append(replacements, word, " ")
	}
	value := strings.Join(strings.Fields(strings.NewReplacer(replacements...).Replace(query)), " ")
	if value == "" {
		return query
	}
	return value
}

func (s Service) retrievalQueryVectors(ctx context.Context, scope Scope, r factRetrieval, asks []retrievedAsk) (retrievalVectors, error) {
	queries := []string{}
	needConcepts := false
	for _, ask := range asks {
		queries = append(queries, ask.Query)
		if len(ask.Attributes) != 0 {
			needConcepts = true
			queries = append(queries, r.conceptQuery(ask.Query))
			for _, attr := range ask.Attributes {
				if !slices.Contains(r.attributes, attr) && !slices.Contains(r.attributes, r.config.AttributeAliases[keyOf(attr)]) {
					queries = append(queries, attr)
				}
			}
		}
	}
	queries = uniqueStrings(queries)
	data, err := s.embedRetrievalQueries(ctx, scope, queries)
	out := retrievalVectors{model: data.Model, byText: map[string][]float32{}, concepts: map[string][]float32{}}
	if err != nil || data.Model == "" {
		return out, err
	}
	for i, query := range queries {
		out.byText[query] = data.Vectors[i]
	}
	if !needConcepts {
		return out, nil
	}
	missingNames, missingText := []string{}, []string{}
	for _, name := range r.attributes {
		concept, ok := r.config.Concepts[name]
		text := r.conceptText[name]
		if ok && concept.Model == data.Model && concept.Hash == fmt.Sprintf("%x", sha256.Sum256([]byte(text))) &&
			len(concept.Vector) == len(data.Vectors[0]) {
			out.concepts[name] = concept.Vector
		} else if len(missingNames) < 48 {
			missingNames = append(missingNames, name)
			missingText = append(missingText, text)
		}
	}
	data, err = s.embedRetrievalQueries(ctx, scope, missingText)
	if err != nil {
		return out, err
	}
	if data.Model == out.model {
		for i, name := range missingNames {
			out.concepts[name] = data.Vectors[i]
		}
	}
	return out, nil
}

type attributeScore struct {
	name  string
	score float64
}

func nearestAttributes(query []float32, concepts map[string][]float32, excluded []string, threshold float64) []attributeScore {
	out := []attributeScore{}
	if len(query) == 0 {
		return out
	}
	for attr, vector := range concepts {
		if len(vector) != len(query) || slices.Contains(excluded, attr) {
			continue
		}
		score := semantic.CosineSimilarity(vector, query)
		if score >= threshold {
			out = append(out, attributeScore{attr, score})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].score == out[j].score {
			return out[i].name < out[j].name
		}
		return out[i].score > out[j].score
	})
	return out
}

func (r factRetrieval) normalizeAttributes(ask *retrievedAsk, vectors retrievalVectors) {
	if len(ask.Attributes) == 0 {
		return
	}
	normalized := []string{}
	for _, attribute := range ask.Attributes {
		name := attribute
		if !slices.Contains(r.attributes, name) {
			name = r.config.AttributeAliases[keyOf(attribute)]
			if !slices.Contains(r.attributes, name) {
				name = ""
				matches := nearestAttributes(vectors.byText[attribute], vectors.concepts, nil, .55)
				if len(matches) > 0 {
					name = matches[0].name
				}
			}
		}
		if name != "" {
			normalized = append(normalized, name)
			if name != attribute {
				ask.Notes = append(ask.Notes, "属性："+attribute+" → "+name)
			}
		}
	}
	normalized = uniqueStrings(normalized)
	limit := len(normalized) + 3
	query := r.conceptQuery(ask.Query)
	if !r.attributeNamed(ask.Text, normalized) || len(normalized) == 0 {
		for _, match := range nearestAttributes(vectors.byText[query], vectors.concepts, normalized, .45) {
			if len(normalized) >= limit {
				break
			}
			normalized = append(normalized, match.name)
			ask.Notes = append(ask.Notes, fmt.Sprintf("问题扩展：%s（%.2f）", match.name, match.score))
		}
	}
	ask.Attributes = normalized
}

type rankedFact struct {
	fact retrievalFact
	hit  danmakutest.Hit
}

func (r factRetrieval) candidates(ask retrievedAsk, vectors retrievalVectors, broad bool) []rankedFact {
	if ask.Unresolved {
		return nil
	}
	subject := ""
	if len(ask.Subjects) == 1 {
		subject = ask.Subjects[0]
	}
	globalOnly := subject != "" && subject == r.config.GlobalName
	vector := vectors.byText[ask.Query]
	filterAttr := !broad && !globalOnly && len(ask.Attributes) > 0
	if !broad && ask.AttributeScope == "明确" && len(ask.Attributes) == 0 {
		return nil
	}
	scopeOnly := !filterAttr && subject != ""
	out := []rankedFact{}
	for _, fact := range r.facts {
		global := fact.hit.Scope == "全局"
		if globalOnly && !global || !global && subject != "" && !slices.Contains(fact.owners, subject) {
			continue
		}
		if filterAttr && !slices.Contains(ask.Attributes, fact.hit.Attribute) {
			continue
		}
		hit := fact.hit
		var covered []string
		if ask.Region != "" {
			for _, owner := range fact.owners {
				covered = append(covered, r.regionCoverage[[2]string{owner, hit.Attribute}]...)
			}
		}
		hit.ConditionStatus = danmakutest.EvidenceCondition(fact.claim, ask.Region, uniqueStrings(covered))
		if hit.ConditionStatus == "不适用" {
			continue
		}
		if vectors.model != "" && fact.claim.EmbeddingModel == vectors.model && len(vector) > 0 && len(fact.vector) == len(vector) {
			hit.Score = semantic.CosineSimilarity(vector, fact.vector)
		} else {
			hit.Score = 0
		}
		if !scopeOnly && !filterAttr && hit.Score < .35 {
			continue
		}
		out = append(out, rankedFact{fact: fact, hit: hit})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].hit.Score > out[j].hit.Score })
	if !scopeOnly && len(out) > factRecallLimit {
		// First retain each object with a matching attribute. Then fill the
		// remaining recall slots with the highest-scoring candidates.
		out = keepObjectCoverage(out, factRecallLimit)
	}
	return out
}

func keepObjectCoverage(items []rankedFact, limit int) []rankedFact {
	seen := map[string]bool{}
	hosts := map[int64]bool{}
	out := make([]rankedFact, 0, limit)
	for _, item := range items {
		if !seen[item.hit.Scope] {
			seen[item.hit.Scope] = true
			hosts[item.hit.ID] = true
			out = append(out, item)
		}
	}
	for _, item := range items {
		if len(out) >= limit {
			break
		}
		if !hosts[item.hit.ID] {
			out = append(out, item)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].hit.Score > out[j].hit.Score })
	return out
}

func rerankFacts(ctx context.Context, r factRetrieval, ask *retrievedAsk, items []rankedFact) {
	if len(items) < 2 {
		return
	}
	if r.config.Rerank == nil {
		ask.Notes = append(ask.Notes, "精排未启用，沿用召回顺序")
		return
	}
	head := items[:min(64, len(items))]
	texts := make([]string, len(head))
	for i, item := range head {
		texts[i] = item.hit.Scope + "：" + item.fact.text
	}
	scores, err := r.config.Rerank(ctx, ask.Query, texts)
	if err != nil || len(scores) != len(head) {
		ask.Notes = append(ask.Notes, "精排不可用，沿用召回顺序")
		return
	}
	type scoredFact struct {
		item  rankedFact
		score float64
	}
	ranks := make([]scoredFact, len(head))
	for i, score := range scores {
		if math.IsNaN(score) || math.IsInf(score, 0) {
			return
		}
		ranks[i] = scoredFact{head[i], score}
	}
	sort.SliceStable(ranks, func(i, j int) bool { return ranks[i].score > ranks[j].score })
	for i, item := range ranks {
		head[i] = item.item
	}
	ask.Reranked = true
}

type retrievedEvidence struct {
	IDs  []int64 `json:"ids"`
	Text string  `json:"text"`
}

type retrievedFactGroup struct {
	Object string              `json:"object"`
	Facts  []retrievedEvidence `json:"facts"`
}

func (s Service) recallForAnswer(ctx context.Context, scope Scope, r factRetrieval, intent *retrievedIntent, out *RetrievedResult) ([]retrievedFactGroup, error) {
	start := time.Now()
	vectors, err := s.retrievalQueryVectors(ctx, scope, r, intent.Asks)
	if err != nil {
		return nil, err
	}
	out.Recall = danmakutest.Recall{Groups: []danmakutest.Group{}, Hits: []danmakutest.Hit{}, Notices: []string{}}
	if len(intent.Asks) > 0 && vectors.model == "" {
		out.Recall.Notices = append(out.Recall.Notices, "查询向量不可用，本次按已确认的对象和属性取证")
	}
	lists := make([][]rankedFact, len(intent.Asks))
	for i := range intent.Asks {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		ask := &intent.Asks[i]
		r.normalizeAttributes(ask, vectors)
		items := r.candidates(*ask, vectors, false)
		if len(items) == 0 && ask.CompareGroup != "" && len(ask.Subjects) > 0 {
			items = r.candidates(*ask, vectors, true)
			if len(items) > 0 {
				ask.Notes = append(ask.Notes, "无直接证据，补充对比对象事实")
			}
		}
		if len(items) == 0 && !ask.Unresolved {
			items, err = s.stepBackRecall(ctx, scope, r, *ask, vectors)
			if err != nil {
				return nil, err
			}
			if len(items) > 0 {
				ask.Notes = append(ask.Notes, "按原诉求重新检索")
			}
		}
		if intent.Category == "陈述" && len(ask.Attributes) > 0 && len(ask.Subjects) == 0 {
			for _, broad := range r.candidates(*ask, vectors, true) {
				if !slices.ContainsFunc(items, func(item rankedFact) bool { return item.hit.ID == broad.hit.ID }) {
					items = append(items, broad)
				}
			}
		}
		rerankFacts(ctx, r, ask, items)
		if ask.Region != "" {
			ask.Notes = append(ask.Notes, "地区："+ask.Region)
		}
		lists[i] = items
	}
	chosen, used, trimmed := budgetFacts(lists)
	out.Retrieval.EstimatedFactTokens = used
	out.Retrieval.FactBudgetTrimmed = trimmed
	facts := []retrievedFactGroup{}
	positions := map[string]int{}
	seen := map[int64]bool{}
	for i, items := range lists {
		ask := &intent.Asks[i]
		legacyAsk := danmakutest.Ask{Text: ask.Text, Subjects: ask.Subjects, Attribute: strings.Join(ask.Attributes, "、")}
		if ask.Region != "" {
			legacyAsk.Conditions = map[string]string{"region": ask.Region}
		}
		out.Intent.Asks = append(out.Intent.Asks, legacyAsk)
		group := danmakutest.Group{Ask: legacyAsk, Hits: []danmakutest.Hit{}, Notice: strings.Join(ask.Notes, "。")}
		for _, item := range items {
			hit := item.hit
			if !chosen[hit.ID] {
				continue
			}
			ask.FactIDs = append(ask.FactIDs, hit.ID)
			group.Hits = append(group.Hits, hit)
			if seen[hit.ID] {
				continue
			}
			seen[hit.ID] = true
			out.Recall.Hits = append(out.Recall.Hits, hit)
			if _, ok := positions[hit.Scope]; !ok {
				positions[hit.Scope] = len(facts)
				facts = append(facts, retrievedFactGroup{Object: hit.Scope})
			}
			idx := positions[hit.Scope]
			facts[idx].Facts = append(facts[idx].Facts, retrievedEvidence{IDs: []int64{hit.ID}, Text: item.fact.text})
		}
		out.Recall.Groups = append(out.Recall.Groups, group)
	}
	out.Retrieval.Asks = intent.Asks
	out.Retrieval.FactCount = len(out.Recall.Hits)
	out.Retrieval.RecallElapsedMS = time.Since(start).Milliseconds()
	if len(facts) == 0 {
		out.Recall.Notices = append(out.Recall.Notices, "没有找到可用于回答的事实")
	}
	if trimmed {
		out.Recall.Notices = append(out.Recall.Notices, "证据达到输入预算，优先保留每个诉求及对象的事实")
	}
	return facts, ctx.Err()
}

func budgetFacts(lists [][]rankedFact) (map[int64]bool, int, bool) {
	chosen := map[int64]bool{}
	checked := map[int64]bool{}
	left := factEvidenceBudget
	trimmed := false
	add := func(item rankedFact) {
		if checked[item.hit.ID] {
			return
		}
		checked[item.hit.ID] = true
		size := 16 + estimatedFactTokens(item.fact.text) + estimatedFactTokens(item.hit.Scope)
		if left < size {
			trimmed = true
			return
		}
		left -= size
		chosen[item.hit.ID] = true
	}
	maxRank := 0
	for _, list := range lists {
		maxRank = max(maxRank, len(list))
		if len(list) > 0 {
			add(list[0])
		}
		for _, item := range list {
			if item.hit.Scope != "全局" {
				add(item)
				break
			}
		}
	}
	scopes := map[string]bool{}
	for _, list := range lists {
		for _, item := range list {
			if !scopes[item.hit.Scope] {
				scopes[item.hit.Scope] = true
				add(item)
			}
		}
	}
	for rank := 0; rank < maxRank; rank++ {
		for _, list := range lists {
			if rank < len(list) {
				add(list[rank])
			}
		}
	}
	return chosen, factEvidenceBudget - left, trimmed
}

func estimatedFactTokens(text string) int {
	ascii, other := 0, 0
	for _, c := range text {
		if c < unicode.MaxASCII {
			ascii++
		} else {
			other++
		}
	}
	return (ascii+2)/3 + other
}
