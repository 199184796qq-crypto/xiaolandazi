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
			case "话术骨架", "一句话上下文", "改写内容", "话术内容":
				key, value = candidate, trimmed[pos+width:]
			}
		}
		if key != "" {
			if _, exists := fields[key]; exists {
				return out, errors.New("改写结果含重复字段")
			}
			current = key
			fields[key] = []string{value}
		} else if current != "" {
			fields[current] = append(fields[current], line)
		} else if trimmed != "" {
			preamble = append(preamble, line)
		}
	}
	if len(fields) == 0 {
		// Keep legacy plain-body responses, but never speak JSON/fences or
		// unparseable KV headers as if they were the actual script.
		for _, prefix := range []string{"话术骨架", "一句话上下文", "改写内容", "话术内容", "{", "```"} {
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
		_, newBody := fields["改写内容"]
		_, oldBody := fields["话术内容"]
		_, hasContext := fields["一句话上下文"]
		if newBody && oldBody {
			return scriptRewriteOutput{}, errors.New("改写结果含两个正文字段")
		}
		if newBody || hasContext {
			if !newBody || out.Skeleton == "" || out.Context == "" {
				return scriptRewriteOutput{}, errors.New("改写结果缺少三个必需字段")
			}
			out.Script = get("改写内容")
		} else if oldBody {
			out.Script = get("话术内容")
		}
	}
	if strings.TrimSpace(out.Script) == "" {
		return scriptRewriteOutput{}, errors.New("改写结果没有返回正文")
	}
	return out, nil
}

// Compatibility for legacy callers; malformed structured output fails closed.
func parseRewriteOutput(raw string) (skeleton, script string) {
	result, err := parseScriptRewriteOutput(raw)
	if err != nil {
		return "", ""
	}
	return result.Skeleton, result.Script
}
