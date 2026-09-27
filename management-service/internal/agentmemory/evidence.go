package agentmemory

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"livecompanion/management/internal/model"
)

func normalizeEvidenceText(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(value)), " "))
}

func evidenceAnyText(value any) string {
	if value == nil {
		return ""
	}
	if text, ok := value.(string); ok {
		return strings.TrimSpace(text)
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return strings.TrimSpace(fmt.Sprint(value))
	}
	return strings.TrimSpace(string(raw))
}

// EvidenceValue returns a stable value used only for user-owned learning evidence statistics.
// It never consumes audience/public-screen text, so viewer repetition cannot become a fact.
func EvidenceValue(memoryType, resultText string, structured map[string]any) string {
	if structured == nil {
		structured = map[string]any{}
	}
	switch strings.TrimSpace(memoryType) {
	case model.AgentMemoryTypeFact:
		if value := evidenceAnyText(structured["value"]); value != "" {
			return value
		}
	case model.AgentMemoryTypeSemantic:
		if value := evidenceAnyText(structured["instruction"]); value != "" {
			return value
		}
	case model.AgentMemoryTypeWording:
		payload := map[string]any{}
		for _, key := range []string{"subject", "avoid", "prefer"} {
			if value, exists := structured[key]; exists {
				payload[key] = value
			}
		}
		if len(payload) > 0 {
			if value := evidenceAnyText(payload); value != "" {
				return value
			}
		}
	case model.AgentMemoryTypeStyle:
		if value := evidenceAnyText(structured["rules"]); value != "" {
			return value
		}
	}
	return strings.TrimSpace(resultText)
}

// ObservedEvidenceValue prefers the value captured before historical protection/review.
// This keeps occurrence statistics faithful to what the user actually expressed in that turn.
func ObservedEvidenceValue(memoryType, resultText string, structured map[string]any) string {
	if structured != nil {
		if raw, ok := structured["user_evidence_observation"].(map[string]any); ok {
			if value := evidenceAnyText(raw["value"]); value != "" {
				return value
			}
		}
	}
	return EvidenceValue(memoryType, resultText, structured)
}

func EvidenceSignature(value string) string {
	normalized := normalizeEvidenceText(value)
	if normalized == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(sum[:])
}

func LooksLikeExplicitCorrection(feedback string) bool {
	feedback = strings.TrimSpace(feedback)
	if feedback == "" {
		return false
	}
	for _, cue := range []string{
		"写错了", "写错", "打错了", "打错", "说错了", "说错", "更正", "纠正",
		"改成", "改为", "应该是", "正确的是", "正确是", "现在是", "现在改成", "现在改为",
		"从现在开始", "以后统一", "以后改成", "以后改为",
	} {
		if strings.Contains(feedback, cue) {
			return true
		}
	}
	return strings.Contains(feedback, "不是") && strings.Contains(feedback, "是")
}

// EvidenceScore gives explicit adoption/correction more weight than passive repetition.
func EvidenceScore(stat model.AgentMemoryEvidenceStat) int64 {
	return int64(stat.OccurrenceCount) +
		int64(stat.ConsecutiveCount)*2 +
		int64(stat.ExplicitCorrectionCount)*6 +
		int64(stat.AdoptedCount)*10
}

// StrongDominantEvidence returns a stable historical value only when it clearly outweighs
// the current one-off value. An explicit correction always bypasses frequency protection.
func StrongDominantEvidence(
	stats []model.AgentMemoryEvidenceStat,
	candidateSignature string,
	explicitCorrection bool,
) (*model.AgentMemoryEvidenceStat, bool) {
	if explicitCorrection || candidateSignature == "" || len(stats) == 0 {
		return nil, false
	}
	var dominant *model.AgentMemoryEvidenceStat
	var candidateScore int64 = 1 // current user input counts as one fresh observation
	for index := range stats {
		stat := &stats[index]
		score := EvidenceScore(*stat)
		if stat.ValueSignature == candidateSignature {
			candidateScore += score
			continue
		}
		if dominant == nil || score > EvidenceScore(*dominant) {
			dominant = stat
		}
	}
	if dominant == nil {
		return nil, false
	}
	dominantScore := EvidenceScore(*dominant)
	stableHistory := dominant.OccurrenceCount >= 3 &&
		(dominant.AdoptedCount > 0 || dominant.ConsecutiveCount >= 3 || dominant.OccurrenceCount >= 5)
	if !stableHistory || dominantScore < candidateScore+6 {
		return nil, false
	}
	return dominant, true
}
