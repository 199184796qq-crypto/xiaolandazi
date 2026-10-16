package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/anchorstylescore"
	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
	"net/http"
	"strings"
	"time"
)

type anchorGenerationInitializationStep struct {
	Key     string `json:"key"`
	Label   string `json:"label"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

func anchorGenerationStep(key, label, successMessage string, err error) anchorGenerationInitializationStep {
	if err != nil {
		return anchorGenerationInitializationStep{Key: key, Label: label, Status: "failed", Message: err.Error()}
	}
	return anchorGenerationInitializationStep{Key: key, Label: label, Status: "success", Message: successMessage}
}

func anchorGenerationInitializationNotice(steps []anchorGenerationInitializationStep) (initialized, partial bool, notice string) {
	succeeded := 0
	for _, step := range steps {
		if step.Status == "success" {
			succeeded++
		}
	}
	initialized = len(steps) > 0 && succeeded == len(steps)
	partial = succeeded > 0 && succeeded < len(steps)
	switch {
	case initialized:
		notice = "四项初始化均已完成。"
	case partial:
		notice = fmt.Sprintf("分项初始化完成：成功 %d 项，失败 %d 项；已成功内容不会因其它项目失败而回退。", succeeded, len(steps)-succeeded)
	default:
		notice = fmt.Sprintf("分项初始化未完成：%d 项均失败，请按各项提示处理后重试。", len(steps))
	}
	return
}

func (s *Server) importBoundCompiledStyleParts(ctx context.Context, tenant, plan, actor int64) (styleErr, scoreErr error) {
	style, err := s.store.GetLiveAnchorStyleForPlan(ctx, tenant, plan)
	if err != nil {
		return errors.New("请先把已调教的主播风格绑定到当前方案"), errors.New("评分基准等待已调教的主播风格")
	}
	d := style.Profile.Delivery
	if d == nil {
		return errors.New("主播风格尚未完成元提示词调教"), errors.New("主播风格尚未完成评分基准调教")
	}
	samples, err := s.store.ListLiveAnchorStyleSamples(ctx, tenant, style.ID)
	if err != nil {
		return err, err
	}
	sample := ""
	version, versionErr := s.store.ActiveLiveAnchorAnalysis(ctx, tenant, style.ID)
	if versionErr == nil {
		// A version owns its exact source corpus and calibrated baseline, even
		// after individual sample rows have subsequently been deleted.
		d = version.Profile.Delivery
		sample = version.SourceText
		if d == nil {
			return errors.New("采纳版本缺少风格信息"), errors.New("采纳版本缺少评分基准")
		}
	} else if !errors.Is(versionErr, appdb.ErrAnchorAnalysisVersionNotFound) {
		return versionErr, versionErr
	}
	for _, item := range samples {
		if sample != "" {
			break
		}
		if item.AnalysisStatus == "analyzed" && derivedHash(item.ReadableText) == d.SourceSHA256 {
			sample = item.ReadableText
			break
		}
	}
	if sample == "" {
		err = errors.New("风格对应的原始样本已删除或改变，请重新开始调教")
		return err, err
	}
	if strings.TrimSpace(d.MetaPromptMD) == "" {
		styleErr = errors.New("主播风格尚未完成元提示词调教")
	} else {
		meta, metaErr := s.store.GetActiveLiveAnchorStyleMetaPrompt(ctx, tenant, plan)
		if metaErr != nil && !errors.Is(metaErr, appdb.ErrLiveAnchorStyleMetaPromptNotFound) {
			styleErr = metaErr
		} else {
			if meta.ID == 0 || meta.Status != "confirmed" || meta.SampleSHA256 != d.SourceSHA256 || meta.EffectiveMetaPromptMD() != d.MetaPromptMD {
				meta, metaErr = s.store.SaveLiveAnchorStyleMetaPrompt(ctx, tenant, plan, actor, model.SaveLiveAnchorStyleMetaPromptInput{Scene: "带货直播", SampleSHA256: d.SourceSHA256, OriginalMetaPromptMD: d.MetaPromptMD, MonitorJSON: d.MonitoringJSON, AnalysisPromptSource: d.AnalysisPromptSource, Provider: "compiled_style_asset"})
				if metaErr == nil {
					meta, metaErr = s.store.ConfirmLiveAnchorStyleMetaPrompt(ctx, tenant, plan, meta.ID, actor, d.MetaPromptMD)
				}
			}
			styleErr = metaErr
		}
	}
	if styleErr != nil {
		return styleErr, errors.New("评分基准等待主播风格初始化成功")
	}
	meta, err := s.store.GetActiveLiveAnchorStyleMetaPrompt(ctx, tenant, plan)
	if err != nil || meta.ID == 0 {
		return nil, errors.New("评分基准等待已确认的主播风格元提示词")
	}
	if strings.TrimSpace(d.ScoringBaselineJSON) == "" {
		return nil, errors.New("主播风格尚未完成评分基准调教")
	}
	if _, err = anchorstylescore.Parse(d.ScoringBaselineJSON); err != nil {
		return nil, err
	}
	card, err := s.store.GetActiveLiveAnchorStyleScorecard(ctx, tenant, plan)
	if err != nil && !errors.Is(err, appdb.ErrLiveAnchorStyleScorecardNotFound) {
		return nil, err
	}
	if card.ID == 0 || card.Status != "confirmed" || card.MetaPromptAssetID != meta.ID || card.EffectiveScorecardJSON() != d.ScoringBaselineJSON {
		card, err = s.store.SaveLiveAnchorStyleScorecard(ctx, tenant, plan, actor, model.SaveLiveAnchorStyleScorecardInput{MetaPromptAssetID: meta.ID, SampleSHA256: d.SourceSHA256, SampleText: sample, OriginalScorecardJSON: d.ScoringBaselineJSON, PromptSource: d.ScorecardPromptSource, Provider: "program_calibrated_style_asset", Status: "draft"})
		if err != nil {
			return nil, err
		}
		_, err = s.store.ConfirmLiveAnchorStyleScorecard(ctx, tenant, plan, card.ID, actor, d.ScoringBaselineJSON)
		if err != nil {
			return nil, err
		}
	}
	return nil, nil
}

func (s *Server) initializeSellingPointWheel(ctx context.Context, tenant, plan, actor int64) (points, angles int, err error) {
	p0, p1, p2, err := loadRiskAnnotationMothers()
	if err != nil {
		return 0, 0, err
	}
	source, err := s.loadDerivedInput(ctx, tenant, plan, p0, p1, p2)
	if err != nil {
		return 0, 0, err
	}
	prompt, promptSource, err := loadSellingPointExtractionPrompt()
	if err != nil {
		return 0, 0, err
	}
	finalAuditPrompt, _, err := loadSellingPointWheelFinalAuditPrompt()
	if err != nil {
		return 0, 0, err
	}
	cfg, err := s.store.AnchorGenerationSettings(ctx)
	if err != nil {
		return 0, 0, err
	}
	if cfg.SellingPointProfileID == "" {
		return 0, 0, errors.New("请先在后台配置卖点挖掘模型")
	}
	// 行业优先：方案选了行业就注入行业四件套的挖掘维度；否则回退品类维度（旧链路）。
	if pack, packErr := s.loadIndustryPackForPlan(ctx, tenant, plan); packErr == nil && pack != nil {
		prompt = injectIndustrySellingDimensions(prompt, pack.IndustryName, pack.SellingDimensions)
	} else {
		category, _ := s.resolveProductCategory(ctx, tenant, plan, cfg.SellingPointProfileID, source.Facts)
		prompt, err = injectCategorySellingDimensions(prompt, category)
		if err != nil {
			return 0, 0, err
		}
	}
	// 两段式：一次挖掘 + 程序确定性删违规，不再走五阶段重审核。
	snapshot := prompt
	if handled, points, angles, reuseErr := s.reuseOrUpdateWheel(ctx, tenant, plan, actor, source.Facts, finalAuditPrompt, snapshot, cfg.SellingPointProfileID); handled {
		if reuseErr == nil {
			// 复用/增量更新成功后，仍确保每商品独立卖点池存在（旧数据迁移补建）。
			// 若每商品池已由用户配置，syncProductSellingPointPools 会按「已调保留、未调等权」合并。
			reuseErr = s.syncProductSellingPointPools(ctx, tenant, plan, actor, source, snapshot, cfg.SellingPointProfileID)
		}
		return points, angles, reuseErr
	}
	reportAnchorAnalysis(ctx, 40, "正在为你梳理商品讲解要点和讲解角度")
	ctx = withExchangeScope(ctx, appdb.ModelExchangeScope{TenantID: tenant, PlanID: plan})
	user, _ := json.Marshal(map[string]string{"fact_package": source.Facts})
	result, actual, err := s.completeAnchorStyleWorkbench(ctx, cfg.SellingPointProfileID, "selling_point_extraction", agentgateway.ResponseJSON, []agentgateway.Message{{Role: "system", Content: strings.ReplaceAll(prompt, "{{FACT_PACKAGE}}", "")}, {Role: "user", Content: string(user)}}, 7000, false)
	if err != nil {
		return 0, 0, err
	}
	canonical, points, angles, validation := normalizeAnchorStyleSellingPointWheelJSON(result.Text)
	if validation == nil {
		validation = validateSellingPointPromptContract(result.Text, prompt)
	}
	programRemovedBoundaryItems := make([]string, 0)
	if validation == nil {
		if violations := sellingPointFactBoundaryViolations(canonical, source.Facts); len(violations) > 0 {
			filteredCanonical, removed, filteredPoints, filteredAngles, filterErr := removeSellingPointFactBoundaryViolations(canonical, source.Facts)
			programRemovedBoundaryItems = removed
			if filterErr != nil {
				validation = fmt.Errorf("讲解要点仍含资料中未提供的高风险表达且无法安全删除：%w", filterErr)
			} else {
				canonical, points, angles = filteredCanonical, filteredPoints, filteredAngles
			}
		}
	}
	if validation == nil {
		if tiered, terr := assignSellingPointTiers(canonical); terr == nil {
			canonical = tiered
		} else {
			validation = fmt.Errorf("卖点分级失败：%w", terr)
		}
	}
	status := "draft"
	if validation != nil {
		status = "validation_failed"
		if canonical == "" {
			canonical = result.Text
		}
	}
	latest, err := s.loadDerivedInput(ctx, tenant, plan, p0, p1, p2)
	if err != nil || latest.Facts != source.Facts {
		return 0, 0, errors.New("初始化期间正式事实已变化，请重新初始化；原始返回已记录")
	}
	rawBundle, _ := json.MarshalIndent(map[string]any{
		"extraction_raw_response":        result.Text,
		"program_removed_boundary_items": programRemovedBoundaryItems,
		"pipeline":                       "two-stage",
	}, "", "  ")
	trace := traceAnchorStyleWorkbenchMessages(actual)
	wheel, err := s.store.SaveLiveSellingPointWheel(ctx, tenant, plan, actor, model.SaveLiveSellingPointWheelInput{Status: status, ExtractionPromptMD: prompt, PromptSource: promptSource, FactPackageSHA256: derivedHash(source.Facts), OriginalWheelJSON: canonical, RequestSystem: trace.System, RequestUser: trace.User, RawResponse: string(rawBundle), Provider: result.Provider, Model: result.Model, LatencyMS: result.LatencyMS, InputTokens: result.InputTokens, OutputTokens: result.OutputTokens})
	if err != nil {
		return 0, 0, errors.New("轮盘保存失败")
	}
	if validation != nil {
		return 0, 0, validation
	}
	if _, err = s.store.ConfirmLiveSellingPointWheel(ctx, tenant, plan, wheel.ID, actor, canonical); err != nil {
		return 0, 0, err
	}
	// 每商品独立卖点池：全局轮盘生成确认后，为每个 active 商品单独调模型挖卖点，
	// 归属由程序保证（productLinkID），输入只投该商品的商品卡行 + 共享事实。
	// 权重合并：用户已调过的卖点/角度权重保留，未调过的默认等权。
	if err = s.syncProductSellingPointPools(ctx, tenant, plan, actor, source, prompt, cfg.SellingPointProfileID); err != nil {
		return 0, 0, err
	}
	return points, angles, nil
}

func derefString(p *string) string {
	if p == nil {
		return ""
	}
	return strings.TrimSpace(*p)
}

func containsLink(links []string, key string) bool {
	for _, v := range links {
		if strings.TrimSpace(v) == key {
			return true
		}
	}
	return false
}

// productFactPackage 为该商品拼「单商品事实包」：该商品卡行 + 无链接共享事实 + 该商品福利行。
// 普通事实无链接归属 → 全方案共享，对每个商品都可用（如「39年老油坊」这种品牌背书）。
func productFactPackage(linkKey string, facts string) string {
	var card, benefit []string
	shared := []string{}
	for _, line := range strings.Split(facts, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "- [商品卡]") {
			if safeProductField(trimmed, "链接") == linkKey {
				card = append(card, trimmed)
			}
			continue
		}
		if strings.HasPrefix(trimmed, "- [有效福利]") {
			links := parseCardLinks(trimmed)
			// 无链接字段或覆盖全部商品 → 共享；否则仅当明确列出该商品时归属它。
			if len(links) == 0 || containsLink(links, linkKey) {
				benefit = append(benefit, trimmed)
			}
			continue
		}
		// 普通事实：无链接字段，属全方案共享背书，对每个商品可用。
		if strings.HasPrefix(trimmed, "- [") {
			shared = append(shared, trimmed)
		}
	}
	parts := append([]string{}, card...)
	parts = append(parts, shared...)
	parts = append(parts, benefit...)
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "\n")
}

// mergedProductSellingPointWeight 合并权重：旧池同名卖点存在则保留旧权重，否则等权 1。
func mergedProductSellingPointWeight(existing []model.LiveAgentPlanProductSellingPoint, name string) int {
	for _, p := range existing {
		if strings.TrimSpace(p.SellingPoint) == name {
			if p.PointWeight >= 1 {
				return p.PointWeight
			}
			return 1
		}
	}
	return 1
}

// mergedProductSellingAngleWeight 合并角度权重：旧池同名卖点下同名角度存在则保留，否则等权 1。
func mergedProductSellingAngleWeight(existing []model.LiveAgentPlanProductSellingPoint, pointName, angle string) int {
	for _, p := range existing {
		if strings.TrimSpace(p.SellingPoint) != pointName {
			continue
		}
		for _, a := range p.Angles {
			if strings.TrimSpace(a.Angle) == angle {
				if a.Weight >= 1 {
					return a.Weight
				}
				return 1
			}
		}
	}
	return 1
}

// syncProductSellingPointPools 为每个 active 商品单独调模型挖卖点，落库到每商品独立卖点池。
// 商品归属由 productLinkID 程序保证；权重合并保留用户已调、未调等权。
// 鲁棒性：逐商品隔离——单商品失败不中断其它商品，成功商品保留、失败商品汇总报出。
func (s *Server) syncProductSellingPointPools(ctx context.Context, tenant, plan, actor int64, source *derivedInput, prompt, profileID string) error {
	links, err := s.store.ListLiveAgentPlanProductLinks(ctx, tenant, plan)
	if err != nil {
		return err
	}
	active := make([]model.LiveAgentPlanProductLink, 0, len(links))
	for _, l := range links {
		if strings.EqualFold(strings.TrimSpace(l.Status), "active") {
			active = append(active, l)
		}
	}
	if len(active) == 0 {
		return nil
	}
	reportAnchorAnalysis(ctx, 92, "正在为每个商品单独梳理讲解要点")
	scoped := withExchangeScope(ctx, appdb.ModelExchangeScope{TenantID: tenant, PlanID: plan})
	succeeded, failed := 0, 0
	failures := make([]string, 0, len(active))
	for _, link := range active {
		if ctx.Err() != nil {
			failed++
			failures = append(failures, fmt.Sprintf("商品%s：%v", link.LinkKey, ctx.Err()))
			break
		}
		if err := s.syncSingleProductSellingPointPool(scoped, tenant, plan, actor, source, prompt, profileID, link); err != nil {
			failed++
			failures = append(failures, fmt.Sprintf("商品%s：%v", link.LinkKey, err))
			continue
		}
		succeeded++
	}
	if failed == 0 {
		return nil
	}
	return fmt.Errorf("商品讲解要点梳理：成功 %d 项、失败 %d 项（%s）；已成功内容保留，可重试补齐失败商品", succeeded, failed, strings.Join(failures, "；"))
}

// syncSingleProductSellingPointPool 梳理并保存单个商品的卖点池；模型调用带一次瞬时错误重试。
func (s *Server) syncSingleProductSellingPointPool(ctx context.Context, tenant, plan, actor int64, source *derivedInput, prompt, profileID string, link model.LiveAgentPlanProductLink) error {
	pkg := productFactPackage(link.LinkKey, source.Facts)
	if strings.TrimSpace(pkg) == "" {
		return fmt.Errorf("商品%s无可用的商品资料", link.LinkKey)
	}
	user, _ := json.Marshal(map[string]string{"fact_package": pkg})
	result, callErr := s.completeSellingPointExtractionWithRetry(ctx, profileID, prompt, string(user))
	if callErr != nil {
		return fmt.Errorf("讲解要点梳理失败：%w", callErr)
	}
	canonical, _, _, validation := normalizeAnchorStyleSellingPointWheelJSON(result.Text)
	if validation == nil {
		validation = validateSellingPointPromptContract(result.Text, prompt)
	}
	if validation != nil {
		return fmt.Errorf("卖点结果校验未通过：%w", validation)
	}
	if violations := sellingPointFactBoundaryViolations(canonical, pkg); len(violations) > 0 {
		if filtered, _, _, _, filterErr := removeSellingPointFactBoundaryViolations(canonical, pkg); filterErr == nil {
			canonical = filtered
		} else {
			return fmt.Errorf("卖点含事实边界问题且无法安全删除：%w", filterErr)
		}
	}
	if tiered, terr := assignSellingPointTiers(canonical); terr == nil {
		canonical = tiered
	}
	var wheel anchorStyleSellingPointWheel
	if err := json.Unmarshal([]byte(canonical), &wheel); err != nil {
		return fmt.Errorf("卖点解析失败：%w", err)
	}
	existing := link.SellingPoints
	inputs := make([]model.LiveAgentPlanProductSellingPointInput, 0, len(wheel.Items))
	for _, item := range wheel.Items {
		angles := make([]model.LiveAgentPlanProductSellingAngle, 0, len(item.Angles))
		for _, angle := range item.Angles {
			angles = append(angles, model.LiveAgentPlanProductSellingAngle{Angle: strings.TrimSpace(angle), Weight: mergedProductSellingAngleWeight(existing, item.Name, strings.TrimSpace(angle))})
		}
		if len(angles) == 0 {
			continue
		}
		inputs = append(inputs, model.LiveAgentPlanProductSellingPointInput{
			SellingPoint:   strings.TrimSpace(item.Name),
			Tier:           item.Tier,
			PointWeight:    mergedProductSellingPointWeight(existing, strings.TrimSpace(item.Name)),
			IsMain:         item.Tier == "core",
			Angles:         angles,
			TargetAudience: derefString(item.TargetAudience),
			PainPoint:      derefString(item.PainPoint),
		})
	}
	if len(inputs) == 0 {
		return errors.New("卖点池为空")
	}
	if _, err := s.store.SaveLiveAgentPlanProductSellingPoints(ctx, tenant, plan, link.ID, actor, inputs, "product_selling_point_extraction", ""); err != nil {
		return fmt.Errorf("卖点池保存失败：%w", err)
	}
	return nil
}

// completeSellingPointExtractionWithRetry 调模型梳理商品卖点，瞬时错误自动重试一次（确定性校验失败不重试）。
func (s *Server) completeSellingPointExtractionWithRetry(ctx context.Context, profileID, prompt, factPackageJSON string) (agentgateway.Response, error) {
	var result agentgateway.Response
	var lastErr error
	for attempt := 1; attempt <= 2; attempt++ {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		result, _, lastErr = s.completeAnchorStyleWorkbench(ctx, profileID, "product_selling_point_extraction", agentgateway.ResponseJSON,
			[]agentgateway.Message{{Role: "system", Content: strings.ReplaceAll(prompt, "{{FACT_PACKAGE}}", "")}, {Role: "user", Content: factPackageJSON}}, 5000, false)
		if lastErr == nil {
			return result, nil
		}
		// 仅瞬时/传输类错误重试；参数校验类错误重试无意义。此处统一对模型调用错误重试一次，
		// 结果文本的确定性校验在调用方单独判定（不重试）。
		if attempt == 1 {
			select {
			case <-ctx.Done():
				return result, ctx.Err()
			case <-time.After(300 * time.Millisecond):
			}
		}
	}
	return result, lastErr
}

func (s *Server) liveAnchorGenerationInitialize(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	plan, ok := liveAgentPlanPathID(w, r)
	if !ok {
		return
	}
	tenant, ok := s.resolveLiveAgentPlanTenant(w, r, actor, 0, true)
	if !ok {
		return
	}
	w, r = anchorAnalysisStream(w, r)
	reportAnchorAnalysis(r.Context(), 5, "正在读取当前方案与已确认资源")
	ctx, cancel := context.WithTimeout(r.Context(), 240*time.Second)
	defer cancel()
	styleErr, scoreErr := s.importBoundCompiledStyleParts(ctx, tenant, plan, actor.UserID)
	reportAnchorAnalysis(ctx, 20, "主播风格与评分基准已检查，正在核对事实")
	factErr := s.refreshPlanFactRiskRegistry(ctx, tenant, plan)
	reportAnchorAnalysis(ctx, 30, "正在核对商品讲解要点与最新资料的变化")
	points, angles, wheelErr := s.initializeSellingPointWheel(ctx, tenant, plan, actor.UserID)
	steps := []anchorGenerationInitializationStep{
		anchorGenerationStep("anchor_style", "主播风格", "主播风格已关联保存", styleErr),
		anchorGenerationStep("scorecard", "评分基准", "评分基准已关联保存", scoreErr),
		anchorGenerationStep("fact_package", "事实包", "事实包已独立刷新", factErr),
		anchorGenerationStep("selling_point_wheel", "讲解要点", fmt.Sprintf("已梳理 %d 个讲解要点、%d 种讲解角度", points, angles), wheelErr),
	}
	initialized, partial, notice := anchorGenerationInitializationNotice(steps)
	// Optional content analysis does not change the four existing prerequisites.
	strategyStep := anchorGenerationInitializationStep{Key: "content_strategy", Label: "讲解内容策略", Status: "skipped", Message: "等待资料与讲解要点梳理完成，原流程不变"}
	if factErr == nil && wheelErr == nil {
		reportAnchorAnalysis(ctx, 96, "正在准备讲解内容策略，原有方案保留")
		strategyErr := s.initializeContentStrategy(ctx, tenant, plan, actor.UserID)
		strategyStep = anchorGenerationStep("content_strategy", "讲解内容策略", "方案内容策略已准备，原有流程继续执行", strategyErr)
		if strategyErr != nil {
			notice += " 内容策略未准备成功，原生成流程可继续：" + strategyErr.Error()
		}
	}
	reportAnchorAnalysis(ctx, 100, notice)
	writeJSON(w, 200, map[string]any{"initialized": initialized, "partial": partial, "selling_points": points, "angles": angles, "notice": notice, "steps": steps, "content_strategy": strategyStep})
}
