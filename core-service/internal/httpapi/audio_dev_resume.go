package httpapi

import (
	"strings"
)

type devResumeResolution struct {
	Changed        bool
	Mode           string
	ResumeUnit     string
	ResumeOffsetMS int
	SkipUnits      []string
	ResumeText     string
	CoveredTopics  []string
}

func canonicalCoreTopic(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "delivery", "shipping", "logistics", "shipping_time", "delivery_time", "物流", "发货", "时效", "到货":
		return "delivery"
	case "cooking", "how_to_eat", "eat", "吃法", "做法", "烹饪":
		return "cooking"
	case "spec_state", "spec", "weight", "规格", "重量":
		return "spec_state"
	case "identity_source", "origin", "产地", "来源":
		return "identity_source"
	case "appearance_meat", "meat", "texture", "口感", "肉质":
		return "appearance_meat"
	case "raising_activity", "raising", "放养", "饲养":
		return "raising_activity"
	case "feeding", "喂养", "饲料":
		return "feeding"
	case "slaughter_offal", "offal", "内脏", "宰杀":
		return "slaughter_offal"
	case "original_cut", "cut", "原切", "切块":
		return "original_cut"
	case "cta", "成交", "下单":
		return "cta"
	case "general", "普通", "通用":
		return "general"
	default:
		return value
	}
}

func inferDevTopicsFromText(text string) []string {
	text = strings.ToLower(strings.TrimSpace(text))
	topics := make([]string, 0, 4)
	add := func(topic string) {
		for _, existing := range topics {
			if existing == topic {
				return
			}
		}
		topics = append(topics, topic)
	}
	if strings.Contains(text, "物流") || strings.Contains(text, "发货") || strings.Contains(text, "到货") ||
		strings.Contains(text, "隔天") || strings.Contains(text, "几天") || strings.Contains(text, "时效") ||
		strings.Contains(text, "江浙沪") || strings.Contains(text, "快递") {
		add("delivery")
	}
	if strings.Contains(text, "怎么吃") || strings.Contains(text, "做法") || strings.Contains(text, "清蒸") ||
		strings.Contains(text, "炖") || strings.Contains(text, "煮") || strings.Contains(text, "炒") ||
		strings.Contains(text, "空气炸锅") || strings.Contains(text, "微波炉") {
		add("cooking")
	}
	if strings.Contains(text, "毛重") || strings.Contains(text, "净重") || strings.Contains(text, "斤") ||
		strings.Contains(text, "规格") {
		add("spec_state")
	}
	if strings.Contains(text, "产地") || strings.Contains(text, "皖南") || strings.Contains(text, "哪里") {
		add("identity_source")
	}
	if strings.Contains(text, "肉质") || strings.Contains(text, "口感") || strings.Contains(text, "皮薄") ||
		strings.Contains(text, "紧实") {
		add("appearance_meat")
	}
	if strings.Contains(text, "放养") || strings.Contains(text, "跑山") || strings.Contains(text, "运动量") {
		add("raising_activity")
	}
	if strings.Contains(text, "喂") || strings.Contains(text, "饲料") || strings.Contains(text, "小虫") {
		add("feeding")
	}
	if strings.Contains(text, "鸡胗") || strings.Contains(text, "鸡肝") || strings.Contains(text, "鸡心") ||
		strings.Contains(text, "宰杀") {
		add("slaughter_offal")
	}
	if strings.Contains(text, "切块") || strings.Contains(text, "原切") {
		add("original_cut")
	}
	return topics
}

func mergeDevTopics(values ...[]string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, 8)
	for _, group := range values {
		for _, value := range group {
			topic := canonicalCoreTopic(value)
			if topic == "" {
				continue
			}
			if _, ok := seen[topic]; ok {
				continue
			}
			seen[topic] = struct{}{}
			out = append(out, topic)
		}
	}
	return out
}

func sentenceTopicOverlap(sentence devMainlineSentence, covered map[string]struct{}) bool {
	for _, topic := range sentence.Topics {
		if _, ok := covered[canonicalCoreTopic(topic)]; ok {
			return true
		}
	}
	return false
}

func sentenceHasGeneralTopic(sentence devMainlineSentence) bool {
	for _, topic := range sentence.Topics {
		if canonicalCoreTopic(topic) == "general" {
			return true
		}
	}
	return false
}

func sentenceEntryMS(sentence devMainlineSentence) int {
	if sentence.PlayStartMS > 0 {
		return sentence.PlayStartMS
	}
	return sentence.StartMS
}

func resolveDevMainlineResume(
	stopMS int,
	coveredTopics []string,
	replyCore string,
	suggestedResumeUnit string,
	suggestedSkipUnits []string,
) devResumeResolution {
	data, err := loadDevMainlineMap()
	if err != nil || len(data.Sentences) == 0 {
		return devResumeResolution{}
	}

	allCovered := mergeDevTopics(coveredTopics, inferDevTopicsFromText(replyCore))
	coveredSet := map[string]struct{}{}
	for _, topic := range allCovered {
		coveredSet[topic] = struct{}{}
	}

	future := make([]devMainlineSentence, 0, len(data.Sentences))
	for _, sentence := range data.Sentences {
		if sentenceEntryMS(sentence) >= stopMS {
			future = append(future, sentence)
		}
	}
	if len(future) == 0 {
		return devResumeResolution{CoveredTopics: allCovered}
	}

	suggestedSkip := map[string]struct{}{}
	for _, id := range suggestedSkipUnits {
		suggestedSkip[strings.TrimSpace(id)] = struct{}{}
	}

	skip := make([]string, 0, len(future))
	skipping := false
	lastSkippedIndex := -1
	for i, sentence := range future {
		_, explicitlySkipped := suggestedSkip[sentence.ID]
		overlap := sentenceTopicOverlap(sentence, coveredSet)
		generalTail := skipping && sentenceHasGeneralTopic(sentence)
		if !skipping && !explicitlySkipped && !overlap {
			break
		}
		if explicitlySkipped || overlap || generalTail {
			skip = append(skip, sentence.ID)
			skipping = true
			lastSkippedIndex = i
			continue
		}
		if skipping {
			break
		}
	}

	var resume devMainlineSentence
	hasResume := false
	if lastSkippedIndex >= 0 && lastSkippedIndex+1 < len(future) {
		resume = future[lastSkippedIndex+1]
		hasResume = true
	} else if suggestedResumeUnit != "" {
		for _, sentence := range future {
			if sentence.ID == strings.TrimSpace(suggestedResumeUnit) {
				resume = sentence
				hasResume = true
				break
			}
		}
	}

	if len(skip) == 0 {
		if hasResume && sentenceEntryMS(resume) > stopMS {
			return devResumeResolution{
				Changed:        true,
				Mode:           "BRIDGE",
				ResumeUnit:     resume.ID,
				ResumeOffsetMS: sentenceEntryMS(resume),
				ResumeText:     resume.Text,
				CoveredTopics:  allCovered,
			}
		}
		return devResumeResolution{CoveredTopics: allCovered}
	}

	mode := "FUSION_SKIP"
	if len(skip) > 1 {
		mode = "CROSS_RESUME"
	}
	resolution := devResumeResolution{
		Changed:       true,
		Mode:          mode,
		SkipUnits:     skip,
		CoveredTopics: allCovered,
	}
	if hasResume {
		resolution.ResumeUnit = resume.ID
		resolution.ResumeOffsetMS = sentenceEntryMS(resume)
		resolution.ResumeText = resume.Text
	}
	return resolution
}
