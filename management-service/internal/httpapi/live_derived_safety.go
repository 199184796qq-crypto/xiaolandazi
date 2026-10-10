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
	Facts     string
	WheelJSON string
	lines     []string
	factLines []derivedFactLine
	wheel     anchorStyleSellingPointWheel
	Fields    []derivedField
	Reviews   []model.LiveRiskReview
	Statuses  map[string]model.LiveRiskReview
	Hashes    map[string]string
	SourceSHA string
	rewritten map[string]bool
}
type derivedFactLine struct {
	index      int
	prefix     string
	parts      []string
	separators []string
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
			return fmt.Sprintf("程序命中规则「%s」（%s），匹配内容“%s”。该规则需要结合完整语境复核，程序命中不直接等同于P0。", label, hit.RuleID, matched)
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
				semanticDecision = "not_required"
				if hit.ReviewMode == "semantic" {
					semanticDecision = "pending"
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
			if (review.SemanticDecision == "pass" || review.SemanticDecision == "evidence_required") && review.Status == "confirmed" && strings.TrimSpace(review.Evidence) != "" {
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
		claims = append(claims, r.ClaimSHA256+":"+s.Status+":"+s.SemanticDecision+":"+derivedHash(s.Evidence))
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
	prompt, _, err := loadReplaceablePromptAsset("ANCHOR_STYLE_FACT_RISK_SEMANTIC_PROMPT_PATH", "configs/prompts/anchor-style/compliance/fact-risk-semantic-review.md", "事实风险语义复核母版")
	if err != nil {
		return err
	}
	promptSHA := derivedHash(prompt)
	for i := range d.Reviews {
		if d.Reviews[i].Severity == "P0" {
			d.Reviews[i].SemanticPromptSHA = promptSHA
			if d.Reviews[i].RuleID == "l1.p0.absolute_claim" && firstSequenceReference.MatchString(d.Reviews[i].Text) && !requiresVerbatimGeneratedFact(d.Reviews[i].Text) {
				d.Reviews[i].RecheckLegacyBoundaryBlock = true
			}
		}
	}
	statuses, err := s.store.SyncLiveRiskReviews(ctx, tenant, plan, d.Reviews)
	if err != nil {
		return err
	}
	d.Statuses = statuses
	return nil
}

var derivedNumerals = regexp.MustCompile(`[0-9]+(?:\.[0-9]+)?`)

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
		if len(report.Hits) > 0 {
			return nil, fmt.Errorf("%s替换后仍有风险表达", r.ID)
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
	semanticPending := false
	for _, review := range d.Statuses {
		if review.Severity == "P0" && review.SemanticDecision == "pending" {
			semanticPending = true
			break
		}
	}
	if semanticPending {
		if _, err = s.reviewPendingFactRisks(ctx, tenant, plan); err != nil {
			return model.LiveDerivedSafetyAsset{}, err
		}
		d, err = s.loadDerivedInput(ctx, tenant, plan, p0, p1, p2)
		if err != nil {
			return model.LiveDerivedSafetyAsset{}, err
		}
		if err = s.syncDerivedReviews(ctx, tenant, plan, d); err != nil {
			return model.LiveDerivedSafetyAsset{}, err
		}
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
			return prior, errors.New("发现未确认的风险卖点，请先由后台配置安全改写模型")
		}
		data, _ := json.Marshal(map[string]any{"targets": targets, "safe_material": safeMaterial})
		ctx = withExchangeScope(ctx, appdb.ModelExchangeScope{TenantID: tenant, PlanID: plan})
		lastIssue := ""
		for attempt := 0; attempt < 3; attempt++ {
			result, _, callErr := s.completeAnchorStyleWorkbench(ctx, profile, "derived_assets_safe_rewrite", agentgateway.ResponseJSON, []agentgateway.Message{
				{Role: "system", Content: prompt + "\n\n" + p0 + "\n\n" + p1 + "\n\n" + p2},
				{Role: "user", Content: string(data) + "\n程序复检问题：" + lastIssue},
			}, 7000, false)
			if callErr != nil {
				return prior, callErr
			}
			changes, err = applyDerivedReplacements(d, result.Text, strings.Join(safeMaterial, "\n"), p0, p1, p2)
			if err == nil {
				break
			}
			lastIssue = err.Error()
		}
		if err != nil {
			return prior, fmt.Errorf("安全改写三次复检未通过，原始材料未修改：%w", err)
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
	for _, field := range d.Fields {
		if !d.rewritten[field.ID] && d.needsRewrite(field) {
			return prior, errors.New("完整派生事实包仍包含未处理风险，未放行保存发布")
		}
	}
	changesJSON, _ := json.Marshal(changes)
	notice := "事实包与卖点角度已完成风险检查，原始材料保持不变。"
	if len(changes) > 0 {
		notice = fmt.Sprintf("已将%d项未审核风险表达转换为安全事实说明/卖点角度，可继续保存和发布；原始材料与待审核资质保持不变。", len(changes))
	}
	return s.store.SaveLiveDerivedSafety(ctx, actor, model.LiveDerivedSafetyAsset{TenantID: tenant, PlanID: plan, SourceSHA256: d.SourceSHA, OriginalFacts: d.Facts, OriginalWheelJSON: d.WheelJSON, SafeFacts: strings.Join(d.lines, "\n"), SafeWheelJSON: safeWheel, PolicyHashesJSON: string(hashes), ChangesJSON: string(changesJSON), RewrittenCount: len(changes), Notice: notice})
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
