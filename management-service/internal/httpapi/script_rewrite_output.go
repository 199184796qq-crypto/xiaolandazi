package httpapi

import (
	"errors"
	"strings"
)

// Metadata is kept separately from the only field allowed into TTS.
type scriptRewriteOutput struct {
	Skeleton string `json:"skeleton"`
	Context  string `json:"context"`
	Script   string `json:"script"`
}

// Every new request now carries the three-field contract. Legacy readers below
// stay compatible, but a fresh malformed response cannot overwrite context.
func parseScriptRewriteResponse(raw string) (scriptRewriteOutput, error) {
	result, err := parseScriptRewriteOutput(raw)
	if err != nil {
		return scriptRewriteOutput{}, err
	}
	if result.Skeleton == "" || result.Context == "" {
		return scriptRewriteOutput{}, errors.New("本次改写缺少骨架或一句话上下文")
	}
	lines := strings.Split(strings.ReplaceAll(strings.ReplaceAll(raw, "\r\n", "\n"), "\r", "\n"), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		if pos, width, ok := findColon(line); ok && (strings.TrimSpace(line[:pos]) == "话术" || strings.TrimSpace(line[:pos]) == "改写内容") && strings.TrimSpace(line[pos+width:]) != "" {
			return result, nil
		}
		break
	}
	return scriptRewriteOutput{}, errors.New("本次改写正文须为末行的完整键值对")
}

func parseScriptRewriteOutput(raw string) (scriptRewriteOutput, error) {
	text := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(raw), "\ufeff"))
	text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
	var out scriptRewriteOutput
	fields := map[string][]string{}
	current := ""
	var preamble []string
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		key := ""
		value := ""
		if pos, width, ok := findColon(trimmed); ok {
			candidate := strings.TrimSpace(trimmed[:pos])
			switch candidate {
			case "话术骨架", "一句话上下文", "改写内容", "话术内容", "话术":
				key, value = candidate, trimmed[pos+width:]
			}
		}
		if key != "" {
			if current == "话术" || current == "改写内容" || current == "话术内容" {
				return out, errors.New("改写结果正文后存在其它字段")
			}
			if _, exists := fields[key]; exists {
				return out, errors.New("改写结果含重复字段")
			}
			current = key
			fields[key] = []string{value}
		} else if current != "" {
			// A body value may start on the next line for legacy responses,
			// but trailing lines must never silently become spoken commentary.
			if trimmed != "" && isBodyField(current) {
				if strings.TrimSpace(strings.Join(fields[current], "")) != "" {
					return out, errors.New("改写正文必须为单行，不能追加字段外文字")
				}
			}
			fields[current] = append(fields[current], line)
		} else if trimmed != "" {
			preamble = append(preamble, line)
		}
	}
	if len(fields) == 0 {
		// Keep legacy plain-body responses, but never speak JSON/fences or
		// unparseable KV headers as if they were the actual script.
		for _, prefix := range []string{"话术骨架", "一句话上下文", "改写内容", "话术内容", "话术", "{", "```"} {
			if strings.HasPrefix(text, prefix) {
				return out, errors.New("改写结果字段格式错误")
			}
		}
		out.Script = text
	} else {
		if len(preamble) != 0 {
			return out, errors.New("改写结果含字段外文字")
		}
		get := func(key string) string { return strings.TrimSpace(strings.Join(fields[key], "\n")) }
		out.Skeleton, out.Context = get("话术骨架"), get("一句话上下文")
		_, unifiedBody := fields["话术"]
		_, aliasBody := fields["改写内容"]
		if unifiedBody && aliasBody {
			return out, errors.New("改写结果含两个正文字段")
		}
		newBody := unifiedBody
		if !newBody {
			_, newBody = fields["改写内容"]
		}
		_, oldBody := fields["话术内容"]
		_, hasContext := fields["一句话上下文"]
		if newBody && oldBody {
			return scriptRewriteOutput{}, errors.New("改写结果含两个正文字段")
		}
		if newBody || hasContext {
			if !newBody || out.Skeleton == "" || out.Context == "" {
				return scriptRewriteOutput{}, errors.New("改写结果缺少三个必需字段")
			}
			if body, ok := fields["话术"]; ok {
				out.Script = strings.TrimSpace(strings.Join(body, ""))
			} else {
				out.Script = strings.TrimSpace(strings.Join(fields["改写内容"], ""))
			}
		} else if oldBody {
			out.Script = get("话术内容")
		}
	}
	if strings.TrimSpace(out.Script) == "" {
		return scriptRewriteOutput{}, errors.New("改写结果没有返回正文")
	}
	return out, nil
}

// joinRewriteBodyLines 在交给严格解析器之前，把正文的续行并回首行：
// 模型常常分段写正文，解析器要求单行，这里先合并，话外音丢掉。
func joinRewriteBodyLines(raw string) string {
	text := strings.ReplaceAll(strings.ReplaceAll(raw, "\r\n", "\n"), "\r", "\n")
	lines := strings.Split(text, "\n")
	out := make([]string, 0, len(lines))
	inBody := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if bodyFieldStart(trimmed) {
			inBody = true
			out = append(out, strings.TrimRight(line, " \t"))
			continue
		}
		if inBody {
			if trimmed == "" {
				continue
			}
			if otherFieldStart(trimmed) {
				inBody = false
				out = append(out, line)
				continue
			}
			if isTrailingComment(trimmed) {
				continue
			}
			out[len(out)-1] += trimmed
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

// stripBracketed 去掉成对的括号及其内容，避免 TTS 念出“左括号”“重点”这类标注。
// 只处理成对出现的情况；未闭合的括号保留原样，避免误删半句。
func stripBracketed(text string) string {
	pairs := [][2]string{
		{"【", "】"}, {"（", "）"}, {"(", ")"}, {"[", "]"}, {"〔", "〕"}, {"〖", "〗"}, {"｛", "｝"}, {"{", "}"},
	}
	out := text
	for i := 0; i < 3; i++ {
		before := out
		for _, p := range pairs {
			out = stripPair(out, p[0], p[1])
		}
		if out == before {
			break
		}
	}
	return out
}

func stripPair(text, open, close string) string {
	for {
		start := strings.Index(text, open)
		if start < 0 {
			return text
		}
		end := strings.Index(text[start+len(open):], close)
		if end < 0 {
			return text
		}
		text = text[:start] + text[start+len(open)+end+len(close):]
	}
}

func bodyFieldStart(line string) bool {
	for _, key := range []string{"话术：", "话术:", "改写内容：", "改写内容:", "话术内容：", "话术内容:"} {
		if strings.HasPrefix(line, key) {
			return true
		}
	}
	return false
}

func otherFieldStart(line string) bool {
	for _, key := range []string{"话术骨架：", "话术骨架:", "一句话上下文：", "一句话上下文:", "话术要求", "说明：", "说明:", "备注"} {
		if strings.HasPrefix(line, key) {
			return true
		}
	}
	return false
}

func isBodyField(key string) bool {
	return key == "话术" || key == "改写内容" || key == "话术内容"
}

// isTrailingComment 只拦明确的话外音，正常分段不算。
func isTrailingComment(line string) bool {
	for _, prefix := range []string{"以上", "如需", "如果需", "注：", "注:", "备注", "说明：", "说明:", "（注", "(注", "改动说明", "改写说明"} {
		if strings.HasPrefix(line, prefix) {
			return true
		}
	}
	return false
}

// Compatibility for legacy callers; malformed structured output fails closed.
func parseRewriteOutput(raw string) (skeleton, script string) {
	result, err := parseScriptRewriteOutput(raw)
	if err != nil {
		return "", ""
	}
	return result.Skeleton, result.Script
}
