package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/contentpools"
)

// These fields belong to storage/UI, never to the model's response contract.
type poolContent struct {
	Point    string `json:"selling_point"`
	Angle    string `json:"angle"`
	Audience string `json:"target_audience,omitempty"`
	Pain     string `json:"pain_point,omitempty"`
}

func taskContent(t contentpools.Task) poolContent {
	return poolContent{t.Point, t.Angle, t.TargetAudience, t.PainPoint}
}
func poolMaterial(p contentpools.Pool, products []contentpools.Product) map[string]any {
	facts := []string{}
	for _, f := range p.Facts {
		facts = append(facts, f.Text)
	}
	items := []map[string]string{}
	for _, product := range products {
		if contentpools.Contains(p.Products, product.ID) {
			items = append(items, map[string]string{"link": product.Link, "name": product.Name})
		}
	}
	return map[string]any{"facts": facts, "products": items, "common": p.Owner == 0}
}
func poolNotice(p contentpools.Pool, content, kind, reason string, retry bool) contentpools.Notice {
	return contentpools.Notice{Pool: p.Label, Content: content, Kind: kind, Reason: reason, Retryable: retry}
}
func poolMessages(mother, p0 string, packet map[string]any) []agentgateway.Message {
	data, _ := json.Marshal(packet)
	return []agentgateway.Message{{Role: "system", Content: p0 + "\n\n" + mother}, {Role: "user", Content: "本次实际输入 JSON：\n" + string(data)}}
}

// One request owns one content unit. Accept prose, Markdown, lists and legacy
// content envelopes. Never read model IDs, scopes or relationships. Unknown
// layouts remain text and receive the same semantic review, not a format veto.
func poolReplyContent(raw string) string {
	text := strings.TrimSpace(stripPolicyJSONFence(raw))
	var value any
	if json.Unmarshal([]byte(text), &value) == nil {
		if content := poolEnvelopeText(value); strings.TrimSpace(content) != "" {
			return strings.TrimSpace(content)
		}
	}
	// A truncated JSON wrapper must not turn otherwise intact content into a
	// format failure. Recover complete string values in program code; identity
	// and relation fields are never content or association authority.
	if strings.HasPrefix(text, "{") || strings.HasPrefix(text, "[") {
		parts := []string{}
		for _, match := range poolStringFields.FindAllStringSubmatch(text, -1) {
			key, e := strconv.Unquote(match[1])
			if e != nil || poolMetadataKey(key) {
				continue
			}
			value, e := strconv.Unquote(match[2])
			if e == nil && strings.TrimSpace(value) != "" {
				parts = append(parts, value)
			}
		}
		if len(parts) > 0 {
			return strings.Join(parts, "\n")
		}
	}
	return text
}

var poolStringFields = regexp.MustCompile(`("(?:[^"\\]|\\.)*")\s*:\s*("(?:[^"\\]|\\.)*")`)

func poolMetadataKey(key string) bool {
	key = strings.ToLower(key)
	return key == "id" || key == "index" || key == "status" || key == "passed" || strings.HasSuffix(key, "_id") || strings.HasSuffix(key, "_ids")
}
func poolEnvelopeText(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case []any:
		parts := []string{}
		for _, item := range v {
			if text := poolEnvelopeText(item); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "\n")
	case map[string]any:
		for _, key := range []string{"text", "angle", "正文", "讲述内容", "内容", "content", "tasks", "任务"} {
			if text := poolEnvelopeText(v[key]); text != "" {
				return text
			}
		}
		keys := []string{}
		for key := range v {
			if !poolMetadataKey(key) {
				keys = append(keys, key)
			}
		}
		sort.Strings(keys)
		parts := []string{}
		for _, key := range keys {
			if text := poolEnvelopeText(v[key]); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
}

type poolJudgment struct {
	known, passed bool
	reason        string
}

// Parse a semantic conclusion, not an output schema. An explicit conflicting
// or uncertain conclusion cannot become approval. Unclear content gets one
// separate content judgment; no model is asked to fix fields or fill a table.
func readPoolJudgment(raw string, scope bool) poolJudgment {
	text := strings.TrimSpace(stripPolicyJSONFence(raw))
	var legacy struct {
		Passed *bool  `json:"passed"`
		Reason string `json:"reason"`
	}
	if json.Unmarshal([]byte(text), &legacy) == nil && legacy.Passed != nil {
		if !*legacy.Passed && strings.TrimSpace(legacy.Reason) == "" {
			legacy.Reason = "模型判断这条内容有问题，暂不入池"
		}
		return poolJudgment{true, *legacy.Passed, strings.TrimSpace(legacy.Reason)}
	}
	text = strings.ToLower(strings.NewReplacer("**", "", "__", "", "`", "", "#", "", "\r", "").Replace(text))
	positive := []string{"通过", "内容正确", "没有发现问题", "未发现问题", "没有问题", "可以保留", "可以入池", "符合事实", "可以通过", "合格", "pass", "approved"}
	negative := []string{"不通过", "不能通过", "未通过", "有问题", "存在问题", "内容不正确", "内容错误", "不合格", "不能保留", "不能入池", "不符合", "不完全符合", "不应通过", "不宜通过", "not approved", "not correct", "not pass", "reject", "fail"}
	uncertain := []string{"不确定", "不明确", "不一定", "是否", "能否", "？", "?", "无法判断", "不能确定", "无法确定", "尚不能判断", "需要更多", "待确认", "uncertain"}
	if scope {
		positive = []string{"适用", "可以用于", "可以关联", "可以作为", "可用于", "applicable"}
		negative = []string{"不适用", "不能适用", "不能用于", "不可用于", "不能关联", "不应关联", "不适合", "not applicable", "inapplicable"}
	}
	// Only the conclusion portion is interpreted; quoted examples/reasons do
	// not change it. Permit common natural preambles and list/Markdown forms.
	for _, line := range strings.Split(text, "\n") {
		// A heading or explanatory preamble is harmless. Look for the first
		// actual conclusion rather than requiring it at a fixed line/field.
		first := strings.TrimSpace(line)
		if strings.HasPrefix(first, ">") {
			continue
		}
		for _, separator := range []string{"。", "；", ";"} {
			first = strings.SplitN(first, separator, 2)[0]
		}
		for _, phrase := range uncertain {
			if poolJudgmentContains(first, phrase) {
				return poolJudgment{reason: strings.TrimSpace(raw)}
			}
		}
		// Read natural negation as meaning, e.g. "没有问题" is approval rather
		// than a substring hit on "有问题". No fixed reply layout is required.
		first = strings.NewReplacer("并非没有问题", "有问题", "没有问题", "可以保留", "不存在问题", "可以保留", "没有发现问题", "可以保留", "未发现问题", "可以保留", "没有内容错误", "可以保留", "不存在内容错误", "可以保留").Replace(first)
		for _, phrase := range negative {
			if poolJudgmentContains(first, phrase) {
				return poolJudgment{true, false, strings.TrimSpace(raw)}
			}
		}
		for _, phrase := range positive {
			if poolJudgmentContains(first, phrase) {
				return poolJudgment{true, true, strings.TrimSpace(raw)}
			}
		}
	}
	return poolJudgment{reason: strings.TrimSpace(raw)}
}
func poolJudgmentContains(text, phrase string) bool {
	if phrase[0] >= 'a' && phrase[0] <= 'z' {
		matched, _ := regexp.MatchString(`\b`+regexp.QuoteMeta(phrase)+`\b`, text)
		return matched
	}
	return strings.Contains(text, phrase)
}
func (s *Server) judgePoolContent(ctx context.Context, profile, stage, mother, p0 string, packet map[string]any, scope bool) (poolJudgment, error) {
	var judgment poolJudgment
	for attempt := 0; attempt < 2; attempt++ {
		call, stop := context.WithTimeout(ctx, 30*time.Second)
		response, _, err := s.completeAnchorStyleWorkbench(call, profile, stage, agentgateway.ResponseText, poolMessages(mother, p0, packet), 1200, false)
		stop()
		if err != nil {
			return judgment, err
		}
		judgment = readPoolJudgment(response.Text, scope)
		if judgment.known {
			return judgment, nil
		}
		// Re-evaluate the original candidate, not the previous ambiguous reply.
		packet["clarify_conclusion"] = true
	}
	return judgment, errors.New("模型尚未明确判断这条内容，已留在池外；其他内容继续")
}
func bindPoolContent(p contentpools.Pool, item poolContent, previous []contentpools.Task) contentpools.Task {
	refs := []string{}
	for _, f := range p.Facts {
		refs = append(refs, f.ID)
	}
	t := contentpools.Task{ID: "task-" + contentpools.Digest(struct {
		Pool    string
		Content poolContent
	}{p.Key, item})[:32], Point: item.Point, Angle: item.Angle, TargetAudience: item.Audience, PainPoint: item.Pain, Sources: refs}
	for _, old := range previous {
		if taskContent(old) == item {
			t.ID = old.ID
			t.Disabled = old.Disabled
			break
		}
	}
	return t
}
func (s *Server) reviewSimplePoolContent(ctx context.Context, profile, mother, p0 string, p contentpools.Pool, products []contentpools.Product, corrections []map[string]any, item poolContent) (poolContent, string, error) {
	packet := poolMaterial(p, products)
	packet["candidate"] = item.Angle
	packet["content_purpose"], packet["audience"], packet["concern"] = item.Point, item.Audience, item.Pain
	packet["user_confirmed_corrections"] = corrections
	judgment, err := s.judgePoolContent(ctx, profile, "content_pool_task_review", mother, p0, packet, false)
	if err != nil {
		return item, "", err
	}
	if !judgment.passed {
		return item, judgment.reason, nil
	}
	// A verdict never silently rewrites the candidate it approves.
	return item, "", nil
}

type poolUnit struct {
	title string
	facts []contentpools.Fact
}

func poolContentUnits(p contentpools.Pool, source contentpools.Sources) []poolUnit {
	rows := map[string]contentpools.SourceRow{}
	for _, row := range source.Rows {
		rows[row.ID] = row
	}
	units := []poolUnit{}
	positions := map[string]int{}
	for _, fact := range p.Facts {
		// Group by program-owned provenance, never by a model's title/index.
		key := contentpools.Digest(fact.Sources)
		if len(fact.Sources) == 0 {
			key = fact.ID
		}
		if index, ok := positions[key]; ok {
			units[index].facts = append(units[index].facts, fact)
			continue
		}
		title := "资料讲解"
		if len(fact.Sources) > 0 {
			row := rows[fact.Sources[0]]
			for _, field := range []string{"名称", "属性"} {
				if row.Fields[field] != "" {
					title = row.Fields[field]
					break
				}
			}
			if title == "资料讲解" && row.Label != "" {
				title = row.Label
			}
		}
		positions[key] = len(units)
		units = append(units, poolUnit{title, []contentpools.Fact{fact}})
	}
	return units
}
func (s *Server) generateSimplePool(ctx context.Context, profile, gen, review, p0 string, p *contentpools.Pool, previous []contentpools.Task, source contentpools.Sources, beforeCall func() error) ([]contentpools.Notice, error) {
	notes := []contentpools.Notice{}
	p.Tasks = []contentpools.Task{}
	seen := map[string]bool{}
	corrections := contentpools.CorrectionView(source, p.Products)
	check := func(draft poolContent, boundary contentpools.Pool) error {
		if err := beforeCall(); err != nil {
			return err
		}
		checked, reason, err := s.reviewSimplePoolContent(ctx, profile, review, p0, *p, source.Products, corrections, draft)
		if err != nil {
			notes = append(notes, poolNotice(*p, draft.Angle, "unavailable", "本条内容核对尚未完成，留在池外；其他内容继续，可稍后重试", true))
			return nil
		}
		if reason != "" {
			notes = append(notes, poolNotice(*p, draft.Angle, "excluded", reason, false))
			return nil
		}
		t := bindPoolContent(boundary, checked, previous)
		if !seen[t.ID] {
			p.Tasks = append(p.Tasks, t)
			seen[t.ID] = true
		}
		return nil
	}
	// Preserve existing edits with the same current semantic review. Their
	// provenance remains the old explicit subset where still available.
	for _, old := range previous {
		if old.Disabled {
			continue
		}
		if strings.TrimSpace(old.Angle) == "" {
			notes = append(notes, poolNotice(*p, old.Point, "excluded", "旧条目没有讲述内容，未入池；其他内容继续", false))
			continue
		}
		boundary := *p
		boundary.Facts = nil
		for _, f := range p.Facts {
			for _, ref := range old.Sources {
				if ref == f.ID {
					boundary.Facts = append(boundary.Facts, f)
					break
				}
			}
		}
		// When material changed, the current reviewed request is the new boundary.
		if len(boundary.Facts) == 0 {
			boundary.Facts = p.Facts
		}
		if err := check(taskContent(old), boundary); err != nil {
			return notes, err
		}
	}
	for _, unit := range poolContentUnits(*p, source) {
		if err := beforeCall(); err != nil {
			return notes, err
		}
		packet := poolMaterial(*p, source.Products)
		selected := []string{}
		for _, f := range unit.facts {
			selected = append(selected, f.Text)
		}
		packet["selected_material"], packet["user_confirmed_corrections"] = selected, corrections
		call, stop := context.WithTimeout(ctx, 30*time.Second)
		response, _, err := s.completeAnchorStyleWorkbench(call, profile, "content_pool_task_generation", agentgateway.ResponseText, poolMessages(gen, p0, packet), 1800, false)
		stop()
		if err != nil {
			notes = append(notes, poolNotice(*p, strings.Join(selected, "\n"), "unavailable", "本条内容生成暂未完成；其他资料继续，可稍后重试", true))
			continue
		}
		text := poolReplyContent(response.Text)
		if text == "" {
			notes = append(notes, poolNotice(*p, strings.Join(selected, "\n"), "unavailable", "本条没有返回讲述内容，其他资料继续", true))
			continue
		}
		// Request context owns title, source IDs and association. No format gate.
		// The full pool is the review context, including cross-field conditions.
		if err := check(poolContent{Point: unit.title, Angle: text}, *p); err != nil {
			return notes, err
		}
	}
	if len(p.Tasks) == 0 {
		notes = append(notes, poolNotice(*p, "", "empty", "这个池暂无审核通过内容，其他池可继续核对与保存", false))
	}
	return notes, ctx.Err()
}

// One request owns one known fact/product pair. Model answers only the content
// question; it never selects/echoes a list to maintain program relationships.
func (s *Server) classifySimplePoolFacts(ctx context.Context, profile string, source contentpools.Sources, facts []contentpools.Fact, p0 string, progress ...func(int, int)) ([]contentpools.Fact, []contentpools.Notice, error) {
	original := facts
	facts = mergeEqualProductFacts(facts)
	mother, _, err := loadReplaceablePromptAsset("CONTENT_POOL_SIMPLE_SCOPE_PATH", "configs/prompts/anchor-style/content-pool-simple-scope.md", "内容池单资料范围母版")
	if err != nil {
		return nil, nil, err
	}
	type scopeUnit struct {
		fact       int
		product    contentpools.Product
		attributes []string
	}
	units := []scopeUnit{}
	for i, f := range facts {
		if f.Owner != 0 || f.Private {
			continue
		}
		facts[i].Products = nil
		for _, product := range source.Products {
			attributes := []string{}
			for _, other := range original {
				if other.Owner == product.ID {
					attributes = append(attributes, other.Text)
				}
			}
			units = append(units, scopeUnit{i, product, attributes})
		}
	}
	type scopeResult struct {
		judgment poolJudgment
		err      error
	}
	results := make([]scopeResult, len(units))
	jobs := make(chan int)
	var wg sync.WaitGroup
	var mu sync.Mutex
	done := 0
	for worker := 0; worker < 6; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for index := range jobs {
				unit := units[index]
				packet := map[string]any{"product": map[string]string{"link": unit.product.Link, "name": unit.product.Name}, "product_attributes": unit.attributes, "candidate": facts[unit.fact].Text, "user_confirmed_corrections": contentpools.CorrectionView(source, []int64{unit.product.ID})}
				results[index].judgment, results[index].err = s.judgePoolContent(ctx, profile, "content_pool_fact_scope", mother, p0, packet, true)
				mu.Lock()
				done++
				if len(progress) > 0 && progress[0] != nil {
					progress[0](done, len(units))
				}
				mu.Unlock()
			}
		}()
	}
enqueue:
	for i := range units {
		select {
		case jobs <- i:
		case <-ctx.Done():
			break enqueue
		}
	}
	close(jobs)
	wg.Wait()
	if ctx.Err() != nil {
		return facts, nil, ctx.Err()
	}
	notes := []contentpools.Notice{}
	for i, unit := range units {
		result := results[i]
		label := unit.product.Link + "号 · " + unit.product.Name
		if result.err != nil || !result.judgment.known {
			notes = append(notes, contentpools.Notice{Pool: label, Content: facts[unit.fact].Text, Kind: "unavailable", Reason: "本条资料适用范围尚未明确，暂不关联；其他资料继续", Retryable: true})
			continue
		}
		if result.judgment.passed {
			facts[unit.fact].Products = append(facts[unit.fact].Products, unit.product.ID)
		} else {
			notes = append(notes, contentpools.Notice{Pool: label, Content: facts[unit.fact].Text, Kind: "excluded", Reason: result.judgment.reason})
		}
	}
	return facts, notes, nil
}
