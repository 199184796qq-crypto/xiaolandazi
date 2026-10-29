package interactionpurpose

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"unicode"

	"livecompanion/management/internal/contentstrategy"
	"livecompanion/management/internal/domaincard"
	"livecompanion/management/internal/factselect"
	"livecompanion/management/internal/semantic"
)

const ContentType = "interaction_purpose"

type Result struct {
	Question         bool                  `json:"question"`
	Relevant         bool                  `json:"relevant"`
	IntentType       string                `json:"intent_type,omitempty"`
	Need             string                `json:"need,omitempty"`
	ProductRelevance string                `json:"product_relevance,omitempty"`
	Confidence       float64               `json:"confidence"`
	PurposeID        string                `json:"purpose_id"`
	Purpose          string                `json:"purpose"`
	Scope            string                `json:"scope"`
	Angles           []contentstrategy.Row `json:"task_angles"`
	Ambiguous        bool                  `json:"ambiguous,omitempty"`
	Clarify          string                `json:"clarify,omitempty"`
	Options          []string              `json:"options,omitempty"`
	Constraints      *Constraints          `json:"constraints,omitempty"`
	Topic            string                `json:"topic,omitempty"`
	Source           string                `json:"source,omitempty"`
	FactsSHA         string                `json:"facts_sha256,omitempty"`
	Input            string                `json:"input,omitempty"`
}

type Constraints struct {
	Region  string   `json:"region,omitempty"`
	Include []string `json:"include,omitempty"`
	Exclude []string `json:"exclude,omitempty"`
}
type Repository interface {
	GetInteractionPurpose(context.Context, int64, int64, int64, string) (string, error)
	UpsertSemanticDocument(context.Context, semantic.StoredDocument) error
	ListPurposeBuckets(context.Context, int64, int64, int64, int) ([]semantic.PurposeBucket, error)
	UpsertPurposeBucket(context.Context, int64, int64, int64, string, string, string) error
	GetPurposeBucketConclusion(context.Context, int64, int64, int64, string) (string, error)
}

type Complete func(context.Context, string) (string, error)
type Resolver struct {
	Repo     Repository
	Semantic *semantic.Service
	Complete Complete
	// BucketEnabled explicitly opts into judge-based bucket reads and writes.
	// Existing bucket records are retained while this policy is disabled.
	BucketEnabled bool
	// Judge, when non-nil, classifies whether a new question is semantically the
	// same as one of the room's existing purpose buckets, so the resolution model
	// can be skipped for repeats. It consumes only question text (no fact bundle).
	Judge Complete
}
type Input struct {
	Tenant, Room, Plan  int64
	Text, Facts, Prompt string
	Purposes            []contentstrategy.Purpose
	// DomainCard and CardFacts are compact, plan-local intent context. They
	// never replace the approved fact package; they tell the intent model which
	// dimension this question likely belongs to.
	DomainCard *domaincard.Card
	CardFacts  []domaincard.Fact
	// Semantics is the plan's question-library sense table (keywords, sense,
	// exclusions). It disambiguates local slang without becoming an answer.
	Semantics []SemanticEntry
	// Recent is the last few viewer/answer turns. It resolves pronouns and
	// follow-ups; it is never used as fact evidence.
	Recent []RecentTurn
}

type RecentTurn struct {
	Question string `json:"question"`
	Answer   string `json:"answer"`
}

type SemanticEntry struct {
	Keywords []string `json:"keywords"`
	Sense    string   `json:"sense,omitempty"`
	Exclude  []string `json:"exclude,omitempty"`
}

func Normalize(text string) string {
	var out strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(text)) {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			out.WriteRune(r)
		}
	}
	return out.String()
}
func SourceID(input Input) string {
	cardHash := ""
	if input.DomainCard != nil {
		cardHash = input.DomainCard.SourceSHA256
	}
	return contentstrategy.SHA(input.Facts + input.Prompt + cardHash)[:24] + ":" + contentstrategy.SHA(Normalize(input.Text))
}

func preciseFamily(text string) string {
	for _, family := range []struct {
		name  string
		parts []string
	}{
		{"运费", []string{"邮费", "运费", "包邮"}}, {"快递公司", []string{"什么快递", "哪个快递", "哪家快递", "快递公司", "发哪个快递"}},
		{"发货地", []string{"哪里发", "哪儿发", "哪发货", "发货地", "从哪发"}}, {"时效", []string{"几天", "多久", "好久", "什么时候发", "什么时候到"}},
		{"生熟", []string{"生油", "熟油", "生的还是熟", "熟的还是生"}}, {"优惠", []string{"优惠", "券", "折扣"}}, {"价值", []string{"贵不贵", "太贵", "贵吗", "划算"}},
		{"价格", []string{"多少钱", "多钱", "价格", "价钱", "几多钱"}},
	} {
		for _, part := range family.parts {
			if strings.Contains(text, part) {
				return family.name
			}
		}
	}
	return ""
}

func (r *Resolver) Resolve(ctx context.Context, input Input) (Result, error) {
	if input.Tenant <= 0 || input.Room <= 0 || input.Plan <= 0 || Normalize(input.Text) == "" {
		return Result{}, fmt.Errorf("目的解析作用域或原话缺失")
	}
	id := SourceID(input)
	factsSHA := contentstrategy.SHA(input.Facts)
	// Semantic reuse is intentionally disabled: every viewer question gets a
	// fresh intent call. Region/person/link context must never be reused from a
	// different question.
	// Intent mining gets the full atomic facts plus the plan's sense table. The
	// answer stage gets only the facts selected for this intent.
	payloadMap := map[string]any{"观众原话": input.Text, "已有目的": input.Purposes, "全量事实": input.Facts}
	if len(input.Semantics) > 0 {
		payloadMap["语义库"] = input.Semantics
	}
	if len(input.Recent) > 0 {
		payloadMap["最近对话"] = input.Recent
	}
	if input.DomainCard != nil {
		dimensions := make([]map[string]any, 0, len(input.DomainCard.Dimensions))
		for _, dimension := range input.DomainCard.Dimensions {
			dimensions = append(dimensions, map[string]any{
				"名称": dimension.Name, "问法": dimension.Phrases, "条件": dimension.Conditions,
			})
		}
		payloadMap["领域卡"] = map[string]any{"行业": input.DomainCard.Domain, "维度": dimensions}
	}
	if len(input.CardFacts) > 0 {
		lines := make([]string, 0, len(input.CardFacts))
		for _, fact := range input.CardFacts {
			lines = append(lines, fact.ID+" "+strings.TrimSpace(fact.Text))
		}
		payloadMap["本问题命中事实"] = strings.Join(lines, "\n")
	}
	payload, _ := json.Marshal(payloadMap)
	raw, err := r.Complete(ctx, string(payload))
	if err != nil {
		return Result{}, err
	}
	var result Result
	var fields map[string]json.RawMessage
	if err = contentstrategy.Decode(raw, &fields); err != nil {
		return Result{}, err
	}
	allowed := map[string]bool{"question": true, "relevant": true, "confidence": true, "purpose_id": true, "purpose": true, "scope": true, "task_angles": true, "ambiguous": true, "clarify": true, "options": true, "constraints": true, "intent_type": true, "need": true, "product_relevance": true}
	for _, name := range []string{"question", "relevant", "confidence", "purpose_id", "purpose", "scope", "task_angles"} {
		if _, ok := fields[name]; !ok {
			return Result{}, fmt.Errorf("目的返回字段缺失：%s", name)
		}
	}
	for name, value := range fields {
		if !allowed[name] || strings.TrimSpace(string(value)) == "null" {
			return Result{}, fmt.Errorf("目的返回字段无效：%s", name)
		}
	}
	result, decodeErr := decodeIntentResult(raw, input.Facts)
	if decodeErr != nil {
		return Result{}, fmt.Errorf("目的返回协议无效：%w", decodeErr)
	}
	if result.Ambiguous {
		if strings.TrimSpace(result.Clarify) == "" || len(result.Options) < 2 || len(result.Options) > 3 {
			return Result{}, fmt.Errorf("澄清问题缺少选项或澄清话术")
		}
		result.FactsSHA, result.Input, result.Source = factsSHA, input.Text, "model"
		result.Topic = "CLARIFY:" + contentstrategy.SHA(input.Text)[:16]
		return result, nil
	}
	if result.Confidence < 0 || result.Confidence > 1 || len([]rune(result.Purpose)) > 80 || len([]rune(result.Scope)) > 120 || len(result.Angles) > 3 {
		return Result{}, fmt.Errorf("目的返回字段越界")
	}
	known := purposeKnown(input.Purposes, result.PurposeID)
	uncertain := result.Purpose == "" || result.Confidence < .75 || (result.PurposeID != "" && !known)
	if result.Question && result.Relevant && !known && uncertain {
		// One repair attempt with an explicit requirement. A low-confidence guess
		// must not throw the viewer's question away.
		if retryRaw, retryErr := r.Complete(ctx, string(payload)+"\n\n"+purposeRetryInstruction); retryErr == nil {
			if again, againErr := decodeIntentResult(retryRaw, input.Facts); againErr == nil &&
				again.Confidence >= 0 && again.Confidence <= 1 && len([]rune(again.Purpose)) <= 80 && len(again.Angles) <= 3 {
				result = again
				known = purposeKnown(input.Purposes, result.PurposeID)
				uncertain = result.Purpose == "" || result.Confidence < .75 || (result.PurposeID != "" && !known)
			}
		}
	}
	if result.Question && result.Relevant && !known && uncertain {
		// Still unsure: keep it a question, but answer from the boundary instead
		// of guessing a purpose or trusting an invented id.
		result.Relevant = false
		result.PurposeID = ""
		result.Purpose = ""
		result.Angles = nil
	}
	if result.PurposeID != "" && !purposeKnown(input.Purposes, result.PurposeID) {
		result.PurposeID = ""
	}
	if known && strings.TrimSpace(result.Purpose) == "" {
		for _, purpose := range input.Purposes {
			if purpose.ID == result.PurposeID {
				result.Purpose = purpose.Name
			}
		}
	}
	// relevant=false（超出事实包范围/与当前直播无关）时，目的编号没有意义：
	// 不该有 purpose_id，也不该触发 L 兜底生成假 ID。直接清空，让下游走「缺事实兜底」。
	if !result.Relevant {
		result.PurposeID = ""
		result.Purpose = ""
		result.Angles = nil
	}
	if result.PurposeID == "" && result.Purpose != "" {
		result.PurposeID = "L" + contentstrategy.SHA(result.Purpose)[:12]
	}
	for i := range result.Angles {
		result.Angles[i].PurposeID = result.PurposeID
	}
	result.Angles = assignAngleFacts(input, result)
	if len(result.Angles) > 0 {
		// 解意图的 task_angles 是「轻量角度」，模型只输出「角度/讲述任务/关联事实」，
		// 不输出快照专属的「优先级/任务类型/适用条件」。这里只校验解意图真正要求的：
		//   1) 关联事实必须引用已存在的事实编号（防模型编造）
		//   2) 角度、讲述任务非空且不超长
		//   3) 采用条件（若给出）合法
		// 因此用 ValidateRowPolicy(required=false)（跳过优先级/任务类型/采用条件的非空要求），
		// 并单独校验关联事实引用，不再套用快照级 Validate（它强制适用条件/优先级等）。
		knownFacts := map[string]bool{}
		for _, f := range contentstrategy.Facts(input.Facts) {
			knownFacts[f.ID] = true
		}
		// Angle rows are advisory: drop the unusable ones instead of failing the
		// whole question, which would silently lose the viewer's intent.
		result.Angles = pruneAngles(result.Angles, knownFacts)
		for _, row := range result.Angles {
			if err = contentstrategy.ValidateRowPolicy(row, false); err != nil {
				return Result{}, err
			}
			if err = contentstrategy.ValidateFactRefs(row.FactIDs, knownFacts); err != nil {
				return Result{}, err
			}
			if len([]rune(strings.TrimSpace(row.Angle))) == 0 || len([]rune(row.Angle)) > 60 ||
				len([]rune(strings.TrimSpace(row.Task))) == 0 || len([]rune(row.Task)) > 240 {
				return Result{}, fmt.Errorf("解意图角度字段缺失或过长")
			}
		}
	}
	result.FactsSHA, result.Input, result.Source = factsSHA, input.Text, "model"
	result.Topic = "PURPOSE:" + contentstrategy.SHA(result.PurposeID + "/" + preciseFamily(input.Text) + "/" + result.Scope)[:24]
	if result.Relevant && result.Purpose != "" && result.Confidence >= .85 {
		if err = r.save(ctx, input, id, result); err != nil {
			return Result{}, fmt.Errorf("保存目的知识失败：%w", err)
		}
		r.storeBucket(ctx, input, result)
	}
	return result, nil
}

// assignAngleFacts fills each angle's fact references on the program side: the
// configured purpose's facts first, then a retrieval pass over the fact package.
// The model is no longer asked for fact ids, so a wrong or invented id cannot
// leak in.
func assignAngleFacts(input Input, result Result) []contentstrategy.Row {
	known := map[string]bool{}
	facts := contentstrategy.Facts(input.Facts)
	all := make([]factselect.Fact, 0, len(facts))
	for _, fact := range facts {
		known[fact.ID] = true
		all = append(all, factselect.Fact{ID: fact.ID, Text: fact.Text})
	}
	cardIDs := make([]string, 0, len(input.CardFacts))
	for _, fact := range input.CardFacts {
		if known[fact.ID] {
			cardIDs = append(cardIDs, fact.ID)
		}
	}
	if input.DomainCard != nil {
		domainFacts := make([]domaincard.Fact, 0, len(facts))
		for _, fact := range facts {
			domainFacts = append(domainFacts, domaincard.Fact{ID: fact.ID, Text: fact.Text})
		}
		context := input.Text + " " + result.Purpose
		for _, angle := range result.Angles {
			context += " " + angle.Angle + " " + angle.Task
		}
		if scope := domaincard.ResolveScope(context, input.DomainCard, domainFacts); len(scope.Facts) > 0 {
			cardIDs = cardIDs[:0]
			for _, fact := range scope.Facts {
				if known[fact.ID] {
					cardIDs = append(cardIDs, fact.ID)
				}
			}
		}
	}
	var purpose *contentstrategy.Purpose
	for i := range input.Purposes {
		if input.Purposes[i].ID == result.PurposeID {
			purpose = &input.Purposes[i]
			break
		}
	}
	filterKnown := func(ids []string) []string {
		out := make([]string, 0, len(ids))
		for _, id := range ids {
			if known[strings.TrimSpace(id)] {
				out = append(out, strings.TrimSpace(id))
			}
		}
		return out
	}
	if len(result.Angles) == 0 && len(cardIDs) > 0 {
		result.Angles = []contentstrategy.Row{{
			PurposeID: result.PurposeID,
			Angle:     "按命中维度回答",
			Task:      input.Text,
			FactIDs:   append([]string(nil), cardIDs...),
		}}
	}
	if len(result.Angles) == 0 && input.Text != "" {
		// The model may return no angle at all (a vague "在哪里"). Do not give up:
		// retrieve by the viewer's words plus the purpose it named, then let the
		// fact boundary decide.
		query := strings.TrimSpace(input.Text + " " + result.Purpose)
		picked := factselect.Select(query, nil, nil, all, 3)
		if len(picked) > 0 {
			row := contentstrategy.Row{
				PurposeID: result.PurposeID,
				Angle:     "按观众原话回答",
				Task:      input.Text,
			}
			for _, fact := range picked {
				row.FactIDs = append(row.FactIDs, fact.ID)
			}
			result.Angles = []contentstrategy.Row{row}
		}
	}
	if len(result.Angles) == 0 && purpose != nil && len(purpose.FactIDs) > 0 {
		task := strings.TrimSpace(purpose.Concern)
		if task == "" {
			task = "按目的回答观众关心的问题"
		}
		result.Angles = []contentstrategy.Row{{
			PurposeID: result.PurposeID,
			Angle:     purpose.Name,
			Task:      task,
			FactIDs:   filterKnown(purpose.FactIDs),
		}}
	}
	kept := make([]contentstrategy.Row, 0, len(result.Angles))
	for _, row := range result.Angles {
		base := make([]string, 0, len(row.FactIDs)+len(cardIDs)+8)
		base = append(base, row.FactIDs...)
		base = append(base, cardIDs...)
		if len(base) == 0 && purpose != nil {
			base = append(base, filterKnown(purpose.FactIDs)...)
		}
		// Always add retrieval candidates: a purpose's configured facts may be
		// partial (e.g. shipping policy without the 包邮 benefit).
		angleTexts := []string{row.Angle, row.Task, result.Purpose}
		for _, fact := range factselect.Select(input.Text, angleTexts, nil, all, 5) {
			base = append(base, fact.ID)
		}
		candidates := make([]factselect.Fact, 0, len(base))
		seen := map[string]bool{}
		for _, id := range base {
			if !known[id] || seen[id] {
				continue
			}
			seen[id] = true
			for _, fact := range all {
				if fact.ID == id {
					candidates = append(candidates, fact)
					break
				}
			}
		}
		picked := factselect.Select(input.Text, angleTexts, nil, candidates, 3)
		row.FactIDs = row.FactIDs[:0]
		for _, fact := range picked {
			row.FactIDs = append(row.FactIDs, fact.ID)
		}
		if len(row.FactIDs) == 0 {
			continue
		}
		kept = append(kept, row)
	}
	return kept
}

func (r *Resolver) save(ctx context.Context, input Input, id string, result Result) error {
	raw, _ := json.Marshal(result)
	doc := semantic.Document{TenantID: input.Tenant, RoomID: input.Room, PlanID: input.Plan, ContentType: ContentType, SourceID: id, SourceVersion: 1, Status: "active", Text: string(raw)}
	if r.Semantic != nil && r.Semantic.Enabled() {
		_, err := r.Semantic.Resolve(ctx, []semantic.Document{doc})
		return err
	}
	return r.Repo.UpsertSemanticDocument(ctx, semantic.StoredDocument{Document: doc, TextHash: semantic.HashText(doc.Text), Model: "purpose-exact-v1"})
}

// judgeReuse asks the judge model whether input.Text belongs to one of the
// room's existing purpose buckets. On a confident hit it reuses the stored
// conclusion; on a miss it returns ok=false so the caller falls through to the
// full resolution model.
func (r *Resolver) judgeReuse(ctx context.Context, input Input, factsSHA string) (Result, bool) {
	if !r.BucketEnabled {
		return Result{}, false
	}
	buckets, err := r.Repo.ListPurposeBuckets(ctx, input.Tenant, input.Room, input.Plan, 20)
	if err != nil || len(buckets) == 0 {
		return Result{}, false
	}
	payload, _ := json.Marshal(map[string]any{
		"新问题":     input.Text,
		"历史已分析问题": bucketQuestions(buckets),
	})
	raw, err := r.Judge(ctx, string(payload))
	if err != nil {
		return Result{}, false
	}
	var verdict struct {
		Same       bool `json:"same"`
		MatchIndex *int `json:"match_index"`
	}
	if contentstrategy.Decode(raw, &verdict) != nil || !verdict.Same {
		return Result{}, false
	}
	// same=true 但缺 match_index 时 MatchIndex 为 nil，绝不能默认命中第 0 桶。
	if verdict.MatchIndex == nil || *verdict.MatchIndex < 0 || *verdict.MatchIndex >= len(buckets) {
		return Result{}, false
	}
	hit := buckets[*verdict.MatchIndex]
	conclusion, err := r.Repo.GetPurposeBucketConclusion(ctx, input.Tenant, input.Room, input.Plan, hit.SourceID)
	if err != nil || conclusion == "" {
		return Result{}, false
	}
	var cached Result
	if json.Unmarshal([]byte(conclusion), &cached) != nil {
		return Result{}, false
	}
	if cached.FactsSHA != factsSHA {
		// 事实包变了，旧结论可能引用已失效的事实，不冒险复用。
		return Result{}, false
	}
	cached.Input = input.Text
	cached.Source = "bucket"
	return cached, true
}

func bucketQuestions(buckets []semantic.PurposeBucket) []string {
	out := make([]string, 0, len(buckets))
	for _, b := range buckets {
		if b.Question != "" {
			out = append(out, b.Question)
		}
	}
	return out
}

// storeBucket records the current question + conclusion as a semantic bucket for
// future judge-model reuse. Called after a successful resolution.
func (r *Resolver) storeBucket(ctx context.Context, input Input, result Result) {
	if !r.BucketEnabled {
		return
	}
	if result.Confidence < .85 {
		return
	}
	if result.Source != "model" {
		return
	}
	conclusion, _ := json.Marshal(result)
	sourceID := "bucket:" + contentstrategy.SHA(Normalize(input.Text))
	if err := r.Repo.UpsertPurposeBucket(ctx, input.Tenant, input.Room, input.Plan, sourceID, input.Text, string(conclusion)); err != nil {
		// A failed write means the bucket is never reused (every repeat re-runs
		// full resolution). Log it so silent write failures don't mask the issue.
		log.Printf("purpose bucket upsert failed room=%d sourceID=%s: %v", input.Room, sourceID, err)
	}
}
