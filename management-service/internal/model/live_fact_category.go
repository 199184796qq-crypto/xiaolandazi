package model

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

type LiveFactCategory struct {
	Value       string `json:"value"`
	Label       string `json:"label"`
	LabelEN     string `json:"label_en,omitempty"`
	Description string `json:"description"`
	Group       string `json:"group"`
	GroupEN     string `json:"group_en,omitempty"`
	Family      string `json:"family"`
}

func NewLiveFactCategory(label, family, description string) (LiveFactCategory, error) {
	label, family, description = strings.TrimSpace(label), strings.TrimSpace(family), strings.TrimSpace(description)
	if family == "" {
		family = "other"
	}
	if description == "" {
		description = label
	}
	allowed := map[string]bool{"product": true, "link": true, "trade": true, "fulfillment": true, "identity_location": true, "other": true}
	if label == "" || utf8.RuneCountInString(label) > 40 || utf8.RuneCountInString(description) > 200 || !allowed[family] {
		return LiveFactCategory{}, errors.New("分类名称须为1～40字，说明不超过200字，所属类型必须有效")
	}
	for _, r := range label + description {
		if unicode.IsControl(r) {
			return LiveFactCategory{}, errors.New("分类名称和说明不能包含控制字符")
		}
	}
	hash := sha256.Sum256([]byte(strings.ToLower(label)))
	return LiveFactCategory{Value: fmt.Sprintf("custom_%x", hash[:10]), Label: label, Family: family, Description: description, Group: "自定义分类"}, nil
}
