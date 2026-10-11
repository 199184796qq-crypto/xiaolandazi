package anchorcycle

import (
	"errors"
	"strings"
)

// Unicode offsets and source text belong to the program. A model supplies only
// replacement content. The complete resulting text must still pass review.
type SentenceRephraseTarget struct {
	SourceSHA  string
	Start, End int
	Original   string
}

func SelectSentenceRephraseTarget(source string, start, end int) (SentenceRephraseTarget, error) {
	runes := []rune(source)
	if start < 0 || end <= start || end > len(runes) {
		return SentenceRephraseTarget{}, errors.New("选中句段范围无效")
	}
	return SentenceRephraseTarget{SourceSHA: ProductDigest(source), Start: start, End: end, Original: string(runes[start:end])}, nil
}

func ApplySentenceRephrase(source string, target SentenceRephraseTarget, replacement string) (string, error) {
	runes := []rune(source)
	if ProductDigest(source) != target.SourceSHA || target.Start < 0 || target.End <= target.Start || target.End > len(runes) || string(runes[target.Start:target.End]) != target.Original {
		return "", errors.New("原文已变化，不能套用旧句段替换")
	}
	if strings.TrimSpace(replacement) == "" {
		return "", errors.New("改写内容未完成")
	}
	return string(runes[:target.Start]) + strings.TrimSpace(replacement) + string(runes[target.End:]), nil
}
