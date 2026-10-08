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
	reviewPrompt, reviewPromptSource, err := loadSellingPointWheelReviewPrompt()
	if err != nil {
		return 0, 0, err
	}
	expansionPrompt, expansionPromptSource, err := loadSellingPointWheelExpansionPrompt()
	if err != nil {
		return 0, 0, err
	}
	finalAuditPrompt, finalAuditPromptSource, err := loadSellingPointWheelFinalAuditPrompt()
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
	snapshot := strings.Join([]string{prompt, "# 第二阶段：事实终审母版\n\n" + reviewPrompt, "# 稀疏角度补全母版\n\n" + expansionPrompt, "# 最终事实裁决母版\n\n" + finalAuditPrompt}, "\n\n---\n\n")
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
	finalResult := result
	totalLatencyMS := result.LatencyMS
	totalInputTokens := result.InputTokens
	totalOutputTokens := result.OutputTokens
	reviewRaw := ""
	expansionRaw := ""
	finalReviewRaw := ""
	boundaryRepairRaw := ""
	stageError := ""
	coverageTargets := make([]sellingPointWheelCoverageTarget, 0)
	boundaryViolations := make([]string, 0)
	programRemovedBoundaryItems := make([]string, 0)
	if validation == nil {
		reviewUser := renderSellingPointWheelReviewRequest(source.Facts, canonical)
		reportAnchorAnalysis(ctx, 60, "正在核对新生成卖点的事实依据")
		reviewResult, reviewActual, reviewErr := s.completeAnchorStyleWorkbench(ctx, cfg.SellingPointProfileID, "selling_point_review", agentgateway.ResponseJSON, []agentgateway.Message{{Role: "system", Content: reviewPrompt}, {Role: "user", Content: reviewUser}}, 7000, false)
		actual = append(actual, reviewActual...)
		totalLatencyMS += reviewResult.LatencyMS
		totalInputTokens += reviewResult.InputTokens
		totalOutputTokens += reviewResult.OutputTokens
		if reviewErr != nil {
			validation = fmt.Errorf("卖点轮盘事实终审失败：%w", reviewErr)
			stageError = validation.Error()
		} else {
			reviewRaw = reviewResult.Text
			finalResult = reviewResult
			reviewCanonical, reviewPoints, reviewAngles, reviewValidation := normalizeAnchorStyleSellingPointWheelJSON(reviewResult.Text)
			if reviewValidation == nil {
				reviewValidation = validateSellingPointPromptContract(reviewResult.Text, reviewPrompt)
			}
			if reviewValidation != nil {
				validation = fmt.Errorf("卖点轮盘事实终审返回无效：%w", reviewValidation)
				stageError = validation.Error()
				if reviewCanonical != "" {
					canonical = reviewCanonical
				}
			} else {
				canonical, points, angles = reviewCanonical, reviewPoints, reviewAngles
			}
		}
	}
	if validation == nil {
		coverageTargets, err = sellingPointWheelCoverageTargets(canonical, 5)
		if err != nil {
			validation = fmt.Errorf("检查卖点角度覆盖失败：%w", err)
			stageError = validation.Error()
		}
	}
	if validation == nil && len(coverageTargets) > 0 {
		reportAnchorAnalysis(ctx, 74, "正在补充事实支持的缺失角度")
		expansionUser := renderSellingPointWheelExpansionRequest(source.Facts, canonical, coverageTargets)
		expansionResult, expansionActual, expansionErr := s.completeAnchorStyleWorkbench(ctx, cfg.SellingPointProfileID, "selling_point_angle_expansion", agentgateway.ResponseJSON, []agentgateway.Message{{Role: "system", Content: expansionPrompt}, {Role: "user", Content: expansionUser}}, 7000, false)
		actual = append(actual, expansionActual...)
		totalLatencyMS += expansionResult.LatencyMS
		totalInputTokens += expansionResult.InputTokens
		totalOutputTokens += expansionResult.OutputTokens
		if expansionErr != nil {
			validation = fmt.Errorf("卖点轮盘稀疏角度补全失败：%w", expansionErr)
			stageError = validation.Error()
		} else {
			expansionRaw = expansionResult.Text
			finalResult = expansionResult
			expansionCanonical, expansionPoints, expansionAngles, expansionValidation := normalizeAnchorStyleSellingPointWheelJSON(expansionResult.Text)
			if expansionValidation == nil {
				expansionValidation = validateSellingPointPromptContract(expansionResult.Text, expansionPrompt)
			}
			if expansionValidation != nil {
				validation = fmt.Errorf("卖点轮盘稀疏角度补全返回无效：%w", expansionValidation)
				stageError = validation.Error()
				if expansionCanonical != "" {
					canonical = expansionCanonical
				}
			} else {
				canonical, points, angles = expansionCanonical, expansionPoints, expansionAngles
			}
		}
	}
	if validation == nil {
		finalReviewUser := renderSellingPointWheelFinalAuditRequest(source.Facts, canonical, nil)
		reportAnchorAnalysis(ctx, 86, "正在完成新卖点池的最后核对")
		finalReviewResult, finalReviewActual, finalReviewErr := s.completeAnchorStyleWorkbench(ctx, cfg.SellingPointProfileID, "selling_point_final_review", agentgateway.ResponseJSON, []agentgateway.Message{{Role: "system", Content: finalAuditPrompt}, {Role: "user", Content: finalReviewUser}}, 7000, false)
		actual = append(actual, finalReviewActual...)
		totalLatencyMS += finalReviewResult.LatencyMS
		totalInputTokens += finalReviewResult.InputTokens
		totalOutputTokens += finalReviewResult.OutputTokens
		if finalReviewErr != nil {
			validation = fmt.Errorf("卖点轮盘最终事实裁决失败：%w", finalReviewErr)
			stageError = validation.Error()
		} else {
			finalReviewRaw = finalReviewResult.Text
			finalResult = finalReviewResult
			finalCanonical, finalPoints, finalAngles, finalValidation := normalizeAnchorStyleSellingPointWheelJSON(finalReviewResult.Text)
			if finalValidation == nil {
				finalValidation = validateSellingPointPromptContract(finalReviewResult.Text, finalAuditPrompt)
			}
			if finalValidation != nil {
				validation = fmt.Errorf("卖点轮盘最终事实裁决返回无效：%w", finalValidation)
				stageError = validation.Error()
				if finalCanonical != "" {
					canonical = finalCanonical
				}
			} else {
				canonical, points, angles = finalCanonical, finalPoints, finalAngles
				boundaryViolations = sellingPointFactBoundaryViolations(canonical, source.Facts)
			}
		}
	}
	if validation == nil && len(boundaryViolations) > 0 {
		repairUser := renderSellingPointWheelFinalAuditRequest(source.Facts, canonical, boundaryViolations)
		repairResult, repairActual, repairErr := s.completeAnchorStyleWorkbench(ctx, cfg.SellingPointProfileID, "selling_point_final_boundary_repair", agentgateway.ResponseJSON, []agentgateway.Message{{Role: "system", Content: finalAuditPrompt}, {Role: "user", Content: repairUser}}, 7000, false)
		actual = append(actual, repairActual...)
		totalLatencyMS += repairResult.LatencyMS
		totalInputTokens += repairResult.InputTokens
		totalOutputTokens += repairResult.OutputTokens
		if repairErr != nil {
			validation = fmt.Errorf("卖点轮盘事实边界修复失败：%w", repairErr)
			stageError = validation.Error()
		} else {
			boundaryRepairRaw = repairResult.Text
			finalResult = repairResult
			repairCanonical, repairPoints, repairAngles, repairValidation := normalizeAnchorStyleSellingPointWheelJSON(repairResult.Text)
			if repairValidation == nil {
				repairValidation = validateSellingPointPromptContract(repairResult.Text, finalAuditPrompt)
			}
			if repairValidation != nil {
				validation = fmt.Errorf("卖点轮盘事实边界修复返回无效：%w", repairValidation)
				stageError = validation.Error()
				if repairCanonical != "" {
					canonical = repairCanonical
				}
			} else {
				canonical, points, angles = repairCanonical, repairPoints, repairAngles
				boundaryViolations = sellingPointFactBoundaryViolations(canonical, source.Facts)
				if len(boundaryViolations) > 0 {
					filteredCanonical, removed, filteredPoints, filteredAngles, filterErr := removeSellingPointFactBoundaryViolations(canonical, source.Facts)
					programRemovedBoundaryItems = removed
					if filterErr != nil {
						validation = fmt.Errorf("卖点轮盘仍含事实正文未提供的高风险表达且无法安全删除：%w", filterErr)
						stageError = validation.Error()
					} else {
						canonical, points, angles = filteredCanonical, filteredPoints, filteredAngles
						boundaryViolations = sellingPointFactBoundaryViolations(canonical, source.Facts)
					}
				}
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
	promptSnapshot := strings.Join([]string{prompt, "# 第二阶段：事实终审母版\n\n" + reviewPrompt, "# 稀疏角度补全母版\n\n" + expansionPrompt, "# 最终事实裁决母版\n\n" + finalAuditPrompt}, "\n\n---\n\n")
	promptSourceSnapshot := strings.Join([]string{promptSource, reviewPromptSource, expansionPromptSource, finalAuditPromptSource}, " | ")
	rawBundle, _ := json.MarshalIndent(map[string]any{
		"extraction_raw_response":        result.Text,
		"review_raw_response":            reviewRaw,
		"expansion_raw_response":         expansionRaw,
		"final_review_raw_response":      finalReviewRaw,
		"boundary_repair_raw_response":   boundaryRepairRaw,
		"coverage_targets":               coverageTargets,
		"boundary_violations":            boundaryViolations,
		"program_removed_boundary_items": programRemovedBoundaryItems,
		"pipeline_error":                 stageError,
	}, "", "  ")
	trace := traceAnchorStyleWorkbenchMessages(actual)
	wheel, err := s.store.SaveLiveSellingPointWheel(ctx, tenant, plan, actor, model.SaveLiveSellingPointWheelInput{Status: status, ExtractionPromptMD: promptSnapshot, PromptSource: promptSourceSnapshot, FactPackageSHA256: derivedHash(source.Facts), OriginalWheelJSON: canonical, RequestSystem: trace.System, RequestUser: trace.User, RawResponse: string(rawBundle), Provider: finalResult.Provider, Model: finalResult.Model, LatencyMS: totalLatencyMS, InputTokens: totalInputTokens, OutputTokens: totalOutputTokens})
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
	reportAnchorAnalysis(ctx, 100, notice)
	writeJSON(w, 200, map[string]any{"initialized": initialized, "partial": partial, "selling_points": points, "angles": angles, "notice": notice, "steps": steps})
}
