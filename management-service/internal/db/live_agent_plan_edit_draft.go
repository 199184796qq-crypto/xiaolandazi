package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"unicode/utf8"

	"livecompanion/management/internal/model"
	"livecompanion/management/internal/planprompt"
	"livecompanion/management/internal/qapair"
)

var ErrLiveAgentPlanEditConflict = errors.New("方案已在其他页面保存，请刷新核对最新内容后再提交")
var ErrPlanReviewFactsChanged = errors.New("核对后事实清单已更新，请重新核对话术")
var ErrLiveAgentPlanNotReady = errors.New("不能开播")

type LiveAgentPlanEditContent struct {
	RoomIntro  string                   `json:"room_intro"`
	Prompt     *LiveAgentPlanEditPrompt `json:"prompt,omitempty"`
	Workspace  json.RawMessage          `json:"workspace,omitempty"`
	OpenIssues []string                 `json:"open_issues"`
}

type LiveAgentPlanEditPrompt struct {
	PersonaMD         string `json:"persona_md"`
	RedLinesMD        string `json:"red_lines_md"`
	PersonaEditorJSON string `json:"persona_editor_json,omitempty"`
	// Old drafts serialized an empty speech_requirements_md even though the section was not editable.
	// Only an explicit edit should replace the saved requirements.
	SpeechRequirementsMD  *string `json:"speech_requirements_text,omitempty"`
	InteractionGuidanceMD *string `json:"interaction_guidance_md,omitempty"`
}

func (p LiveAgentPlanEditPrompt) activeSections() planprompt.Sections {
	return planprompt.Sections{PersonaMD: p.PersonaMD, RedLinesMD: p.RedLinesMD, PersonaEditorJSON: p.PersonaEditorJSON}
}

func (p LiveAgentPlanEditPrompt) Validate() error {
	if p.SpeechRequirementsMD != nil && utf8.RuneCountInString(*p.SpeechRequirementsMD) > planprompt.MaxPromptRunes {
		return errors.New("话术要求不能超过20000字，请缩短内容")
	}
	if p.InteractionGuidanceMD != nil && utf8.RuneCountInString(*p.InteractionGuidanceMD) > planprompt.MaxPromptRunes {
		return errors.New("互动要求不能超过20000字，请缩短内容")
	}
	return p.activeSections().Validate()
}

type LiveAgentPlanEditDraft struct {
	Revision              string                    `json:"revision"`
	Draft                 *LiveAgentPlanEditContent `json:"draft"`
	ExpectedFactsRevision string                    `json:"expected_facts_revision,omitempty"`
}

func MaterialReviewRevision(claims []model.PlanMaterialClaim, catalog model.MaterialObjectCatalog, links []QALink) string {
	return qapair.Hash([]any{MaterialLibraryRevision(claims), catalog, links})
}

func readPlanEditDraft(ctx context.Context, reader interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, tenantID, planID int64, locking bool) (LiveAgentPlanEditDraft, error) {
	query := `SELECT COALESCE(edit.revision,''), edit.draft_json
		FROM live_agent_plans plan
		LEFT JOIN live_agent_plan_edit_drafts edit ON edit.tenant_id=plan.tenant_id AND edit.plan_id=plan.id
		WHERE plan.tenant_id=? AND plan.id=? AND plan.status='active'`
	if locking {
		query += " FOR UPDATE"
	}
	var saved LiveAgentPlanEditDraft
	var raw []byte
	err := reader.QueryRowContext(ctx, query, tenantID, planID).Scan(&saved.Revision, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return saved, ErrLiveAgentPlanNotFound
	}
	if err != nil || len(raw) == 0 {
		return saved, err
	}
	return saved, json.Unmarshal(raw, &saved.Draft)
}

func (s *Store) GetLiveAgentPlanEditDraft(ctx context.Context, tenantID, planID int64) (LiveAgentPlanEditDraft, error) {
	return readPlanEditDraft(ctx, s.db, tenantID, planID, false)
}

func (s *Store) SaveLiveAgentPlanEditDraft(ctx context.Context, tenantID, planID int64, input LiveAgentPlanEditDraft) (LiveAgentPlanEditDraft, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return LiveAgentPlanEditDraft{}, err
	}
	defer tx.Rollback()

	saved, err := readPlanEditDraft(ctx, tx, tenantID, planID, true)
	if err != nil {
		return LiveAgentPlanEditDraft{}, err
	}
	if saved.Revision != input.Revision {
		return LiveAgentPlanEditDraft{}, ErrLiveAgentPlanEditConflict
	}
	if input.ExpectedFactsRevision != "" {
		claims, err := listMaterialClaims(ctx, lockedMaterialQuery{tx}, tenantID, planID)
		if err != nil {
			return LiveAgentPlanEditDraft{}, err
		}
		catalog, links, _, err := readQAObjects(ctx, tx, tenantID, planID, true)
		if err != nil {
			return LiveAgentPlanEditDraft{}, err
		}
		if MaterialReviewRevision(claims, catalog, links) != input.ExpectedFactsRevision {
			return LiveAgentPlanEditDraft{}, ErrPlanReviewFactsChanged
		}
		input.ExpectedFactsRevision = ""
	}
	// Older editors omit these sections when saving the other prompt pages.
	if saved.Draft != nil && saved.Draft.Prompt != nil && input.Draft != nil && input.Draft.Prompt != nil {
		if input.Draft.Prompt.SpeechRequirementsMD == nil {
			input.Draft.Prompt.SpeechRequirementsMD = saved.Draft.Prompt.SpeechRequirementsMD
		}
		if input.Draft.Prompt.InteractionGuidanceMD == nil {
			input.Draft.Prompt.InteractionGuidanceMD = saved.Draft.Prompt.InteractionGuidanceMD
		}
	}
	raw, err := json.Marshal(input.Draft)
	if err != nil {
		return LiveAgentPlanEditDraft{}, err
	}
	input.Revision, err = newLiveReference("EDIT")
	if err != nil {
		return LiveAgentPlanEditDraft{}, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO live_agent_plan_edit_drafts (tenant_id,plan_id,revision,draft_json)
		VALUES (?,?,?,CAST(? AS JSON))
		ON DUPLICATE KEY UPDATE revision=VALUES(revision),draft_json=VALUES(draft_json)`,
		tenantID, planID, input.Revision, string(raw))
	if err != nil {
		return LiveAgentPlanEditDraft{}, err
	}
	if err := tx.Commit(); err != nil {
		return LiveAgentPlanEditDraft{}, err
	}
	return input, nil
}

func (draft LiveAgentPlanEditDraft) AllowsPublish(revision string) bool {
	if draft.Draft == nil {
		return revision == ""
	}
	return revision != "" && draft.Revision == revision
}

func (draft LiveAgentPlanEditDraft) CheckStart() error {
	if draft.Draft == nil {
		return nil
	}
	if len(draft.Draft.OpenIssues) > 0 {
		return fmt.Errorf("%w：%s。请补齐后重新保存方案", ErrLiveAgentPlanNotReady, draft.Draft.OpenIssues[0])
	}
	return fmt.Errorf("%w：最新保存的方案还未通过开播检查，请重新保存方案", ErrLiveAgentPlanNotReady)
}

func finishLiveAgentPlanEditDraft(ctx context.Context, tx *sql.Tx, tenantID, planID, actorID int64, draft LiveAgentPlanEditDraft) error {
	if draft.Draft == nil {
		return nil
	}
	if prompt := draft.Draft.Prompt; prompt != nil {
		if err := saveLiveAgentPlanActivePromptSections(ctx, tx, tenantID, planID, actorID, *prompt); err != nil {
			return err
		}
	}
	revision, err := newLiveReference("EDIT")
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE live_agent_plan_edit_drafts SET draft_json=NULL,revision=? WHERE tenant_id=? AND plan_id=?`, revision, tenantID, planID)
	return err
}
