package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"unicode/utf8"

	"livecompanion/management/internal/model"
)

const maxAgentMaterialDraftItems = 500

type agentMaterialDraftItem struct {
	Destination    string          `json:"destination"`
	ScopeName      string          `json:"scope_name"`
	ScopeRaw       string          `json:"scope_raw,omitempty"`
	Attribute      string          `json:"attribute"`
	Value          string          `json:"value"`
	Polarity       string          `json:"polarity"`
	Conditions     json.RawMessage `json:"conditions"`
	Exceptions     json.RawMessage `json:"exceptions"`
	SourceQuote    string          `json:"source_quote,omitempty"`
	SourceSentence string          `json:"source_sentence"`
	SourceContext  []string        `json:"source_context"`
}

type agentMaterialDraftIssue struct {
	Index int    `json:"index"`
	Error string `json:"error"`
}

func (s *Server) planMaterialAgentDraft(w http.ResponseWriter, r *http.Request) {
	_, planID, tenantID, ok := s.resolvePlanMaterialClaimScope(w, r)
	if !ok {
		return
	}
	var input struct {
		JSON            string    `json:"json"`
		PreviewRevision string    `json:"preview_revision"`
		SelectedIDs     *[]string `json:"selected_ids,omitempty"`
	}
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "事实 JSON 请求格式不正确或超过 1 MB")
		return
	}
	var document struct {
		SchemaVersion int               `json:"schema_version"`
		Items         []json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal([]byte(input.JSON), &document); err != nil || document.SchemaVersion != 1 || len(document.Items) == 0 || len(document.Items) > maxAgentMaterialDraftItems {
		writeError(w, http.StatusBadRequest, "请选择含 1 至 500 条资料的第 1 版事实 JSON")
		return
	}
	catalog, err := s.store.PlanMaterialObjects(r.Context(), tenantID, planID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取方案对象失败")
		return
	}
	links, err := s.materialProductLinks(r, tenantID, planID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取方案链接失败")
		return
	}
	objects := materialRowObjects(materialAnalysisLinks(links, catalog.Directory), catalog)
	items, issues, source := prepareAgentMaterialDraft(document.Items, objects)
	// Normalize the model's free-form attribute names against the system
	// attribute catalog before preview/commit. Raw names stay in details_json.
	items = s.normalizeAgentMaterialAttributes(r.Context(), items)
	snapshot, _ := json.Marshal(items)
	previewRevision := materialSourceID(fmt.Sprintf("%d/%s/%s", catalog.Revision, materialSourceID(input.JSON), snapshot))
	if input.PreviewRevision != "" && input.PreviewRevision != previewRevision {
		writeError(w, http.StatusConflict, "预览或对象目录已更新，请重新预览转入")
		return
	}
	if input.SelectedIDs != nil {
		if input.PreviewRevision == "" {
			writeError(w, http.StatusBadRequest, "请先预览要转入的条目")
			return
		}
		var err error
		items, source, err = selectAgentMaterialDraft(items, source, *input.SelectedIDs)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	// Preview is read-only. Save quoted evidence when the user confirms the
	// transfer so the existing fact-commit handler can verify it later.
	if input.PreviewRevision != "" && len(items) > 0 {
		if err := s.store.SaveMaterialSource(r.Context(), tenantID, planID, materialSourceID(source), source); err != nil {
			writeError(w, http.StatusInternalServerError, "原句保存失败，未转入草稿，请重试")
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "issues": issues, "preview_revision": previewRevision})
}

func selectAgentMaterialDraft(items []model.PlanMaterialClaimInput, source string, ids []string) ([]model.PlanMaterialClaimInput, string, error) {
	if len(ids) == 0 || len(ids) > len(items) {
		return nil, "", errors.New("请选择要转入草稿库的条目")
	}
	remaining := make(map[string]bool, len(ids))
	for _, id := range ids {
		if remaining[id] {
			return nil, "", errors.New("转入条目重复，请重新预览")
		}
		remaining[id] = true
	}
	var selectedSource strings.Builder
	selected := make([]model.PlanMaterialClaimInput, 0, len(ids))
	ranges := make(map[model.MaterialSourceRange]model.MaterialSourceRange)
	for _, item := range items {
		if !remaining[item.ClientID] {
			continue
		}
		delete(remaining, item.ClientID)
		reference := *item.SourceRef
		reference.Ranges = make([]model.MaterialSourceRange, 0, len(item.SourceRef.Ranges))
		for _, span := range item.SourceRef.Ranges {
			newSpan, exists := ranges[span]
			if !exists {
				start := selectedSource.Len()
				selectedSource.WriteString(source[span.Start:span.End])
				newSpan = model.MaterialSourceRange{Start: start, End: selectedSource.Len()}
				selectedSource.WriteByte('\n')
				ranges[span] = newSpan
			}
			reference.Ranges = append(reference.Ranges, newSpan)
		}
		item.SourceRef = &reference
		selected = append(selected, item)
	}
	if len(remaining) != 0 {
		return nil, "", errors.New("转入条目与预览不一致，请重新预览")
	}
	raw := selectedSource.String()
	sourceID := materialSourceID(raw)
	for i := range selected {
		selected[i].SourceRef.ID = sourceID
	}
	return selected, raw, nil
}

func prepareAgentMaterialDraft(rows []json.RawMessage, objects []materialRowObject) ([]model.PlanMaterialClaimInput, []agentMaterialDraftIssue, string) {
	items := make([]model.PlanMaterialClaimInput, 0, len(rows))
	issues := []agentMaterialDraftIssue{}
	var source strings.Builder
	spans := map[string]model.MaterialSourceRange{}
	sourceSpan := func(quote string) model.MaterialSourceRange {
		if span, exists := spans[quote]; exists {
			return span
		}
		span := model.MaterialSourceRange{Start: source.Len(), End: source.Len() + len(quote)}
		source.WriteString(quote)
		source.WriteByte('\n')
		spans[quote] = span
		return span
	}
	for i, row := range rows {
		var input agentMaterialDraftItem
		if err := json.Unmarshal(row, &input); err != nil {
			issues = append(issues, agentMaterialDraftIssue{i + 1, "条目字段格式不正确"})
			continue
		}
		if err := validateAgentMaterialDraftItem(&input); err != nil {
			issues = append(issues, agentMaterialDraftIssue{i + 1, err.Error()})
			continue
		}
		sourceQuote := strings.TrimSpace(input.SourceQuote)
		if sourceQuote == "" || !strings.Contains(input.SourceSentence, sourceQuote) {
			sourceQuote = input.SourceSentence
		}
		ranges := []model.MaterialSourceRange{sourceSpan(input.SourceSentence)}
		evidence := []string{input.SourceSentence}
		for _, quote := range input.SourceContext {
			quote = strings.TrimSpace(quote)
			if quote != "" && !slices.Contains(evidence, quote) {
				evidence = append(evidence, quote)
				ranges = append(ranges, sourceSpan(quote))
			}
		}
		targets := agentMaterialDraftTargets(input.ScopeName, strings.Join(evidence, "\n"), objects)
		item := model.PlanMaterialClaimInput{
			MaterialClaimDetails: model.MaterialClaimDetails{
				Destination: input.Destination, ObjectTargets: targets, Exceptions: input.Exceptions,
				NeedsConfirmation: true, SourceRef: &model.MaterialSourceReference{Ranges: ranges},
			},
			ClaimKey: input.Attribute, Topic: input.Attribute, Attribute: input.Attribute,
			Value: input.Value, Content: input.Value, Polarity: input.Polarity,
			Conditions: input.Conditions, SourceQuote: sourceQuote, SourceSentence: input.SourceSentence,
		}
		if item.Destination == "fact" && item.Polarity == "" {
			item.Polarity = "positive"
		}
		annotateMaterialClaim(&item, strings.Join(evidence, "\n"))
		item.ScopeName = input.ScopeName
		item.RawScopeName = strings.TrimSpace(input.ScopeRaw)
		if input.Destination == "fact" && input.Polarity == "" {
			item.PolarityIssue = "请确认事实的肯定或否定"
		}
		// A shorter citation does not make an existing draft a new fact.
		input.SourceQuote = ""
		encoded, _ := json.Marshal(input)
		item.ClientID = "agent-" + materialSourceID(string(encoded))
		items = append(items, item)
	}
	raw := source.String()
	sourceID := materialSourceID(raw)
	for i := range items {
		item := &items[i]
		item.SourceRef.ID = sourceID
		if err := validateMaterialImportEvidence(item, raw); err != nil {
			item.SourceIssue = err.Error()
		}
		for _, span := range item.SourceRef.Ranges[1:] {
			item.SourceContext = append(item.SourceContext, raw[span.Start:span.End])
		}
	}
	return items, issues, raw
}

func validateAgentMaterialDraftItem(input *agentMaterialDraftItem) error {
	input.ScopeName = strings.TrimSpace(input.ScopeName)
	input.Attribute = strings.TrimSpace(input.Attribute)
	input.Value = strings.TrimSpace(input.Value)
	input.SourceSentence = strings.TrimSpace(input.SourceSentence)
	if input.Destination != "fact" && input.Destination != "material" {
		return errors.New("条目类型需为事实或表达素材")
	}
	if input.Destination == "fact" && input.Polarity != "" && input.Polarity != "positive" && input.Polarity != "negative" {
		return errors.New("极性字段格式不正确")
	}
	if input.Destination == "material" {
		input.Polarity = ""
	}
	if input.Attribute == "" || len([]rune(input.Attribute)) > 128 || input.Value == "" || utf8.RuneCountInString(input.Value) > 12000 {
		return errors.New("属性或值为空或过长")
	}
	if input.SourceSentence == "" || utf8.RuneCountInString(input.ScopeName) > 255 || len(input.SourceContext) > 7 {
		return errors.New("原句为空、对象名称过长或补充原句过多")
	}
	for _, predicates := range []*json.RawMessage{&input.Conditions, &input.Exceptions} {
		// Use the same shape the draft editor can subsequently display and edit.
		var entries []struct {
			Field    string   `json:"field,omitempty"`
			Op       string   `json:"op"`
			Values   []string `json:"values"`
			Raw      string   `json:"raw"`
			Universe string   `json:"universe,omitempty"`
			Status   string   `json:"status,omitempty"`
		}
		if err := json.Unmarshal(*predicates, &entries); err != nil || entries == nil {
			return errors.New("条件和例外都应填写为数组")
		}
		for i := range entries {
			entry := &entries[i]
			switch entry.Op {
			case "eq", "in", "not_in", "gt", "between":
				if entry.Field == "" || len(entry.Values) == 0 || (entry.Op == "between" && len(entry.Values) != 2) {
					entry.Status = "unresolved"
				}
			case "raw":
				entry.Status = "unresolved"
			default:
				return errors.New("条件或例外的判断方式无效")
			}
		}
		*predicates, _ = json.Marshal(entries)
	}
	return nil
}

func agentMaterialDraftTargets(label, source string, objects []materialRowObject) []model.MaterialObjectTarget {
	key := materialHeadingKey(label)
	targets := []model.MaterialObjectTarget{}
	if key == "" {
		return targets
	}
	for _, object := range objects {
		aliases := []string{object.DisplayName, object.ProductName, object.LinkKey}
		switch object.Kind {
		case "plan":
			aliases = append(aliases, "全局", "全场", "全门店", "全部链接")
		case "product":
			if number := materialSourceNumberedLink.FindStringSubmatch(materialHeadingKey(object.LinkKey)); number != nil {
				aliases = append(aliases, number[1]+"号链接", number[1]+"号", "第"+number[1]+"号链接")
			}
		}
		if slices.ContainsFunc(aliases, func(alias string) bool { return materialHeadingKey(alias) == key }) {
			targets = append(targets, object.MaterialObjectTarget)
		}
	}
	if len(targets) != 1 {
		return targets[:0]
	}
	if targets[0].Kind == "plan" && !strings.Contains(materialHeadingKey(source), key) {
		return targets[:0]
	}
	return materialRowTargets(targets, objects, source)
}
