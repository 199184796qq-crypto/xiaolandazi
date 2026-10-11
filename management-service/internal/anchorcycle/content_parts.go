package anchorcycle

import (
	"encoding/json"
	"errors"
	"strings"
)

// Models provide content fields only. Request ownership and fact bindings stay
// with the caller. Missing structure discards just this candidate.
type ContentParts struct {
	Main      string `json:"main"`
	Extension string `json:"extension"`
}

func ParseContentParts(raw string) (ContentParts, error) {
	s := strings.TrimSpace(raw)
	if strings.HasPrefix(s, "```") {
		if start := strings.Index(s, "\n"); start >= 0 && strings.HasSuffix(s, "```") {
			s = strings.TrimSpace(s[start+1 : len(s)-3])
		}
	}
	var p ContentParts
	if json.Unmarshal([]byte(s), &p) == nil && strings.TrimSpace(p.Main) != "" {
		p.Main, p.Extension = strings.TrimSpace(p.Main), strings.TrimSpace(p.Extension)
		return p, nil
	}
	for _, mainMarker := range []string{"【主回答】", "【主内容】"} {
		if strings.HasPrefix(s, mainMarker) {
			if end := strings.Index(s, "【延展】"); end >= len(mainMarker) {
				p = ContentParts{strings.TrimSpace(s[len(mainMarker):end]), strings.TrimSpace(s[end+len("【延展】"):])}
				if p.Main != "" {
					return p, nil
				}
			}
		}
	}
	return ContentParts{}, errors.New("本段主回答内容未完成")
}

func ParseExtensionContent(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if strings.HasPrefix(s, "```") {
		if start := strings.Index(s, "\n"); start >= 0 && strings.HasSuffix(s, "```") {
			s = strings.TrimSpace(s[start+1 : len(s)-3])
		}
	}
	var p map[string]json.RawMessage
	if json.Unmarshal([]byte(s), &p) == nil {
		value, ok := p["extension"]
		if !ok {
			return "", errors.New("本次延展内容未返回")
		}
		var content string
		if json.Unmarshal(value, &content) != nil {
			return "", errors.New("延展不是文本内容")
		}
		return strings.TrimSpace(content), nil
	}
	if strings.HasPrefix(s, "【延展】") {
		return strings.TrimSpace(strings.TrimPrefix(s, "【延展】")), nil
	}
	return "", errors.New("本次延展内容未完成")
}

func ComposeContentParts(p ContentParts, extension string) string {
	// A replacement has no access to change the primary answer.
	return p.Main + strings.TrimSpace(extension)
}
