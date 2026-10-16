package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"sync"
	"time"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/anchorcycle"
	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
)

// productAnalysisView 商品归属分析只看链接/名称/角色；价格与规格仅供程序比对，
// 不进分析输入（避免模型把价格/规格误写进归属，也保持分析指纹与历史一致）。
type productAnalysisView struct {
	LinkKey string   `json:"link_key"`
	Name    string   `json:"name"`
	Roles   []string `json:"roles,omitempty"`
}

func loadCycleProductFocusPrompt() (string, string, error) {
	return loadReplaceablePromptAsset("ANCHOR_CYCLE_PRODUCT_FOCUS_PROMPT_PATH", "configs/prompts/anchor-style/cycle-product-focus.md", "商品范围与回灌母版")
}

// focusSellingPointsFromModel 把商品链接上的卖点池（model 层）转成调度层的 FocusSellingPoint。
func focusSellingPointsFromModel(points []model.LiveAgentPlanProductSellingPoint) []anchorcycle.FocusSellingPoint {
	if len(points) == 0 {
		return nil
	}
	result := make([]anchorcycle.FocusSellingPoint, 0, len(points))
	for _, p := range points {
		if p.Status != "active" || strings.TrimSpace(p.SellingPoint) == "" {
			continue
		}
		angles := make([]anchorcycle.FocusSellingAngle, 0, len(p.Angles))
		for _, a := range p.Angles {
			if strings.TrimSpace(a.Angle) == "" {
				continue
			}
			angles = append(angles, anchorcycle.FocusSellingAngle{Angle: strings.TrimSpace(a.Angle), Weight: a.Weight})
		}
		if len(angles) == 0 {
			continue
		}
		result = append(result, anchorcycle.FocusSellingPoint{
			ID:             p.ID,
			Name:           strings.TrimSpace(p.SellingPoint),
			Tier:           p.Tier,
			Weight:         p.PointWeight,
			IsMain:         p.IsMain,
			TargetAudience: p.TargetAudience,
			PainPoint:      p.PainPoint,
			Angles:         angles,
		})
	}
	return result
}

// hasProductLevelSellingPoints 判断所有商品是否都已配置独立的商品级卖点池。
// 只有全部商品都有卖点池才走商品级展开路径，否则回退旧的模型归属判定。
func hasProductLevelSellingPoints(products []anchorcycle.FocusProduct) bool {
	if len(products) < 2 {
		return false
	}
	for _, p := range products {
		if len(p.SellingPoints) == 0 {
			return false
		}
	}
	return true
}
func loadCycleProductScopePrompt() (string, string, error) {
	return loadReplaceablePromptAsset("ANCHOR_CYCLE_PRODUCT_SCOPE_PROMPT_PATH", "configs/prompts/anchor-style/cycle-product-scope-classification.md", "商品归属分析母版")
}
func loadCycleProductScopeItemPrompt() (string, string, error) {
	return loadReplaceablePromptAsset("ANCHOR_CYCLE_PRODUCT_SCOPE_ITEM_PROMPT_PATH", "configs/prompts/anchor-style/cycle-product-scope-item.md", "单条商品归属判定母版")
}
func loadCycleProductAuditPrompt() (string, string, error) {
	return loadReplaceablePromptAsset("ANCHOR_CYCLE_PRODUCT_AUDIT_PROMPT_PATH", "configs/prompts/anchor-style/cycle-product-focus-audit.md", "商品范围复核母版")
}
func decodeProductJSON(raw string, output any) error {
	d := json.NewDecoder(strings.NewReader(stripPolicyJSONFence(raw)))
	d.DisallowUnknownFields()
	if err := d.Decode(output); err != nil {
		return err
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return errors.New("商品范围返回包含额外数据")
	}
	return nil
}
func loadCycleProductPolicy() (anchorcycle.ProductPolicy, string, error) {
	md, source, err := loadCycleProductFocusPrompt()
	var p anchorcycle.ProductPolicy
	if err != nil {
		return p, source, err
	}
	match := regexp.MustCompile("(?s)```json\\s*(.*?)\\s*```").FindStringSubmatch(md)
	if len(match) != 2 {
		return p, source, errors.New("商品调度母版缺少JSON策略")
	}
	if err = decodeProductJSON(match[1], &p); err != nil {
		return p, source, err
	}
	return p, source, p.Validate()
}

type cycleScopeFactInput struct {
	ID   int    `json:"id"`
	Text string `json:"text"`
}
type cycleScopeTaskInput struct {
	ID           int    `json:"id"`
	SellingPoint string `json:"selling_point"`
	Angle        string `json:"angle"`
}
type cycleScopeResponse struct {
	Facts []struct {
		ID     int      `json:"id"`
		Links  []string `json:"links"`
		Shared *bool    `json:"shared"`
	} `json:"facts"`
	Tasks []struct {
		ID         int      `json:"id"`
		Links      []string `json:"links"`
		Shared     *bool    `json:"shared"`
		Comparison *bool    `json:"comparison"`
	} `json:"tasks"`
}

func safeProductField(line, key string) string {
	pattern := regexp.MustCompile(regexp.QuoteMeta(key) + `=([^；;\n]*)`)
	if match := pattern.FindStringSubmatch(line); len(match) == 2 {
		return strings.TrimSpace(match[1])
	}
	return ""
}

// parseCardLinks 解析「链接=」字段的链接清单。商品卡为单链接（"1"），有效福利可为
// 多链接（"1、2、3"，顿号/逗号分隔）。返回去重后的链接列表，空表示没有链接字段。
func parseCardLinks(line string) []string {
	key := safeProductField(line, "链接")
	if key == "" {
		return nil
	}
	parts := strings.FieldsFunc(key, func(r rune) bool {
		return r == '、' || r == ',' || r == '，' || r == ' ' || r == '\t'
	})
	result := []string{}
	seen := map[string]bool{}
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" && !seen[p] {
			seen[p] = true
			result = append(result, p)
		}
	}
	return result
}
func validateScopeLinks(links []string, shared bool, products []anchorcycle.FocusProduct) error {
	if shared && len(links) > 0 {
		return errors.New("共享事实不能同时带专属链接")
	}
	known := map[string]bool{}
	for _, p := range products {
		known[p.LinkKey] = true
	}
	seen := map[string]bool{}
	for _, key := range links {
		if !known[key] || seen[key] {
			return errors.New("商品归属返回未知或重复链接")
		}
		seen[key] = true
	}
	return nil
}
func parseCycleProductScope(raw string, scope *anchorcycle.ProductFocus, facts []cycleScopeFactInput, tasks []anchorcycle.Task) error {
	var response cycleScopeResponse
	if err := decodeProductJSON(raw, &response); err != nil {
		return fmt.Errorf("商品归属返回格式无效：%w", err)
	}
	if len(response.Facts) != len(facts) || len(response.Tasks) != len(tasks) {
		return errors.New("商品归属缺少事实或角度，未保存，请重试")
	}
	seen := map[int]bool{}
	scope.Facts = nil
	for _, item := range response.Facts {
		if item.Shared == nil || item.Links == nil {
			return errors.New("商品事实归属缺少必需字段")
		}
		if item.ID < 1 || item.ID > len(facts) || seen[item.ID] {
			return errors.New("商品事实归属ID无效")
		}
		seen[item.ID] = true
		if err := validateScopeLinks(item.Links, *item.Shared, scope.Products); err != nil {
			return err
		}
		text := facts[item.ID-1].Text
		if strings.HasPrefix(text, "- [商品卡]") || strings.HasPrefix(text, "- [有效福利]") {
			key := safeProductField(text, "链接")
			if key != "" && (*item.Shared || len(item.Links) != 1 || item.Links[0] != key) {
				return errors.New("商品卡或专属福利归属不匹配，未保存")
			}
		}
		// Explicit references cannot silently become global facts.
		for _, p := range scope.Products {
			if anchorcycle.MentionsLink(text, p.LinkKey) && (*item.Shared || !hasScopeLink(item.Links, p.LinkKey)) {
				return fmt.Errorf("事实%d明确引用商品链接%s，归属不能设为共享且必须包含该链接", item.ID, p.LinkKey)
			}
		}
		scope.Facts = append(scope.Facts, anchorcycle.ScopedFact{Text: text, Links: item.Links, Shared: *item.Shared})
	}
	seen = map[int]bool{}
	scope.Tasks = map[string]anchorcycle.ScopedTask{}
	for _, item := range response.Tasks {
		if item.Shared == nil || item.Comparison == nil || item.Links == nil {
			return errors.New("商品角度归属缺少必需字段")
		}
		if item.ID < 1 || item.ID > len(tasks) || seen[item.ID] {
			return errors.New("商品角度归属ID无效")
		}
		seen[item.ID] = true
		if err := validateScopeLinks(item.Links, *item.Shared, scope.Products); err != nil {
			return err
		}
		if *item.Comparison && (*item.Shared || len(item.Links) < 2) {
			return errors.New("对比任务必须明确比较的链接")
		}
		// Links describe applicability, independently of whether this angle
		// actually compares products. Generation keeps the current focus and
		// scoped facts; an applicable angle does not grant joint promotion.
		t := tasks[item.ID-1]
		for _, p := range scope.Products {
			if anchorcycle.MentionsLink(t.SellingPoint+" "+t.Angle, p.LinkKey) && (*item.Shared || !hasScopeLink(item.Links, p.LinkKey)) {
				return fmt.Errorf("角度%d明确引用商品链接%s，归属不能设为共享且必须包含该链接", item.ID, p.LinkKey)
			}
		}
		scope.Tasks[t.Key] = anchorcycle.ScopedTask{Links: item.Links, Shared: *item.Shared, Comparison: *item.Comparison}
	}
	return nil
}
func hasScopeLink(links []string, key string) bool {
	for _, v := range links {
		if v == key {
			return true
		}
	}
	return false
}

// Invalid provider output is never coerced or silently stripped. Ask the model
// to correct it, preserving every original exchange and validating each result.
func validatedCycleProductScope(ctx context.Context, scope *anchorcycle.ProductFocus, facts []cycleScopeFactInput, tasks []anchorcycle.Task, messages []agentgateway.Message, repairPrompt string, complete func(context.Context, string, []agentgateway.Message) (string, error)) error {
	var validationErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt == 0 {
			reportAnchorAnalysis(ctx, 60, "正在确认各商品的资料范围")
		} else {
			reportAnchorAnalysis(ctx, 60+attempt*8, "正在校正商品资料的讲解范围")
		}
		stage := "cycle_product_scope"
		if attempt > 0 {
			stage = "cycle_product_scope_repair"
		}
		raw, err := complete(ctx, stage, messages)
		if err != nil {
			return fmt.Errorf("商品资料归属分析失败，未保存新版本，旧方案不变：%w", err)
		}
		parsed := *scope
		validationErr = parseCycleProductScope(raw, &parsed, facts, tasks)
		if validationErr == nil {
			*scope = parsed
			return nil
		}
		feedback, _ := json.Marshal(map[string]string{"validation_error": validationErr.Error()})
		messages = append(messages, agentgateway.Message{Role: "assistant", Content: raw}, agentgateway.Message{Role: "user", Content: repairPrompt + "\n" + string(feedback)})
	}
	return fmt.Errorf("商品归属结果经两次校正仍未通过校验，未保存或发布新版本，请重试；旧方案不变：%w", validationErr)
}

func cachedProductScope(ctx context.Context, scope *anchorcycle.ProductFocus, facts []cycleScopeFactInput, tasks []anchorcycle.Task, load func() (string, error), save func(string) error, generate func() (string, error)) error {
	raw, err := load()
	reused := err == nil
	if err != nil {
		if !errors.Is(err, appdb.ErrPlanAnalysisAssetNotFound) {
			return err
		}
		raw, err = generate()
		if err != nil {
			return err
		}
	}
	parsed := *scope
	if err = parseCycleProductScope(raw, &parsed, facts, tasks); err != nil {
		return fmt.Errorf("商品归属结果校验失败，未采用：%w", err)
	}
	if !reused {
		if err = save(raw); err != nil {
			return err
		}
	} else {
		reportAnchorAnalysis(ctx, 82, "商品资料与角度池未变化，复用已验证的讲解范围")
	}
	*scope = parsed
	return nil
}

// itemScopeResult 单条归属判定结果。
type itemScopeResult struct {
	Links      []string
	Shared     bool
	Comparison bool
}

// classifyProductScopeItem 对单条 fact/task 调用模型判定商品归属。单条最多重试 2 次，
// 最终仍失败时按「未归属」兜底（shared=false, links=[]），不阻塞整体。
func (s *Server) classifyProductScopeItem(ctx context.Context, profileID, mother string, products []productAnalysisView, itemType, itemText string) itemScopeResult {
	data, _ := json.Marshal(map[string]any{"products": products, "item_type": itemType, "item": itemText})
	messages := []agentgateway.Message{{Role: "system", Content: mother}, {Role: "user", Content: string(data)}}
	for attempt := 0; attempt < 2; attempt++ {
		call, stop := context.WithTimeout(ctx, 30*time.Second)
		result, _, err := s.completeAnchorStyleWorkbench(call, profileID, "cycle_product_scope_item", agentgateway.ResponseJSON, messages, 400, false)
		stop()
		if err != nil {
			continue
		}
		var resp struct {
			Links      []string `json:"links"`
			Shared     *bool    `json:"shared"`
			Comparison *bool    `json:"comparison"`
		}
		if decodeProductJSON(result.Text, &resp) != nil || resp.Shared == nil || resp.Links == nil {
			continue
		}
		if *resp.Shared && len(resp.Links) > 0 {
			continue // shared 与 links 不能同时设置
		}
		r := itemScopeResult{Links: resp.Links, Shared: *resp.Shared}
		if resp.Comparison != nil {
			r.Comparison = *resp.Comparison
		}
		return r
	}
	return itemScopeResult{}
}

// classifyProductScopeItems 并发逐条判定普通事实 + 任务，progress(done,total) 回推进度。
// 单条失败只影响该条（按未归属兜底），不再因「返回数量对不上」整体失败。
func (s *Server) classifyProductScopeItems(ctx context.Context, profileID, mother string, products []productAnalysisView, modelFacts []cycleScopeFactInput, tasks []anchorcycle.Task, progress func(int, int)) ([]itemScopeResult, map[string]itemScopeResult, error) {
	total := len(modelFacts) + len(tasks)
	if total == 0 {
		return []itemScopeResult{}, map[string]itemScopeResult{}, nil
	}
	type job struct {
		kind  string
		index int
		text  string
		key   string
	}
	jobs := make(chan job)
	factResults := make([]itemScopeResult, len(modelFacts))
	taskResults := map[string]itemScopeResult{}
	var mu sync.Mutex
	done := 0
	var wg sync.WaitGroup
	for worker := 0; worker < 6; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				r := s.classifyProductScopeItem(ctx, profileID, mother, products, j.kind, j.text)
				mu.Lock()
				if j.kind == "fact" {
					factResults[j.index] = r
				} else {
					taskResults[j.key] = r
				}
				done++
				if progress != nil {
					progress(done, total)
				}
				mu.Unlock()
			}
		}()
	}
enqueueFacts:
	for i, f := range modelFacts {
		select {
		case jobs <- job{kind: "fact", index: i, text: f.Text}:
		case <-ctx.Done():
			break enqueueFacts
		}
	}
enqueueTasks:
	for _, t := range tasks {
		select {
		case jobs <- job{kind: "task", text: t.SellingPoint + " × " + t.Angle, key: t.Key}:
		case <-ctx.Done():
			break enqueueTasks
		}
	}
	close(jobs)
	wg.Wait()
	if ctx.Err() != nil {
		return nil, nil, ctx.Err()
	}
	return factResults, taskResults, nil
}

type productScopeCacheFact struct {
	ID     int      `json:"id"`
	Links  []string `json:"links"`
	Shared *bool    `json:"shared"`
}
type productScopeCacheTask struct {
	Key        string   `json:"key"`
	Links      []string `json:"links"`
	Shared     *bool    `json:"shared"`
	Comparison *bool    `json:"comparison"`
}
type productScopeCache struct {
	Facts []productScopeCacheFact `json:"facts"`
	Tasks []productScopeCacheTask `json:"tasks"`
}

// applyProductScopeCache 把缓存里的归属结果填进 scope（普通事实 + 任务）。商品卡/福利
// 已由程序自判填进 scope.Facts，不在缓存里。
func applyProductScopeCache(raw string, modelFacts []cycleScopeFactInput, tasks []anchorcycle.Task, scope *anchorcycle.ProductFocus) error {
	var cache productScopeCache
	if err := json.Unmarshal([]byte(raw), &cache); err != nil {
		return err
	}
	if len(cache.Facts) != len(modelFacts) || len(cache.Tasks) != len(tasks) {
		return errors.New("商品归属缓存与当前资料不一致")
	}
	if scope.Tasks == nil {
		scope.Tasks = map[string]anchorcycle.ScopedTask{}
	}
	for i, cf := range cache.Facts {
		if cf.ID != i+1 || cf.Shared == nil {
			return errors.New("商品归属缓存事实编号无效")
		}
		scope.Facts = append(scope.Facts, anchorcycle.ScopedFact{Text: modelFacts[i].Text, Links: cf.Links, Shared: *cf.Shared})
	}
	for i, ct := range cache.Tasks {
		if ct.Key != tasks[i].Key || ct.Shared == nil || ct.Comparison == nil {
			return errors.New("商品归属缓存角度编号无效")
		}
		scope.Tasks[ct.Key] = anchorcycle.ScopedTask{Links: ct.Links, Shared: *ct.Shared, Comparison: *ct.Comparison}
	}
	return nil
}

// resolveProductScopeItems 先尝试读缓存，未命中则逐条并发判定并保存缓存。商品卡/福利的
// 确定性归属由调用方程序自判，这里只处理「普通事实 + 任务」的模型判定部分。
func (s *Server) resolveProductScopeItems(ctx context.Context, profileID, mother string, products []productAnalysisView, modelFacts []cycleScopeFactInput, tasks []anchorcycle.Task, fingerprint string, state *anchorcycle.State, scope *anchorcycle.ProductFocus) error {
	raw, err := s.store.GetPlanAnalysisAsset(ctx, state.TenantID, state.PlanID, "product_scope", fingerprint)
	if err == nil {
		if applyProductScopeCache(raw, modelFacts, tasks, scope) == nil {
			reportAnchorAnalysis(ctx, 82, "商品资料与角度池未变化，复用已验证的讲解范围")
			return nil
		}
	} else if !errors.Is(err, appdb.ErrPlanAnalysisAssetNotFound) {
		return err
	}
	factResults, taskResults, err := s.classifyProductScopeItems(ctx, profileID, mother, products, modelFacts, tasks, func(done, total int) {
		reportAnchorAnalysis(ctx, 45+done*40/max(1, total), fmt.Sprintf("正在确认商品资料范围 %d/%d", done, total))
	})
	if err != nil {
		return err
	}
	for i, f := range modelFacts {
		scope.Facts = append(scope.Facts, anchorcycle.ScopedFact{Text: f.Text, Links: factResults[i].Links, Shared: factResults[i].Shared})
	}
	if scope.Tasks == nil {
		scope.Tasks = map[string]anchorcycle.ScopedTask{}
	}
	for _, t := range tasks {
		r := taskResults[t.Key]
		scope.Tasks[t.Key] = anchorcycle.ScopedTask{Links: r.Links, Shared: r.Shared, Comparison: r.Comparison}
	}
	cache := productScopeCache{}
	for i := range modelFacts {
		shared := factResults[i].Shared
		cache.Facts = append(cache.Facts, productScopeCacheFact{ID: i + 1, Links: factResults[i].Links, Shared: &shared})
	}
	for _, t := range tasks {
		r := taskResults[t.Key]
		shared, comparison := r.Shared, r.Comparison
		cache.Tasks = append(cache.Tasks, productScopeCacheTask{Key: t.Key, Links: r.Links, Shared: &shared, Comparison: &comparison})
	}
	cacheJSON, _ := json.Marshal(cache)
	_ = s.store.SavePlanAnalysisAsset(ctx, state.TenantID, state.PlanID, "product_scope", fingerprint, string(cacheJSON))
	return nil
}

// Build once from the safety snapshot. This never rewrites source facts, roles,
// the selling-point wheel or a previously saved plan version.
func (s *Server) buildCycleProductFocus(ctx context.Context, state *anchorcycle.State, links []model.LiveAgentPlanProductLink, safeWheelJSON ...string) (*anchorcycle.ProductFocus, error) {
	active := []model.LiveAgentPlanProductLink{}
	for _, link := range links {
		if link.Status == "active" {
			active = append(active, link)
		}
	}
	if len(active) == 0 {
		return nil, nil
	}
	asset, err := s.store.GetActiveLiveSellingPointWheel(ctx, state.TenantID, state.PlanID)
	if err != nil {
		return nil, err
	}
	industrySignature, err := s.productCommonIndustrySignature(ctx, state.TenantID, state.PlanID)
	if err != nil {
		return nil, err
	}
	if !strings.Contains(asset.ExtractionPromptMD, productCommonIndustryMarker(industrySignature)) {
		return nil, errors.New("所属行业或行业挖掘维度已变化，请重新初始化直播智能体")
	}
	if _, err = s.store.GetPlanAnalysisAsset(ctx, state.TenantID, state.PlanID, "product_common_ready", productCommonReadyKey(asset, active)); err != nil {
		return nil, errors.New("商品与公共资料尚未完整初始化，请先点击初始化直播智能体，再保存方案")
	}
	p0, p1, p2, err := loadRiskAnnotationMothers()
	if err != nil {
		return nil, err
	}
	current, err := s.loadDerivedInput(ctx, state.TenantID, state.PlanID, p0, p1, p2)
	if err != nil {
		return nil, err
	}
	if asset.FactPackageSHA256 != derivedHash(current.Facts) {
		return nil, errors.New("事实已变化，请先重新初始化直播智能体再保存方案")
	}
	var wheel anchorStyleSellingPointWheel
	materialWheel := asset.EffectiveWheelJSON()
	if len(safeWheelJSON) > 0 && safeWheelJSON[0] != "" {
		materialWheel = safeWheelJSON[0]
	}
	if err = json.Unmarshal([]byte(materialWheel), &wheel); err != nil {
		return nil, err
	}
	// The saved safety wheel is authoritative: retain only directions still in
	// the safe queue, never resurrect an angle removed by risk preparation.
	safe := map[string]bool{}
	for _, task := range state.Queue {
		safe[task.SellingPoint+"×"+task.Angle] = true
	}
	for i := range wheel.Items {
		if len(safeWheelJSON) > 0 && safeWheelJSON[0] != "" {
			break
		}
		filtered := []string{}
		for _, angle := range wheel.Items[i].Angles {
			if safe[wheel.Items[i].Name+"×"+angle] {
				filtered = append(filtered, angle)
			}
		}
		wheel.Items[i].Angles = filtered
	}
	facts, err := s.resolveProductCommonFacts(ctx, state.TenantID, state.PlanID, state.Facts, active, "")
	if err != nil {
		return nil, err
	}
	return buildClassifiedProductFocus(state, active, facts, wheel)
}

func buildClassifiedProductFocus(state *anchorcycle.State, links []model.LiveAgentPlanProductLink, facts []anchorcycle.ScopedFact, wheel anchorStyleSellingPointWheel) (*anchorcycle.ProductFocus, error) {
	policy, source, err := loadCycleProductPolicy()
	if err != nil {
		return nil, err
	}
	common, err := commonWheelPoints(wheel)
	if err != nil {
		return nil, err
	}
	focus := &anchorcycle.ProductFocus{MaterialVersion: productCommonMaterialVersion, Policy: policy, PolicySource: source, FactsSHA: derivedHash(state.Facts), Facts: facts}
	cards := map[string]string{}
	for _, block := range productFactBlocks(state.Facts) {
		if strings.HasPrefix(block, "- [商品卡]") {
			key := safeProductField(block, "链接")
			if key == "" || cards[key] != "" {
				return nil, errors.New("安全商品卡缺少或重复链接")
			}
			cards[key] = block
		}
	}
	for _, link := range links {
		if link.Status != "active" {
			continue
		}
		if cards[link.LinkKey] == "" {
			return nil, errors.New("安全事实包缺少商品卡，请重新初始化")
		}
		own := focusSellingPointsFromModel(link.SellingPoints)
		if len(own) == 0 && len(common) == 0 {
			return nil, fmt.Errorf("链接%s没有可用的商品或公共卖点，请初始化直播智能体", link.LinkKey)
		}
		focus.Products = append(focus.Products, anchorcycle.FocusProduct{LinkKey: link.LinkKey, Name: safeProductField(cards[link.LinkKey], "商品"), Roles: append([]string(nil), link.RoomRoles...), Spec: link.Spec, DailyPrice: link.DailyPrice, Quantity: link.Quantity, FrequencyWeight: link.FrequencyWeight, SellingPoints: own, CommonPoints: common})
	}
	if err = focus.ValidateProducts(); err != nil {
		return nil, err
	}
	if err = validateProductFactSnapshot(state.Facts, focus); err != nil {
		return nil, err
	}
	focus.RefreshProductBundles()
	state.Queue = anchorcycle.ExpandProductSellingPoints(focus.Products, &focus.Policy)
	focus.Tasks = anchorcycle.ProductTaskScopes(state.Queue)
	focus.ResetRun()
	return focus, nil
}

func cycleProductScopeData(state *anchorcycle.State, t anchorcycle.Task) map[string]any {
	return map[string]any{"main_link": state.ProductFocus.MainLink(), "focus_link": t.FocusLink, "allowed_links": t.ProductLinks(), "mode": t.ProductMode, "max_chars": t.ProductMaxChars, "policy": state.ProductFocus.Policy, "completed_main_rounds": state.ProductFocus.CompletedMainRounds, "authorized_facts": state.ProductFacts(t)}
}
func renderCycleProductScope(state *anchorcycle.State) (string, error) {
	if state.ProductFocus == nil {
		return "", nil
	}
	prompt, _, err := loadCycleProductFocusPrompt()
	if err != nil {
		return "", err
	}
	data, _ := json.Marshal(cycleProductScopeData(state, state.Task()))
	violation := ""
	if len(state.Attempts) > 0 {
		last := state.Attempts[len(state.Attempts)-1]
		if strings.HasPrefix(last.ContentIssue, "product_focus_") {
			raw, _ := json.Marshal(map[string]any{"issue": last.ContentIssue, "audit": last.ProductAudit})
			violation = string(raw)
		}
	}
	// The policy block documents defaults only. The active policy is the frozen
	// version, not today's possibly edited mother defaults.
	prompt = regexp.MustCompile("(?s)```json\\s*.*?\\s*```").ReplaceAllString(prompt, "")
	return strings.NewReplacer("{{scope}}", string(data), "{{violation}}", violation).Replace(prompt), nil
}
func (s *Server) auditCycleProductCandidate(ctx context.Context, state *anchorcycle.State, c *anchorcycle.Candidate) {
	if state.ProductFocus == nil {
		return
	}
	t := state.Task()
	text := cycleVoiceText(*c)
	// Keep the exact TTS text (including the existing terminal punctuation
	// normalization) bound to the audit. Original model text remains untouched.
	c.OutputText = text
	audit := &anchorcycle.ProductAudit{TextSHA: anchorcycle.ProductDigest(text), ScopeSHA: state.ProductScopeSHA(t)}
	c.ProductAudit = audit
	if t.ProductMode == "auxiliary" && len([]rune(text)) > t.ProductMaxChars {
		audit.Reason = "辅推超过本段篇幅上限，请缩短为主推的补充选项并回扣主推"
		return
	}
	if state.ProductTextIssue(c.Text, t) != "" || state.ProductTextIssue(text, t) != "" {
		audit.Reason = "单品段出现当前范围之外的链接，请删除其它链接信息重写"
		return
	}
	prompt, source, err := loadCycleProductAuditPrompt()
	audit.PromptSource = source
	if err != nil {
		audit.Reason = "商品范围复核母版不可用"
		state.Warn(err.Error())
		return
	}
	data, _ := json.Marshal(map[string]any{"scope": cycleProductScopeData(state, t), "text": text})
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	result, _, err := s.completeAnchorStyleWorkbench(ctx, state.Config.FeedbackID, "cycle_product_focus_audit", agentgateway.ResponseJSON, []agentgateway.Message{{Role: "system", Content: prompt}, {Role: "user", Content: string(data)}}, 700, false)
	if err != nil {
		audit.Reason = "商品范围复核未完成，不能直接转为声音"
		state.Warn("商品范围复核失败：" + err.Error())
		return
	}
	var output struct {
		Passed *bool  `json:"passed"`
		Quote  string `json:"quote"`
		Reason string `json:"reason"`
	}
	err = decodeProductJSON(result.Text, &output)
	if err != nil || output.Passed == nil || (!*output.Passed && (strings.TrimSpace(output.Reason) == "" || strings.TrimSpace(output.Quote) == "" || !strings.Contains(text, output.Quote))) || (*output.Passed && (output.Quote != "" || output.Reason != "")) {
		audit.Reason = "商品范围复核返回无效，原始返回已记录，请重试"
		return
	}
	audit.Passed, audit.Quote, audit.Reason = *output.Passed, output.Quote, output.Reason
}

func (s *Server) captureRealtimeProductFocus(ctx context.Context, tenant, plan int64, input *model.CreateLiveAgentPlanVersionInput, safety model.LiveDerivedSafetyAsset) error {
	if !isRealtimeScheme(input.GenerationContext) {
		return nil
	}
	scheme, err := realtimeScheme(input.GenerationContext)
	if err != nil {
		return err
	}
	links, err := s.store.ListLiveAgentPlanProductLinks(ctx, tenant, plan)
	if err != nil {
		return err
	}
	var wheel anchorStyleSellingPointWheel
	if err = json.Unmarshal([]byte(safety.SafeWheelJSON), &wheel); err != nil {
		return err
	}
	probe := &anchorcycle.State{TenantID: tenant, PlanID: plan, RoomID: input.RoomID, Facts: safety.SafeFacts, Phase: "opening", CycleNumber: 1}
	for _, t := range flattenAnchorStyleWheelTasks(wheel) {
		probe.Queue = append(probe.Queue, anchorcycle.Task{SellingPoint: t.SellingPoint, Angle: t.Angle, Key: t.Key, Tier: t.Tier})
	}
	scheme.ProductFocus, err = s.buildCycleProductFocus(ctx, probe, links, safety.SafeWheelJSON)
	if err != nil {
		return err
	}
	if scheme.ProductFocus != nil && scheme.ProductFocus.MaterialVersion == productCommonMaterialVersion {
		if err = validateProductFactSnapshot(safety.SafeFacts, scheme.ProductFocus); err != nil {
			return err
		}
	}
	scheme.SingleProductSchedule = singleProductSchedule(links)
	input.GenerationContext["anchor_generation"] = scheme
	return nil
}

// Capture only scheduling inputs for a single product. Its existing fact path
// stays unchanged; never create a ProductFocus merely to carry point weights.
func singleProductSchedule(links []model.LiveAgentPlanProductLink) *anchorcycle.FocusProduct {
	var active []model.LiveAgentPlanProductLink
	for _, l := range links {
		if l.Status == "active" {
			active = append(active, l)
		}
	}
	if len(active) != 1 {
		return nil
	}
	l := active[0]
	points := focusSellingPointsFromModel(l.SellingPoints)
	if len(points) == 0 {
		return nil
	}
	return &anchorcycle.FocusProduct{LinkKey: l.LinkKey, SellingPoints: points}
}
