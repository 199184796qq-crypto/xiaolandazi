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
	"unicode"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/contentpools"
)

// These fields belong to storage/UI, never to the model's response contract.
type poolContent struct {
	Point    string `json:"selling_point"`
	Angle    string `json:"angle"`
	Audience string `json:"target_audience,omitempty"`
	Pain     string `json:"pain_point,omitempty"`
	Weight   int    `json:"weight,omitempty"`
}

func taskContent(t contentpools.Task) poolContent {
	return poolContent{Point: t.Point, Angle: t.Angle, Audience: t.TargetAudience, Pain: t.PainPoint, Weight: t.Weight}
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
	weight        int
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
		return poolJudgment{known: true, passed: *legacy.Passed, reason: strings.TrimSpace(legacy.Reason)}
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
				return poolJudgment{known: true, passed: false, reason: strings.TrimSpace(raw)}
			}
		}
		for _, phrase := range positive {
			if poolJudgmentContains(first, phrase) {
				return poolJudgment{known: true, passed: true, reason: strings.TrimSpace(raw)}
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

var poolWeightPattern = regexp.MustCompile(`(?i)(?:权重|weight)\s*[:：]?\s*([1-5])\b`)

// poolWeight 从审核模型的自然语言结论里提取讲解权重（1-5）。未给出或越界返回 0，
// 由下游缺省为 1。
func poolWeight(raw string) int {
	if m := poolWeightPattern.FindStringSubmatch(raw); m != nil {
		if w, err := strconv.Atoi(m[1]); err == nil && w >= 1 && w <= 5 {
			return w
		}
	}
	return 0
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
		if judgment.weight == 0 {
			judgment.weight = poolWeight(response.Text)
		}
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
	}{p.Key, item})[:32], Point: item.Point, Angle: item.Angle, TargetAudience: item.Audience, PainPoint: item.Pain, Weight: item.Weight, Sources: refs}
	for _, old := range previous {
		if taskContent(old) == item {
			t.ID = old.ID
			t.Disabled = old.Disabled
			break
		}
	}
	return t
}

// normalizePoolAngle 去掉标点、空白与大小写差异，只保留字符，用于角度去重的 2-gram 比较。
func normalizePoolAngle(text string) string {
	var b strings.Builder
	for _, r := range text {
		if unicode.IsSpace(r) || strings.ContainsRune("，。！？、；：,.!?;:'\"“”‘’（）()《》【】…—～-", r) {
			continue
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}

// poolAngleNGrams 提取文本的 2-gram 字符集合。
func poolAngleNGrams(text string) map[string]bool {
	runes := []rune(text)
	result := make(map[string]bool, len(runes))
	for i := 0; i+2 <= len(runes); i++ {
		result[string(runes[i:i+2])] = true
	}
	return result
}

// poolAngleOverlapRate 计算两个角度文本的 2-gram 重叠率（0~1，取较短的 gram 数为分母）。
func poolAngleOverlapRate(a, b string) float64 {
	a = normalizePoolAngle(a)
	b = normalizePoolAngle(b)
	if a == "" || b == "" {
		return 0
	}
	gramsA := poolAngleNGrams(a)
	gramsB := poolAngleNGrams(b)
	if len(gramsA) == 0 || len(gramsB) == 0 {
		return 0
	}
	overlap := 0
	for g := range gramsA {
		if gramsB[g] {
			overlap++
		}
	}
	denom := len(gramsA)
	if len(gramsB) < denom {
		denom = len(gramsB)
	}
	return float64(overlap) / float64(denom)
}

// poolAngleDuplicated 判断新角度是否与池内已有角度高度重叠（同一主题被拆成多个相似角度）。
// 用 2-gram 字符重叠率（去标点空白、忽略大小写），产品无关，换任何商品都适用。
// 返回命中的已有角度文本（用于记录提示），无重复返回空串。
func poolAngleDuplicated(existing []contentpools.Task, angle string) string {
	if strings.TrimSpace(angle) == "" {
		return ""
	}
	for _, t := range existing {
		if poolAngleOverlapRate(t.Angle, angle) >= 0.55 {
			return t.Angle
		}
	}
	return ""
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
	item.Weight = judgment.weight
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
// factsChanged 判断一组 fact 里是否有任一 fact 内容变化（新增/改动/删除）。
// changed 为 nil 时保守返回 true（全部重跑），保证旧调用与测试行为不变。
func factsChanged(facts []contentpools.Fact, changed map[string]bool) bool {
	if changed == nil {
		return true
	}
	for _, f := range facts {
		if changed[f.ID] {
			return true
		}
	}
	return false
}

// sourceIDsChanged 判断 task 引用的 fact ID 里是否有任一变化。
func sourceIDsChanged(ids []string, changed map[string]bool) bool {
	if changed == nil {
		return true
	}
	for _, id := range ids {
		if changed[id] {
			return true
		}
	}
	return false
}

func (s *Server) generateSimplePool(ctx context.Context, profile, gen, review, p0 string, p *contentpools.Pool, previous []contentpools.Task, source contentpools.Sources, changed map[string]bool, beforeCall func() error) ([]contentpools.Notice, error) {
	notes := []contentpools.Notice{}
	p.Tasks = []contentpools.Task{}
	seen := map[string]bool{}
	corrections := contentpools.CorrectionView(source, p.Products)
	check := func(draft poolContent, boundary contentpools.Pool) error {
		if err := beforeCall(); err != nil {
			return err
		}
		checked, reason, err := s.reviewSimplePoolContent(ctx, profile, review, p0, boundary, source.Products, corrections, draft)
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
			// 角度去重：与池内已有角度高度重叠的不重复入池，避免同一主题被拆成多个相似角度。
			if duplicated := poolAngleDuplicated(p.Tasks, t.Angle); duplicated != "" {
				notes = append(notes, poolNotice(*p, t.Angle, "excluded", "与已有角度内容高度重叠，未重复入池", false))
				return nil
			}
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
		// task 级增量：Sources 未变化的旧 task 直接复用，不再逐条重新核对。
		if !sourceIDsChanged(old.Sources, changed) {
			if !seen[old.ID] {
				p.Tasks = append(p.Tasks, old)
				seen[old.ID] = true
			}
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
		// task 级增量：unit facts 未变化的跳过（旧 task 已在复用循环保留）
		if !factsChanged(unit.facts, changed) {
			continue
		}
		if err := beforeCall(); err != nil {
			return notes, err
		}
		// 【剥离中间层】不再调用 content_pool_task_generation 扩写口播文案。
		// 直接用卖点轮盘角度名作为 angle：正文生成用「角度名 + 侧面筛事实」即可，
		// 原 angle 文案被角度钩子绕过、属冗余生成，剥离后省一次模型调用。
		boundary := *p
		boundary.Facts = p.Facts
		if err := check(poolContent{Point: unit.title, Angle: unit.title}, boundary); err != nil {
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
func (s *Server) classifySimplePoolFacts(ctx context.Context, profile string, source contentpools.Sources, facts []contentpools.Fact, p0 string, prev map[string]contentpools.Fact, progress ...func(int, int)) ([]contentpools.Fact, []contentpools.Notice, error) {
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
	validProducts := map[int64]bool{}
	for _, p := range source.Products {
		validProducts[p.ID] = true
	}
	for i, f := range facts {
		if f.Owner != 0 || f.Private {
			continue
		}
		// 增量：未变化的 fact 复用上次分类的适用范围，不再逐商品核对。
		if old, ok := prev[f.ID]; ok && old.Text == f.Text && len(old.Products) > 0 {
			kept := []int64{}
			for _, pid := range old.Products {
				if validProducts[pid] {
					kept = append(kept, pid)
				}
			}
			if len(kept) > 0 {
				facts[i].Products = contentpools.IDs(kept)
				continue
			}
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
