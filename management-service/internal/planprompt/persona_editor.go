package planprompt

import (
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"
)

// This UI provenance is stored with originals, never normalized/compiled into
// any model prompt. It is an editing constraint, not a new authorization layer.
func (s Sections) validatePersonaEditor() error {
	if s.PersonaEditorJSON == "" {
		return nil
	}
	invalid := errors.New("主播人设节点来源数据无效，请重新读取后编辑")
	if len(s.PersonaEditorJSON) > 256<<10 {
		return invalid
	}
	var metadata struct {
		Version   int     `json:"version"`
		Source    *string `json:"source"`
		UserNodes []struct {
			Index *int   `json:"index"`
			Name  string `json:"name"`
		} `json:"user_nodes"`
		UserFields json.RawMessage `json:"user_fields"`
	}
	if json.Unmarshal([]byte(s.PersonaEditorJSON), &metadata) != nil || metadata.Version != 1 || metadata.Source == nil || *metadata.Source != s.PersonaMD || metadata.UserNodes == nil {
		return invalid
	}
	seen := make(map[int]bool)
	maxIndex := utf8.RuneCountInString(s.PersonaMD)
	for _, node := range metadata.UserNodes {
		if node.Index == nil || *node.Index < 0 || *node.Index > maxIndex || seen[*node.Index] || strings.TrimSpace(node.Name) == "" || utf8.RuneCountInString(node.Name) > 40 {
			return invalid
		}
		seen[*node.Index] = true
	}
	// Optional in v1 for compatibility with already saved custom-node sources.
	// Fields have independent ownership, including additions under model nodes.
	if len(metadata.UserFields) > 0 {
		var fields []struct {
			NodeIndex *int   `json:"node_index"`
			Index     *int   `json:"index"`
			Key       string `json:"key"`
		}
		if json.Unmarshal(metadata.UserFields, &fields) != nil || fields == nil {
			return invalid
		}
		fieldSeen := make(map[[2]int]bool)
		for _, field := range fields {
			if field.NodeIndex == nil || field.Index == nil || *field.NodeIndex < 0 || *field.NodeIndex > maxIndex || *field.Index < 0 || *field.Index > maxIndex || strings.TrimSpace(field.Key) == "" || utf8.RuneCountInString(field.Key) > 40 {
				return invalid
			}
			identity := [2]int{*field.NodeIndex, *field.Index}
			if fieldSeen[identity] {
				return invalid
			}
			fieldSeen[identity] = true
		}
	}
	return nil
}
