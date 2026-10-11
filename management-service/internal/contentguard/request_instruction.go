package contentguard

import (
	"errors"
	"strings"
)

// Delete only copies of this request's writing controls or its explicit internal
// labels. The caller supplies no facts, viewer questions or sample speech here.
func StripRequestInstructions(text string, sources, labels []string) (string, []OpeningInstructionEdit, error) {
	fragments := []string{}
	for _, guide := range sources {
		for _, fragment := range strings.FieldsFunc(guide, func(r rune) bool { return strings.ContainsRune("。！？!?；;\n", r) }) {
			fragment = trimInstruction(fragment)
			if instructionForm.MatchString(fragment) {
				fragments = append(fragments, fragment)
			}
		}
	}
	var out strings.Builder
	edits := []OpeningInstructionEdit{}
	start := 0
	flush := func(end int) {
		clause := text[start:end]
		drop := false
		for _, label := range labels {
			if label != "" && strings.Contains(clause, label) {
				drop = true
				break
			}
		}
		if !drop {
			for _, fragment := range fragments {
				if trimInstruction(clause) == fragment {
					drop = true
					break
				}
			}
		}
		if drop {
			edits = append(edits, OpeningInstructionEdit{Removed: clause, Source: "request_control_exact_match"})
		} else {
			out.WriteString(clause)
		}
		start = end
	}
	for at, r := range text {
		if strings.ContainsRune("。！？!?；;，,\n", r) {
			flush(at + len(string(r)))
		}
	}
	if start < len(text) {
		flush(len(text))
	}
	cleaned := strings.TrimSpace(out.String())
	if len(edits) > 0 && cleaned == "" {
		return text, nil, errors.New("删除本次指导后没有口播正文")
	}
	return cleaned, edits, nil
}

// Remove a supplied content-first control only before an authorized product
// label. Ordinary viewer questions and the product facts remain intact.
func StripLeadingProductControl(text, control string, productLabels []string) (string, *OpeningInstructionEdit) {
	if control == "" || !strings.HasPrefix(text, control) {
		return text, nil
	}
	rest := strings.TrimLeft(text[len(control):], " ，,:：\t")
	for _, label := range productLabels {
		if label != "" && strings.HasPrefix(rest, label) {
			return rest, &OpeningInstructionEdit{Removed: text[:len(text)-len(rest)], Source: "request_content_first_control"}
		}
	}
	return text, nil
}
