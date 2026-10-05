package stylecontract

import (
	"context"
	"fmt"
	"math"
	"strings"
	"unicode/utf8"

	"livecompanion/management/internal/model"
)

// StyleVectorEmbedder is intentionally smaller than the semantic service. The
// style compiler only needs an ephemeral pair embedding; it does not persist or
// retrieve these shadow scores.
type StyleVectorEmbedder interface {
	Enabled() bool
	Model() string
	Embed(context.Context, []string) ([][]float32, error)
}

type StyleVectorEvaluation struct {
	Available  bool    `json:"available"`
	ShadowOnly bool    `json:"shadow_only"`
	Model      string  `json:"model,omitempty"`
	Similarity float64 `json:"similarity,omitempty"`
	Score      int     `json:"score,omitempty"`
	Error      string  `json:"error,omitempty"`
}

func styleLengthBucket(length int) string {
	switch {
	case length <= 8:
		return "短"
	case length <= 16:
		return "中短"
	case length <= 30:
		return "中长"
	default:
		return "长"
	}
}

func countRune(text string, target rune) int {
	count := 0
	for _, current := range text {
		if current == target {
			count++
		}
	}
	return count
}

func perThousand(count, chars int) int {
	if count <= 0 || chars <= 0 {
		return 0
	}
	return int(math.Round(float64(count*1000) / float64(chars)))
}

// BuildStyleVectorSignature removes lexical content entirely. It describes
// cadence, punctuation, paragraphing and the density of already-grounded habit
// categories. A general semantic embedding therefore cannot prefer a candidate
// merely because it copied the source product or facts.
func BuildStyleVectorSignature(profile model.LiveAgentPlanAnchorStyleProfile, text string) string {
	text = strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n"))
	chars := utf8.RuneCountInString(text)
	paragraphs := 0
	for _, paragraph := range strings.Split(text, "\n") {
		if strings.TrimSpace(paragraph) != "" {
			paragraphs++
		}
	}
	if paragraphs == 0 && text != "" {
		paragraphs = 1
	}
	lengths := make([]string, 0, 64)
	sentenceCount, sentenceChars := 0, 0
	for _, sentence := range sentenceBreak.Split(text, -1) {
		length := utf8.RuneCountInString(strings.TrimSpace(sentence))
		if length == 0 {
			continue
		}
		sentenceCount++
		sentenceChars += length
		if len(lengths) < 64 {
			lengths = append(lengths, styleLengthBucket(length))
		}
	}
	average := 0
	if sentenceCount > 0 {
		average = sentenceChars / sentenceCount
	}

	kindCounts := map[string]int{}
	if profile.Delivery != nil {
		byKind := map[string][]string{}
		for _, habit := range profile.Delivery.Habits {
			byKind[habit.Kind] = append(byKind[habit.Kind], habit.Text)
		}
		for kind, alternatives := range byKind {
			kindCounts[kind] = countAnyNonOverlapping(text, alternatives)
		}
	}

	return fmt.Sprintf(
		"纯风格结构；段落=%d；分句=%d；平均分句=%d；分句序列=%s；问句密度=%d；感叹密度=%d；逗号密度=%d；分号密度=%d；顿号密度=%d；主播自称密度=%d；观众称呼密度=%d；语气词密度=%d；连接词密度=%d；中性口头禅密度=%d",
		paragraphs,
		sentenceCount,
		average,
		strings.Join(lengths, "/"),
		perThousand(countRune(text, '？')+countRune(text, '?'), chars),
		perThousand(countRune(text, '！')+countRune(text, '!'), chars),
		perThousand(countRune(text, '，')+countRune(text, ','), chars),
		perThousand(countRune(text, '；')+countRune(text, ';'), chars),
		perThousand(countRune(text, '、'), chars),
		perThousand(kindCounts["self_address"], chars),
		perThousand(kindCounts["audience_address"], chars),
		perThousand(kindCounts["particle"], chars),
		perThousand(kindCounts["connector"], chars),
		perThousand(kindCounts["catchphrase"], chars),
	)
}

func styleCosine(left, right []float32) float64 {
	if len(left) == 0 || len(left) != len(right) {
		return 0
	}
	var dot, leftNorm, rightNorm float64
	for index := range left {
		l, r := float64(left[index]), float64(right[index])
		dot += l * r
		leftNorm += l * l
		rightNorm += r * r
	}
	if leftNorm == 0 || rightNorm == 0 {
		return 0
	}
	return dot / (math.Sqrt(leftNorm) * math.Sqrt(rightNorm))
}

// EvaluateStyleVectorShadow never gates generation. A general-purpose
// embedding model has not yet been trained on same-anchor/different-product
// pairs, so the score is collected only for calibration.
func EvaluateStyleVectorShadow(ctx context.Context, embedder StyleVectorEmbedder, profile model.LiveAgentPlanAnchorStyleProfile, source, candidate string) StyleVectorEvaluation {
	result := StyleVectorEvaluation{ShadowOnly: true}
	if embedder == nil || !embedder.Enabled() {
		result.Error = "style embedding unavailable"
		return result
	}
	result.Model = embedder.Model()
	vectors, err := embedder.Embed(ctx, []string{BuildStyleVectorSignature(profile, source), BuildStyleVectorSignature(profile, candidate)})
	if err != nil {
		result.Error = err.Error()
		return result
	}
	if len(vectors) != 2 || len(vectors[0]) == 0 || len(vectors[0]) != len(vectors[1]) {
		result.Error = "style embedding returned incompatible vectors"
		return result
	}
	result.Available = true
	result.Similarity = styleCosine(vectors[0], vectors[1])
	result.Score = clampInt(int(math.Round(result.Similarity*100)), 0, 100)
	return result
}
