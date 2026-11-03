package planprompt

import "strings"

const RewriteIntro = `你是直播话术改写器。你的工作不是润色，是"换一套说法把同一件事重新讲一遍"。`

func rewriteContext(s Sections) string {
	s = s.Normalize()
	return RewriteIntro + "\n\n【主播人设】\n" + s.PersonaMD +
		"\n\n【事实依据】\n" + s.FactsMD + "\n\n【红线禁止】\n" + s.RedLinesMD
}

// CompileRewritePlanning adds only the existing context envelope. Sample text
// is supplied by the rewrite request, never by Save or by the editable rules.
func CompileRewritePlanning(s Sections, planning string) string {
	// 客户写的「输出格式」一律丢弃，换成系统契约；「输出要求」原样保留。
	rules := StripOutputFormatSection(planning)
	body := WithOutputContract(rules, RewriteOutputContract)
	return rewriteContext(s) + "\n\n【样本话术】\n{{样本话术}}\n\n" + body
}

// LegacyRewritePlanning recognizes the exact old generated envelope. Do not
// reverse-parse arbitrary headings inside customer text or expose that envelope
// as editable planning. Standalone rules from the first editor remain verbatim.
func LegacyRewritePlanning(prompt string, s Sections) (string, bool) {
	if prompt == "" {
		return "", false
	}
	text := strings.ReplaceAll(prompt, "\r\n", "\n")
	for _, sample := range []string{"{{样本话术}}", ""} {
		prefix := rewriteContext(s) + "\n\n【样本话术】\n" + sample + "\n\n"
		if strings.HasPrefix(text, prefix) {
			return stripRewriteOutputContract(strings.TrimPrefix(text, prefix)), true
		}
	}
	if !strings.HasPrefix(strings.TrimSpace(text), RewriteIntro) {
		return prompt, true
	}
	return "", false
}

// stripRewriteOutputContract 去掉系统追加的输出契约，编辑器只回显客户自己的规则。
func stripRewriteOutputContract(rules string) string {
	return StripSystemOutputContract(rules)
}
