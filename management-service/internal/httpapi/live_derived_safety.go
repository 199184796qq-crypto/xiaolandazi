package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/contentguard"
	"livecompanion/management/internal/contentpools"
	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
)

type derivedField struct {
	ID             string `json:"id"`
	Text           string `json:"text"`
	MaxChars       int    `json:"max_chars"`
	slot           *string
	prefix         string
	Hits           []contentguard.Annotation `json:"hits,omitempty"`
	reviewScopes   map[string]string
	reviewSubjects map[string]string
}
type derivedInput struct {
	Facts       string
	AtomicFacts string
	WheelJSON   string
	lines       []string
	factLines   []derivedFactLine
	wheel       anchorStyleSellingPointWheel
	Fields      []derivedField
	Reviews     []model.LiveRiskReview
	Statuses    map[string]model.LiveRiskReview
	Hashes      map[string]string
	SourceSHA   string
	rewritten   map[string]bool
}
type derivedFactLine struct {
	index      int
	prefix     string
	parts      []string
	separators []string
}

// removedRisk 记录一条因无法安全改写而从安全事实包中整条剔除的风险事实，
// 供保存回执返回给前端定位（area + claim_sha256 与风险审查定位机制复用）。
type removedRisk struct {
	Area        string `json:"area"`
	ClaimSHA256 string `json:"claim_sha256"`
	Text        string `json:"text"`
	Reason      string `json:"reason"`
}

var derivedFactSeparators = regexp.MustCompile(`[；;]`)

func collapseContextualP0CoveredByEvidence(hits []contentguard.Annotation) []contentguard.Annotation {
	result := make([]contentguard.Annotation, 0, len(hits))
	for _, hit := range hits {
		covered := false
		if hit.Severity == "P0" && hit.ReviewMode == "semantic" {
			for _, evidence := range hits {
				if evidence.Severity == "P1" && evidence.Start < hit.End && evidence.End > hit.Start {
					covered = true
					break
				}
			}
		}
		if !covered {
			result = append(result, hit)
		}
	}
	return result
}

func (d *derivedInput) rebuildFactLines() {
	for _, line := range d.factLines {
		var builder strings.Builder
		builder.WriteString(line.prefix)
		for i, part := range line.parts {
			builder.WriteString(part)
			if i < len(line.separators) {
				builder.WriteString(line.separators[i])
			}
		}
		d.lines[line.index] = builder.String()
	}
}

func derivedHash(text string) string { return fmt.Sprintf("%x", sha256.Sum256([]byte(text))) }
func derivedClaimHash(field derivedField, hit contentguard.Annotation, hashes map[string]string) string {
	// A risk review represents a business question, not the place where that
	// question happened to be found. The same wording can appear in both the
	// fact package and the selling-point wheel, so source IDs must not create
	// duplicate review records.
	identity := field.Text
	if scope := field.reviewScopes[hit.RuleID]; scope != "" {
		identity = scope
	}
	return model.LiveRiskReviewClaimSHA256(identity, hit.RuleID, hit.Severity, hashes[hit.Severity])
}
func derivedRiskReviewReason(hit contentguard.Annotation) string {
	matched := strings.TrimSpace(hit.Text)
	label := strings.TrimSpace(hit.ReviewTitle)
	if label == "" {
		label = hit.Category
	}
	switch hit.Severity {
	case "P0":
		if hit.ReviewMode == "semantic" {
			return fmt.Sprintf("程序命中规则「%s」（%s），匹配内容“%s”。该规则需结合事实依据判断：事实卡有对应依据时由你确认放行，无依据则需修改或申请后台复核。", label, hit.RuleID, matched)
		}
		return fmt.Sprintf("命中P0硬拦截规则「%s」（%s），匹配内容“%s”。P0不允许人工确认放行，必须修改原始内容。", label, hit.RuleID, matched)
	case "P1":
		return fmt.Sprintf("命中P1依据核验规则「%s」（%s），匹配内容“%s”。%s", label, hit.RuleID, matched, strings.TrimSpace(hit.Note))
	case "P2":
		return fmt.Sprintf("命中P2表达确认规则「%s」（%s），匹配内容“%s”。%s", label, hit.RuleID, matched, strings.TrimSpace(hit.Note))
	default:
		return fmt.Sprintf("命中风险规则「%s」（%s），匹配内容“%s”。", label, hit.RuleID, matched)
	}
}

// firstSequenceReference 匹配「第一次/第一步/第一天/第一口」等序数词。
// 「第一」作序数词时不是排名宣传，不触发轮盘硬绝对词边界。
var firstSequenceReference = regexp.MustCompile(`第[一二三四五六七八九十百千万0-9]+(?:个|次|步|天|口|口吃|遍|眼|下|回|条|瓶|桶|袋|单|件|人|名|家|场|轮|款|代)`)

// hardAbsoluteTerms 是「不可语境豁免」的硬绝对化宣传词子集。这些词在直播口播
// 语境里几乎只能是宣传（唯一/独家/最低价/100%），不象「最好」可作食用建议、
// 「第一」可作序数词。轮盘（模型生成）若凭空编造这些词且事实包无逐字支撑，
// 必须强制 block，不能让用户误确认。
var hardAbsoluteTerms = []string{
	"唯一", "独家", "首个", "首家", "首选", "首创",
	"全网首发", "全球首发", "全国首家", "全国唯一",
	"中国第一", "全球第一", "全网第一", "销量第一", "排名第一", "第一品牌", "行业第一", "世界第一",
	"100%", "百分百", "最低价", "全网最低价", "全网最低",
	"零风险", "独一无二", "绝无仅有", "填补空白", "史无前例",
	"宇宙级", "国家级", "世界级", "遥遥领先",
}

// wheelGeneratedHardAbsolute 判断一条 absolute_claim 命中的 review 是否属于
// 「轮盘生成的硬绝对词、事实包无逐字支撑」。满足：来源含 wheel/ 前缀、文本含
// 硬绝对词、且归一化后的事实包正文里找不到这个命中的逐字内容。
func wheelGeneratedHardAbsolute(d *derivedInput, sourceKey, matched string) bool {
	if !strings.HasPrefix(sourceKey, "wheel/") {
		return false
	}
	normalized := model.NormalizeLiveRiskReviewText(matched)
	if normalized == "" {
		return false
	}
	hard := false
	for _, term := range hardAbsoluteTerms {
		if strings.Contains(normalized, term) {
			hard = true
			break
		}
	}
	if !hard {
		return false
	}
	// 「第一」类：只有非序数词（不是「第一次/第一步」）才算硬绝对词。
	if strings.Contains(normalized, "第一") && firstSequenceReference.MatchString(normalized) {
		return false
	}
	facts := model.NormalizeLiveRiskReviewText(d.Facts)
	return !strings.Contains(facts, normalized)
}
func buildDerivedInput(facts, wheelJSON, p0, p1, p2 string) (*derivedInput, error) {
	input := &derivedInput{Facts: facts, WheelJSON: wheelJSON, lines: strings.Split(facts, "\n"), Statuses: map[string]model.LiveRiskReview{}, rewritten: map[string]bool{}}
	emptyReport, err := contentguard.Annotate("", p0, p1, p2)
	if err != nil {
		return nil, err
	}
	input.Hashes = emptyReport.RuleHashes
	add := func(id string, slot *string, maxChars int, prefix string) {
		input.Fields = append(input.Fields, derivedField{ID: id, Text: strings.TrimPrefix(*slot, prefix), MaxChars: maxChars, slot: slot, prefix: prefix})
	}
	for i := range input.lines {
		line := input.lines[i]
		if strings.HasPrefix(line, "- [") {
			prefix := ""
			if at := strings.Index(line, "] "); at >= 0 {
				prefix = line[:at+2]
			}
			value := strings.TrimPrefix(line, prefix)
			parts, separators := []string{}, []string{}
			offset := 0
			for _, boundary := range derivedFactSeparators.FindAllStringIndex(value, -1) {
				parts = append(parts, value[offset:boundary[0]])
				separators = append(separators, value[boundary[0]:boundary[1]])
				offset = boundary[1]
			}
			parts = append(parts, value[offset:])
			input.factLines = append(input.factLines, derivedFactLine{index: i, prefix: prefix, parts: parts, separators: separators})
			for partIndex := range parts {
				add(fmt.Sprintf("facts/%d/%d", i, partIndex), &parts[partIndex], 4000, "")
			}
		}
	}
	if strings.TrimSpace(wheelJSON) != "" {
		canonical, _, _, err := normalizeAnchorStyleSellingPointWheelJSON(wheelJSON)
		if err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(canonical), &input.wheel); err != nil {
			return nil, err
		}
		for i := range input.wheel.Items {
			item := &input.wheel.Items[i]
			add(fmt.Sprintf("wheel/%d/name", i), &item.Name, 40, "")
			if item.TargetAudience != nil {
				add(fmt.Sprintf("wheel/%d/audience", i), item.TargetAudience, 200, "")
			}
			if item.PainPoint != nil {
				add(fmt.Sprintf("wheel/%d/pain", i), item.PainPoint, 200, "")
			}
			for j := range item.Angles {
				add(fmt.Sprintf("wheel/%d/angle/%d", i, j), &item.Angles[j], 120, "")
			}
		}
	}
	for i := range input.Fields {
		field := &input.Fields[i]
		report, err := contentguard.Annotate(field.Text, p0, p1, p2)
		if err != nil {
			return nil, err
		}
		field.Hits = collapseContextualP0CoveredByEvidence(report.Hits)
		input.Hashes = report.RuleHashes
	}
	if err := input.assignReviewScopes(p0, p1, p2); err != nil {
		return nil, err
	}
	seenReviews := map[string]int{}
	for i := range input.Fields {
		field := &input.Fields[i]
		for _, hit := range field.Hits {
			hash := derivedClaimHash(*field, hit, input.Hashes)
			source := model.LiveRiskReviewSource{SourceKey: field.ID, Text: field.Text}
			if at, exists := seenReviews[hash]; exists {
				duplicate := false
				for _, prior := range input.Reviews[at].Sources {
					if prior == source {
						duplicate = true
						break
					}
				}
				if !duplicate {
					input.Reviews[at].Sources = append(input.Reviews[at].Sources, source)
				}
				continue
			}
			seenReviews[hash] = len(input.Reviews)
			text := field.Text
			if strings.HasPrefix(field.reviewScopes[hit.RuleID], "atomic/") {
				text = hit.ReviewTitle
			}
			semanticDecision := "not_applicable"
			if hit.Severity == "P0" {
				// 语义复核已下线：semantic 类 P0（绝对词/限时/数据背书）归为 not_required，
				// 由用户确认/举证直接放行；hard 类 P0 标记为 hard，只能提交后台复核。
				if hit.ReviewMode == "semantic" {
					semanticDecision = "not_required"
				} else {
					semanticDecision = "hard"
				}
				// 轮盘绝对化用语程序边界（不依赖模型）：轮盘（模型生成的卖点角度）
				// 里出现硬绝对词（唯一/独家/最低价/100% 等），且事实包没有逐字支撑时，
				// 强制标记为 hard，用户不能直接确认放行，只能修改原文或提交后台复核。
				// 防止模型在轮盘里"发明"绝对化宣传词后被用户误确认。
				if hit.RuleID == "l1.p0.absolute_claim" && wheelGeneratedHardAbsolute(input, field.ID, text) {
					semanticDecision = "hard"
				}
			}
			input.Reviews = append(input.Reviews, model.LiveRiskReview{ClaimSHA256: hash, SourceKey: field.ID, Text: text, RuleID: hit.RuleID, Severity: hit.Severity, RuleSHA256: input.Hashes[hit.Severity], Status: "pending", Reason: derivedRiskReviewReason(hit), ProgramDecision: "matched", SemanticDecision: semanticDecision, SemanticKey: field.reviewScopes[hit.RuleID], Subject: field.reviewSubjects[hit.RuleID], Sources: []model.LiveRiskReviewSource{source}})
		}
	}
	return input, nil
}
func (d *derivedInput) needsRewrite(f derivedField) bool {
	for _, hit := range f.Hits {
		review := d.Statuses[derivedClaimHash(f, hit, d.Hashes)]
		if hit.Severity == "P0" {
			// 语义复核已下线：P0 与 P1/P2 一样，用户确认/举证即放行。
			if review.Status == "confirmed" && strings.TrimSpace(review.Evidence) != "" {
				continue
			}
			return true
		}
		if (hit.Severity != "P1" && hit.Severity != "P2") || review.Status != "confirmed" || strings.TrimSpace(review.Evidence) == "" {
			return true
		}
	}
	return false
}
func (d *derivedInput) fingerprint(extra string) string {
	claims := []string{}
	for _, r := range d.Reviews {
		s := d.Statuses[r.ClaimSHA256]
		claims = append(claims, r.ClaimSHA256+":"+s.Status+":"+derivedHash(s.Evidence))
	}
	sort.Strings(claims)
	raw, _ := json.Marshal(struct {
		Facts, Wheel, Extra string
		Hashes              map[string]string
		Claims              []string
	}{d.Facts, d.WheelJSON, extra, d.Hashes, claims})
	return derivedHash(string(raw))
}
func (s *Server) loadDerivedInput(ctx context.Context, tenant, plan int64, p0, p1, p2 string) (*derivedInput, error) {
	planData, err := s.store.GetLiveAgentPlan(ctx, tenant, plan)
	if err != nil {
		return nil, err
	}
	facts, err := s.store.ListLiveAgentPlanFacts(ctx, tenant, plan)
	if err != nil {
		return nil, err
	}
	benefits, err := s.store.ListActiveLiveAgentPlanBenefits(ctx, tenant, plan, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	links, err := s.store.ListLiveAgentPlanProductLinks(ctx, tenant, plan)
	if err != nil {
		return nil, err
	}
	wheel, err := s.store.GetActiveLiveSellingPointWheel(ctx, tenant, plan)
	if err != nil && !errors.Is(err, appdb.ErrLiveSellingPointWheelNotFound) {
		return nil, err
	}
	scopes, err := s.store.ListBenefitScopes(ctx, tenant, plan)
	if err != nil {
		return nil, err
	}
	benefits = applyContentBenefitScopes(benefits, links, scopes)
	text, origins := renderAnchorStyleWorkbenchFactsWithOrigins(planData, facts, benefits, links)
	corrections, correctionErr := s.store.ListContentCorrections(ctx, tenant, plan)
	if correctionErr != nil {
		return nil, correctionErr
	}
	correctionSource := contentpools.Sources{Corrections: corrections}
	for _, link := range links {
		correctionSource.Products = append(correctionSource.Products, contentpools.Product{ID: link.ID, Link: link.LinkKey})
	}
	text += contentpools.RenderCorrections(correctionSource)
	d, err := buildDerivedInput(text, wheel.EffectiveWheelJSON(), p0, p1, p2)
	if err == nil {
		attachRiskSourceOrigins(d, origins)
	}
	return d, err
}

func attachRiskSourceOrigins(d *derivedInput, origins map[int]model.LiveRiskReviewSource) {
	for i := range d.Reviews {
		for j := range d.Reviews[i].Sources {
			source := &d.Reviews[i].Sources[j]
			var line, part int
			if _, err := fmt.Sscanf(source.SourceKey, "facts/%d/%d", &line, &part); err == nil {
				origin, exists := origins[line]
				if exists {
					source.Area, source.EntityID = origin.Area, origin.EntityID
					if field, _, ok := strings.Cut(source.Text, "="); ok {
						source.Field = strings.TrimSpace(field)
					}
				}
			} else if strings.HasPrefix(source.SourceKey, "wheel/") {
				source.Area = "miner"
			}
		}
	}
}
func (s *Server) syncDerivedReviews(ctx context.Context, tenant, plan int64, d *derivedInput) error {
	// 语义复核已下线：不再给 P0 风险卡设置 semantic_decision=pending，也不再触发
	// 模型语境判断。P0 与 P1/P2 一样，由用户在风险卡界面直接确认/举证放行。
	statuses, err := s.store.SyncLiveRiskReviews(ctx, tenant, plan, d.Reviews)
	if err != nil {
		return err
	}
	d.Statuses = statuses
	return nil
}

var derivedNumerals = regexp.MustCompile(`[0-9]+(?:\.[0-9]+)?`)

// consumptionHintWords 识别"保存/食用/加热"等消费提示语境。依据《广告绝对化用语执法指南》
// 第六条第(二)项：仅宣传使用方法、使用时间、保存期限等消费提示的，不适用绝对化用语规定。
// 词表是通用消费行为词，产品无关，换食品/建材/日化都适用。
var consumptionHintWords = regexp.MustCompile(`(保存|食用|饮用|冷藏|加热|复热|当天|吃完|保质|存放|尽快|建议|即食|吃法|口感|风味)`)

// isConsumptionHintHit 判断 absolute_claim 的"最/最好/最佳"命中是否属于消费提示语境。
// 只有"最/最好/最佳"这类修饰消费行为的词，且整个字段是消费提示，才不算绝对化宣传；
// "第一/唯一/独家/老字号"等资质排名词，或"全网最好"这种修饰商品质量的，仍拦截。
func isConsumptionHintHit(text string, h contentguard.Annotation) bool {
	if h.RuleID != "l1.p0.absolute_claim" {
		return false
	}
	matched := strings.TrimSpace(h.Text)
	if matched != "最" && matched != "最好" && matched != "最佳" && matched != "最对味" {
		return false
	}
	return consumptionHintWords.MatchString(text)
}

func applyDerivedReplacements(d *derivedInput, raw, allowed, p0, p1, p2 string) ([]map[string]string, error) {
	var output struct {
		Replacements []struct {
			ID   string `json:"id"`
			Text string `json:"text"`
		} `json:"replacements"`
	}
	decoder := json.NewDecoder(strings.NewReader(stripPolicyJSONFence(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&output); err != nil {
		return nil, err
	}
	if decoder.Decode(new(any)) != io.EOF {
		return nil, errors.New("安全改写返回含额外内容")
	}
	targets := map[string]derivedField{}
	for _, f := range d.Fields {
		if d.needsRewrite(f) {
			targets[f.ID] = f
		}
	}
	if len(output.Replacements) != len(targets) {
		return nil, errors.New("安全改写没有逐项对应全部风险字段")
	}
	replacements := map[string]string{}
	allowedNumbers := map[string]bool{}
	for _, n := range derivedNumerals.FindAllString(allowed, -1) {
		allowedNumbers[n] = true
	}
	changes := []map[string]string{}
	for _, r := range output.Replacements {
		field, ok := targets[r.ID]
		if !ok {
			return nil, errors.New("安全改写包含未知字段")
		}
		if _, ok = replacements[r.ID]; ok {
			return nil, errors.New("安全改写字段重复")
		}
		text := strings.TrimSpace(r.Text)
		if text == "" || strings.ContainsAny(text, "\r\n") || utf8.RuneCountInString(text) > field.MaxChars {
			return nil, errors.New("安全改写为空、跨行或超过字段长度")
		}
		report, err := contentguard.Annotate(text, p0, p1, p2)
		if err != nil {
			return nil, err
		}
		// New replacements have no independent user proof. Rechecking all three tiers
		// prevents the model from swapping one unconfirmed claim for another.
		// hard 命中直接拦；semantic 命中（如"最好"）只在非消费提示语境才拦，
		// "最好当天吃完"这类保存期限/使用方法消费提示不算绝对化宣传（《指南》第六条第(二)项）。
		for _, h := range report.Hits {
			if h.ReviewMode == "hard" || !isConsumptionHintHit(text, h) {
				return nil, fmt.Errorf("%s替换后仍有风险表达", r.ID)
			}
		}
		for _, n := range derivedNumerals.FindAllString(text, -1) {
			if !allowedNumbers[n] {
				return nil, fmt.Errorf("%s引入了安全材料之外的新数字", r.ID)
			}
		}
		replacements[r.ID] = text
		changes = append(changes, map[string]string{"id": r.ID, "before": field.Text, "after": text})
	}
	// Roll back derived fields if pool structural validation rejects the patch.
	previous := map[*string]string{}
	for _, f := range d.Fields {
		if text, ok := replacements[f.ID]; ok {
			previous[f.slot] = *f.slot
			*f.slot = f.prefix + text
		}
	}
	if d.WheelJSON != "" {
		rawWheel, _ := json.Marshal(d.wheel)
		if _, _, _, err := normalizeAnchorStyleSellingPointWheelJSON(string(rawWheel)); err != nil {
			for slot, text := range previous {
				*slot = text
			}
			return nil, err
		}
	}
	d.rebuildFactLines()
	for _, field := range d.Fields {
		if _, changed := replacements[field.ID]; changed {
			continue
		}
		if d.needsRewrite(field) {
			for slot, text := range previous {
				*slot = text
			}
			d.rebuildFactLines()
			return nil, errors.New("重组事实包仍包含未处理风险，安全派生版本不放行")
		}
	}
	for id := range replacements {
		d.rewritten[id] = true
	}
	return changes, nil
}

// removeUnsafeFactLines 找出改写后仍未处理风险的事实字段，把其所在的整条事实
// 行从 d.lines 中删除（不再进入 SafeFacts），并返回剔除的事实数及风险明细。
// 只处理 facts/* 字段；wheel/* 卖点字段保持结构化、不在此按行剔除。
func (d *derivedInput) removeUnsafeFactLines() (int, []removedRisk) {
	// 找出所有「未改写且仍需改写」的事实字段，映射到其所在行号。
	lineRemoved := map[int]bool{}
	removed := []removedRisk{}
	byLine := map[int][]derivedField{}
	for _, field := range d.Fields {
		if d.rewritten[field.ID] || !d.needsRewrite(field) {
			continue
		}
		if !strings.HasPrefix(field.ID, "facts/") {
			continue
		}
		line, ok := d.factLineIndex(field.ID)
		if !ok {
			continue
		}
		byLine[line] = append(byLine[line], field)
		lineRemoved[line] = true
	}
	if len(lineRemoved) == 0 {
		return 0, nil
	}
	// 逐行剔除，并收集该行首条风险明细（claim_sha256 供前端定位）。
	for _, field := range d.Fields {
		if d.rewritten[field.ID] || !d.needsRewrite(field) || !strings.HasPrefix(field.ID, "facts/") {
			continue
		}
		line, ok := d.factLineIndex(field.ID)
		if !ok || !lineRemoved[line] {
			continue
		}
		claim := d.fieldClaimHash(field)
		reason := d.fieldRiskReason(field)
		removed = append(removed, removedRisk{Area: "knowledge", ClaimSHA256: claim, Text: field.Text, Reason: reason})
		// 同一行只记录一次。
		lineRemoved[line] = false
	}
	// 重建 lines：跳过被剔除的行。
	kept := make([]string, 0, len(d.lines))
	for i, line := range d.lines {
		if _, drop := byLine[i]; drop {
			continue
		}
		kept = append(kept, line)
	}
	d.lines = kept
	return len(removed), removed
}

// factLineIndex 从字段 ID（facts/<行号>/<子句号>）解析行号。
func (d *derivedInput) factLineIndex(fieldID string) (int, bool) {
	rest, ok := strings.CutPrefix(fieldID, "facts/")
	if !ok {
		return 0, false
	}
	head, _, _ := strings.Cut(rest, "/")
	idx, err := strconv.Atoi(head)
	if err != nil {
		return 0, false
	}
	return idx, true
}

// fieldClaimHash 返回字段对应风险审查记录的 claim_sha256（取首个命中）。
func (d *derivedInput) fieldClaimHash(field derivedField) string {
	for _, hit := range field.Hits {
		return derivedClaimHash(field, hit, d.Hashes)
	}
	return ""
}

// fieldRiskReason 汇总字段命中的风险原因，供前端展示。
func (d *derivedInput) fieldRiskReason(field derivedField) string {
	reasons := []string{}
	for _, hit := range field.Hits {
		reasons = append(reasons, derivedRiskReviewReason(hit))
	}
	return strings.Join(reasons, "；")
}

func (s *Server) prepareDerivedSafety(ctx context.Context, tenant, plan, actor int64) (model.LiveDerivedSafetyAsset, error) {
	p0, p1, p2, err := loadRiskAnnotationMothers()
	if err != nil {
		return model.LiveDerivedSafetyAsset{}, err
	}
	prompt, source, err := loadReplaceablePromptAsset("ANCHOR_STYLE_DERIVED_SAFETY_PROMPT_PATH", "configs/prompts/anchor-style/compliance/derived-assets-safe-rewrite.md", "派生卖点安全改写母版")
	if err != nil {
		return model.LiveDerivedSafetyAsset{}, err
	}
	d, err := s.loadDerivedInput(ctx, tenant, plan, p0, p1, p2)
	if err != nil {
		return model.LiveDerivedSafetyAsset{}, err
	}
	if err = s.syncDerivedReviews(ctx, tenant, plan, d); err != nil {
		return model.LiveDerivedSafetyAsset{}, err
	}
	d.SourceSHA = d.fingerprint(source)
	prior, err := s.store.GetLiveDerivedSafety(ctx, tenant, plan, d.SourceSHA)
	if err == nil {
		return prior, nil
	}
	if !errors.Is(err, appdb.ErrDerivedSafetyNotFound) {
		return prior, err
	}
	targets := []derivedField{}
	safeMaterial := []string{}
	for _, f := range d.Fields {
		if d.needsRewrite(f) {
			targets = append(targets, f)
		} else {
			safeMaterial = append(safeMaterial, f.Text)
		}
	}
	changes := []map[string]string{}
	if len(targets) > 0 {
		config, err := s.store.AnchorGenerationSettings(ctx)
		if err != nil {
			return prior, err
		}
		profile := config.SafetyProfileID
		if profile == "" {
			profile = config.SellingPointProfileID
		}
		if profile == "" {
			// 未配置安全改写模型：视为本次无法改写，交由后续剔除未处理风险事实。
			changes = nil
		} else {
			data, _ := json.Marshal(map[string]any{"targets": targets, "safe_material": safeMaterial})
			ctx = withExchangeScope(ctx, appdb.ModelExchangeScope{TenantID: tenant, PlanID: plan})
			lastIssue := ""
			for attempt := 0; attempt < 3; attempt++ {
				result, _, callErr := s.completeAnchorStyleWorkbench(ctx, profile, "derived_assets_safe_rewrite", agentgateway.ResponseJSON, []agentgateway.Message{
					{Role: "system", Content: prompt + "\n\n" + p0 + "\n\n" + p1 + "\n\n" + p2},
					{Role: "user", Content: string(data) + "\n程序复检问题：" + lastIssue},
				}, 7000, false)
				if callErr != nil {
					// 模型调用失败视为本次无法改写，跳出循环改由后续剔除未处理风险事实。
					err = callErr
					changes = nil
					break
				}
				changes, err = applyDerivedReplacements(d, result.Text, strings.Join(safeMaterial, "\n"), p0, p1, p2)
				if err == nil {
					break
				}
				lastIssue = err.Error()
			}
			// 改写未完成（三次复检未通过或模型调用失败）：不再拦保存，改由后续
			// removeUnsafeFactLines 把这些未处理风险事实整条剔除并回报。
			if err != nil {
				changes = nil
			}
		}
	}
	// Check current sources again after model latency; never release a stale snapshot.
	current, err := s.loadDerivedInput(ctx, tenant, plan, p0, p1, p2)
	if err != nil {
		return prior, err
	}
	if err = s.syncDerivedReviews(ctx, tenant, plan, current); err != nil {
		return prior, err
	}
	if current.fingerprint(source) != d.SourceSHA {
		return prior, errors.New("原始事实、轮盘或审核状态已变化，请重新保存")
	}
	safeWheel := d.WheelJSON
	if safeWheel != "" {
		raw, _ := json.Marshal(d.wheel)
		safeWheel, _, _, err = normalizeAnchorStyleSellingPointWheelJSON(string(raw))
		if err != nil {
			return prior, err
		}
	}
	hashes, _ := json.Marshal(d.Hashes)
	// 对改写后仍存在未处理风险的事实字段，不再拦保存：改为把该风险事实整条从
	// 安全事实包中剔除，并收集风险明细，随保存回执返回供前端定位。轮盘卖点（wheel/*）
	// 属结构化字段，剔除策略单独处理，不在这里按行删除。
	removed, remainingRisk := d.removeUnsafeFactLines()
	changesJSON, _ := json.Marshal(changes)
	notice := "事实包与卖点角度已完成风险检查，原始材料保持不变。"
	if len(changes) > 0 {
		notice = fmt.Sprintf("已将%d项未审核风险表达转换为安全事实说明/卖点角度，可继续保存和发布；原始材料与待审核资质保持不变。", len(changes))
	}
	if removed > 0 {
		notice += fmt.Sprintf("另有%d条风险事实因未能安全改写已从本次方案中剔除，不进入口播素材。", removed)
	}
	removedJSON, _ := json.Marshal(remainingRisk)
	saved, saveErr := s.store.SaveLiveDerivedSafety(ctx, actor, model.LiveDerivedSafetyAsset{TenantID: tenant, PlanID: plan, SourceSHA256: d.SourceSHA, OriginalFacts: d.Facts, OriginalWheelJSON: d.WheelJSON, SafeFacts: strings.Join(d.lines, "\n"), SafeWheelJSON: safeWheel, PolicyHashesJSON: string(hashes), ChangesJSON: string(changesJSON), RewrittenCount: len(changes), RemovedRisksJSON: string(removedJSON), RemovedRiskCount: removed, Notice: notice})
	if saveErr != nil {
		return saved, saveErr
	}
	// 风险剔除明细只随本次保存回执传递，不落库（避免改动 live_derived_safety_assets
	// 表结构）；落库读回会丢失该内存字段，这里填回。
	saved.RemovedRisksJSON = string(removedJSON)
	saved.RemovedRiskCount = removed
	return saved, nil
}

func (s *Server) liveDerivedSafetyPrepare(w http.ResponseWriter, r *http.Request) {
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
	asset, err := s.prepareDerivedSafety(r.Context(), tenant, plan, actor.UserID)
	if err != nil {
		writeError(w, 409, err.Error())
		return
	}
	writeJSON(w, 200, asset)
}
