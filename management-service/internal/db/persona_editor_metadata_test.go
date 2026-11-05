package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"strings"
	"testing"

	"livecompanion/management/internal/planprompt"
)

func TestPersonaEditorMetadataSavedAtomicallyAndScoped(t *testing.T) {
	state := &promptSectionSQL{rows: map[[2]int64][]driver.Value{}}
	database := sql.OpenDB(promptSectionConnector{state})
	defer database.Close()
	s := &Store{db: database}
	sections := planprompt.Sections{PersonaMD: "【身份】\n姓名：甲\n【自建】\n语速：中等\n"}
	metadata, _ := json.Marshal(map[string]any{"version": 1, "source": sections.PersonaMD, "user_nodes": []any{map[string]any{"index": 1, "name": "自建"}}})
	sections.PersonaEditorJSON = string(metadata)
	doc, err := s.SaveLiveAgentPlanPromptPlanning(context.Background(), 14, 4, 28, sections, "规则")
	if err != nil || doc.Sections != sections || state.writes != 1 {
		t.Fatal("metadata and text must save together", doc, err, state.writes)
	}
	if strings.Contains(doc.PromptMD+doc.SpeechPromptMD+doc.RewritePromptMD, "user_nodes") {
		t.Fatal("metadata leaked into model input")
	}
	for _, scope := range [][2]int64{{14, 4}, {14, 5}, {15, 4}} {
		reread, err := s.GetLiveAgentPlanPromptDocument(context.Background(), scope[0], scope[1])
		if err != nil || (reread.Sections.PersonaEditorJSON == sections.PersonaEditorJSON) != (scope == [2]int64{14, 4}) {
			t.Fatal("metadata readback scope mismatch", scope, reread, err)
		}
	}
	sections.PersonaMD = "新的模型正文"
	if _, err := s.SaveLiveAgentPlanPromptPlanning(context.Background(), 14, 4, 28, sections, "规则"); err == nil || state.writes != 1 {
		t.Fatal("stale metadata wrote over current original")
	}
	sections.PersonaEditorJSON = ""
	if _, err := s.SaveLiveAgentPlanPromptPlanning(context.Background(), 14, 4, 28, sections, "规则"); err != nil || state.writes != 2 {
		t.Fatal("new model text could not clear old custom nodes", err)
	}
}
