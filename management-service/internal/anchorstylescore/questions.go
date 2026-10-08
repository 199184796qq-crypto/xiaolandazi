package anchorstylescore

import (
	"regexp"
	"strings"
)

var spokenQuestion = regexp.MustCompile(`(?:不|没|么|吗)$|(?:对不对|是不是|有没有|行不行|划不划算|好不好|要不要|能不能|够不够)(?:嘛|啊|哟|呢)?$`)
var questionPieces = regexp.MustCompile(`[^，,。！？!?；;\n]+[，,。！？!?；;\n]?`)

// Recognize complete clause endings. A question mark and its spoken ending
// count once; generic "嘛" and internal 不/没 (不得不、没得、说不定) don't.
func CountQuestions(text string) int {
	n := 0
	for _, piece := range questionPieces.FindAllString(text, -1) {
		piece = strings.TrimSpace(piece)
		if strings.HasSuffix(piece, "？") || strings.HasSuffix(piece, "?") {
			n++
			continue
		}
		piece = strings.TrimRight(piece, "，,。！!；;\n\r \t\"”」’")
		if spokenQuestion.MatchString(piece) {
			n++
		}
	}
	return n
}

func questionTarget(b Baseline) float64 {
	if b.Quant.QuestionSentences > 0 {
		return b.Quant.QuestionSentences
	}
	return b.Quant.Questions // Old confirmed rulers stay valid until recalibrated.
}
