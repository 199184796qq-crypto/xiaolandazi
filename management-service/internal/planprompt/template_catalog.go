package planprompt

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

// TemplateCatalog contains ready-to-edit text, independently of the legacy
// model-generation mothers. Reading an empty catalog never seeds old instructions.
type PromptTemplate struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Content string `json:"content"`
}
type TemplateCatalog struct {
	Version int                         `json:"version"`
	Pages   map[string][]PromptTemplate `json:"pages"`
}

var catalogID = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,80}$`)

func EmptyTemplateCatalog() TemplateCatalog {
	c := TemplateCatalog{Version: 1, Pages: make(map[string][]PromptTemplate)}
	for _, key := range TemplateKeys {
		c.Pages[key] = []PromptTemplate{}
	}
	return c
}
func IsTemplatePage(page string) bool {
	for _, key := range TemplateKeys {
		if page == key {
			return true
		}
	}
	return false
}
func (c TemplateCatalog) Validate() error {
	if c.Version != 1 || len(c.Pages) != len(TemplateKeys) {
		return fmt.Errorf("请完整提交六个标签的模板目录")
	}
	ids := map[string]bool{}
	for _, key := range TemplateKeys {
		items, ok := c.Pages[key]
		if !ok || items == nil || len(items) > 20 {
			return fmt.Errorf("每个标签须提供模板列表，最多20份")
		}
		for _, item := range items {
			if !catalogID.MatchString(item.ID) || ids[item.ID] {
				return fmt.Errorf("模板标识无效或重复")
			}
			ids[item.ID] = true
			if !utf8.ValidString(item.Name) || strings.TrimSpace(item.Name) == "" || utf8.RuneCountInString(item.Name) > 60 {
				return fmt.Errorf("模板名称须为1至60字")
			}
			if !utf8.ValidString(item.Content) || len(item.Content) > 60000 || utf8.RuneCountInString(item.Content) > 20000 {
				return fmt.Errorf("每份模板内容最多20000字、60000字节")
			}
		}
		data, err := json.Marshal(items)
		if err != nil || len(data) > 60000 {
			return fmt.Errorf("每个标签的模板合计最多60000字节（含格式），请精简内容")
		}
	}
	return nil
}
func (c TemplateCatalog) Revision() string {
	data, _ := json.Marshal(c)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
