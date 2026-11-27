package db

import (
	"context"
	"errors"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"livecompanion/management/internal/model"
	"livecompanion/management/internal/planprompt"
)

func TestLiveAgentPlanEditDraftMySQL(t *testing.T) {
	s := salesIsolatedMySQL(t)
	ctx := context.Background()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := s.db.ExecContext(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	wanted := map[string]bool{
		"live_agent_plans": true, "live_agent_plan_edit_drafts": true,
		"live_agent_plan_versions": true, "live_agent_room_plan_selections": true,
		"live_runtime_sessions": true,
	}
	for _, statement := range strings.Split(strings.ReplaceAll(liveRuntimeSchema, "\r\n", "\n"), "\n-- +statement\n") {
		fields := strings.Fields(statement)
		if len(fields) > 5 && wanted[fields[5]] {
			exec(statement)
		}
	}
	schema, err := os.ReadFile("db.go")
	if err != nil {
		t.Fatal(err)
	}
	prompts := regexp.MustCompile("(?s)`(CREATE TABLE IF NOT EXISTS live_agent_plan_prompts .*?)`").FindSubmatch(schema)
	if len(prompts) != 2 {
		t.Fatal("missing plan prompt DDL")
	}
	exec(string(prompts[1]))
	exec(`CREATE TABLE core_rooms (id BIGINT PRIMARY KEY, tenant_id BIGINT NOT NULL)`)
	exec(`CREATE TABLE mgmt_room_deletions (room_id BIGINT PRIMARY KEY, tenant_id BIGINT NOT NULL)`)
	exec(`INSERT INTO core_rooms VALUES (29,14)`)
	exec(`INSERT INTO live_agent_plans(id,tenant_id,name,description) VALUES (7,14,'Test','')`)
	exec(`INSERT INTO live_agent_room_plan_selections(tenant_id,room_id,plan_id) VALUES (14,29,7)`)
	version := func(revision string) (model.LiveAgentPlanVersion, error) {
		return s.CreateLiveAgentPlanVersion(ctx, 14, 7, 2, model.CreateLiveAgentPlanVersionInput{
			DurationMinutes: 20, RoundMinutes: 4, EditDraftRevision: revision,
			GenerationContext: map[string]any{"content_mode": model.LiveContentUserAudio},
		})
	}
	previous, err := version("")
	if err != nil {
		t.Fatal(err)
	}
	exec(`INSERT INTO live_agent_plan_prompts(tenant_id,plan_id,prompt_md,sections_json,speech_prompt_md) VALUES (14,7,'',JSON_OBJECT('speech_requirements_md','已有话术要求','interaction_requirements_md','历史互动回答要求'),'既有运行提示词')`)
	draft := LiveAgentPlanEditDraft{Draft: &LiveAgentPlanEditContent{
		RoomIntro: "已有直播简介", Prompt: &LiveAgentPlanEditPrompt{PersonaMD: "主播人设"}, OpenIssues: []string{"主线话术至少需要 400 字符"},
	}}
	saved, err := s.SaveLiveAgentPlanEditDraft(ctx, 14, 7, draft)
	if err != nil || saved.Revision == "" {
		t.Fatalf("draft save: %+v, %v", saved, err)
	}
	exec(`UPDATE live_agent_plan_edit_drafts SET draft_json=JSON_SET(draft_json,'$.prompt.speech_requirements_md','') WHERE tenant_id=14 AND plan_id=7`)
	loaded, err := s.GetLiveAgentPlanEditDraft(ctx, 14, 7)
	if err != nil || loaded.Draft == nil || loaded.Draft.RoomIntro != draft.Draft.RoomIntro || loaded.Draft.Prompt == nil || loaded.Draft.Prompt.SpeechRequirementsMD != nil {
		t.Fatalf("draft reload: %+v, %v", loaded, err)
	}
	if _, err := s.GetLiveAgentPlanEditDraft(ctx, 24, 7); !errors.Is(err, ErrLiveAgentPlanNotFound) {
		t.Fatalf("cross-tenant draft read: %v", err)
	}
	if _, err := s.SaveLiveAgentPlanEditDraft(ctx, 14, 7, draft); !errors.Is(err, ErrLiveAgentPlanEditConflict) {
		t.Fatalf("stale draft write: %v", err)
	}
	if _, err := version(""); !errors.Is(err, ErrLiveAgentPlanEditConflict) {
		t.Fatalf("unrelated version save with pending draft: %v", err)
	}
	if _, err := s.StartLiveRuntimeSession(ctx, 14, 29, 2, nil, "test", time.Now(), previous); !errors.Is(err, ErrLiveAgentPlanNotReady) || !strings.Contains(err.Error(), "400") {
		t.Fatalf("new session while draft is unready: %v", err)
	}

	exec(`INSERT INTO live_runtime_sessions(external_id,tenant_id,room_id,status,execution_realm,started_at,last_billed_at) VALUES ('running-edit',14,29,'running','test',UTC_TIMESTAMP(3),UTC_TIMESTAMP(3))`)
	saved.Draft.OpenIssues = nil
	speech := "先讲清规格\n再讲使用方式"
	saved.Draft.Prompt.SpeechRequirementsMD = &speech
	interaction := "  先回答观众的问题\n再补充适用条件  "
	saved.Draft.Prompt.InteractionGuidanceMD = &interaction
	updated, err := s.SaveLiveAgentPlanEditDraft(ctx, 14, 7, saved)
	if err != nil {
		t.Fatalf("draft save during live session: %v", err)
	}
	if _, err := version(updated.Revision); !errors.Is(err, ErrLiveAgentPlanInUse) {
		t.Fatalf("formal save during live session: %v", err)
	}
	updated.Draft.Prompt.SpeechRequirementsMD = nil
	updated.Draft.Prompt.InteractionGuidanceMD = nil
	updated, err = s.SaveLiveAgentPlanEditDraft(ctx, 14, 7, updated)
	if err != nil || updated.Draft.Prompt.SpeechRequirementsMD == nil || *updated.Draft.Prompt.SpeechRequirementsMD != speech {
		t.Fatalf("older editor must retain saved draft guidance: %+v, %v", updated, err)
	}
	if updated.Draft.Prompt.InteractionGuidanceMD == nil || *updated.Draft.Prompt.InteractionGuidanceMD != interaction {
		t.Fatalf("older editor dropped interaction guidance from draft: %+v", updated.Draft.Prompt)
	}
	exec(`UPDATE live_runtime_sessions SET status='stopped',ended_at=UTC_TIMESTAMP(3) WHERE external_id='running-edit'`)
	current, err := version(updated.Revision)
	if err != nil || current.VersionNo != previous.VersionNo+1 {
		t.Fatalf("formal save: %+v, %v", current, err)
	}
	cleared, err := s.GetLiveAgentPlanEditDraft(ctx, 14, 7)
	if err != nil || cleared.Draft != nil || cleared.Revision == updated.Revision {
		t.Fatalf("cleared editing draft: %+v, %v", cleared, err)
	}
	document, err := s.GetLiveAgentPlanPromptDocument(ctx, 14, 7)
	if err != nil || document.Sections.PersonaMD != "主播人设" || document.Sections.SpeechRequirementsMD != speech || document.SpeechPromptMD != "既有运行提示词" {
		t.Fatalf("saved prompt sections: %+v, %v", document, err)
	}
	if document.InteractionGuidanceMD != interaction || document.Sections.InteractionRequirementsMD != "历史互动回答要求" || document.InteractionPromptMD != "" {
		t.Fatalf("interaction guidance must persist separately from compiled and historical prompts: %+v", document)
	}
	if _, err := s.SaveLiveAgentPlanActivePromptSections(ctx, 14, 7, 2, planprompt.Sections{PersonaMD: "新主播人设"}); err != nil {
		t.Fatalf("legacy prompt save: %v", err)
	}
	document, err = s.GetLiveAgentPlanPromptDocument(ctx, 14, 7)
	if err != nil || document.Sections.SpeechRequirementsMD != speech || document.InteractionGuidanceMD != interaction {
		t.Fatalf("legacy prompt save dropped requirements: %+v, %v", document, err)
	}
	if _, err := s.StartLiveRuntimeSession(ctx, 14, 29, 2, nil, "test", time.Now(), previous); !errors.Is(err, ErrLiveAgentPlanNotReady) {
		t.Fatalf("new session with stale formal version: %v", err)
	}
	if _, err := s.StartLiveRuntimeSession(ctx, 14, 29, 2, nil, "test", time.Now(), current); !errors.Is(err, ErrLiveDeviceNotBound) {
		t.Fatalf("ready snapshot must reach the existing device check: %v", err)
	}
	blank := ""
	clear, err := s.SaveLiveAgentPlanEditDraft(ctx, 14, 7, LiveAgentPlanEditDraft{Revision: cleared.Revision, Draft: &LiveAgentPlanEditContent{
		Prompt: &LiveAgentPlanEditPrompt{PersonaMD: "新主播人设", SpeechRequirementsMD: &blank, InteractionGuidanceMD: &blank},
	}})
	if err != nil {
		t.Fatalf("save draft with cleared requirements: %v", err)
	}
	if _, err := version(clear.Revision); err != nil {
		t.Fatalf("publish cleared requirements: %v", err)
	}
	document, err = s.GetLiveAgentPlanPromptDocument(ctx, 14, 7)
	if err != nil || document.Sections.SpeechRequirementsMD != "" || document.InteractionGuidanceMD != "" || document.SpeechPromptMD != "既有运行提示词" {
		t.Fatalf("clear requirements must preserve the existing runtime prompt: %+v, %v", document, err)
	}
}
