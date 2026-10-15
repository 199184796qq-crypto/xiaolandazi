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
	category, _ := s.detectProductCategory(ctx, tenant, plan, cfg.SellingPointProfileID, source.Facts)
	prompt, err = injectCategorySellingDimensions(prompt, category)
	if err != nil {
		return 0, 0, err
	}
	// 两段式：一次挖掘 + 程序确定性删违规，不再走五阶段重审核。
	snapshot := prompt
	if handled, points, angles, reuseErr := s.reuseOrUpdateWheel(ctx, tenant, plan, actor, source.Facts, finalAuditPrompt, snapshot, cfg.SellingPointProfileID); handled {
		return points, angles, reuseErr
	}
	reportAnchorAnalysis(ctx, 40, "正在首次生成卖点与角度池")
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
				validation = fmt.Errorf("卖点轮盘仍含事实正文未提供的高风险表达且无法安全删除：%w", filterErr)
			} else {
				canonical, points, angles = filteredCanonical, filteredPoints, filteredAngles
			}
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
	return points, angles, nil
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
	reportAnchorAnalysis(ctx, 30, "正在比较现有卖点池与当前事实的变化")
	points, angles, wheelErr := s.initializeSellingPointWheel(ctx, tenant, plan, actor.UserID)
	steps := []anchorGenerationInitializationStep{
		anchorGenerationStep("anchor_style", "主播风格", "主播风格已关联保存", styleErr),
		anchorGenerationStep("scorecard", "评分基准", "评分基准已关联保存", scoreErr),
		anchorGenerationStep("fact_package", "事实包", "事实包已独立刷新", factErr),
		anchorGenerationStep("selling_point_wheel", "卖点角度", fmt.Sprintf("已生成 %d 个卖点、%d 个角度组合", points, angles), wheelErr),
	}
	initialized, partial, notice := anchorGenerationInitializationNotice(steps)
	// Optional content analysis does not change the four existing prerequisites.
	strategyStep := anchorGenerationInitializationStep{Key: "content_strategy", Label: "目的角度内容策略", Status: "skipped", Message: "等待事实与卖点初始化完成，原流程不变"}
	if factErr == nil && wheelErr == nil {
		reportAnchorAnalysis(ctx, 96, "正在准备独立的目的角度内容策略，原有方案保留")
		strategyErr := s.initializeContentStrategy(ctx, tenant, plan, actor.UserID)
		strategyStep = anchorGenerationStep("content_strategy", "目的角度内容策略", "方案内容策略已准备，原有调度继续执行", strategyErr)
		if strategyErr != nil {
			notice += " 内容策略未准备成功，原生成流程可继续：" + strategyErr.Error()
		}
	}
	reportAnchorAnalysis(ctx, 100, notice)
	writeJSON(w, 200, map[string]any{"initialized": initialized, "partial": partial, "selling_points": points, "angles": angles, "notice": notice, "steps": steps, "content_strategy": strategyStep})
}
