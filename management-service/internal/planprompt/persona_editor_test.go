package planprompt

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPersonaEditorMetadataNeverEntersPrompts(t *testing.T) {
	s := Sections{PersonaMD: "【身份】\n姓名：甲\n【自建】\n语速：中等\n", FactsMD: "已确认事实"}
	metadata, _ := json.Marshal(map[string]any{"version": 1, "source": s.PersonaMD, "user_nodes": []any{map[string]any{"index": 1, "name": "自建"}}, "user_fields": []any{map[string]any{"node_index": 1, "index": 0, "key": "语速"}}})
	base := s
	s.PersonaEditorJSON = string(metadata)
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	if s.Interaction() != base.Interaction() || s.Speech() != base.Speech() || CompileRewritePlanning(s, "规则") != CompileRewritePlanning(base, "规则") || s.Normalize().PersonaEditorJSON != "" {
		t.Fatal("editor metadata changed model prompts")
	}
	encoded, err := json.Marshal(s)
	var reread Sections
	if err != nil || json.Unmarshal(encoded, &reread) != nil || reread != s {
		t.Fatal("editor metadata did not round trip with originals")
	}
}

func TestPersonaEditorFieldMetadataCompatibilityAndValidation(t *testing.T) {
	for _, extra := range []string{"", `,"user_fields":[]`, `,"user_fields":[{"node_index":0,"index":0,"key":"键"}]`} {
		metadata := `{"version":1,"source":"键：值","user_nodes":[]` + extra + `}`
		if err := (Sections{PersonaMD: "键：值", PersonaEditorJSON: metadata}).Validate(); err != nil {
			t.Fatal("old and field-aware metadata must both be accepted", err)
		}
	}
	for _, fields := range []string{
		`null`, `{}`, `[null]`, `[{"index":0,"key":"键"}]`,
		`[{"node_index":0,"key":"键"}]`, `[{"node_index":-1,"index":0,"key":"键"}]`,
		`[{"node_index":99,"index":0,"key":"键"}]`, `[{"node_index":0,"index":-1,"key":"键"}]`,
		`[{"node_index":0,"index":99,"key":"键"}]`, `[{"node_index":0,"index":0.5,"key":"键"}]`,
		`[{"node_index":0,"index":0,"key":" "}]`,
		`[{"node_index":0,"index":0,"key":"键"},{"node_index":0,"index":0,"key":"别的"}]`,
	} {
		t.Run(fields, func(t *testing.T) {
			metadata := `{"version":1,"source":"键：值","user_nodes":[],"user_fields":` + fields + `}`
			if err := (Sections{PersonaMD: "键：值", PersonaEditorJSON: metadata}).Validate(); err == nil {
				t.Fatal("invalid field ownership accepted")
			}
		})
	}
}

func TestPersonaEditorMetadataRejectsInvalidAndStaleData(t *testing.T) {
	for _, value := range []string{
		"not json", `null`, `{}`, `{"version":2,"source":"原文","user_nodes":[]}`,
		`{"version":1,"source":"别的正文","user_nodes":[]}`,
		`{"version":1,"source":"原文","user_nodes":null}`,
		`{"version":1,"source":"原文","user_nodes":[{"name":"自建"}]}`,
		`{"version":1,"source":"原文","user_nodes":[{"index":-1,"name":"自建"}]}`,
		`{"version":1,"source":"原文","user_nodes":[{"index":99,"name":"自建"}]}`,
		`{"version":1,"source":"原文","user_nodes":[{"index":0,"name":" "}]}`,
		`{"version":1,"source":"原文","user_nodes":[{"index":0,"name":"甲"},{"index":0,"name":"乙"}]}`,
		strings.Repeat(" ", 256<<10+1),
	} {
		t.Run(value[:min(len(value), 80)], func(t *testing.T) {
			if err := (Sections{PersonaMD: "原文", PersonaEditorJSON: value}).Validate(); err == nil {
				t.Fatal("invalid metadata accepted")
			}
		})
	}
	if err := (Sections{PersonaMD: "旧版没有来源标记"}).Validate(); err != nil {
		t.Fatal("old clients must remain compatible", err)
	}
}
