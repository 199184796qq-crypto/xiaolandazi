package httpapi

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const anchorStyleAnalysisPromptRelativePath = "configs/prompts/anchor-style/style-meta-prompt-extraction.md"
const sellingPointExtractionPromptRelativePath = "configs/prompts/anchor-style/qwen-selling-point-angle-extraction.md"
const sellingPointWheelReviewPromptRelativePath = "configs/prompts/anchor-style/selling-point-wheel-review.md"
const sellingPointWheelExpansionPromptRelativePath = "configs/prompts/anchor-style/selling-point-wheel-angle-expansion.md"
const sellingPointWheelFinalAuditPromptRelativePath = "configs/prompts/anchor-style/selling-point-wheel-final-audit.md"
const speechMinerGuidePromptRelativePath = "configs/prompts/anchor-style/speech-miner-guide.md"
const factGapChecklistPromptRelativePath = "configs/prompts/anchor-style/fact-gap-checklist.md"
const categorySellingDimensionsPromptRelativePath = "configs/prompts/anchor-style/category-selling-dimensions.md"
const categoryDetectionPromptRelativePath = "configs/prompts/anchor-style/category-detection.md"
const factSynonymVariantsPromptRelativePath = "configs/prompts/anchor-style/fact-synonym-variants.md"
const factSpeechRewritePromptRelativePath = "configs/prompts/anchor-style/fact-speech-rewrite.md"
const auditFourLayerPromptRelativePath = "configs/prompts/anchor-style/compliance/audit-four-layer.md"
const anchorStyleReviewPromptRelativePath = "configs/prompts/anchor-style/style-feedback-review.md"
const anchorStyleScorecardPromptRelativePath = "configs/prompts/anchor-style/anchor-style-qualitative-extraction.md"
const anchorStyleScoreFeedbackPromptRelativePath = "configs/prompts/anchor-style/score-feedback-template.md"

// Project mode deliberately ignores stale browser/database template snapshots.
// Manual overrides must be explicitly selected for this request.
func resolveRequestPrompt(mode, manual string, load func() (string, string, error)) (string, string, error) {
	switch strings.TrimSpace(mode) {
	case "", "project":
		return load()
	case "manual":
		manual = strings.TrimSpace(manual)
		if len(manual) < 100 || len(manual) > 256*1024 {
			return "", "", errors.New("手动提示词须为100字节到256KB")
		}
		return manual, fmt.Sprintf("manual-input#sha256=%x", sha256.Sum256([]byte(manual))), nil
	default:
		return "", "", errors.New("提示词来源须为project或manual")
	}
}

func configuredPromptPath(envName, relativePath string) string {
	if explicit := strings.TrimSpace(os.Getenv(envName)); explicit != "" {
		return explicit
	}
	if directory := strings.TrimSpace(os.Getenv("ANCHOR_STYLE_PROMPT_DIR")); directory != "" {
		relative := strings.TrimPrefix(filepath.ToSlash(relativePath), "configs/prompts/anchor-style/")
		return filepath.Join(directory, filepath.FromSlash(relative))
	}
	return ""
}

func promptVersionSource(path, content string) string {
	return fmt.Sprintf("%s#sha256=%x", path, sha256.Sum256([]byte(content)))
}

func loadAnchorStyleScoreFeedbackPrompt() (string, string, error) {
	return loadReplaceablePromptAsset("ANCHOR_STYLE_SCORE_FEEDBACK_PROMPT_PATH", anchorStyleScoreFeedbackPromptRelativePath, "程序评分回灌模板")
}

// loadAnchorStyleAnalysisPrompt reads the Markdown on every compilation. This
// is intentional: operators can replace the analysis method without changing
// Go code or restarting a long-running style-tuning session.
func loadAnchorStyleAnalysisPrompt() (string, string, error) {
	override := configuredPromptPath("ANCHOR_STYLE_ANALYSIS_PROMPT_PATH", anchorStyleAnalysisPromptRelativePath)
	candidates := []string{}
	if override != "" {
		candidates = append(candidates, override)
	} else {
		if cwd, err := os.Getwd(); err == nil {
			current := cwd
			for range 7 {
				candidates = append(candidates, filepath.Join(current, filepath.FromSlash(anchorStyleAnalysisPromptRelativePath)))
				parent := filepath.Dir(current)
				if parent == current {
					break
				}
				current = parent
			}
		}
		if executable, err := os.Executable(); err == nil {
			candidates = append(candidates,
				filepath.Join(filepath.Dir(executable), "..", filepath.FromSlash(anchorStyleAnalysisPromptRelativePath)),
				filepath.Join(filepath.Dir(executable), filepath.FromSlash(anchorStyleAnalysisPromptRelativePath)),
			)
		}
	}
	seen := map[string]bool{}
	for _, candidate := range candidates {
		candidate = filepath.Clean(candidate)
		if candidate == "" || seen[candidate] {
			continue
		}
		seen[candidate] = true
		info, err := os.Stat(candidate)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return "", candidate, fmt.Errorf("读取主播风格分析提示词失败: %w", err)
		}
		if info.IsDir() || info.Size() < 100 || info.Size() > 256*1024 {
			return "", candidate, errors.New("主播风格分析提示词必须是100字到256KB的Markdown文件")
		}
		raw, err := os.ReadFile(candidate)
		if err != nil {
			return "", candidate, fmt.Errorf("读取主播风格分析提示词失败: %w", err)
		}
		content := strings.TrimSpace(string(raw))
		if content == "" {
			return "", candidate, errors.New("主播风格分析提示词为空")
		}
		absolute, _ := filepath.Abs(candidate)
		return content, promptVersionSource(absolute, content), nil
	}
	if override != "" {
		return "", override, errors.New("ANCHOR_STYLE_ANALYSIS_PROMPT_PATH 指向的文件不存在")
	}
	return "", anchorStyleAnalysisPromptRelativePath, errors.New("项目内主播风格分析提示词不存在")
}

// loadSellingPointExtractionPrompt is deliberately separate from the style
// compiler asset: operators may tune selling-point coverage without changing
// how anchor voice is learned.
func loadSellingPointExtractionPrompt() (string, string, error) {
	override := configuredPromptPath("SELLING_POINT_EXTRACTION_PROMPT_PATH", sellingPointExtractionPromptRelativePath)
	candidates := []string{}
	if override != "" {
		candidates = append(candidates, override)
	} else {
		if cwd, err := os.Getwd(); err == nil {
			current := cwd
			for range 7 {
				candidates = append(candidates, filepath.Join(current, filepath.FromSlash(sellingPointExtractionPromptRelativePath)))
				parent := filepath.Dir(current)
				if parent == current {
					break
				}
				current = parent
			}
		}
		if executable, err := os.Executable(); err == nil {
			candidates = append(candidates,
				filepath.Join(filepath.Dir(executable), "..", filepath.FromSlash(sellingPointExtractionPromptRelativePath)),
				filepath.Join(filepath.Dir(executable), filepath.FromSlash(sellingPointExtractionPromptRelativePath)),
			)
		}
	}
	seen := map[string]bool{}
	for _, candidate := range candidates {
		candidate = filepath.Clean(candidate)
		if candidate == "" || seen[candidate] {
			continue
		}
		seen[candidate] = true
		info, err := os.Stat(candidate)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return "", candidate, fmt.Errorf("读取卖点挖掘提示词失败: %w", err)
		}
		if info.IsDir() || info.Size() < 100 || info.Size() > 256*1024 {
			return "", candidate, errors.New("卖点挖掘提示词必须是100字到256KB的Markdown文件")
		}
		raw, err := os.ReadFile(candidate)
		if err != nil {
			return "", candidate, fmt.Errorf("读取卖点挖掘提示词失败: %w", err)
		}
		content := strings.TrimSpace(string(raw))
		if content == "" {
			return "", candidate, errors.New("卖点挖掘提示词为空")
		}
		absolute, _ := filepath.Abs(candidate)
		return content, promptVersionSource(absolute, content), nil
	}
	if override != "" {
		return "", override, errors.New("SELLING_POINT_EXTRACTION_PROMPT_PATH 指向的文件不存在")
	}
	return "", sellingPointExtractionPromptRelativePath, errors.New("项目内卖点挖掘提示词不存在")
}

func loadSellingPointWheelReviewPrompt() (string, string, error) {
	return loadReplaceablePromptAsset(
		"SELLING_POINT_WHEEL_REVIEW_PROMPT_PATH",
		sellingPointWheelReviewPromptRelativePath,
		"卖点轮盘事实审计母版",
	)
}

func loadSellingPointWheelExpansionPrompt() (string, string, error) {
	return loadReplaceablePromptAsset(
		"SELLING_POINT_WHEEL_EXPANSION_PROMPT_PATH",
		sellingPointWheelExpansionPromptRelativePath,
		"卖点轮盘稀疏角度补全母版",
	)
}

func loadSellingPointWheelFinalAuditPrompt() (string, string, error) {
	return loadReplaceablePromptAsset(
		"SELLING_POINT_WHEEL_FINAL_AUDIT_PROMPT_PATH",
		sellingPointWheelFinalAuditPromptRelativePath,
		"卖点轮盘最终事实裁决母版",
	)
}

func loadSpeechMinerGuidePrompt() (string, string, error) {
	return loadReplaceablePromptAsset(
		"SPEECH_MINER_GUIDE_PROMPT_PATH",
		speechMinerGuidePromptRelativePath,
		"话术挖掘机引导母版",
	)
}

func loadFactGapChecklistPrompt() (string, string, error) {
	return loadReplaceablePromptAsset(
		"FACT_GAP_CHECKLIST_PROMPT_PATH",
		factGapChecklistPromptRelativePath,
		"商品事实必查母版",
	)
}

// loadCategorySellingDimensionsPrompt 读取「品类核心卖点维度表」，供卖点挖掘母版
// 按商品品类注入必查维度，替代原先硬编码的食品类维度。运营可在不改 Go 代码的前提下
// 增删品类，重跑卖点挖掘即生效。
func loadCategorySellingDimensionsPrompt() (string, string, error) {
	return loadReplaceablePromptAsset(
		"CATEGORY_SELLING_DIMENSIONS_PROMPT_PATH",
		categorySellingDimensionsPromptRelativePath,
		"品类核心卖点维度表",
	)
}

// loadCategoryDetectionPrompt 读取「商品品类判定母版」，供卖点挖掘前自动判定方案品类。
func loadCategoryDetectionPrompt() (string, string, error) {
	return loadReplaceablePromptAsset(
		"CATEGORY_DETECTION_PROMPT_PATH",
		categoryDetectionPromptRelativePath,
		"商品品类判定母版",
	)
}

// injectIndustrySellingDimensions 把行业配置包的挖掘维度 md 注入 {{CATEGORY_MUST_CHECK}} 占位。
// sellingDimensionsMD 来自行业四件套的 selling-dimensions.md（文件即源），industryName 用于
// 显式标注本方案行业。占位不存在或 md 为空时原样返回。
func injectIndustrySellingDimensions(promptMD, industryName, sellingDimensionsMD string) string {
	if !strings.Contains(promptMD, "{{CATEGORY_MUST_CHECK}}") {
		return promptMD
	}
	block := strings.TrimSpace(sellingDimensionsMD)
	if block == "" {
		return promptMD
	}
	if name := strings.TrimSpace(industryName); name != "" {
		block = "本方案行业已判定为「" + name + "」，必须优先套用下面的核心必查维度，不得遗漏或合并。\n\n" + block
	}
	return strings.ReplaceAll(promptMD, "{{CATEGORY_MUST_CHECK}}", block)
}

// injectIndustryFactChecklist 把行业配置包的挖掘维度注入清单挖掘母版的
// {{INDUSTRY_MUST_CHECK}} 占位，作为「行业必查层」。与卖点挖掘的
// injectIndustrySellingDimensions 不同：这里无论有没有行业包都要消掉占位——
// 有包时注入带标题的行业维度块，无包时替换为空串，占位原文不能漏进提示词。
func injectIndustryFactChecklist(promptMD, industryName, sellingDimensionsMD string) string {
	block := strings.TrimSpace(sellingDimensionsMD)
	if block == "" {
		return strings.ReplaceAll(promptMD, "{{INDUSTRY_MUST_CHECK}}", "")
	}
	if name := strings.TrimSpace(industryName); name != "" {
		block = "### 三、行业必查层（本方案行业已判定为「" + name + "」，必须逐项核对，不得遗漏或合并）\n\n" + block
	} else {
		block = "### 三、行业必查层（必须逐项核对，不得遗漏或合并）\n\n" + block
	}
	return strings.ReplaceAll(promptMD, "{{INDUSTRY_MUST_CHECK}}", block)
}

// injectCategorySellingDimensions 把 {{CATEGORY_MUST_CHECK}} 占位替换为品类维度表。
// category 为已判定（自动或人工）的品类 code；非空时在维度表前显式标注本方案品类，
// 让模型优先套用对应品类维度。母版没有占位（例如用户手动提交的提示词）时原样返回。
func injectCategorySellingDimensions(promptMD, category string) (string, error) {
	if !strings.Contains(promptMD, "{{CATEGORY_MUST_CHECK}}") {
		return promptMD, nil
	}
	dims, _, err := loadCategorySellingDimensionsPrompt()
	if err != nil {
		return "", err
	}
	block := strings.TrimSpace(dims)
	if label := productCategoryLabel(strings.TrimSpace(category)); label != "" {
		block = "本方案商品品类已判定为「" + label + "」，必须优先套用下面对应品类的核心必查维度，不得遗漏或合并。\n\n" + block
	}
	return strings.ReplaceAll(promptMD, "{{CATEGORY_MUST_CHECK}}", block), nil
}

func loadFactSynonymVariantsPrompt() (string, string, error) {
	return loadReplaceablePromptAsset(
		"FACT_SYNONYM_VARIANTS_PROMPT_PATH",
		factSynonymVariantsPromptRelativePath,
		"事实同义变体生成母版",
	)
}

func loadFactSpeechRewritePrompt() (string, string, error) {
	return loadReplaceablePromptAsset(
		"FACT_SPEECH_REWRITE_PROMPT_PATH",
		factSpeechRewritePromptRelativePath,
		"口播正文同义改写母版",
	)
}

func loadAuditFourLayerPrompt() (string, string, error) {
	return loadReplaceablePromptAsset(
		"AUDIT_FOUR_LAYER_PROMPT_PATH",
		auditFourLayerPromptRelativePath,
		"四层直播话术审核母版",
	)
}

func loadAnchorStyleReviewPrompt() (string, string, error) {
	override := configuredPromptPath("ANCHOR_STYLE_REVIEW_PROMPT_PATH", anchorStyleReviewPromptRelativePath)
	candidates := []string{}
	if override != "" {
		candidates = append(candidates, override)
	} else {
		if cwd, err := os.Getwd(); err == nil {
			current := cwd
			for range 7 {
				candidates = append(candidates, filepath.Join(current, filepath.FromSlash(anchorStyleReviewPromptRelativePath)))
				parent := filepath.Dir(current)
				if parent == current {
					break
				}
				current = parent
			}
		}
		if executable, err := os.Executable(); err == nil {
			candidates = append(candidates,
				filepath.Join(filepath.Dir(executable), "..", filepath.FromSlash(anchorStyleReviewPromptRelativePath)),
				filepath.Join(filepath.Dir(executable), filepath.FromSlash(anchorStyleReviewPromptRelativePath)),
			)
		}
	}
	seen := map[string]bool{}
	for _, candidate := range candidates {
		candidate = filepath.Clean(candidate)
		if candidate == "" || seen[candidate] {
			continue
		}
		seen[candidate] = true
		info, err := os.Stat(candidate)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return "", candidate, fmt.Errorf("读取风格监督提示词失败: %w", err)
		}
		if info.IsDir() || info.Size() < 100 || info.Size() > 256*1024 {
			return "", candidate, errors.New("风格监督提示词必须是100字到256KB的Markdown文件")
		}
		raw, err := os.ReadFile(candidate)
		if err != nil {
			return "", candidate, fmt.Errorf("读取风格监督提示词失败: %w", err)
		}
		content := strings.TrimSpace(string(raw))
		if content == "" {
			return "", candidate, errors.New("风格监督提示词为空")
		}
		absolute, _ := filepath.Abs(candidate)
		return content, promptVersionSource(absolute, content), nil
	}
	if override != "" {
		return "", override, errors.New("ANCHOR_STYLE_REVIEW_PROMPT_PATH 指向的文件不存在")
	}
	return "", anchorStyleReviewPromptRelativePath, errors.New("项目内风格监督提示词不存在")
}

func loadAnchorStyleScorecardPrompt() (string, string, error) {
	return loadReplaceablePromptAsset(
		"ANCHOR_STYLE_SCORECARD_PROMPT_PATH",
		anchorStyleScorecardPromptRelativePath,
		"主播腔调评分基准提取提示词",
	)
}

func loadReplaceablePromptAsset(envName, relativePath, label string) (string, string, error) {
	override := configuredPromptPath(envName, relativePath)
	candidates := []string{}
	if override != "" {
		candidates = append(candidates, override)
	} else {
		if cwd, err := os.Getwd(); err == nil {
			current := cwd
			for range 7 {
				candidates = append(candidates, filepath.Join(current, filepath.FromSlash(relativePath)))
				parent := filepath.Dir(current)
				if parent == current {
					break
				}
				current = parent
			}
		}
		if executable, err := os.Executable(); err == nil {
			candidates = append(candidates,
				filepath.Join(filepath.Dir(executable), "..", filepath.FromSlash(relativePath)),
				filepath.Join(filepath.Dir(executable), filepath.FromSlash(relativePath)),
			)
		}
	}
	seen := map[string]bool{}
	for _, candidate := range candidates {
		candidate = filepath.Clean(candidate)
		if candidate == "" || seen[candidate] {
			continue
		}
		seen[candidate] = true
		info, err := os.Stat(candidate)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return "", candidate, fmt.Errorf("读取%s失败: %w", label, err)
		}
		if info.IsDir() || info.Size() < 100 || info.Size() > 256*1024 {
			return "", candidate, fmt.Errorf("%s必须是100字到256KB的Markdown文件", label)
		}
		raw, err := os.ReadFile(candidate)
		if err != nil {
			return "", candidate, fmt.Errorf("读取%s失败: %w", label, err)
		}
		content := strings.TrimSpace(string(raw))
		if content == "" {
			return "", candidate, fmt.Errorf("%s为空", label)
		}
		absolute, _ := filepath.Abs(candidate)
		return content, promptVersionSource(absolute, content), nil
	}
	if override != "" {
		return "", override, fmt.Errorf("%s 指向的文件不存在", envName)
	}
	return "", relativePath, fmt.Errorf("项目内%s不存在", label)
}

func renderSellingPointExtractionRequest(promptMD, factPackage string) string {
	promptMD = strings.TrimSpace(promptMD)
	factPackage = strings.TrimSpace(factPackage)
	if strings.Contains(promptMD, "{{FACT_PACKAGE}}") {
		return renderStablePrefixTemplate(promptMD, "{{FACT_PACKAGE}}", factPackage)
	}
	return promptMD + "\n\n## 正式事实包\n\n" + factPackage
}

func renderSellingPointWheelReviewRequest(factPackage, draftWheel string) string {
	draft := json.RawMessage(strings.TrimSpace(draftWheel))
	data, _ := json.Marshal(map[string]any{
		"fact_package": strings.TrimSpace(factPackage),
		"draft_wheel":  draft,
	})
	return string(data)
}

func renderSellingPointWheelFinalAuditRequest(factPackage, draftWheel string, violations []string) string {
	draft := json.RawMessage(strings.TrimSpace(draftWheel))
	data, _ := json.Marshal(map[string]any{
		"fact_package":             strings.TrimSpace(factPackage),
		"draft_wheel":              draft,
		"hard_boundary_violations": violations,
	})
	return string(data)
}

type sellingPointWheelCoverageTarget struct {
	Name          string `json:"卖点名"`
	CurrentAngles int    `json:"当前角度数"`
	MinimumAngles int    `json:"期望最低角度数"`
}

func sellingPointWheelCoverageTargets(wheelJSON string, minimum int) ([]sellingPointWheelCoverageTarget, error) {
	if minimum < 1 {
		minimum = 1
	}
	var wheel anchorStyleSellingPointWheel
	if err := json.Unmarshal([]byte(strings.TrimSpace(wheelJSON)), &wheel); err != nil {
		return nil, err
	}
	targets := make([]sellingPointWheelCoverageTarget, 0)
	for _, item := range wheel.Items {
		if len(item.Angles) >= minimum {
			continue
		}
		targets = append(targets, sellingPointWheelCoverageTarget{
			Name: strings.TrimSpace(item.Name), CurrentAngles: len(item.Angles), MinimumAngles: minimum,
		})
	}
	return targets, nil
}

func renderSellingPointWheelExpansionRequest(factPackage, reviewedWheel string, targets []sellingPointWheelCoverageTarget) string {
	wheel := json.RawMessage(strings.TrimSpace(reviewedWheel))
	data, _ := json.Marshal(map[string]any{
		"fact_package":     strings.TrimSpace(factPackage),
		"reviewed_wheel":   wheel,
		"coverage_targets": targets,
	})
	return string(data)
}

func renderAnchorStyleAnalysisRequest(basePrompt, promptSource, scene, sample string) (string, error) {
	if strings.TrimSpace(scene) == "" {
		scene = "带货直播"
	}
	protocol, _, err := loadReplaceablePromptAsset("", "configs/prompts/anchor-style/style-extraction-integration.md", "风格集成协议")
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("# 基础分析提示词\n%s\n\n%s\n\n# 本次输入\n- 基础提示词来源：%s\n- 生成场景：%s\n\n【本次主播真实话术】\n%s", basePrompt, protocol, promptSource, scene, sample), nil
}

func renderAnchorStyleScorecardRequest(basePrompt, promptSource, sample string) string {
	return fmt.Sprintf(`# 本次必须执行的通用评分基准提取方法
%s

# 本次输入
- 通用框架来源：%s

【本次字段分工（覆盖基础方法中的统计要求）】
模型只输出“主播识别信息”和“禁用词（反推）”两个定性对象；程序计算全部量化值。
语气词用“候选语气词”和“各词功能”；称呼用“候选称呼”“称呼别名”和“称呼使用场景”。
禁止输出次数、每百字频次、比例、总字数和主力/辅助排序，不输出量化基准。
自指词不包含单独“我”“我们”。句长只给区间，算账句式给原句数组。
原句价格数字和句长区间可以保留，不能估计统计数字。

【本次主播真实话术样本】
%s

严格执行上面的通用框架，只返回一份合法 JSON。不要代码围栏，不要解释，不要把任何其他主播的默认词带进来。`, strings.TrimSpace(basePrompt), strings.TrimSpace(promptSource), strings.TrimSpace(sample))
}
