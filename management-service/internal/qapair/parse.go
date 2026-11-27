package qapair

import (
	"errors"
	"fmt"
	"livecompanion/management/internal/model"
	"strings"
)

// Parse is deterministic. Only a complete independent label line starts a new
// field; incomplete or ambiguous blocks are rejected instead of guessed.
func Parse(text string, targets []model.MaterialObjectTarget) ([]Entry, error) {
	if len([]rune(text)) > 20000 || strings.TrimSpace(text) == "" {
		return nil, errors.New("请粘贴1至20000字问答")
	}
	text = strings.ReplaceAll(text, "\r\n", "\n")
	lines := strings.Split(text, "\n")
	out := []Entry{}
	var current *Entry
	field := ""
	original := []string{}
	finish := func() error {
		if current == nil {
			return nil
		}
		current.Original = strings.Join(original, "\n")
		e, err := Normalize(*current)
		if err != nil {
			return err
		}
		out = append(out, e)
		return nil
	}
	for i, line := range lines {
		trim := strings.TrimSpace(line)
		label, value, recognized := "", "", false
		for _, key := range []string{"问题", "回答", "相似问法", "适用条件", "例外", "有效期"} {
			for _, colon := range []string{"：", ":"} {
				if strings.HasPrefix(trim, key+colon) {
					label = key
					value = strings.TrimSpace(strings.TrimPrefix(trim, key+colon))
					recognized = true
					break
				}
			}
			if recognized {
				break
			}
		}
		if label == "问题" {
			if err := finish(); err != nil {
				return nil, fmt.Errorf("第%d行之前：%w", i+1, err)
			}
			current = &Entry{Question: value, ClientID: fmt.Sprintf("row-%d", len(out)+1), Targets: targets, Enabled: true}
			field = "问题"
			original = []string{line}
			continue
		}
		if current == nil {
			if trim == "" {
				continue
			}
			return nil, fmt.Errorf("第%d行请以“问题：”开始；大白话可先整理", i+1)
		}
		original = append(original, line)
		if recognized {
			switch label {
			case "回答":
				if current.Answer != "" {
					return nil, fmt.Errorf("第%d行出现第二个回答标记，请核对问答边界", i+1)
				}
				current.Answer = value
			case "相似问法":
				for _, s := range strings.FieldsFunc(value, func(r rune) bool { return r == '|' || r == '｜' || r == '；' || r == ';' }) {
					if s = strings.TrimSpace(s); s != "" {
						current.Aliases = append(current.Aliases, s)
					}
				}
			case "适用条件":
				current.Conditions = value
			case "例外":
				current.Exceptions = value
			case "有效期":
				return nil, fmt.Errorf("第%d行有效期请在预览中的时间字段设置，避免猜测日期", i+1)
			}
			field = label
			continue
		}
		if trim == "" {
			if field == "回答" && current.Answer != "" {
				current.Answer += "\n"
			}
			continue
		}
		switch field {
		case "回答":
			current.Answer += "\n" + line
		case "适用条件":
			current.Conditions += "\n" + line
		case "例外":
			current.Exceptions += "\n" + line
		default:
			return nil, fmt.Errorf("第%d行边界不明确，请使用问题和回答标记", i+1)
		}
	}
	if err := finish(); err != nil {
		return nil, err
	}
	if len(out) > 100 {
		return nil, errors.New("请一次保存不超过100条问答")
	}
	// Delimiter-adjacent blank lines are formatting, not answer content.
	for i := range out {
		out[i].Answer = strings.TrimRight(out[i].Answer, "\n")
	}
	return out, nil
}
