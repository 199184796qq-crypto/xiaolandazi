package planprompt

import (
	"strings"
	"testing"
)

func TestTemplateCatalogValidationAndRawIdentity(t *testing.T) {
	c := EmptyTemplateCatalog()
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	before := c.Revision()
	c.Pages["persona_md"] = []PromptTemplate{{ID: "first", Name: "甲模板", Content: "  【身份】\n名字：小林😀\n{{直播间名}}  "}, {ID: "second", Name: "空模板", Content: ""}}
	if err := c.Validate(); err != nil || c.Revision() == before {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		mutate func(*TemplateCatalog)
	}{
		{"missing page", func(c *TemplateCatalog) { delete(c.Pages, "facts_md") }},
		{"null list", func(c *TemplateCatalog) { c.Pages["facts_md"] = nil }},
		{"unknown page", func(c *TemplateCatalog) { c.Pages["x"] = []PromptTemplate{} }},
		{"duplicate id", func(c *TemplateCatalog) { c.Pages["facts_md"] = []PromptTemplate{{ID: "first", Name: "同标识"}} }},
		{"bad id", func(c *TemplateCatalog) { c.Pages["persona_md"][0].ID = "../x" }},
		{"empty name", func(c *TemplateCatalog) { c.Pages["persona_md"][0].Name = " \n" }},
		{"long name", func(c *TemplateCatalog) { c.Pages["persona_md"][0].Name = strings.Repeat("字", 61) }},
		{"long text", func(c *TemplateCatalog) { c.Pages["persona_md"][0].Content = strings.Repeat("字", 20001) }},
		{"byte limit", func(c *TemplateCatalog) { c.Pages["persona_md"][0].Content = strings.Repeat("😀", 15001) }},
		{"too many", func(c *TemplateCatalog) { c.Pages["facts_md"] = make([]PromptTemplate, 21) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			next := EmptyTemplateCatalog()
			next.Pages["persona_md"] = append([]PromptTemplate{}, c.Pages["persona_md"]...)
			test.mutate(&next)
			if next.Validate() == nil {
				t.Fatal("invalid catalog accepted")
			}
		})
	}
	if !IsTemplatePage("rewrite_planning_md") || IsTemplatePage("persona_editor_json") {
		t.Fatal("page whitelist")
	}
}
