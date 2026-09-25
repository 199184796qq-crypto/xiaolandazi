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
		"director_progress":   input.DirectorProgress,
		"director_atmosphere": input.DirectorAtmosphere,
		"humanization_kind":   input.HumanizationKind,
		"resume_mode_hint":    input.ResumeModeHint,
		"current_mainline":    input.CurrentMainline,
		"next_mainline_units": input.NextMainlineUnits,
		"top_topics":          input.TopTopics,
		"prompt_directives":   input.PromptDirectives,
		"tts_rate":            input.TTSRate,
	})
	return "你是直播场控导演的互动话术策划器。当前是链路测试，不加载行业层/用户层限制，但不能编造具体商品事实。\n\n" +
		"用户问题：\n" + input.Question + "\n\n" +
		"导演上下文：\n" + string(contextJSON) + "\n\n" +
		"prompt_directives 是本次研发测试临时加载的已确认规则/上下文线索。存在时必须优先遵守，用它帮助理解口语、方言、多义词、事实边界和表达习惯；但它不能覆盖明确的真实商品事实。\n" +
		"总口播目标约 " + itoa(input.TargetSeconds) + " 秒，当前 TTS 语速为 " + fmt.Sprintf("%.1fx", input.TTSRate) +
		"。entry_lead + reply_core + resume_tail 合计约 " + itoa(targetUnits) +
		" 个有效中文文字/数字单位，建议范围 " + itoa(minUnits) + " 到 " + itoa(maxUnits) +
		"。宁可略长，不要明显短于下限。\n" +
		"如果信息本身较少，可以增加不依赖商品私有事实的自然解释、通用使用提醒、互动承接或口感处理思路来补足长度，但不得编造价格、规格、库存、功效等事实。\n" +
		"你还必须判断这次从主线切入互动应该用哪种 entry_mode：DIRECT=顺接，SOFT=软接，HARD=硬接。\n" +
		"DIRECT：主线停止前最后一句与回答本身能自然连上，entry_lead 留空。\n" +
		"SOFT：语义有跳转但不需要强调打断，entry_lead 用一句很短的自然承接，例如‘有朋友正好问到这个，我顺手说一下’。\n" +
		"HARD：问题优先级高，允许明确打断主线，entry_lead 用一句简短的显式切入，例如‘先插一句回答下公屏这个问题’。硬接是有意策略，不因话题跳转本身判坏。\n" +
		"切入侧只要求听众能理解为什么突然转到这个问题，不要求像回归主线一样严格同主题；不要为了顺接强行篡改主线事实。\n" +
		"resume_tail 是回答结束时自然接回主线或引向下一个节点的最后一句，必须在本次 TTS 前一起生成，不能等回答播完再临时补。\n" +
		"不要机械说‘回到正题’或‘继续刚才的话题’，桥接要像主播自己顺口带过去。\n" +
		"resume_tail 只负责语气过渡，绝不能提前复述、改写或预告 resume_unit 正文里的具体事实、动作、教程、规格、物流、吃法等内容；如果主线下一句本身可以自然接上，FUSION_SKIP/CROSS_RESUME 的 resume_tail 可以留空。\n" +
		"next_mainline_units 中每个对象都包含真实 id、text、topics。回答如果已经覆盖某个后续语义段的 topics，必须把该语义段 id 放入 skip_units，并从第一个未覆盖语义段 id 作为 resume_unit。\n" +
		"如果连续覆盖多个后续语义段，使用 CROSS_RESUME；只覆盖一个使用 FUSION_SKIP；没有覆盖后续语义段时才允许 DIRECT/BRIDGE。\n" +
		"covered_fact_ids 没有可靠事实ID时留空。\n" +
		"允许的 resume_mode 只有 DIRECT, BRIDGE, FUSION_SKIP, CROSS_RESUME, RE_ANCHOR, SWITCH_PLAN。\n" +
		"模型只提出语义回接建议，最终跳转由程序策略层校验。Humanization 只影响表达方式，不得增加商品事实。\n\n" +
		"只返回 JSON，不要 Markdown。字段必须是 entry_mode, entry_lead, reply_core, resume_tail, covered_topics, covered_fact_ids, skip_units, resume_unit, resume_mode。"
}

func refineDevInteractionPrompt(plan devInteractionPlan, input devAnswerTTSInput, targetUnits, currentUnits int) string {
	minUnits := int(float64(targetUnits) * 0.92)
	maxUnits := int(float64(targetUnits) * 1.08)
	raw, _ := json.Marshal(plan)
	direction := "扩写"
	if currentUnits > targetUnits {
		direction = "压缩"
	}
	return "当前 entry_lead + reply_core + resume_tail 只有 " + itoa(currentUnits) + " 个有效文字单位，目标是约 " +
		itoa(input.TargetSeconds) + " 秒，TTS 语速 " + fmt.Sprintf("%.1fx", input.TTSRate) +
		"。请明确" + direction + "到 " + itoa(minUnits) + " 到 " + itoa(maxUnits) +
		" 个有效中文文字/数字单位；扩写时必须不少于下限。\n" +
		"不能只是换同义词。长度不足时只扩展 reply_core，加入自然、有用、且不依赖商品私有事实的解释或使用建议；不要靠重复 resume_unit 的内容来凑 resume_tail。长度过长时删掉重复表达。\n" +
		"可以调整 entry_mode、entry_lead、reply_core 和 resume_tail 的表达与长度，保留 covered_topics、covered_fact_ids、skip_units、resume_unit、resume_mode 的语义含义。\n" +
		"resume_tail 只能做语气过渡，不能复述或改写回归目标句的具体内容；FUSION_SKIP/CROSS_RESUME 在能直接衔接时允许 resume_tail 为空。只返回同样结构 JSON。\n\n原计划：" + string(raw)
}
