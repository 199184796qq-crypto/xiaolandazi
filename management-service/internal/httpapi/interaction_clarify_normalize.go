package httpapi

import "strings"

func normalizeClarifyText(v string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(v)) {
		switch r {
		case ' ', '\t', '\n', '，', '。', '！', '？', '、', '；', '：', '?', '!', ',', '.', '~', '～':
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
