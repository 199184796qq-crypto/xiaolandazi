package interactionplan

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"

	"livecompanion/management/internal/agentgateway"
)

// Diagnostics never enter Intent, the next model prompt or the spoken body.
// Ranges are inclusive, one-based lines in the original supplier text. The raw
// response remains in the existing model-exchange receipt; its hash joins it to
// these diagnostics without copying customer content into another log.
type ParseWarning struct {
	Code  string `json:"code"`
	Field string `json:"field,omitempty"`
	Line  int    `json:"line,omitempty"`
}
type ParseRange struct {
	Start int `json:"start_line"`
	End   int `json:"end_line"`
}
type ParseDiagnostics struct {
	RawSHA256    string         `json:"raw_sha256"`
	FinishReason string         `json:"finish_reason"`
	Boundary     string         `json:"boundary,omitempty"`
	BodyRanges   []ParseRange   `json:"body_ranges,omitempty"`
	Warnings     []ParseWarning `json:"warnings,omitempty"`
	Outcome      string         `json:"outcome"`
	Reason       string         `json:"reason,omitempty"`
	GroupIndex   int            `json:"group_index,omitempty"`
	GroupCount   int            `json:"group_count,omitempty"`
	SpokenField  string         `json:"spoken_field,omitempty"`
	Assignment   string         `json:"assignment,omitempty"`
}

type parseRequestKey struct{}

// WithParseRequest supplies the existing task/batch identity for local logs;
// it does not change the model request or create another model call.
func WithParseRequest(ctx context.Context, request string) context.Context {
	return context.WithValue(ctx, parseRequestKey{}, request)
}
func logParseDiagnostics(ctx context.Context, scope Scope, result BatchResult) {
	request, _ := ctx.Value(parseRequestKey{}).(string)
	if len(result.Issues) > 0 {
		entry := struct {
			Scope    Scope    `json:"scope"`
			Request  string   `json:"request,omitempty"`
			Inputs   int      `json:"input_items"`
			Groups   int      `json:"content_groups"`
			Speech   int      `json:"accepted_speech_groups"`
			Clarify  int      `json:"accepted_clarification_groups"`
			Unmapped int      `json:"unmapped_groups"`
			Issues   []string `json:"issues"`
		}{scope, request, len(result.Items), result.Groups, result.SpeechGroups, result.ClarificationGroups, result.UnmappedGroups, result.Issues}
		encoded, _ := json.Marshal(entry)
		log.Printf("interaction_parse_batch %s", encoded)
	}
	for _, item := range result.Items {
		if item.Err == nil && len(item.Parse.Warnings) == 0 {
			continue
		}
		entry := struct {
			Scope   Scope            `json:"scope"`
			Request string           `json:"request,omitempty"`
			Item    string           `json:"item_id"`
			Parse   ParseDiagnostics `json:"parse"`
		}{scope, request, item.ID, item.Parse}
		encoded, _ := json.Marshal(entry)
		log.Printf("interaction_parse %s", encoded)
	}
}

func (d *ParseDiagnostics) warn(code, field string, line int) {
	if len(d.Warnings) < 64 {
		d.Warnings = append(d.Warnings, ParseWarning{code, field, line})
	} else if len(d.Warnings) == 64 {
		d.Warnings = append(d.Warnings, ParseWarning{Code: "warning_limit"})
	}
}

type outputField struct {
	key, value string
	span       ParseRange
}
type outputBlock struct {
	id          string
	intentSeen  bool
	outputSeen  bool
	endMatched  bool
	badIdentity bool
	fields      []*outputField
	last        *outputField
	closed      bool
	err         error
	diag        ParseDiagnostics
}

func canonicalOutputKey(key string) string {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "item_id", "id":
		return "编号"
	case "end":
		return "结束"
	case "正文", "答复", "body", "reply":
		return "话术"
	case "intent":
		return "意图"
	case "type":
		return "类型"
	case "ambiguity":
		return "歧义"
	}
	return key
}

// Only explicit labels introduce fields. Markdown decoration is removed from
// labels, never from the body text itself. Arbitrary JSON or bare text is not
// guessed into one of the requested people's answers.
func outputLine(line string) (key, value string, ok bool) {
	line = strings.TrimSpace(strings.TrimPrefix(line, "\ufeff"))
	line = strings.TrimPrefix(line, "- ")
	key, value, ok = splitKVLine(line)
	if strings.HasPrefix(key, "**") && !strings.HasSuffix(key, "**") && strings.HasSuffix(value, "**") {
		value = strings.TrimSuffix(value, "**")
	}
	key = strings.TrimSpace(strings.Trim(key, "*`"))
	return
}

// Recognize a detached opaque label as framing, never as spoken continuation.
// Malformed U1-style labels remain unusable identities and use group fallback.
func looseOutputFraming(line string) (string, string, bool) {
	bare := func(value string) string {
		value = strings.TrimSpace(strings.Trim(strings.TrimSpace(value), "#*`[]【】"))
		if len(value) < 2 || len(value) > 4 || value[0] != 'U' {
			return ""
		}
		for _, c := range value[1:] {
			if c < '0' || c > '9' {
				return ""
			}
		}
		return value
	}
	for _, key := range []string{"编号", "结束"} {
		if strings.HasPrefix(line, key) {
			if value := bare(strings.TrimSpace(strings.TrimPrefix(line, key))); value != "" {
				return key, value, true
			}
		}
	}
	if value := bare(line); value != "" {
		return "编号", value, true
	}
	return "", "", false
}

// First group the supplier text by intent and terminal spoken fields. Then map
// groups to program task IDs. Complete unique labels take precedence. When
// labels are unusable, exact group counts and consistent remaining labels allow
// the user-authorized input-order fallback; omissions never shift later replies.
func parseBatch(s Snapshot, items []BatchItem, raw string, finishReason ...string) BatchResult {
	out := BatchResult{Items: make([]ItemResult, len(items))}
	indices := make(map[string]int, len(items))
	diagnostic := ParseDiagnostics{RawSHA256: fmt.Sprintf("%x", sha256.Sum256([]byte(raw))), Outcome: "rejected"}
	if len(finishReason) != 0 {
		diagnostic.FinishReason = finishReason[0]
	}
	for i, item := range items {
		indices[item.ID] = i
		out.Items[i] = ItemResult{ID: item.ID, Err: errors.New("模型遗漏该人结果"), Parse: diagnostic}
	}
	if len(raw) > 128<<10 {
		out.Issues = append(out.Issues, "互动批次返回超出范围")
		for i := range out.Items {
			out.Items[i].Err = errors.New("互动批次返回超出范围")
			out.Items[i].Parse.Reason = "response_too_large"
		}
		return out
	}
	lines := strings.Split(strings.ReplaceAll(strings.ReplaceAll(raw, "\r\n", "\n"), "\r", "\n"), "\n")
	var groups []*outputBlock
	var active *outputBlock
	begin := func(id string) {
		active = &outputBlock{id: id, diag: diagnostic}
		groups = append(groups, active)
		active.diag.GroupIndex = len(groups)
	}
	closeGroup := func(boundary string, line int) {
		if active == nil || active.closed {
			return
		}
		active.closed = true
		active.diag.Boundary = boundary
		if boundary == "explicit_end" {
			return
		}
		if !active.outputSeen {
			active.diag.warn("missing_output_fields", "", line)
		}
	}
	for i, sourceLine := range lines {
		lineNo := i + 1
		line := strings.TrimSpace(strings.TrimPrefix(sourceLine, "\ufeff"))
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "```") || strings.HasPrefix(line, "~~~") {
			if active != nil {
				active.diag.warn("markdown_fence", "", lineNo)
			}
			continue
		}
		key, value, labeled := outputLine(line)
		looseFraming := false
		if !labeled {
			key, value, looseFraming = looseOutputFraming(line)
			labeled = looseFraming
		}
		originalKey := key
		key = canonicalOutputKey(key)
		if labeled && key == "编号" {
			if active != nil && active.id == value && len(active.fields) == 0 && !active.closed {
				active.diag.warn("identical_field_repeated", key, lineNo)
				continue
			}
			closeGroup("next_item", lineNo)
			begin(value)
			if looseFraming {
				active.diag.warn("loose_identity_framing", key, lineNo)
			}
			if key != originalKey {
				active.diag.warn("field_alias", key, lineNo)
			}
			continue
		}
		if labeled && key == "结束" {
			if looseFraming && active != nil {
				active.diag.warn("loose_identity_framing", key, lineNo)
			}
			// Complete legacy framing wins. A malformed pair falls back to the
			// content group, rather than discarding an otherwise usable body.
			if active == nil {
				out.Issues = append(out.Issues, "孤立的结束编号，未绑定正文")
				continue
			}
			if active.id == "" && value != "" {
				active.id = value
				active.diag.Assignment = "end_id"
				active.diag.warn("missing_start_id", "编号", lineNo)
				closeGroup("content_end", lineNo)
			} else if active.id == value && value != "" {
				active.endMatched = true
				closeGroup("explicit_end", lineNo)
			} else {
				active.badIdentity = true
				active.diag.warn("mismatched_end", key, lineNo)
				closeGroup("content_end", lineNo)
			}
			active = nil
			continue
		}
		if !labeled {
			if active != nil && active.last != nil && (bodyField(active.last.key) || active.last.key == "澄清句") {
				active.last.span.End = lineNo
				if len(active.last.value)+len(line)+1 > 4096 {
					active.err = errors.New("互动正文超过解析长度上限")
					active.diag.warn("body_too_large", active.last.key, lineNo)
					continue
				}
				active.last.value = strings.TrimSpace(active.last.value + "\n" + line)
				active.diag.warn("multiline_body", active.last.key, lineNo)
			} else if active != nil {
				active.last = nil
				active.diag.warn("unlabeled_metadata_ignored", "", lineNo)
			}
			continue
		}
		if key == "意图" && active != nil && active.intentSeen && active.outputSeen {
			closeGroup("next_intent", lineNo)
			// The next intent starts a new content group, but cannot inherit the
			// previous person's ID. A multi-person reply must carry its own label.
			begin("")
		}
		if active == nil {
			if !isInteractionField(key) {
				continue
			}
			begin("")
		}
		if !isInteractionField(key) {
			active.diag.warn("auxiliary_field_ignored", "", lineNo)
			active.last = nil
			continue
		}
		if originalKey != key {
			active.diag.warn("field_alias", key, lineNo)
		}
		if active.outputSeen && !bodyField(key) && key != "澄清句" {
			active.diag.warn("field_order", key, lineNo)
		}
		field := &outputField{key: key, value: value, span: ParseRange{lineNo, lineNo}}
		active.fields = append(active.fields, field)
		active.last = field
		if key == "意图" {
			active.intentSeen = true
		}
		if bodyField(key) || key == "澄清句" {
			active.outputSeen = true
		}
	}
	completion := agentgateway.Response{FinishReason: diagnostic.FinishReason}
	if completion.CompletedNormally() {
		closeGroup("terminal_field", len(lines))
	} else if strings.TrimSpace(completion.FinishReason) == "" {
		// A successful legacy transport may omit finish_reason. The terminal KV
		// field supplies the normal boundary; known truncation is never inferred.
		closeGroup("terminal_field", len(lines))
		if active != nil {
			active.diag.warn("supplier_status_unknown", "", len(lines))
		}
	}
	out.Groups = len(groups)
	if out.Groups != len(items) {
		out.Issues = append(out.Issues, fmt.Sprintf("互动内容分组%d，输入任务%d", out.Groups, len(items)))
	}
	blocks := map[string]*outputBlock{}
	idCounts := map[string]int{}
	pairCounts := map[string]int{}
	for _, group := range groups {
		group.diag.GroupCount = out.Groups
		if !group.badIdentity {
			idCounts[group.id]++
			if group.endMatched {
				pairCounts[group.id]++
			}
		}
	}
	// An intact unique pair outranks a dangling duplicate start/end label.
	for _, group := range groups {
		if _, known := indices[group.id]; known && group.endMatched && !group.badIdentity && pairCounts[group.id] == 1 {
			blocks[group.id] = group
			group.diag.Assignment = "item_id_pair"
		}
	}
	var unassigned []*outputBlock
	for _, group := range groups {
		if blocks[group.id] == group {
			continue
		}
		_, known := indices[group.id]
		if known && blocks[group.id] == nil && !group.badIdentity && idCounts[group.id] == 1 {
			blocks[group.id] = group
			if group.endMatched {
				group.diag.Assignment = "item_id_pair"
			} else if group.diag.Assignment == "" {
				group.diag.Assignment = "item_id_content"
			}
			continue
		}
		if idCounts[group.id] > 1 && known {
			group.diag.warn("duplicate_item_id", "编号", 0)
		}
		unassigned = append(unassigned, group)
	}
	// A single unlabelled result has no cross-person ordering ambiguity.
	if len(items) == 1 && len(groups) == 1 && groups[0].id == "" && !groups[0].badIdentity {
		blocks[items[0].ID] = groups[0]
		groups[0].diag.Assignment = "single_item"
		unassigned = nil
	}
	canRecoverOrder := len(groups) == len(items) && len(unassigned) > 0
	for _, group := range unassigned {
		// Count equality alone cannot turn a metadata fragment into a person.
		if !canRecoverOrder || !group.intentSeen || !group.outputSeen || blocks[items[group.diag.GroupIndex-1].ID] != nil {
			canRecoverOrder = false
		}
	}
	if canRecoverOrder {
		for _, group := range unassigned {
			id := items[group.diag.GroupIndex-1].ID
			blocks[id] = group
			group.diag.Assignment = "group_order"
			group.diag.warn("identity_recovered_by_group_order", "编号", 0)
		}
		out.Issues = append(out.Issues, fmt.Sprintf("编号不可用，按完整内容分组顺序恢复%d项", len(unassigned)))
	} else {
		out.UnmappedGroups = len(unassigned)
		if len(unassigned) > 0 {
			out.Issues = append(out.Issues, "内容分组数量或有效编号有矛盾，未按顺序错配")
		}
	}
	for _, requested := range items {
		i := indices[requested.ID]
		b := blocks[requested.ID]
		out.Items[i].Parse.GroupCount = out.Groups
		if b == nil {
			out.Items[i].Parse.Reason = "missing_item"
			continue
		}
		fields, fieldErr := b.resolveFields()
		item := ItemResult{ID: requested.ID, Parse: b.diag}
		switch {
		case b.err != nil:
			item.Err = b.err
		case !b.closed:
			item.Err = errors.New("互动尾项被截断或供应商完成状态不可采用")
			item.Parse.warn("unconfirmed_end", "", len(lines))
		case fieldErr != nil:
			item.Err = fieldErr
		default:
			item.Intent, item.Body, item.Err = parseSingleContent(s, requested.Input, fields)
		}
		if item.Err != nil {
			item.Body = ""
			item.Parse.Outcome, item.Parse.Reason = "rejected", item.Err.Error()
		} else if item.Body == "" {
			item.Parse.Outcome = "no_speech"
		} else {
			item.Parse.Outcome = "accepted"
			if item.Intent.NeedClarify {
				item.Parse.SpokenField = "澄清句"
				out.ClarificationGroups++
			} else {
				item.Parse.SpokenField = "话术"
				out.SpeechGroups++
			}
		}
		out.Items[i] = item
	}
	return out
}

func (b *outputBlock) resolveFields() (map[string]string, error) {
	fields := map[string]string{}
	conflicted := map[string]bool{}
	bodyKey := ""
	var intentLines []int
	for _, field := range b.fields {
		if bodyField(field.key) && (bodyKey == "" || field.key == "话术") {
			bodyKey = field.key
		}
	}
	var conflict error
	for _, field := range b.fields {
		key, value := field.key, field.value
		if bodyField(key) || key == "澄清句" {
			value = spokenFieldValue(value)
			if strings.TrimSpace(field.value) != "" && value == "" {
				field.value = ""
				b.diag.warn("empty_spoken_placeholder", key, field.span.Start)
			}
		}
		if bodyField(key) {
			if len(b.diag.BodyRanges) < 64 {
				b.diag.BodyRanges = append(b.diag.BodyRanges, field.span)
			}
			key = bodyKey
		}
		if old, exists := fields[key]; exists {
			if old == value {
				b.diag.warn("identical_field_repeated", key, field.span.Start)
				continue
			}
			if key == "意图" {
				if len(intentLines) >= 3 {
					b.diag.warn("extra_intent_metadata", key, field.span.Start)
					continue
				}
				fields[key] += "\n" + value
				intentLines = append(intentLines, field.span.Start)
				continue
			}
			if (bodyField(key) || key == "澄清句") && (old == "" || value == "") {
				if old == "" {
					fields[key] = value
				}
				b.diag.warn("empty_body_placeholder", key, field.span.Start)
				continue
			}
			switch key {
			case "话术", "回答", "澄清句":
				conflict = fmt.Errorf("互动可播字段%s存在不同正文", key)
				b.diag.warn("conflicting_spoken_field", key, field.span.Start)
			default:
				// Conflicting hints are unknown; never arbitrarily promote priority.
				fields[key], conflicted[key] = "", true
				b.diag.warn("conflicting_auxiliary_field", key, field.span.Start)
			}
			continue
		}
		if !conflicted[key] {
			fields[key] = value
			if key == "意图" {
				intentLines = append(intentLines, field.span.Start)
			}
		}
	}
	_, warnings := parseIntentMetadata(fields["意图"])
	for _, w := range warnings {
		if w.Line > 0 && w.Line <= len(intentLines) {
			w.Line = intentLines[w.Line-1]
		}
		b.diag.warn(w.Code, w.Field, w.Line)
	}
	key, selected, _ := spokenValue(fields)
	if selected != "" {
		b.diag.BodyRanges = nil
		for _, field := range b.fields {
			if ((key == "话术" && bodyField(field.key)) || field.key == key) && strings.TrimSpace(field.value) != "" && len(b.diag.BodyRanges) < 64 {
				b.diag.BodyRanges = append(b.diag.BodyRanges, field.span)
			}
		}
		if (key == "澄清句" && fields["歧义"] != "是") || (key == "话术" && fields["歧义"] != "否") {
			b.diag.warn("ambiguity_inferred_from_spoken_value", "歧义", 0)
		}
	}
	if strings.TrimSpace(fields["类型"]) == "" {
		b.diag.warn("missing_type_metadata", "类型", 0)
	}
	if fields["话术"] != "" && fields["澄清句"] != "" {
		b.diag.warn("unused_spoken_branch", "", 0)
	}
	for _, key := range []string{"优先程度", "优先原因"} {
		if fields[key] != "" {
			b.diag.warn("retired_priority_ignored", key, 0)
		}
	}
	return fields, conflict
}
