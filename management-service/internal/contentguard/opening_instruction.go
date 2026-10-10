package contentguard

import (
	"errors"
	"regexp"
	"strings"
)

// A deletion is scoped to a leading instruction, never a global banned phrase.
// The original model response is retained by the caller.
type OpeningInstructionEdit struct {
	Removed string         `json:"removed_text"`
	Source  string         `json:"source"`
	Review  *ContextReview `json:"context_review,omitempty"`
}

var instructionForm = regexp.MustCompile(`(?:第一句|开头|本次任务|这一段|这段|本段|目标人群|讲述角度|写作指导|表达展开建议).*(?:说|讲|写|解释|展开|介绍|输出)|(?:先|首先|直接)(?:说|讲|写|解释|介绍)|(?:现在|这会儿)就从.*说起`)

func trimInstruction(value string) string {
	return strings.Trim(value, " \t\r\n。！？!?；;，,:：\"“”")
}

// Only exact leading fragments copied from the actual control sources qualify.
// Facts and examples must not be passed as sources. A contextual reviewer may
// confirm a paraphrase, but its quote still has to be a literal leading span.
func StripOpeningInstruction(text string, sources []string, confirmedQuote string) (string, *OpeningInstructionEdit, error) {
	text = strings.TrimSpace(text)
	quote, source := trimInstruction(confirmedQuote), "context_review"
	if quote != "" {
		matched := false
		for _, guide := range sources {
			if strings.Contains(trimInstruction(guide), quote) {
				matched = true
				break
			}
		}
		if !matched {
			return text, nil, nil
		}
	}
	if quote == "" {
		source = "request_control_exact_match"
		for _, guide := range sources {
			for _, fragment := range strings.FieldsFunc(guide, func(r rune) bool { return strings.ContainsRune("。！？!?；;\n", r) }) {
				fragment = trimInstruction(fragment)
				if instructionForm.MatchString(fragment) && strings.HasPrefix(text, fragment) && len(fragment) > len(quote) {
					quote = fragment
				}
			}
		}
	}
	if quote == "" || !instructionForm.MatchString(quote) || !strings.HasPrefix(text, quote) {
		return text, nil, nil
	}
	rest := text[len(quote):]
	if rest != "" && !strings.ContainsRune(" \t\r\n。！？!?；;，,:：\"“”", []rune(rest)[0]) {
		return text, nil, nil // Do not remove a partial word or part of an actual sentence.
	}
	rest = strings.TrimLeft(rest, " \t\r\n。！？!?；;，,:：\"“”")
	if rest == "" {
		return text, nil, errors.New("删除开头指导后没有口播正文")
	}
	return rest, &OpeningInstructionEdit{Removed: text[:len(text)-len(rest)], Source: source}, nil
}
