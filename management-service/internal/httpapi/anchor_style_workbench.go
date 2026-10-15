package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"livecompanion/management/internal/agentgateway"
	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
)

// The style workbench is deliberately sessionless on the server. The browser
// owns the visible room state and sends it back on every step, which makes every
// model request reproducible and exportable during local tuning.

type anchorStyleWorkbenchModel struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Model     string `json:"model"`
	Protocol  string `json:"protocol"`
	Enabled   bool   `json:"enabled"`
	HasAPIKey bool   `json:"has_api_key"`
	IsDefault bool   `json:"is_default"`
}

type anchorStyleWorkbenchContext struct {
	PlanID                     int64                                 `json:"plan_id"`
	PlanName                   string                                `json:"plan_name"`
	FactPackage                string                                `json:"fact_package"`
	SellingPoints              []anchorStyleWorkbenchPoint           `json:"selling_points"`
	Models                     []anchorStyleWorkbenchModel           `json:"models"`
	DefaultGeneratorID         string                                `json:"default_generator_id,omitempty"`
	AnalysisProvider           string                                `json:"analysis_provider"`
	AnalysisModel              string                                `json:"analysis_model"`
	LogitBiasCapability        string                                `json:"logit_bias_capability"`
	SavedMetaPrompt            *anchorStyleWorkbenchMetaResponse     `json:"saved_meta_prompt,omitempty"`
	SellingPointPromptMD       string                                `json:"selling_point_prompt_md"`
	SellingPointPromptSource   string                                `json:"selling_point_prompt_source"`
	SavedSellingPointWheel     *anchorStyleSellingPointWheelResponse `json:"saved_selling_point_wheel,omitempty"`
	ScorecardPromptMD          string                                `json:"scorecard_prompt_md"`
	ScorecardPromptSource      string                                `json:"scorecard_prompt_source"`
	SavedScorecard             *anchorStyleScorecardResponse         `json:"saved_scorecard,omitempty"`
	DefaultScorecardCompilerID string                                `json:"default_scorecard_compiler_id"`
}

type anchorStyleWorkbenchPoint struct {
	Key      string   `json:"key"`
	Label    string   `json:"label"`
	Value    string   `json:"value"`
	Category string   `json:"category"`
	Angles   []string `json:"angles"`
}

type anchorStyleWorkbenchTrace struct {
	System string `json:"system"`
	User   string `json:"user"`
}

type anchorStyleWorkbenchTerm struct {
	Kind          string  `json:"kind"`
	Text          string  `json:"text"`
	SampleCount   int     `json:"sample_count"`
	SamplePer1000 float64 `json:"sample_per_1000"`
	Bias          float64 `json:"bias,omitempty"`
}

type anchorStyleWorkbenchMonitor struct {
	SampleChars      int                        `json:"sample_chars"`
	Terms            []anchorStyleWorkbenchTerm `json:"terms"`
	ForbiddenTerms   []string                   `json:"forbidden_terms"`
	SentenceCharsMin int                        `json:"sentence_chars_min"`
	SentenceCharsMax int                        `json:"sentence_chars_max"`
}

type anchorStyleWorkbenchMetaOutput struct {
	MetaPromptMD string                      `json:"meta_prompt_md"`
	Monitoring   anchorStyleWorkbenchLexicon `json:"monitoring"`
}

type anchorStyleWorkbenchLexicon struct {
	AudienceTerms    []string `json:"audience_terms"`
	SelfTerms        []string `json:"self_terms"`
	AudiencePronouns []string `json:"audience_pronouns"`
	Particles        []string `json:"particles"`
	Connectors       []string `json:"connectors"`
	Catchphrases     []string `json:"catchphrases"`
	ForbiddenTerms   []string `json:"forbidden_terms"`
	SentenceCharsMin int      `json:"sentence_chars_min"`
	SentenceCharsMax int      `json:"sentence_chars_max"`
}

type anchorStyleWorkbenchMetaResponse struct {
	AssetID              int64                       `json:"asset_id"`
	Status               string                      `json:"status"`
	MetaPromptMD         string                      `json:"meta_prompt_md"`
	OriginalMetaPromptMD string                      `json:"original_meta_prompt_md"`
	Monitor              anchorStyleWorkbenchMonitor `json:"monitor"`
	AnalysisPromptSource string                      `json:"analysis_prompt_source"`
	Request              anchorStyleWorkbenchTrace   `json:"request"`
	RawResponse          string                      `json:"raw_response"`
	Provider             string                      `json:"provider"`
	Model                string                      `json:"model"`
	LatencyMS            int64                       `json:"latency_ms"`
	InputTokens          int64                       `json:"input_tokens"`
	OutputTokens         int64                       `json:"output_tokens"`
	PersistedAt          time.Time                   `json:"persisted_at"`
	ConfirmedAt          *time.Time                  `json:"confirmed_at,omitempty"`
}

type anchorStyleSellingPointWheelItem struct {
	Name           string   `json:"卖点名"`
	TargetAudience *string  `json:"目标人群,omitempty"`
	PainPoint      *string  `json:"痛点,omitempty"`
	Angles         []string `json:"角度"`
}

type anchorStyleFactGapSuggestion struct {
	Topic       string   `json:"主题"`
	Scope       string   `json:"适用范围"`
	Category    string   `json:"建议分类"`
	MissingFact string   `json:"待补事实"`
	Value       string   `json:"补充价值"`
	FillPrompts []string `json:"填写提示"`
}

type anchorStyleSellingPointWheel struct {
	Items    []anchorStyleSellingPointWheelItem `json:"卖点列表"`
	FactGaps []anchorStyleFactGapSuggestion     `json:"事实补充建议"`
}

type anchorStyleSellingPointWheelResponse struct {
	AssetID            int64                     `json:"asset_id"`
	Status             string                    `json:"status"`
	FactPackageStale   bool                      `json:"fact_package_stale"`
	WheelJSON          string                    `json:"wheel_json"`
	OriginalWheelJSON  string                    `json:"original_wheel_json"`
	ExtractionPromptMD string                    `json:"extraction_prompt_md"`
	PromptSource       string                    `json:"prompt_source"`
	FactPackageSHA256  string                    `json:"fact_package_sha256"`
	Request            anchorStyleWorkbenchTrace `json:"request"`
	RawResponse        string                    `json:"raw_response"`
	Provider           string                    `json:"provider"`
	Model              string                    `json:"model"`
	LatencyMS          int64                     `json:"latency_ms"`
	InputTokens        int64                     `json:"input_tokens"`
	OutputTokens       int64                     `json:"output_tokens"`
	SellingPointCount  int                       `json:"selling_point_count"`
	CombinationCount   int                       `json:"combination_count"`
	PersistedAt        time.Time                 `json:"persisted_at"`
	ConfirmedAt        *time.Time                `json:"confirmed_at,omitempty"`
	ValidationError    string                    `json:"validation_error,omitempty"`
}

type anchorStyleWheelTask struct {
	Pointer        int    `json:"pointer"`
	Cycle          int    `json:"cycle"`
	Position       int    `json:"position"`
	Total          int    `json:"total"`
	SellingPoint   string `json:"selling_point"`
	Angle          string `json:"angle"`
	Key            string `json:"key"`
	TargetAudience string `json:"target_audience,omitempty"`
	PainPoint      string `json:"pain_point,omitempty"`
}

type anchorStyleWheelTestResponse struct {
	Task          anchorStyleWheelTask      `json:"task"`
	NextPointer   int                       `json:"next_pointer"`
	Request       anchorStyleWorkbenchTrace `json:"request"`
	RawResponse   string                    `json:"raw_response"`
	Provider      string                    `json:"provider"`
	Model         string                    `json:"model"`
	LatencyMS     int64                     `json:"latency_ms"`
	InputTokens   int64                     `json:"input_tokens"`
	OutputTokens  int64                     `json:"output_tokens"`
	MaxChars      int                       `json:"max_chars"`
	Unlimited     bool                      `json:"unlimited_chars"`
	ActualChars   int                       `json:"actual_chars"`
	ExceededLimit bool                      `json:"exceeded_limit"`
}

type anchorStyleWorkbenchState struct {
	Round            int      `json:"round"`
	UsedCombinations []string `json:"used_combinations"`
	History          []string `json:"history"`
	Feedback         []string `json:"feedback"`
}

type anchorStyleWorkbenchTask struct {
	Round             int    `json:"round"`
	SellingPointKey   string `json:"selling_point_key"`
	SellingPointLabel string `json:"selling_point_label"`
	SellingPointValue string `json:"selling_point_value"`
	Angle             string `json:"angle"`
	MainlinePhase     string `json:"mainline_phase"`
	BodyFunction      string `json:"body_function"`
	CTAIntensity      string `json:"cta_intensity"`
	ScarcityLevel     int    `json:"scarcity_level"`
	Callout           bool   `json:"callout"`
	Recap             bool   `json:"recap"`
}

type anchorStyleWorkbenchMetric struct {
	Kind          string  `json:"kind"`
	Text          string  `json:"text"`
	TargetPer1000 float64 `json:"target_per_1000"`
	ActualCount   int     `json:"actual_count"`
	ActualPer1000 float64 `json:"actual_per_1000"`
	Status        string  `json:"status"`
}

type anchorStyleWorkbenchStats struct {
	WindowChars int                          `json:"window_chars"`
	StyleScore  int                          `json:"style_score"`
	Metrics     []anchorStyleWorkbenchMetric `json:"metrics"`
	Forbidden   []string                     `json:"forbidden"`
}

type anchorStyleWorkbenchBias struct {
	Text string  `json:"text"`
	Bias float64 `json:"bias"`
}

type anchorStyleWorkbenchPostprocess struct {
	Enabled bool     `json:"enabled"`
	Changes []string `json:"changes"`
	Text    string   `json:"text"`
}

type anchorStyleWorkbenchStepResponse struct {
	DirectMode        bool                            `json:"direct_mode"`
	Task              anchorStyleWorkbenchTask        `json:"task"`
	Request           anchorStyleWorkbenchTrace       `json:"request"`
	RawResponse       string                          `json:"raw_response"`
	Postprocess       anchorStyleWorkbenchPostprocess `json:"postprocess"`
	LogitBias         []anchorStyleWorkbenchBias      `json:"logit_bias"`
	LogitBiasMode     string                          `json:"logit_bias_mode"`
	Stats             anchorStyleWorkbenchStats       `json:"stats"`
	NextFeedback      []string                        `json:"next_feedback"`
	State             anchorStyleWorkbenchState       `json:"state"`
	Provider          string                          `json:"provider"`
	Model             string                          `json:"model"`
	LatencyMS         int64                           `json:"latency_ms"`
	InputTokens       int64                           `json:"input_tokens"`
	OutputTokens      int64                           `json:"output_tokens"`
	TargetChars       int                             `json:"target_chars"`
	ActualRawChars    int                             `json:"actual_raw_chars"`
	ActualOutputChars int                             `json:"actual_output_chars"`
}

func (s *Server) anchorStyleWorkbenchContext(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	planID, ok := liveAgentPlanPathID(w, r)
	if !ok {
		return
	}
	tenantID, ok := s.resolveLiveAgentPlanTenant(w, r, actor, requestTenantID(r), false)
	if !ok {
		return
	}
	plan, facts, benefits, links, ok := s.anchorStyleWorkbenchPlanData(w, r, tenantID, planID)
	if !ok {
		return
	}
	config, err := s.store.SpeechModels(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取模型配置失败")
		return
	}
	models := make([]anchorStyleWorkbenchModel, 0, len(config.Profiles))
	for _, item := range config.Profiles {
		models = append(models, anchorStyleWorkbenchModel{ID: item.ID, Name: item.Name, Model: item.Model, Protocol: item.Protocol, Enabled: item.Enabled, HasAPIKey: item.HasAPIKey, IsDefault: item.ID == config.DefaultID})
	}
	analysisProvider := strings.TrimSpace(os.Getenv("ANCHOR_ANALYSIS_PROVIDER"))
	if analysisProvider == "" {
		analysisProvider = strings.TrimSpace(os.Getenv("AGENT_PROVIDER"))
	}
	if analysisProvider == "" {
		analysisProvider = agentgateway.ProviderQwen
	}
	analysisModel := strings.TrimSpace(os.Getenv("ANCHOR_ANALYSIS_MODEL"))
	if analysisModel == "" {
		analysisModel = strings.TrimSpace(os.Getenv("LIVE_AGENT_MODEL"))
	}
	if analysisModel == "" && analysisProvider != "compatible" {
		analysisModel = agentgateway.DefaultQwenModel
	}
	factPackage := renderAnchorStyleWorkbenchFacts(plan, facts, benefits, links)
	sellingPrompt, sellingPromptSource, err := loadSellingPointExtractionPrompt()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	var savedMeta *anchorStyleWorkbenchMetaResponse
	asset, savedErr := s.store.GetActiveLiveAnchorStyleMetaPrompt(r.Context(), tenantID, planID)
	if savedErr == nil {
		mapped := anchorStyleWorkbenchMetaResponseFromAsset(asset)
		savedMeta = &mapped
	} else if !errors.Is(savedErr, appdb.ErrLiveAnchorStyleMetaPromptNotFound) {
		writeError(w, http.StatusInternalServerError, "读取已保存风格元提示词失败")
		return
	}
	var savedWheel *anchorStyleSellingPointWheelResponse
	wheelAsset, wheelErr := s.store.GetActiveLiveSellingPointWheel(r.Context(), tenantID, planID)
	if wheelErr == nil {
		mapped, mapErr := anchorStyleSellingPointWheelResponseFromAsset(wheelAsset)
		if mapErr != nil {
			writeError(w, http.StatusInternalServerError, "已保存卖点轮盘格式无效")
			return
		}
		mapped.FactPackageStale = wheelAsset.FactPackageSHA256 != derivedHash(factPackage)
		savedWheel = &mapped
	} else if !errors.Is(wheelErr, appdb.ErrLiveSellingPointWheelNotFound) {
		writeError(w, http.StatusInternalServerError, "读取已保存卖点轮盘失败")
		return
	}
	scorecardPrompt, scorecardSource, err := loadAnchorStyleScorecardPrompt()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	var savedScorecard *anchorStyleScorecardResponse
	card, cardErr := s.store.GetActiveLiveAnchorStyleScorecard(r.Context(), tenantID, planID)
	if cardErr == nil && savedMeta != nil && card.MetaPromptAssetID == savedMeta.AssetID {
		mapped := anchorStyleScorecardResponseFromAsset(card)
		savedScorecard = &mapped
	} else if cardErr != nil && !errors.Is(cardErr, appdb.ErrLiveAnchorStyleScorecardNotFound) {
		writeError(w, http.StatusInternalServerError, "读取主播评分基准失败")
		return
	}
	writeJSON(w, http.StatusOK, anchorStyleWorkbenchContext{
		PlanID: plan.ID, PlanName: plan.Name,
		FactPackage:   factPackage,
		SellingPoints: compileAnchorStyleWorkbenchPoints(facts, benefits, links),
		Models:        models, DefaultGeneratorID: config.DefaultID,
		AnalysisProvider:     analysisProvider,
		AnalysisModel:        analysisModel,
		LogitBiasCapability:  "lexical_prompt_guidance",
		SavedMetaPrompt:      savedMeta,
		SellingPointPromptMD: sellingPrompt, SellingPointPromptSource: sellingPromptSource,
		SavedSellingPointWheel: savedWheel,
		ScorecardPromptMD:      scorecardPrompt, ScorecardPromptSource: scorecardSource, SavedScorecard: savedScorecard,
		DefaultScorecardCompilerID: defaultAnchorStyleScorecardCompiler(models),
	})
}

func (s *Server) anchorStyleWorkbenchMeta(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	planID, ok := liveAgentPlanPathID(w, r)
	if !ok {
		return
	}
	var input struct {
		TenantID        int64  `json:"tenant_id,omitempty"`
		SampleText      string `json:"sample_text"`
		Scene           string `json:"scene,omitempty"`
		CompilerProfile string `json:"compiler_profile_id,omitempty"`
	}
	if readJSON(w, r, &input) != nil {
		writeError(w, http.StatusBadRequest, "样本请求格式错误")
		return
	}
	input.SampleText = strings.TrimSpace(input.SampleText)
	if utf8.RuneCountInString(input.SampleText) < 100 || utf8.RuneCountInString(input.SampleText) > 60000 {
		writeError(w, http.StatusBadRequest, "主播样本须为100到60000字")
		return
	}
	input.Scene = strings.TrimSpace(input.Scene)
	if utf8.RuneCountInString(input.Scene) > 80 {
		writeError(w, http.StatusBadRequest, "生成场景最多80字")
		return
	}
	tenantID, ok := s.resolveLiveAgentPlanTenant(w, r, actor, input.TenantID, false)
	if !ok {
		return
	}
	if _, err := s.store.GetLiveAgentPlan(r.Context(), tenantID, planID); err != nil {
		writeError(w, http.StatusNotFound, "直播方案不存在")
		return
	}
	basePrompt, promptSource, err := loadAnchorStyleAnalysisPrompt()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	systemPrompt := `你是主播口播风格编译器。必须完整执行用户提供的Markdown分析方法；只分析主播怎么说话，不继承样本商品事实。你只运行这一次，产物将长期交给另一个生成模型使用。`
	userPrompt, err := renderAnchorStyleAnalysisRequest(basePrompt, promptSource, input.Scene, input.SampleText)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	messages := []agentgateway.Message{{Role: "system", Content: systemPrompt}, {Role: "user", Content: userPrompt}}
	result, actual, err := s.completeAnchorStyleWorkbench(r.Context(), input.CompilerProfile, "style_analysis", agentgateway.ResponseJSON, messages, 4200, true)
	if err != nil {
		writeError(w, http.StatusBadGateway, "生成元提示词失败："+err.Error())
		return
	}
	var compiled anchorStyleWorkbenchMetaOutput
	raw := strings.TrimSpace(result.Text)
	if json.Unmarshal([]byte(stripPolicyJSONFence(raw)), &compiled) != nil || strings.TrimSpace(compiled.MetaPromptMD) == "" {
		compiled.MetaPromptMD = raw
	}
	compiled.Monitoring = normalizeAnchorStyleWorkbenchLexicon(compiled.Monitoring, input.SampleText)
	monitor := buildAnchorStyleWorkbenchMonitor(compiled.Monitoring, input.SampleText)
	monitorJSON, _ := json.Marshal(monitor)
	trace := traceAnchorStyleWorkbenchMessages(actual)
	asset, err := s.store.SaveLiveAnchorStyleMetaPrompt(r.Context(), tenantID, planID, actor.UserID, model.SaveLiveAnchorStyleMetaPromptInput{
		Scene: input.Scene, SampleSHA256: fmt.Sprintf("%x", sha256.Sum256([]byte(input.SampleText))),
		OriginalMetaPromptMD: strings.TrimSpace(compiled.MetaPromptMD), MonitorJSON: string(monitorJSON),
		AnalysisPromptSource: promptSource, RequestSystem: trace.System, RequestUser: trace.User,
		RawResponse: raw, Provider: result.Provider, Model: result.Model, LatencyMS: result.LatencyMS,
		InputTokens: result.InputTokens, OutputTokens: result.OutputTokens,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "元提示词已生成，但落库保存失败："+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, anchorStyleWorkbenchMetaResponseFromAsset(asset))
}

func (s *Server) anchorStyleWorkbenchMetaConfirm(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	planID, ok := liveAgentPlanPathID(w, r)
	if !ok {
		return
	}
	assetID, ok := liveAnchorStylePathID(w, r, "assetID", "风格元提示词资产")
	if !ok {
		return
	}
	var input struct {
		TenantID     int64  `json:"tenant_id,omitempty"`
		MetaPromptMD string `json:"meta_prompt_md"`
	}
	if readJSON(w, r, &input) != nil {
		writeError(w, http.StatusBadRequest, "确认元提示词请求格式错误")
		return
	}
	input.MetaPromptMD = strings.TrimSpace(input.MetaPromptMD)
	if input.MetaPromptMD == "" || utf8.RuneCountInString(input.MetaPromptMD) > 30000 {
		writeError(w, http.StatusBadRequest, "元提示词须为1到30000字")
		return
	}
	tenantID, ok := s.resolveLiveAgentPlanTenant(w, r, actor, input.TenantID, false)
	if !ok {
		return
	}
	asset, err := s.store.ConfirmLiveAnchorStyleMetaPrompt(r.Context(), tenantID, planID, assetID, actor.UserID, input.MetaPromptMD)
	if errors.Is(err, appdb.ErrLiveAnchorStyleMetaPromptNotFound) {
		writeError(w, http.StatusConflict, "这份元提示词已不是当前生效版本，请刷新后重试")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "保存确认后的元提示词失败")
		return
	}
	writeJSON(w, http.StatusOK, anchorStyleWorkbenchMetaResponseFromAsset(asset))
}

func (s *Server) anchorStyleWorkbenchMetaManualConfirm(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	planID, ok := liveAgentPlanPathID(w, r)
	if !ok {
		return
	}
	var input struct {
		TenantID     int64  `json:"tenant_id,omitempty"`
		Scene        string `json:"scene,omitempty"`
		MetaPromptMD string `json:"meta_prompt_md"`
	}
	if readJSON(w, r, &input) != nil {
		writeError(w, http.StatusBadRequest, "手动加载元提示词请求格式错误")
		return
	}
	input.Scene = strings.TrimSpace(input.Scene)
	input.MetaPromptMD = strings.TrimSpace(input.MetaPromptMD)
	if input.MetaPromptMD == "" || utf8.RuneCountInString(input.MetaPromptMD) > 30000 {
		writeError(w, http.StatusBadRequest, "元提示词须为1到30000字")
		return
	}
	if utf8.RuneCountInString(input.Scene) > 80 {
		writeError(w, http.StatusBadRequest, "生成场景最多80字")
		return
	}
	tenantID, ok := s.resolveLiveAgentPlanTenant(w, r, actor, input.TenantID, true)
	if !ok {
		return
	}
	if _, err := s.store.GetLiveAgentPlan(r.Context(), tenantID, planID); err != nil {
		writeError(w, http.StatusNotFound, "直播方案不存在")
		return
	}
	monitor := anchorStyleWorkbenchMonitor{Terms: []anchorStyleWorkbenchTerm{}, ForbiddenTerms: []string{}}
	monitorJSON, _ := json.Marshal(monitor)
	asset, err := s.store.SaveLiveAnchorStyleMetaPrompt(r.Context(), tenantID, planID, actor.UserID, model.SaveLiveAnchorStyleMetaPromptInput{
		Scene: input.Scene, SampleSHA256: fmt.Sprintf("%x", sha256.Sum256([]byte(input.MetaPromptMD))),
		OriginalMetaPromptMD: input.MetaPromptMD, MonitorJSON: string(monitorJSON),
		AnalysisPromptSource: "manual-import", RawResponse: input.MetaPromptMD,
		Provider: "manual", Model: "manual-import",
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "手动元提示词保存失败："+err.Error())
		return
	}
	asset, err = s.store.ConfirmLiveAnchorStyleMetaPrompt(r.Context(), tenantID, planID, asset.ID, actor.UserID, input.MetaPromptMD)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "手动元提示词确认失败："+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, anchorStyleWorkbenchMetaResponseFromAsset(asset))
}

func anchorStyleWorkbenchMetaResponseFromAsset(asset model.LiveAnchorStyleMetaPromptAsset) anchorStyleWorkbenchMetaResponse {
	monitor := anchorStyleWorkbenchMonitor{Terms: []anchorStyleWorkbenchTerm{}, ForbiddenTerms: []string{}}
	if strings.TrimSpace(asset.MonitorJSON) != "" {
		_ = json.Unmarshal([]byte(asset.MonitorJSON), &monitor)
	}
	return anchorStyleWorkbenchMetaResponse{
		AssetID: asset.ID, Status: asset.Status,
		MetaPromptMD: asset.EffectiveMetaPromptMD(), OriginalMetaPromptMD: asset.OriginalMetaPromptMD,
		Monitor: monitor, AnalysisPromptSource: asset.AnalysisPromptSource,
		Request:     anchorStyleWorkbenchTrace{System: asset.RequestSystem, User: asset.RequestUser},
		RawResponse: asset.RawResponse, Provider: asset.Provider, Model: asset.Model,
		LatencyMS: asset.LatencyMS, InputTokens: asset.InputTokens, OutputTokens: asset.OutputTokens,
		PersistedAt: asset.CreatedAt, ConfirmedAt: asset.ConfirmedAt,
	}
}

func (s *Server) anchorStyleWorkbenchSellingPointWheelCompile(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	planID, ok := liveAgentPlanPathID(w, r)
	if !ok {
		return
	}
	var input struct {
		TenantID        int64  `json:"tenant_id,omitempty"`
		PromptMD        string `json:"prompt_md,omitempty"`
		PromptMode      string `json:"prompt_mode,omitempty"`
		CompilerProfile string `json:"compiler_profile_id,omitempty"`
	}
	if readJSON(w, r, &input) != nil {
		writeError(w, http.StatusBadRequest, "卖点轮盘生成请求格式错误")
		return
	}
	tenantID, ok := s.resolveLiveAgentPlanTenant(w, r, actor, input.TenantID, true)
	if !ok {
		return
	}
	plan, facts, benefits, links, ok := s.anchorStyleWorkbenchPlanData(w, r, tenantID, planID)
	if !ok {
		return
	}
	if len(facts)+len(benefits)+len(links) == 0 {
		writeError(w, http.StatusConflict, "当前方案没有可用于挖掘卖点的正式事实")
		return
	}
	promptMD, promptSource, err := resolveRequestPrompt(input.PromptMode, input.PromptMD, loadSellingPointExtractionPrompt)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	promptMD, err = injectCategorySellingDimensions(promptMD, "")
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if utf8.RuneCountInString(promptMD) < 100 || utf8.RuneCountInString(promptMD) > 60000 {
		writeError(w, http.StatusBadRequest, "卖点挖掘提示词须为100到60000字")
		return
	}
	factPackage := renderAnchorStyleWorkbenchFacts(plan, facts, benefits, links)
	userPrompt := renderSellingPointExtractionRequest(promptMD, factPackage)
	messages := []agentgateway.Message{{Role: "user", Content: userPrompt}}
	result, actual, err := s.completeAnchorStyleWorkbench(r.Context(), input.CompilerProfile, "selling_point_extraction", agentgateway.ResponseJSON, messages, 7000, false)
	if err != nil {
		writeError(w, http.StatusBadGateway, "生成卖点轮盘失败："+err.Error())
		return
	}
	raw := result.Text
	wheelJSON, _, _, err := normalizeAnchorStyleSellingPointWheelJSON(raw)
	validation := validateSellingPointPromptContract(raw, promptMD)
	status := "draft"
	if err != nil || validation != nil {
		status = "validation_failed"
		if wheelJSON == "" {
			wheelJSON = stripPolicyJSONFence(raw)
		}
	}
	trace := traceAnchorStyleWorkbenchMessages(actual)
	asset, err := s.store.SaveLiveSellingPointWheel(r.Context(), tenantID, planID, actor.UserID, model.SaveLiveSellingPointWheelInput{
		Status:             status,
		ExtractionPromptMD: promptMD, PromptSource: promptSource,
		FactPackageSHA256: fmt.Sprintf("%x", sha256.Sum256([]byte(factPackage))), OriginalWheelJSON: wheelJSON,
		RequestSystem: trace.System, RequestUser: trace.User, RawResponse: raw,
		Provider: result.Provider, Model: result.Model, LatencyMS: result.LatencyMS,
		InputTokens: result.InputTokens, OutputTokens: result.OutputTokens,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "卖点轮盘已生成，但保存失败："+err.Error())
		return
	}
	response, err := anchorStyleSellingPointWheelResponseFromAsset(asset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取卖点轮盘失败")
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) anchorStyleWorkbenchSellingPointWheelConfirm(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	planID, ok := liveAgentPlanPathID(w, r)
	if !ok {
		return
	}
	assetID, ok := liveAnchorStylePathID(w, r, "assetID", "卖点轮盘资产")
	if !ok {
		return
	}
	var input struct {
		TenantID  int64  `json:"tenant_id,omitempty"`
		WheelJSON string `json:"wheel_json"`
	}
	if readJSON(w, r, &input) != nil {
		writeError(w, http.StatusBadRequest, "确认卖点轮盘请求格式错误")
		return
	}
	wheelJSON, _, _, err := normalizeAnchorStyleSellingPointWheelJSON(input.WheelJSON)
	if err != nil {
		writeError(w, http.StatusBadRequest, "卖点轮盘JSON无效："+err.Error())
		return
	}
	tenantID, ok := s.resolveLiveAgentPlanTenant(w, r, actor, input.TenantID, true)
	if !ok {
		return
	}
	existing, err := s.store.GetLiveSellingPointWheel(r.Context(), tenantID, planID, assetID)
	if err != nil {
		writeError(w, 409, "卖点轮盘资产不存在或已变化")
		return
	}
	if err = validateSellingPointPromptContract(input.WheelJSON, existing.ExtractionPromptMD); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	asset, err := s.store.ConfirmLiveSellingPointWheel(r.Context(), tenantID, planID, assetID, actor.UserID, wheelJSON)
	if errors.Is(err, appdb.ErrLiveSellingPointWheelNotFound) {
		writeError(w, http.StatusConflict, "这份卖点轮盘已不是当前版本，请刷新后重试")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "保存卖点轮盘失败")
		return
	}
	response, err := anchorStyleSellingPointWheelResponseFromAsset(asset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取卖点轮盘失败")
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func normalizeAnchorStyleSellingPointWheelJSON(raw string) (string, int, int, error) {
	raw = strings.TrimSpace(stripPolicyJSONFence(raw))
	var top map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &top); err != nil {
		return "", 0, 0, errors.New("不是合法JSON")
	}
	if top["卖点列表"] == nil {
		return "", 0, 0, errors.New("顶层必须包含“卖点列表”")
	}
	for key := range top {
		if key != "卖点列表" && key != "事实补充建议" {
			return "", 0, 0, errors.New("顶层只能包含“卖点列表”和“事实补充建议”")
		}
	}
	var rawItems []json.RawMessage
	if err := json.Unmarshal(top["卖点列表"], &rawItems); err != nil {
		return "", 0, 0, errors.New("“卖点列表”必须是数组")
	}
	if len(rawItems) == 0 || len(rawItems) > 40 {
		return "", 0, 0, errors.New("卖点数量须为1到40个")
	}
	result := anchorStyleSellingPointWheel{
		Items:    make([]anchorStyleSellingPointWheelItem, 0, len(rawItems)),
		FactGaps: make([]anchorStyleFactGapSuggestion, 0),
	}
	seenNames := map[string]bool{}
	combinations := 0
	for _, rawItem := range rawItems {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(rawItem, &fields); err != nil {
			return "", 0, 0, errors.New("每个卖点字段无效")
		}
		// 容错：模型偶尔把「角度」写成英文 "angle"，程序归一为「角度」。
		// 字段名由程序维护，模型只负责输出内容，不因中英文字段名混用判失败。
		if fields["角度"] == nil {
			if alt, ok := fields["angle"]; ok {
				fields["角度"] = alt
				delete(fields, "angle")
			}
		}
		if fields["卖点名"] == nil || fields["角度"] == nil {
			return "", 0, 0, errors.New("每个卖点须包含“卖点名”和“角度”")
		}
		for key := range fields {
			if key != "卖点名" && key != "角度" && key != "目标人群" && key != "痛点" {
				return "", 0, 0, errors.New("每个卖点只允许卖点名、目标人群、痛点、角度")
			}
		}
		for _, key := range []string{"目标人群", "痛点"} {
			if value, ok := fields[key]; ok {
				var str string
				if string(value) == "null" || json.Unmarshal(value, &str) != nil || utf8.RuneCountInString(str) > 200 {
					return "", 0, 0, errors.New(key + "须为不超过200字的字符串")
				}
			}
		}
		normalized, _ := json.Marshal(fields)
		var item anchorStyleSellingPointWheelItem
		if err := json.Unmarshal(normalized, &item); err != nil {
			return "", 0, 0, errors.New("卖点字段类型无效")
		}
		item.Name = strings.TrimSpace(item.Name)
		if item.TargetAudience != nil {
			value := strings.TrimSpace(*item.TargetAudience)
			item.TargetAudience = &value
		}
		if item.PainPoint != nil {
			value := strings.TrimSpace(*item.PainPoint)
			item.PainPoint = &value
		}
		if item.Name == "" || utf8.RuneCountInString(item.Name) > 40 || seenNames[item.Name] {
			return "", 0, 0, errors.New("卖点名为空、重复或超过40字")
		}
		seenNames[item.Name] = true
		angles := make([]string, 0, len(item.Angles))
		seenAngles := map[string]bool{}
		for _, angle := range item.Angles {
			angle = strings.TrimSpace(angle)
			if angle == "" || seenAngles[angle] {
				continue
			}
			if utf8.RuneCountInString(angle) > 120 {
				return "", 0, 0, errors.New("单个角度不能超过120字")
			}
			seenAngles[angle] = true
			angles = append(angles, angle)
		}
		if len(angles) == 0 || len(angles) > 20 {
			return "", 0, 0, errors.New("每个卖点须包含1到20个不同角度")
		}
		item.Angles = angles
		combinations += len(angles)
		result.Items = append(result.Items, item)
	}
	if rawGaps, ok := top["事实补充建议"]; ok {
		var gaps []json.RawMessage
		if err := json.Unmarshal(rawGaps, &gaps); err != nil {
			return "", 0, 0, errors.New("“事实补充建议”必须是数组")
		}
		if len(gaps) > 20 {
			return "", 0, 0, errors.New("事实补充建议不能超过20条")
		}
		seenGaps := map[string]bool{}
		categories, _, err := loadFactCategoryCatalog()
		if err != nil {
			return "", 0, 0, err
		}
		for index, rawGap := range gaps {
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(rawGap, &fields); err != nil {
				return "", 0, 0, fmt.Errorf("第%d条事实补充建议不是合法对象", index+1)
			}
			for _, key := range []string{"主题", "适用范围", "建议分类", "待补事实", "补充价值", "填写提示"} {
				if fields[key] == nil {
					return "", 0, 0, fmt.Errorf("第%d条事实补充建议缺少“%s”", index+1, key)
				}
			}
			for key := range fields {
				if key != "主题" && key != "适用范围" && key != "建议分类" && key != "待补事实" && key != "补充价值" && key != "填写提示" {
					return "", 0, 0, errors.New("事实补充建议包含未知字段：“" + key + "”")
				}
			}
			var gap anchorStyleFactGapSuggestion
			if err := json.Unmarshal(rawGap, &gap); err != nil {
				return "", 0, 0, fmt.Errorf("第%d条事实补充建议字段类型无效", index+1)
			}
			gap.Topic = strings.TrimSpace(gap.Topic)
			gap.Scope = strings.TrimSpace(gap.Scope)
			gap.Category = strings.TrimSpace(gap.Category)
			gap.MissingFact = strings.TrimSpace(gap.MissingFact)
			gap.Value = strings.TrimSpace(gap.Value)
			if gap.Topic == "" || gap.Scope == "" || gap.MissingFact == "" || gap.Value == "" {
				return "", 0, 0, fmt.Errorf("第%d条事实补充建议含空字段", index+1)
			}
			if utf8.RuneCountInString(gap.Topic) > 80 || utf8.RuneCountInString(gap.Scope) > 120 || utf8.RuneCountInString(gap.MissingFact) > 120 || utf8.RuneCountInString(gap.Value) > 300 {
				return "", 0, 0, fmt.Errorf("第%d条事实补充建议文字过长", index+1)
			}
			if !categories.contains(gap.Category) {
				return "", 0, 0, fmt.Errorf("第%d条事实补充建议的分类无效", index+1)
			}
			prompts := make([]string, 0, len(gap.FillPrompts))
			seenPrompts := map[string]bool{}
			for _, prompt := range gap.FillPrompts {
				prompt = strings.TrimSpace(prompt)
				if prompt == "" || seenPrompts[prompt] {
					continue
				}
				if utf8.RuneCountInString(prompt) > 200 {
					return "", 0, 0, fmt.Errorf("第%d条事实补充建议的填写提示过长", index+1)
				}
				seenPrompts[prompt] = true
				prompts = append(prompts, prompt)
			}
			if len(prompts) == 0 || len(prompts) > 5 {
				return "", 0, 0, fmt.Errorf("第%d条事实补充建议须包含1至5个填写提示", index+1)
			}
			gap.FillPrompts = prompts
			gapKey := gap.Category + "\x00" + gap.MissingFact
			if seenGaps[gapKey] {
				continue
			}
			seenGaps[gapKey] = true
			result.FactGaps = append(result.FactGaps, gap)
		}
	}
	canonical, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return "", 0, 0, err
	}
	return string(canonical), len(result.Items), combinations, nil
}

func anchorStyleSellingPointWheelResponseFromAsset(asset model.LiveSellingPointWheelAsset) (anchorStyleSellingPointWheelResponse, error) {
	wheelJSON, points, combinations, err := normalizeAnchorStyleSellingPointWheelJSON(asset.EffectiveWheelJSON())
	validationError := ""
	if err != nil {
		validationError = err.Error()
		wheelJSON = asset.EffectiveWheelJSON()
	}
	if err := validateSellingPointPromptContract(asset.EffectiveWheelJSON(), asset.ExtractionPromptMD); err != nil {
		validationError = err.Error()
	}
	return anchorStyleSellingPointWheelResponse{
		ValidationError: validationError,
		AssetID:         asset.ID, Status: asset.Status, WheelJSON: wheelJSON,
		OriginalWheelJSON: asset.OriginalWheelJSON, ExtractionPromptMD: asset.ExtractionPromptMD,
		PromptSource: asset.PromptSource, FactPackageSHA256: asset.FactPackageSHA256,
		Request:     anchorStyleWorkbenchTrace{System: asset.RequestSystem, User: asset.RequestUser},
		RawResponse: asset.RawResponse, Provider: asset.Provider, Model: asset.Model,
		LatencyMS: asset.LatencyMS, InputTokens: asset.InputTokens, OutputTokens: asset.OutputTokens,
		SellingPointCount: points, CombinationCount: combinations,
		PersistedAt: asset.CreatedAt, ConfirmedAt: asset.ConfirmedAt,
	}, nil
}

func (s *Server) anchorStyleWorkbenchSellingPointWheelTest(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	planID, ok := liveAgentPlanPathID(w, r)
	if !ok {
		return
	}
	var input struct {
		TenantID         int64  `json:"tenant_id,omitempty"`
		Pointer          int    `json:"pointer"`
		MaxChars         int    `json:"max_chars"`
		UnlimitedChars   bool   `json:"unlimited_chars"`
		GeneratorProfile string `json:"generator_profile_id,omitempty"`
	}
	if readJSON(w, r, &input) != nil {
		writeError(w, http.StatusBadRequest, "卖点轮盘测试请求格式错误")
		return
	}
	if input.Pointer < 0 {
		writeError(w, http.StatusBadRequest, "轮盘指针不能小于0")
		return
	}
	if !input.UnlimitedChars && input.MaxChars == 0 {
		input.MaxChars = 300
	}
	if !input.UnlimitedChars && (input.MaxChars < 80 || input.MaxChars > 1000) {
		writeError(w, http.StatusBadRequest, "单段最大字数须为80到1000字")
		return
	}
	if input.UnlimitedChars {
		input.MaxChars = 0
	}
	tenantID, ok := s.resolveLiveAgentPlanTenant(w, r, actor, input.TenantID, false)
	if !ok {
		return
	}
	meta, err := s.store.GetActiveLiveAnchorStyleMetaPrompt(r.Context(), tenantID, planID)
	if errors.Is(err, appdb.ErrLiveAnchorStyleMetaPromptNotFound) {
		writeError(w, http.StatusConflict, "请先确认并保存当前方案的主播风格元提示词")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取主播风格元提示词失败")
		return
	}
	if meta.Status != "confirmed" {
		writeError(w, http.StatusConflict, "请先确认并保存当前方案的主播风格元提示词")
		return
	}
	wheelAsset, err := s.store.GetActiveLiveSellingPointWheel(r.Context(), tenantID, planID)
	if errors.Is(err, appdb.ErrLiveSellingPointWheelNotFound) {
		writeError(w, http.StatusConflict, "请先确认并保存当前方案的卖点轮盘")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取卖点轮盘失败")
		return
	}
	if wheelAsset.Status != "confirmed" {
		writeError(w, http.StatusConflict, "请先确认并保存当前方案的卖点轮盘")
		return
	}
	_, _, _, err = normalizeAnchorStyleSellingPointWheelJSON(wheelAsset.EffectiveWheelJSON())
	if err != nil {
		writeError(w, http.StatusConflict, "当前卖点轮盘格式无效")
		return
	}
	var wheel anchorStyleSellingPointWheel
	if json.Unmarshal([]byte(wheelAsset.EffectiveWheelJSON()), &wheel) != nil {
		writeError(w, http.StatusConflict, "当前卖点轮盘无法解析")
		return
	}
	tasks := flattenAnchorStyleWheelTasks(wheel)
	if len(tasks) == 0 {
		writeError(w, http.StatusConflict, "当前卖点轮盘没有可测试组合")
		return
	}
	position := input.Pointer % len(tasks)
	task := tasks[position]
	task.Pointer = input.Pointer
	task.Cycle = input.Pointer/len(tasks) + 1
	task.Position = position + 1
	task.Total = len(tasks)
	plan, facts, benefits, links, ok := s.anchorStyleWorkbenchPlanData(w, r, tenantID, planID)
	if !ok {
		return
	}
	factPackage := renderAnchorStyleWorkbenchFacts(plan, facts, benefits, links)
	userPrompt := renderAnchorStyleWheelTestPrompt(meta.EffectiveMetaPromptMD(), factPackage, task, input.MaxChars)
	messages := []agentgateway.Message{{Role: "user", Content: userPrompt}}
	maxTokens := max(512, input.MaxChars*3)
	if input.UnlimitedChars {
		maxTokens = 4000
	}
	result, actual, err := s.completeAnchorStyleWorkbench(r.Context(), input.GeneratorProfile, "speech_generation", agentgateway.ResponseText, messages, maxTokens, false)
	if err != nil {
		writeError(w, http.StatusBadGateway, "轮盘单格生成失败："+err.Error())
		return
	}
	raw := strings.TrimSpace(result.Text)
	if raw == "" {
		writeError(w, http.StatusBadGateway, "模型返回了空文本")
		return
	}
	actualChars := utf8.RuneCountInString(raw)
	writeJSON(w, http.StatusOK, anchorStyleWheelTestResponse{
		Task: task, NextPointer: input.Pointer + 1, Request: traceAnchorStyleWorkbenchMessages(actual),
		RawResponse: raw, Provider: result.Provider, Model: result.Model, LatencyMS: result.LatencyMS,
		InputTokens: result.InputTokens, OutputTokens: result.OutputTokens,
		MaxChars: input.MaxChars, Unlimited: input.UnlimitedChars, ActualChars: actualChars, ExceededLimit: !input.UnlimitedChars && actualChars > input.MaxChars,
	})
}

func flattenAnchorStyleWheelTasks(wheel anchorStyleSellingPointWheel) []anchorStyleWheelTask {
	maxAngles := 0
	for _, item := range wheel.Items {
		if len(item.Angles) > maxAngles {
			maxAngles = len(item.Angles)
		}
	}
	tasks := make([]anchorStyleWheelTask, 0)
	for angleIndex := 0; angleIndex < maxAngles; angleIndex++ {
		for _, item := range wheel.Items {
			if angleIndex >= len(item.Angles) {
				continue
			}
			point, angle := strings.TrimSpace(item.Name), strings.TrimSpace(item.Angles[angleIndex])
			if point == "" || angle == "" {
				continue
			}
			tasks = append(tasks, anchorStyleWheelTask{SellingPoint: point, Angle: angle, Key: point + "×" + angle, TargetAudience: optionalWheelText(item.TargetAudience), PainPoint: optionalWheelText(item.PainPoint)})
		}
	}
	return tasks
}

func renderAnchorStyleWheelTestPrompt(metaPrompt, factPackage string, task anchorStyleWheelTask, maxChars int) string {
	base := fmt.Sprintf(`%s

%s

# 本轮口播任务
下一段讲：【%s】的【%s】。
只输出主播在直播间实际说的话，不要解释任务，不要输出标题、分析或总结。`, strings.TrimSpace(metaPrompt), strings.TrimSpace(factPackage), task.SellingPoint, task.Angle)
	if task.TargetAudience != "" || task.PainPoint != "" {
		data, _ := json.Marshal(map[string]string{"目标人群": task.TargetAudience, "痛点": task.PainPoint})
		base += "\n\n" + string(data)
	}
	if maxChars <= 0 {
		return base
	}
	return fmt.Sprintf("%s\n本段最多%d个中文字符，不得超过。", base, maxChars)
}

func (s *Server) anchorStyleWorkbenchStep(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	planID, ok := liveAgentPlanPathID(w, r)
	if !ok {
		return
	}
	var input struct {
		TenantID          int64                       `json:"tenant_id,omitempty"`
		MetaPromptAssetID int64                       `json:"meta_prompt_asset_id"`
		MetaPromptMD      string                      `json:"meta_prompt_md"`
		Monitor           anchorStyleWorkbenchMonitor `json:"monitor"`
		State             anchorStyleWorkbenchState   `json:"state"`
		GeneratorProfile  string                      `json:"generator_profile_id,omitempty"`
		TargetChars       int                         `json:"target_chars"`
		Postprocess       bool                        `json:"postprocess"`
		DirectMode        bool                        `json:"direct_mode"`
		TaskOverride      *anchorStyleWorkbenchTask   `json:"task_override,omitempty"`
	}
	if readJSON(w, r, &input) != nil {
		writeError(w, http.StatusBadRequest, "生成请求格式错误")
		return
	}
	if input.TargetChars == 0 {
		input.TargetChars = 260
	}
	if input.TargetChars < 120 || input.TargetChars > 800 || input.State.Round < 0 || len(input.State.History) > 60 || len(input.State.UsedCombinations) > 500 {
		writeError(w, http.StatusBadRequest, "轮次状态或单段字数无效")
		return
	}
	tenantID, ok := s.resolveLiveAgentPlanTenant(w, r, actor, input.TenantID, false)
	if !ok {
		return
	}
	var savedMeta model.LiveAnchorStyleMetaPromptAsset
	var savedErr error
	if input.MetaPromptAssetID > 0 {
		savedMeta, savedErr = s.store.GetLiveAnchorStyleMetaPrompt(r.Context(), tenantID, planID, input.MetaPromptAssetID)
	} else {
		savedMeta, savedErr = s.store.GetActiveLiveAnchorStyleMetaPrompt(r.Context(), tenantID, planID)
	}
	if errors.Is(savedErr, appdb.ErrLiveAnchorStyleMetaPromptNotFound) || !savedMeta.Active {
		writeError(w, http.StatusConflict, "请先生成并确认已保存的主播风格元提示词")
		return
	}
	if savedErr != nil {
		writeError(w, http.StatusInternalServerError, "读取主播风格元提示词失败")
		return
	}
	if savedMeta.Status != "confirmed" {
		writeError(w, http.StatusConflict, "请先确认已保存的主播风格元提示词")
		return
	}
	input.MetaPromptMD = savedMeta.EffectiveMetaPromptMD()
	if strings.TrimSpace(savedMeta.MonitorJSON) != "" {
		_ = json.Unmarshal([]byte(savedMeta.MonitorJSON), &input.Monitor)
	}
	plan, facts, benefits, links, ok := s.anchorStyleWorkbenchPlanData(w, r, tenantID, planID)
	if !ok {
		return
	}
	factPackage := renderAnchorStyleWorkbenchFacts(plan, facts, benefits, links)
	points := compileAnchorStyleWorkbenchPoints(facts, benefits, links)
	if len(points) == 0 {
		writeError(w, http.StatusConflict, "当前方案没有可调度的正式事实")
		return
	}
	task := anchorStyleWorkbenchTask{Round: input.State.Round + 1}
	biases := []anchorStyleWorkbenchBias{}
	messages := []agentgateway.Message{}
	maxTokens := 4000
	if input.DirectMode {
		// Deliberately exact: the confirmed meta prompt followed by the compiled
		// fact package, as one user message. No hidden system prompt, scheduling,
		// history, feedback, density guidance, or length instruction is added.
		messages = append(messages, agentgateway.Message{Role: "user", Content: renderAnchorStyleWorkbenchDirectPrompt(input.MetaPromptMD, factPackage)})
	} else {
		task = nextAnchorStyleWorkbenchTask(points, input.State)
		if input.TaskOverride != nil {
			task = *input.TaskOverride
		}
		task.Round = input.State.Round + 1
		biases = compileAnchorStyleWorkbenchBias(input.Monitor)
		systemPrompt, userPrompt := renderAnchorStyleWorkbenchStepPrompt(input.MetaPromptMD, factPackage, task, input.State, biases, input.TargetChars)
		messages = []agentgateway.Message{{Role: "system", Content: systemPrompt}, {Role: "user", Content: userPrompt}}
		maxTokens = max(1000, input.TargetChars*4)
	}
	result, actual, err := s.completeAnchorStyleWorkbench(r.Context(), input.GeneratorProfile, "speech_generation", agentgateway.ResponseText, messages, maxTokens, !input.DirectMode)
	if err != nil {
		writeError(w, http.StatusBadGateway, "Qwen生成失败："+err.Error())
		return
	}
	raw := strings.TrimSpace(result.Text)
	if raw == "" {
		writeError(w, http.StatusBadGateway, "Qwen返回了空文本")
		return
	}
	post := anchorStyleWorkbenchPostprocess{Enabled: input.Postprocess && !input.DirectMode, Text: raw, Changes: []string{}}
	if post.Enabled {
		post.Text, post.Changes = postprocessAnchorStyleWorkbench(raw, input.Monitor, input.TargetChars)
	}
	nextState := input.State
	nextState.Round++
	combo := strings.Trim(strings.TrimSpace(task.SellingPointKey)+"×"+strings.TrimSpace(task.Angle), "×")
	if combo != "" && !containsAnchorStyleWorkbench(nextState.UsedCombinations, combo) {
		nextState.UsedCombinations = append(nextState.UsedCombinations, combo)
	}
	nextState.History = append(nextState.History, post.Text)
	if len(nextState.History) > 20 {
		nextState.History = nextState.History[len(nextState.History)-20:]
	}
	stats := evaluateAnchorStyleWorkbench(input.Monitor, strings.Join(nextState.History, "\n"))
	if input.DirectMode {
		nextState.Feedback = nil
	} else {
		nextState.Feedback = feedbackAnchorStyleWorkbench(stats)
	}
	writeJSON(w, http.StatusOK, anchorStyleWorkbenchStepResponse{
		DirectMode: input.DirectMode, Task: task, Request: traceAnchorStyleWorkbenchMessages(actual), RawResponse: raw,
		Postprocess: post, LogitBias: biases, LogitBiasMode: map[bool]string{true: "disabled_direct", false: "lexical_prompt_guidance"}[input.DirectMode],
		Stats: stats, NextFeedback: nextState.Feedback, State: nextState,
		Provider: result.Provider, Model: result.Model, LatencyMS: result.LatencyMS,
		InputTokens: result.InputTokens, OutputTokens: result.OutputTokens,
		TargetChars: input.TargetChars, ActualRawChars: utf8.RuneCountInString(raw), ActualOutputChars: utf8.RuneCountInString(post.Text),
	})
}

func (s *Server) anchorStyleWorkbenchPlanData(w http.ResponseWriter, r *http.Request, tenantID, planID int64) (model.LiveAgentPlan, []model.LiveAgentPlanFact, []model.LiveAgentPlanBenefit, []model.LiveAgentPlanProductLink, bool) {
	plan, err := s.store.GetLiveAgentPlan(r.Context(), tenantID, planID)
	if err != nil {
		writeError(w, http.StatusNotFound, "直播方案不存在")
		return plan, nil, nil, nil, false
	}
	facts, err := s.store.ListLiveAgentPlanFacts(r.Context(), tenantID, planID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取正式事实失败")
		return plan, nil, nil, nil, false
	}
	benefits, err := s.store.ListActiveLiveAgentPlanBenefits(r.Context(), tenantID, planID, time.Now())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取福利失败")
		return plan, nil, nil, nil, false
	}
	links, err := s.store.ListLiveAgentPlanProductLinks(r.Context(), tenantID, planID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取商品链接失败")
		return plan, nil, nil, nil, false
	}
	return plan, facts, benefits, links, true
}

func (s *Server) completeAnchorStyleWorkbench(ctx context.Context, profileID, stage string, format agentgateway.ResponseFormat, messages []agentgateway.Message, maxTokens int, includeProfileSystem bool) (agentgateway.Response, []agentgateway.Message, error) {
	profileID = strings.TrimSpace(profileID)
	configured, configErr := s.store.SpeechModels(ctx)
	if configErr != nil {
		return agentgateway.Response{}, messages, configErr
	}
	profileID = workbenchProfileForStage(ctx, configured, profileID, stage)
	// Speech generation always uses the configured profile transport, including
	// its visible system prompt. A named compiler profile does the same. Leaving
	// the compiler blank uses the operator-owned analysis route.
	if profileID != "" || stage == "speech_generation" {
		profile, err := s.store.ResolveSpeechModel(ctx, profileID)
		if err != nil || profile == nil {
			if err == nil {
				err = fmt.Errorf("未配置可用模型")
			}
			return agentgateway.Response{}, messages, err
		}
		actual := append([]agentgateway.Message(nil), messages...)
		if includeProfileSystem && strings.TrimSpace(profile.SystemPrompt) != "" {
			actual = append([]agentgateway.Message{{Role: "system", Content: profile.SystemPrompt}}, actual...)
		}
		request := agentgateway.Request{
			Stage: stage, Model: profile.Model, Messages: actual, MaxTokens: min(maxTokens, profile.MaxTokens),
			EnableThinking: false, ResponseFormat: format, CacheSystemPrompt: true, Timeout: time.Duration(profile.TimeoutMS) * time.Millisecond,
		}
		var prepareErr error
		request, prepareErr = agentgateway.PrepareTextRequest(*profile, request)
		if prepareErr != nil {
			return agentgateway.Response{}, actual, prepareErr
		}
		actual = request.Messages
		id, err := s.beginModelExchange(ctx, stage, profile.ID, request)
		if err != nil {
			return agentgateway.Response{}, actual, err
		}
		started := time.Now()
		result, err := agentgateway.CompleteSpeechEndpoint(ctx, *profile, request)
		result.Provider = "anchor:" + profile.ID
		result.Model = profile.Model
		err = s.finishModelExchange(ctx, id, result, result.Text, started, result.InputTokens, result.OutputTokens, err)
		return result, actual, err
	}
	request := agentgateway.Request{
		Stage: stage, Messages: messages, MaxTokens: maxTokens, EnableThinking: false,
		ResponseFormat: format, Timeout: 120 * time.Second,
	}
	id, err := s.beginModelExchange(ctx, stage, profileID, request)
	if err != nil {
		return agentgateway.Response{}, messages, err
	}
	started := time.Now()
	result, err := s.speechGateway().Complete(ctx, request)
	err = s.finishModelExchange(ctx, id, result, result.Text, started, result.InputTokens, result.OutputTokens, err)
	return result, messages, err
}

func workbenchProfileForStage(ctx context.Context, configured agentgateway.SpeechModels, profileID, stage string) string {
	if agentgateway.UsesReviewModel(stage) && configured.ReviewDefaultID != "" {
		return configured.ReviewDefaultID
	}
	if configured.SystemDefaultID != "" {
		if agentgateway.UsesSpeechModel(stage) {
			if pinned, ok := ctx.Value(pinnedSpeechProfileKey{}).(string); !ok || pinned == "" || pinned != profileID {
				return configured.DefaultID
			}
			return profileID
		}
		return configured.SystemDefaultID
	}
	return profileID
}

func traceAnchorStyleWorkbenchMessages(messages []agentgateway.Message) anchorStyleWorkbenchTrace {
	trace := anchorStyleWorkbenchTrace{}
	for _, message := range messages {
		switch message.Role {
		case "system", "developer":
			if trace.System != "" {
				trace.System += "\n\n"
			}
			trace.System += message.Content
		case "user":
			if trace.User != "" {
				trace.User += "\n\n"
			}
			trace.User += message.Content
		}
	}
	return trace
}

func renderAnchorStyleWorkbenchFacts(plan model.LiveAgentPlan, facts []model.LiveAgentPlanFact, benefits []model.LiveAgentPlanBenefit, links []model.LiveAgentPlanProductLink) string {
	text, _ := renderAnchorStyleWorkbenchFactsWithOrigins(plan, facts, benefits, links)
	return text
}

// Sidecar UI origins do not change model inputs, claim identities or approvals.
func renderAnchorStyleWorkbenchFactsWithOrigins(plan model.LiveAgentPlan, facts []model.LiveAgentPlanFact, benefits []model.LiveAgentPlanBenefit, links []model.LiveAgentPlanProductLink) (string, map[int]model.LiveRiskReviewSource) {
	lines := []string{"# 正式事实包（只决定说什么）", "方案：" + strings.TrimSpace(plan.Name)}
	origins := map[int]model.LiveRiskReviewSource{}
	lineIndex := func() int {
		count := len(lines)
		for _, line := range lines {
			count += strings.Count(line, "\n")
		}
		return count
	}
	for _, fact := range facts {
		if fact.SourceType == "story_lecture" {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(fact.Status), "active") || strings.TrimSpace(fact.Value) == "" {
			continue
		}
		line := fmt.Sprintf("- [%s/%s] %s", strings.TrimSpace(fact.Category), strings.TrimSpace(fact.Key), strings.TrimSpace(fact.Value))
		if strings.TrimSpace(fact.SafeRewrite) != "" {
			line += "；安全改写=" + strings.TrimSpace(fact.SafeRewrite)
		}
		if strings.TrimSpace(fact.ForbiddenWording) != "" {
			line += "；禁说=" + strings.TrimSpace(fact.ForbiddenWording)
		}
		origins[lineIndex()] = model.LiveRiskReviewSource{Area: "knowledge", EntityID: fact.ID}
		lines = append(lines, line)
	}
	for _, link := range links {
		if !strings.EqualFold(strings.TrimSpace(link.Status), "active") {
			continue
		}
		fields := []string{"链接=" + link.LinkKey, "商品=" + link.ProductName, "规格=" + link.Spec, "日常价=" + link.DailyPrice, "数量=" + link.Quantity, "适用=" + link.Audience}
		for _, attr := range link.Attributes {
			if strings.EqualFold(strings.TrimSpace(attr.Status), "active") && strings.TrimSpace(attr.Value) != "" {
				fields = append(fields, attr.Label+"="+attr.Value+attr.Unit)
			}
		}
		origins[lineIndex()] = model.LiveRiskReviewSource{Area: "products", EntityID: link.ID}
		lines = append(lines, "- [商品卡] "+strings.Join(nonEmptyAnchorStyleWorkbench(fields), "；"))
	}
	for _, benefit := range benefits {
		fields := []string{"福利=" + benefit.Key, "链接=" + benefit.LinkKey, "商品=" + benefit.ProductName, "活动价=" + benefit.ActivityPrice, "赠品=" + benefit.Gift, "活动=" + benefit.Activity}
		if benefit.ValidityMode != "permanent" && benefit.StartsAt != nil {
			fields = append(fields, "开始="+benefit.StartsAt.Format(time.RFC3339))
		}
		if benefit.ValidityMode != "permanent" && benefit.EndsAt != nil {
			fields = append(fields, "结束="+benefit.EndsAt.Format(time.RFC3339))
		}
		origins[lineIndex()] = model.LiveRiskReviewSource{Area: "benefits", EntityID: benefit.ID}
		lines = append(lines, "- [有效福利] "+strings.Join(nonEmptyAnchorStyleWorkbench(fields), "；"))
	}
	lines = append(lines, "边界：只能使用以上事实；缺少库存、销量、倒计时或现场事件时不得虚构。")
	return strings.Join(lines, "\n"), origins
}

func compileAnchorStyleWorkbenchPoints(facts []model.LiveAgentPlanFact, benefits []model.LiveAgentPlanBenefit, links []model.LiveAgentPlanProductLink) []anchorStyleWorkbenchPoint {
	result := []anchorStyleWorkbenchPoint{}
	seen := map[string]bool{}
	appendPoint := func(point anchorStyleWorkbenchPoint) {
		point.Key, point.Label, point.Value = strings.TrimSpace(point.Key), strings.TrimSpace(point.Label), strings.TrimSpace(point.Value)
		if point.Key == "" || point.Value == "" || seen[point.Key] || len(result) >= 40 {
			return
		}
		seen[point.Key] = true
		point.Angles = anchorStyleWorkbenchAngles(point.Category)
		result = append(result, point)
	}
	for _, fact := range facts {
		if strings.EqualFold(strings.TrimSpace(fact.Status), "active") {
			appendPoint(anchorStyleWorkbenchPoint{Key: "fact:" + fact.Key, Label: fact.Key, Value: fact.Value, Category: fact.Category})
		}
	}
	for _, link := range links {
		if !strings.EqualFold(strings.TrimSpace(link.Status), "active") {
			continue
		}
		appendPoint(anchorStyleWorkbenchPoint{Key: "link:" + link.LinkKey, Label: link.ProductName + "规格选择", Value: strings.Join(nonEmptyAnchorStyleWorkbench([]string{link.Spec, link.DailyPrice, link.Quantity, link.Audience}), "；"), Category: "trade"})
		for _, attr := range link.Attributes {
			if strings.EqualFold(strings.TrimSpace(attr.Status), "active") {
				appendPoint(anchorStyleWorkbenchPoint{Key: fmt.Sprintf("attribute:%d", attr.ID), Label: attr.Label, Value: attr.Value + attr.Unit, Category: "product"})
			}
		}
	}
	for _, benefit := range benefits {
		appendPoint(anchorStyleWorkbenchPoint{Key: fmt.Sprintf("benefit:%d", benefit.ID), Label: benefit.Key, Value: strings.Join(nonEmptyAnchorStyleWorkbench([]string{benefit.Activity, benefit.ActivityPrice, benefit.Gift}), "；"), Category: "benefit"})
	}
	return result
}

func anchorStyleWorkbenchAngles(category string) []string {
	switch strings.ToLower(strings.TrimSpace(category)) {
	case "trade", "price", "link":
		return []string{"单价与规格选择", "家庭用量", "不同人群怎么选", "降低决策门槛", "算账对照"}
	case "benefit", "assurance", "fulfillment":
		return []string{"规则讲清", "适用条件", "操作步骤", "降低顾虑", "错过会失去什么"}
	case "identity_location", "trust":
		return []string{"来源背景", "可信依据", "与体验的关系", "适合谁关注", "常见误解"}
	default:
		return []string{"直白解释", "家庭场景", "适用人群", "感官体验", "常见顾虑", "选择理由"}
	}
}

func nextAnchorStyleWorkbenchTask(points []anchorStyleWorkbenchPoint, state anchorStyleWorkbenchState) anchorStyleWorkbenchTask {
	used := map[string]bool{}
	for _, item := range state.UsedCombinations {
		used[item] = true
	}
	start := state.Round % len(points)
	point := points[start]
	angle := point.Angles[state.Round%len(point.Angles)]
	found := false
	for offset := 0; offset < len(points) && !found; offset++ {
		candidate := points[(start+offset)%len(points)]
		for angleOffset := 0; angleOffset < len(candidate.Angles); angleOffset++ {
			candidateAngle := candidate.Angles[(state.Round+angleOffset)%len(candidate.Angles)]
			if !used[candidate.Key+"×"+candidateAngle] {
				point, angle, found = candidate, candidateAngle, true
				break
			}
		}
	}
	phasePattern := []string{"opening", "body", "body", "body", "body", "body", "body", "closing"}
	functionPattern := []string{"product", "product", "trust", "assurance", "price", "product", "action", "fulfillment"}
	ctaPattern := []string{"light", "light", "medium", "light", "medium", "light", "heavy", "medium"}
	position := state.Round % len(phasePattern)
	return anchorStyleWorkbenchTask{
		Round: state.Round + 1, SellingPointKey: point.Key, SellingPointLabel: point.Label, SellingPointValue: point.Value, Angle: angle,
		MainlinePhase: phasePattern[position], BodyFunction: functionPattern[position], CTAIntensity: ctaPattern[position],
		ScarcityLevel: min(10, 1+state.Round/3), Callout: true, Recap: (state.Round+1)%5 == 0,
	}
}

func renderAnchorStyleWorkbenchStepPrompt(metaPrompt, facts string, task anchorStyleWorkbenchTask, state anchorStyleWorkbenchState, biases []anchorStyleWorkbenchBias, targetChars int) (string, string) {
	system := `你是持续直播口播的逐段生成模型。元提示词只决定怎么说，正式事实包只决定说什么，程序任务决定本段讲什么。只输出当前一段口播正文，不输出分析、标题、JSON或编稿说明。`
	biasJSON, _ := json.Marshal(biases)
	feedback := "无；按已确认元提示词自然表达"
	if len(state.Feedback) > 0 {
		feedback = strings.Join(state.Feedback, "；")
	}
	history := tailAnchorStyleWorkbench(strings.Join(state.History, "\n"), 1000)
	if history == "" {
		history = "（首段，无前文）"
	}
	user := fmt.Sprintf(`# 已确认的主播风格元提示词
%s

# 正式事实包
%s

# 程序本轮任务
- 轮次：%d
- 主线阶段：%s
- 主体功能：%s
- 卖点：%s
- 卖点事实：%s
- 角度：%s
- 逼单强度：%s
- 稀缺强度：%d/10；只有事实包有真实稀缺事实时才可表达
- 每段呼告：%t
- 每5段信息回环：本段=%t

# 生成前词汇引导
%s
说明：这是词汇概率方向的等价提示层；当前供应商接口未提供可靠的中文词元ID，所以不能伪称已经发送原生token logit_bias。密度最终由程序后处理兜底。

# 上轮风格回灌（只补点，不猛补密度）
%s

# 最近口播尾部
%s

# 输出约束
写约%d个中文字符，只写当前一段。避免复述最近口播；不得虚构事实、现场评论、库存、销量或倒计时。称呼、语气词和口头禅要分散在自然位置，不要连续堆在句首。`, metaPrompt, facts, task.Round, task.MainlinePhase, task.BodyFunction, task.SellingPointLabel, task.SellingPointValue, task.Angle, task.CTAIntensity, task.ScarcityLevel, task.Callout, task.Recap, string(biasJSON), feedback, history, targetChars)
	return system, user
}

func renderAnchorStyleWorkbenchDirectPrompt(metaPrompt, facts string) string {
	return strings.TrimSpace(metaPrompt) + "\n\n" + strings.TrimSpace(facts)
}

// 品相关内容剥离正则（确定性，不依赖任何外部数据）。
var (
	reAnchorStyleLink    = regexp.MustCompile(`\d+\s*号链接|链接\s*\d+|链接编号`)
	reAnchorStylePrice   = regexp.MustCompile(`\d+(?:\.\d+)?\s*[块元](?:\s*\d+)?`)
	reAnchorStyleNumUnit = regexp.MustCompile(`\d+(?:\.\d+)?\s*(?:多)?(?:斤|两|公斤|千克|克|升|毫升|桶|单|年|万单|亩|分钟|小时|天|袋|盒|瓶|支|折|%)`)
	reAnchorStyleBareNum = regexp.MustCompile(`\d+(?:\.\d+)?`)
	reAnchorStylePh      = regexp.MustCompile(`(\[数字\]|\[单位\]|\[链接\]|\[价格\]|\[产品\])+`)
)

// stripAnchorStyleBareNumbers 剥离未被数字单位正则匹配到的裸数字（价格/数量等），
// 跳过列表序号（数字后紧跟 . 、 ． ： ) ） ] 】 等标点），避免误伤 1. 2. 3. 这类编号。
func stripAnchorStyleBareNumbers(s string) string {
	idxs := reAnchorStyleBareNum.FindAllStringIndex(s, -1)
	for i := len(idxs) - 1; i >= 0; i-- {
		start, end := idxs[i][0], idxs[i][1]
		if end < len(s) {
			r, _ := utf8.DecodeRuneInString(s[end:])
			if strings.ContainsRune("、．：）】.:)]", r) {
				continue
			}
		}
		s = s[:start] + "[数字]" + s[end:]
	}
	return s
}

// stripAnchorStyleProductContent 剥离风格文本中品相关内容。
// 单样本下无法跨品对比，只能靠确定性正则 + 词表硬剥离：
//   链接号、价格、数字+单位 用正则（不依赖外部数据）；
//   商品名/品类名 用 productNames 词表（调用方注入，没有就只做正则）。
// 剥离后残留的连续占位符统一压缩成 [已剥离]。
// 返回剥离后的元提示词文本与候选词表。
func stripAnchorStyleProductContent(metaMD string, lexicon anchorStyleWorkbenchLexicon, productNames []string) (string, anchorStyleWorkbenchLexicon) {
	// 1. 文本剥离：链接号 → 价格 → 数字+单位 → 裸数字（顺序不能变）
	md := metaMD
	md = reAnchorStyleLink.ReplaceAllString(md, "[链接]")
	md = reAnchorStylePrice.ReplaceAllString(md, "[价格]")
	md = reAnchorStyleNumUnit.ReplaceAllString(md, "[数字]")
	md = stripAnchorStyleBareNumbers(md)
	// 商品名词表剥离（调用方注入，可为空）
	for _, p := range productNames {
		p = strings.TrimSpace(p)
		if p != "" {
			md = strings.ReplaceAll(md, p, "[产品]")
		}
	}
	md = reAnchorStylePh.ReplaceAllString(md, "[已剥离]")

	// 2. 候选词剥离：含链接号/价格/数字单位/商品名的词直接剔除
	filter := func(terms []string) []string {
		out := make([]string, 0, len(terms))
		for _, t := range terms {
			if isAnchorStyleProductTerm(t, productNames) {
				continue
			}
			out = append(out, t)
		}
		return out
	}
	lexicon.AudienceTerms = filter(lexicon.AudienceTerms)
	lexicon.SelfTerms = filter(lexicon.SelfTerms)
	lexicon.AudiencePronouns = filter(lexicon.AudiencePronouns)
	lexicon.Particles = filter(lexicon.Particles)
	lexicon.Connectors = filter(lexicon.Connectors)
	lexicon.Catchphrases = filter(lexicon.Catchphrases)
	lexicon.ForbiddenTerms = filter(lexicon.ForbiddenTerms)
	return md, lexicon
}

// isAnchorStyleProductTerm 判断候选词是否为品相关（含链接号/价格/数字单位/商品名）。
func isAnchorStyleProductTerm(term string, productNames []string) bool {
	t := strings.TrimSpace(term)
	if t == "" {
		return true
	}
	if reAnchorStyleLink.MatchString(t) || reAnchorStylePrice.MatchString(t) || reAnchorStyleNumUnit.MatchString(t) || reAnchorStyleBareNum.MatchString(t) {
		return true
	}
	for _, p := range productNames {
		p = strings.TrimSpace(p)
		if p != "" && strings.Contains(t, p) {
			return true
		}
	}
	return false
}

func normalizeAnchorStyleWorkbenchLexicon(input anchorStyleWorkbenchLexicon, sample string) anchorStyleWorkbenchLexicon {
	input.AudienceTerms = filterAnchorStyleWorkbenchTerms(input.AudienceTerms, sample, 16)
	input.SelfTerms = filterAnchorStyleWorkbenchTerms(input.SelfTerms, sample, 10)
	input.AudiencePronouns = filterAnchorStyleWorkbenchTerms(input.AudiencePronouns, sample, 10)
	input.Particles = filterAnchorStyleWorkbenchTerms(input.Particles, sample, 16)
	input.Connectors = filterAnchorStyleWorkbenchTerms(input.Connectors, sample, 12)
	input.Catchphrases = filterAnchorStyleWorkbenchTerms(input.Catchphrases, sample, 16)
	input.ForbiddenTerms = uniqueAnchorStyleWorkbench(input.ForbiddenTerms, 16)
	if len(input.AudienceTerms) == 0 {
		input.AudienceTerms = filterAnchorStyleWorkbenchTerms([]string{"哥哥姐姐们", "哥哥姐们", "老乡们", "老乡", "新粉丝们", "新粉丝", "新粉", "老粉丝", "老粉", "家人们"}, sample, 16)
	}
	if len(input.SelfTerms) == 0 {
		input.SelfTerms = filterAnchorStyleWorkbenchTerms([]string{"我们家", "我家", "咱们家", "咱家"}, sample, 10)
	}
	if len(input.AudiencePronouns) == 0 {
		input.AudiencePronouns = filterAnchorStyleWorkbenchTerms([]string{"你们", "大家", "您"}, sample, 10)
	}
	if len(input.Particles) == 0 {
		input.Particles = filterAnchorStyleWorkbenchTerms([]string{"啊", "哟", "嘛", "哈", "呀", "呢", "哦"}, sample, 16)
	}
	if len(input.Connectors) == 0 {
		input.Connectors = filterAnchorStyleWorkbenchTerms([]string{"所以说", "就是说", "然后", "对了", "你看"}, sample, 12)
	}
	if len(input.Catchphrases) == 0 {
		input.Catchphrases = filterAnchorStyleWorkbenchTerms([]string{"正儿八经", "很香的", "不绕弯子"}, sample, 16)
	}
	if input.SentenceCharsMin < 4 || input.SentenceCharsMin > 80 {
		input.SentenceCharsMin = 10
	}
	if input.SentenceCharsMax <= input.SentenceCharsMin || input.SentenceCharsMax > 160 {
		input.SentenceCharsMax = 38
	}
	return input
}

func buildAnchorStyleWorkbenchMonitor(lexicon anchorStyleWorkbenchLexicon, sample string) anchorStyleWorkbenchMonitor {
	chars := max(1, utf8.RuneCountInString(sample))
	monitor := anchorStyleWorkbenchMonitor{SampleChars: chars, ForbiddenTerms: lexicon.ForbiddenTerms, SentenceCharsMin: lexicon.SentenceCharsMin, SentenceCharsMax: lexicon.SentenceCharsMax}
	groups := []struct {
		kind  string
		terms []string
		bias  float64
	}{{"audience_address", lexicon.AudienceTerms, 2.0}, {"self_address", lexicon.SelfTerms, 1.8}, {"audience_pronoun", lexicon.AudiencePronouns, 1.0}, {"particle", lexicon.Particles, 1.2}, {"connector", lexicon.Connectors, 0.8}, {"catchphrase", lexicon.Catchphrases, 0.8}}
	for _, group := range groups {
		for _, term := range group.terms {
			count := strings.Count(sample, term)
			if count == 0 {
				continue
			}
			monitor.Terms = append(monitor.Terms, anchorStyleWorkbenchTerm{Kind: group.kind, Text: term, SampleCount: count, SamplePer1000: float64(count) * 1000 / float64(chars), Bias: group.bias})
		}
	}
	return monitor
}

func compileAnchorStyleWorkbenchBias(monitor anchorStyleWorkbenchMonitor) []anchorStyleWorkbenchBias {
	result := []anchorStyleWorkbenchBias{}
	for _, item := range monitor.Terms {
		if strings.TrimSpace(item.Text) != "" && item.Bias != 0 {
			result = append(result, anchorStyleWorkbenchBias{Text: item.Text, Bias: item.Bias})
		}
	}
	for _, term := range monitor.ForbiddenTerms {
		result = append(result, anchorStyleWorkbenchBias{Text: term, Bias: -3})
	}
	sort.SliceStable(result, func(i, j int) bool { return math.Abs(result[i].Bias) > math.Abs(result[j].Bias) })
	if len(result) > 24 {
		result = result[:24]
	}
	return result
}

func evaluateAnchorStyleWorkbench(monitor anchorStyleWorkbenchMonitor, text string) anchorStyleWorkbenchStats {
	window := tailAnchorStyleWorkbench(text, 1000)
	chars := max(1, utf8.RuneCountInString(window))
	stats := anchorStyleWorkbenchStats{WindowChars: chars, Metrics: []anchorStyleWorkbenchMetric{}, Forbidden: []string{}}
	totalScore := 0.0
	weights := 0.0
	for _, target := range monitor.Terms {
		count := strings.Count(window, target.Text)
		actual := float64(count) * 1000 / float64(chars)
		status := "ok"
		if actual < target.SamplePer1000*0.65 {
			status = "low"
		} else if actual > target.SamplePer1000*1.6 && count > target.SampleCount/2 {
			status = "high"
		}
		stats.Metrics = append(stats.Metrics, anchorStyleWorkbenchMetric{Kind: target.Kind, Text: target.Text, TargetPer1000: roundAnchorStyleWorkbench(target.SamplePer1000), ActualCount: count, ActualPer1000: roundAnchorStyleWorkbench(actual), Status: status})
		ratio := 1.0
		if target.SamplePer1000 > 0 {
			ratio = math.Min(actual/target.SamplePer1000, target.SamplePer1000/math.Max(actual, 0.01))
		}
		weight := 1.0
		if target.Kind == "audience_address" || target.Kind == "self_address" || target.Kind == "particle" {
			weight = 2
		}
		totalScore += math.Max(0, math.Min(1, ratio)) * weight
		weights += weight
	}
	for _, forbidden := range monitor.ForbiddenTerms {
		if forbidden != "" && strings.Contains(window, forbidden) {
			stats.Forbidden = append(stats.Forbidden, forbidden)
		}
	}
	if weights == 0 {
		stats.StyleScore = 100
	} else {
		stats.StyleScore = int(math.Round(totalScore / weights * 100))
	}
	stats.StyleScore = max(0, stats.StyleScore-len(stats.Forbidden)*8)
	return stats
}

func feedbackAnchorStyleWorkbench(stats anchorStyleWorkbenchStats) []string {
	feedback := []string{}
	for _, forbidden := range stats.Forbidden {
		feedback = append(feedback, "避免继续使用“"+forbidden+"”，换回元提示词中的主力体系")
		if len(feedback) >= 3 {
			return feedback
		}
	}
	for _, status := range []string{"low", "high"} {
		for _, metric := range stats.Metrics {
			if metric.Status != status || (stats.WindowChars < 350 && metric.ActualCount > 0) {
				continue
			}
			if status == "low" {
				feedback = append(feedback, "未来1到2段在自然位置轻补“"+metric.Text+"”，本段不要扎堆")
			} else {
				feedback = append(feedback, "未来1到2段减少“"+metric.Text+"”，不必完全停用")
			}
			if len(feedback) >= 3 {
				return feedback
			}
		}
	}
	if len(feedback) == 0 {
		feedback = append(feedback, "滑动窗口处于容差区间，维持当前腔调，不做额外补偿")
	}
	return feedback
}

func postprocessAnchorStyleWorkbench(text string, monitor anchorStyleWorkbenchMonitor, targetChars int) (string, []string) {
	result := strings.TrimSpace(text)
	changes := []string{}
	for _, term := range monitor.ForbiddenTerms {
		if term != "" && strings.Contains(result, term) {
			replacement := firstAnchorStyleWorkbenchTerm(monitor, "audience_address")
			if strings.ContainsAny(term, "家咱我") {
				if self := firstAnchorStyleWorkbenchTerm(monitor, "self_address"); self != "" {
					replacement = self
				}
			}
			if replacement != "" {
				count := strings.Count(result, term)
				result = strings.ReplaceAll(result, term, replacement)
				changes = append(changes, fmt.Sprintf("替换禁用词“%s”×%d", term, count))
			}
		}
	}
	audience := firstAnchorStyleWorkbenchTerm(monitor, "audience_address")
	self := firstAnchorStyleWorkbenchTerm(monitor, "self_address")
	missingAudience := audience != "" && !containsAnyAnchorStyleWorkbenchKind(result, monitor, "audience_address")
	missingSelf := self != "" && !containsAnyAnchorStyleWorkbenchKind(result, monitor, "self_address")
	if missingAudience && missingSelf {
		result = audience + "，" + self + "再把这个点说清楚，" + result
		changes = append(changes, "补一次称呼呼告和主播自指")
	} else if missingAudience {
		result = audience + "，" + result
		changes = append(changes, "补一次称呼呼告")
	} else if missingSelf {
		result = self + "再把这个点说清楚，" + result
		changes = append(changes, "补一次主播自指")
	}
	// Only fill one missing dominant particle per segment. Exact density is
	// governed by the rolling window; this cap prevents mechanical clusters.
	particle := dominantAnchorStyleWorkbenchTerm(monitor, "particle")
	if particle != "" && strings.Count(result, particle) == 0 {
		if index := strings.Index(result, "。"); index >= 0 && utf8.RuneCountInString(result) < targetChars+80 {
			result = result[:index] + particle + result[index:]
			changes = append(changes, "在自然句尾补一次主力语气词“"+particle+"”")
		}
	}
	if !strings.ContainsAny(result, "！!") {
		if index := strings.Index(result, "。"); index >= 0 {
			result = result[:index] + "！" + result[index+len("。"):]
			changes = append(changes, "把一个自然收束句改为感叹句")
		}
	}
	return strings.TrimSpace(result), changes
}

func firstAnchorStyleWorkbenchTerm(monitor anchorStyleWorkbenchMonitor, kind string) string {
	for _, item := range monitor.Terms {
		if item.Kind == kind {
			return item.Text
		}
	}
	return ""
}

func dominantAnchorStyleWorkbenchTerm(monitor anchorStyleWorkbenchMonitor, kind string) string {
	best := ""
	value := -1.0
	for _, item := range monitor.Terms {
		if item.Kind == kind && item.SamplePer1000 > value {
			best, value = item.Text, item.SamplePer1000
		}
	}
	return best
}

func containsAnyAnchorStyleWorkbenchKind(text string, monitor anchorStyleWorkbenchMonitor, kind string) bool {
	for _, item := range monitor.Terms {
		if item.Kind == kind && strings.Contains(text, item.Text) {
			return true
		}
	}
	return false
}

func filterAnchorStyleWorkbenchTerms(values []string, sample string, limit int) []string {
	result := []string{}
	for _, value := range uniqueAnchorStyleWorkbench(values, limit*2) {
		if strings.Contains(sample, value) {
			result = append(result, value)
			if len(result) >= limit {
				break
			}
		}
	}
	return result
}

func uniqueAnchorStyleWorkbench(values []string, limit int) []string {
	result := []string{}
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || utf8.RuneCountInString(value) > 20 || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
		if len(result) >= limit {
			break
		}
	}
	return result
}

func nonEmptyAnchorStyleWorkbench(values []string) []string {
	result := []string{}
	for _, value := range values {
		parts := strings.SplitN(value, "=", 2)
		if len(parts) == 2 && strings.TrimSpace(parts[1]) == "" {
			continue
		}
		if strings.TrimSpace(value) != "" {
			result = append(result, strings.TrimSpace(value))
		}
	}
	return result
}

func tailAnchorStyleWorkbench(value string, maxRunes int) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) <= maxRunes {
		return string(runes)
	}
	return string(runes[len(runes)-maxRunes:])
}

func containsAnchorStyleWorkbench(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func roundAnchorStyleWorkbench(value float64) float64 {
	return math.Round(value*10) / 10
}
