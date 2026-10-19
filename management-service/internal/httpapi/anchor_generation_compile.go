package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/anchorstylescore"
	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
	"strings"
	"unicode/utf8"
)

func (s *Server) compileProductionAnchorStyle(ctx context.Context, tenant, style int64, sample string) (model.LiveAgentPlanAnchorStyleProfile, agentgateway.Response, error) {
	profile := model.LiveAgentPlanAnchorStyleProfile{Dimensions: []model.LiveAgentPlanAnchorStyleDimension{}, ReusableRules: []string{}, CandidatePatterns: []string{}, ExcludedFromStyle: []string{}}
	limit := model.LiveAnchorSampleLimit
	if ctx.Value(fusionAnalysisKey{}) != nil {
		limit = 1000000
	}
	if n := utf8.RuneCountInString(sample); n < 100 || n > limit {
		return profile, agentgateway.Response{}, errors.New("单份主播话术须为100到10000字，多样本总量须在100万字以内")
	}
	settings, err := s.store.AnchorGenerationSettings(ctx)
	if err != nil {
		return profile, agentgateway.Response{}, err
	}
	if settings.StyleProfileID == "" || settings.ScorecardProfileID == "" {
		return profile, agentgateway.Response{}, errors.New("请先在系统设置配置风格提取与评分基准模型")
	}
	analysis, analysisSource, err := loadAnchorStyleAnalysisPrompt()
	if err != nil {
		return profile, agentgateway.Response{}, err
	}
	if ctx.Value(fusionAnalysisKey{}) != nil {
		analysis, analysisSource, err = loadReplaceablePromptAsset("", "configs/prompts/anchor-style/style-multi-sample-fusion.md", "综合风格融合")
		if err != nil {
			return profile, agentgateway.Response{}, err
		}
	}
	protocol, _, err := loadReplaceablePromptAsset("", "configs/prompts/anchor-style/style-extraction-integration.md", "风格集成协议")
	if err != nil {
		return profile, agentgateway.Response{}, err
	}
	reportAnchorAnalysis(ctx, 20, "正在分析口语、称呼、句式和情绪特征")
	ctx = withExchangeScope(ctx, appdb.ModelExchangeScope{TenantID: tenant, StyleID: style})
	productNames := s.anchorStyleProductNames(ctx, tenant, style)

	var metaMD string
	var result agentgateway.Response
	var monitoring anchorStyleWorkbenchLexicon

	if fusion := ctx.Value(fusionAnalysisKey{}); fusion != nil {
		// 多样本融合：单次调用输出综合风格
		data, _ := json.Marshal(map[string]any{"analysis_results": fusion})
		result, _, err = s.completeAnchorStyleWorkbench(ctx, settings.StyleProfileID, "style_analysis", agentgateway.ResponseJSON, []agentgateway.Message{{Role: "system", Content: analysis + "\n\n" + protocol}, {Role: "user", Content: string(data)}}, 6500, true)
		if err != nil {
			return profile, result, err
		}
		var compiled anchorStyleWorkbenchMetaOutput
		if json.Unmarshal([]byte(stripPolicyJSONFence(result.Text)), &compiled) != nil || strings.TrimSpace(compiled.MetaPromptMD) == "" {
			return profile, result, errors.New("风格返回未符合元提示词JSON协议，原始返回已保留")
		}
		for _, section := range []string{"【口语风格】"} {
			if !strings.Contains(compiled.MetaPromptMD, section) {
				return profile, result, errors.New("元提示词缺少必要风格章节，原始返回已保留")
			}
		}
		metaMD = compiled.MetaPromptMD
		monitoring = compiled.Monitoring
	} else {
		// 单样本：分步提取，程序逐维问、逐维答、合并成固定格式
		metaMD, result, err = s.extractAnchorStyleByDimensions(ctx, analysis, sample, settings.StyleProfileID)
		if err != nil {
			return profile, result, err
		}
	}
	// 品相关内容硬剥离：链接号/数字/价格用正则，商品名靠注入词表（单样本下无法跨品对比，只能确定性剥离）。
	metaMD, monitoring = stripAnchorStyleProductContent(metaMD, monitoring, productNames)
	monitor := buildAnchorStyleWorkbenchMonitor(normalizeAnchorStyleWorkbenchLexicon(monitoring, sample), sample)
	monitorJSON, _ := json.Marshal(monitor)
	reportAnchorAnalysis(ctx, 55, "语言特征已提取，正在建立对应评分基准")
	scoreMD, scoreSource, err := loadAnchorStyleScorecardPrompt()
	if err != nil {
		return profile, result, err
	}
	scoreProtocol, _, err := loadReplaceablePromptAsset("", "configs/prompts/anchor-style/scorecard-extraction-integration.md", "评分字段分工协议")
	if err != nil {
		return profile, result, err
	}
	authority, _, err := loadReplaceablePromptAsset("", "configs/prompts/anchor-style/scorecard-effective-style-integration.md", "综合风格评分分工")
	if err != nil {
		return profile, result, err
	}
	scoreData, _ := json.Marshal(map[string]string{"effective_style_analysis": metaMD, "sample_text": sample})
	baseline, _, err := s.completeAnchorStyleWorkbench(ctx, settings.ScorecardProfileID, "style_scorecard_extraction", agentgateway.ResponseJSON, []agentgateway.Message{{Role: "system", Content: scoreMD + "\n\n" + scoreProtocol + "\n\n" + authority}, {Role: "user", Content: string(scoreData)}}, 6500, false)
	if err != nil {
		return profile, result, err
	}
	canonical, err := anchorstylescore.Calibrate(stripPolicyJSONFence(baseline.Text), sample, false)
	if err != nil {
		return profile, result, err
	}
	reportAnchorAnalysis(ctx, 85, "正在按原始话术校对频次与权重，并保存分析结果")
	result.LatencyMS += baseline.LatencyMS
	profile.Summary = "主播元提示词已提取，评分基准已按原始样本程序校对并自动保存。"
	profile.Delivery = &model.LiveAnchorDeliverySpec{Version: "compiled-style-v1", MetaPromptMD: strings.TrimSpace(metaMD), Rulebook: strings.TrimSpace(metaMD), SourceSHA256: derivedHash(sample), SampleChars: utf8.RuneCountInString(sample), ScoringBaselineJSON: canonical, MonitoringJSON: string(monitorJSON), AnalysisPromptSource: analysisSource, ScorecardPromptSource: scoreSource}
	profile.ReusableRules = []string{metaMD}
	return profile, result, nil
}

// anchorStyleProductNames 收集主播风格绑定方案的商品名，作为品相关剥离词表。
// 除完整商品名外，还提取常见品类后缀词，覆盖样本里的简称（如"黄菜籽油"→"菜籽油"）。
func (s *Server) anchorStyleProductNames(ctx context.Context, tenant, style int64) []string {
	planIDs, err := s.store.ListLiveAnchorStylePlanIDs(ctx, tenant, style)
	if err != nil {
		return nil
	}
	names := map[string]bool{}
	for _, pid := range planIDs {
		links, err := s.store.ListLiveAgentPlanProductLinks(ctx, tenant, pid)
		if err != nil {
			continue
		}
		for _, l := range links {
			name := strings.TrimSpace(l.ProductName)
			if name == "" {
				continue
			}
			names[name] = true
			for _, suffix := range []string{"菜籽油", "菜油", "花生油", "大豆油", "橄榄油", "芝麻油", "食用油", "鸭子", "卤鸭", "白酒", "红酒", "茶叶", "大米", "面条", "米粉"} {
				if strings.Contains(name, suffix) {
					names[suffix] = true
				}
			}
		}
	}
	out := make([]string, 0, len(names))
	for n := range names {
		out = append(out, n)
	}
	return out
}
