package httpapi

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"unicode"
)

type devInteractionPlan struct {
	EntryMode      string   `json:"entry_mode"`
	EntryLead      string   `json:"entry_lead"`
	ReplyCore      string   `json:"reply_core"`
	ResumeTail     string   `json:"resume_tail"`
	CoveredTopics  []string `json:"covered_topics"`
	CoveredFactIDs []string `json:"covered_fact_ids"`
	SkipUnits      []string `json:"skip_units"`
	ResumeUnit     string   `json:"resume_unit"`
	ResumeMode     string   `json:"resume_mode"`
}

func normalizeUniqueStrings(values []string, maxItems int) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		key := strings.ToUpper(value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, value)
		if maxItems > 0 && len(out) >= maxItems {
			break
		}
	}
	return out
}

func validDevEntryMode(value string) bool {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "DIRECT", "SOFT", "HARD":
		return true
	default:
		return false
	}
}

func validDevResumeMode(value string) bool {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "DIRECT", "BRIDGE", "FUSION_SKIP", "CROSS_RESUME", "RE_ANCHOR", "SWITCH_PLAN":
		return true
	default:
		return false
	}
}

func normalizeDevInteractionPlan(plan devInteractionPlan) (devInteractionPlan, error) {
	plan.EntryMode = strings.ToUpper(strings.TrimSpace(plan.EntryMode))
	plan.EntryLead = strings.TrimSpace(plan.EntryLead)
	plan.ReplyCore = strings.TrimSpace(plan.ReplyCore)
	plan.ResumeTail = strings.TrimSpace(plan.ResumeTail)
	plan.ResumeUnit = strings.TrimSpace(plan.ResumeUnit)
	plan.ResumeMode = strings.ToUpper(strings.TrimSpace(plan.ResumeMode))
	plan.CoveredTopics = normalizeUniqueStrings(plan.CoveredTopics, 16)
	plan.CoveredFactIDs = normalizeUniqueStrings(plan.CoveredFactIDs, 32)
	plan.SkipUnits = normalizeUniqueStrings(plan.SkipUnits, 16)
	if !validDevEntryMode(plan.EntryMode) {
		if plan.EntryLead != "" {
			plan.EntryMode = "SOFT"
		} else {
			plan.EntryMode = "DIRECT"
		}
	}
	if plan.EntryMode == "DIRECT" {
		plan.EntryLead = ""
	}
	if (plan.EntryMode == "SOFT" || plan.EntryMode == "HARD") && plan.EntryLead == "" {
		plan.EntryMode = "DIRECT"
	}
	if plan.ReplyCore == "" {
		return devInteractionPlan{}, fmt.Errorf("reply_core is empty")
	}
	if !validDevResumeMode(plan.ResumeMode) {
		if plan.ResumeTail != "" {
			plan.ResumeMode = "BRIDGE"
		} else {
			plan.ResumeMode = "DIRECT"
		}
	}
	if (plan.ResumeMode == "BRIDGE" || plan.ResumeMode == "RE_ANCHOR") && plan.ResumeTail == "" {
		// A model can occasionally choose a bridge-style resume mode but forget
		// to provide the bridge text. Treat that as a safe direct resume instead
		// of failing the whole live interaction before semantic repair/quality
		// review gets a chance to run.
		plan.ResumeMode = "DIRECT"
	}
	return plan, nil
}

func decodeDevInteractionPlan(raw string) (devInteractionPlan, error) {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```JSON")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	raw = strings.TrimSpace(raw)
	var plan devInteractionPlan
	if err := json.Unmarshal([]byte(raw), &plan); err != nil {
		return devInteractionPlan{}, err
	}
	return normalizeDevInteractionPlan(plan)
}

func devInteractionFinalText(plan devInteractionPlan) string {
	return strings.TrimSpace(
		strings.TrimSpace(plan.EntryLead) + " " +
			strings.TrimSpace(plan.ReplyCore) + " " +
			strings.TrimSpace(plan.ResumeTail),
	)
}

func trimSpeechToUnits(text string, maxUnits int) string {
	text = strings.TrimSpace(text)
	if text == "" || maxUnits <= 0 || spokenUnitCount(text) <= maxUnits {
		return text
	}
	runes := []rune(text)
	units := 0
	lastBoundary := -1
	cutAt := len(runes)
	for i, r := range runes {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			units++
		}
		if strings.ContainsRune("，。！？；、,.!?;", r) && units <= maxUnits {
			lastBoundary = i + 1
		}
		if units > maxUnits {
			cutAt = i
			break
		}
	}
	if lastBoundary > 0 && spokenUnitCount(string(runes[:lastBoundary])) >= maxUnits*2/3 {
		cutAt = lastBoundary
	}
	if cutAt <= 0 {
		return text
	}
	out := strings.TrimSpace(string(runes[:cutAt]))
	out = strings.TrimRight(out, "，、,；; ")
	if out != "" && !strings.ContainsRune("。！？!?", []rune(out)[len([]rune(out))-1]) {
		out += "。"
	}
	return out
}

func fitDevInteractionPlanMaxUnits(plan devInteractionPlan, maxUnits int) devInteractionPlan {
	if maxUnits <= 0 || spokenUnitCount(devInteractionFinalText(plan)) <= maxUnits {
		return plan
	}
	entryUnits := spokenUnitCount(plan.EntryLead)
	tailUnits := spokenUnitCount(plan.ResumeTail)
	allowedReply := maxUnits - entryUnits - tailUnits
	if allowedReply < 8 {
		allowedTail := maxUnits / 3
		if allowedTail < 4 {
			allowedTail = 4
		}
		plan.ResumeTail = trimSpeechToUnits(plan.ResumeTail, allowedTail)
		tailUnits = spokenUnitCount(plan.ResumeTail)
		allowedReply = maxUnits - entryUnits - tailUnits
	}
	if allowedReply < 8 && entryUnits > 0 {
		allowedEntry := maxUnits / 5
		if allowedEntry < 4 {
			allowedEntry = 4
		}
		plan.EntryLead = trimSpeechToUnits(plan.EntryLead, allowedEntry)
		entryUnits = spokenUnitCount(plan.EntryLead)
		allowedReply = maxUnits - entryUnits - tailUnits
	}
	if allowedReply > 0 {
		plan.ReplyCore = trimSpeechToUnits(plan.ReplyCore, allowedReply)
	}
	return plan
}

func devTextDigest(value string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(value)))
	return hex.EncodeToString(sum[:])
}

func devInteractionPrompt(input devAnswerTTSInput, targetUnits int) string {
	minUnits := int(float64(targetUnits) * 0.90)
	maxUnits := int(float64(targetUnits) * 1.10)
	contextJSON, _ := json.Marshal(map[string]any{
		"question":            input.Question,
		"director_progress":   input.DirectorProgress,
		"director_atmosphere": input.DirectorAtmosphere,
		"humanization_kind":   input.HumanizationKind,
		"resume_mode_hint":    input.ResumeModeHint,
		"current_mainline":    input.CurrentMainline,
		"next_mainline_units": input.NextMainlineUnits,
		"top_topics":          input.TopTopics,
		"prompt_directives":   input.PromptDirectives,
		"tts_rate":            input.TTSRate,
		"target_seconds":      input.TargetSeconds,
		"target_units":        targetUnits,
		"min_units":           minUnits,
		"max_units":           maxUnits,
	})
	return "本次互动动态上下文：\n" + string(contextJSON)
}

func refineDevInteractionPrompt(plan devInteractionPlan, input devAnswerTTSInput, targetUnits, currentUnits int) string {
	minUnits := int(float64(targetUnits) * 0.92)
	maxUnits := int(float64(targetUnits) * 1.08)
	payload, _ := json.Marshal(map[string]any{
		"current_plan":   plan,
		"current_units":  currentUnits,
		"target_units":   targetUnits,
		"min_units":      minUnits,
		"max_units":      maxUnits,
		"target_seconds": input.TargetSeconds,
		"tts_rate":       input.TTSRate,
	})
	return "本次长度调整动态数据：\n" + string(payload)
}
