package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"livecompanion/management/internal/planprompt"
)

type LiveAgentPlanPromptDocument struct {
	Sections            planprompt.Sections `json:"sections"`
	PromptMD            string              `json:"prompt_md"`
	InteractionPromptMD string              `json:"interaction_prompt_md"`
	SpeechPromptMD      string              `json:"speech_prompt_md"`
	LegacyUnstructured  bool                `json:"legacy_unstructured,omitempty"`
	LegacyPromptMD      string              `json:"legacy_prompt_md,omitempty"`
	UpdatedAt           time.Time           `json:"updated_at,omitempty"`
}

func (s *Store) ensureLiveAgentPlanPromptColumns(ctx context.Context) error {
	for _, column := range []string{"sections_json", "speech_prompt_md"} {
		var count int
		if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.columns WHERE table_schema=DATABASE() AND table_name='live_agent_plan_prompts' AND column_name=?`, column).Scan(&count); err != nil {
			return err
		}
		if count == 0 {
			if _, err := s.db.ExecContext(ctx, "ALTER TABLE live_agent_plan_prompts ADD COLUMN "+column+" MEDIUMTEXT NULL"); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Store) GetLiveAgentPlanPromptDocument(ctx context.Context, tenantID, planID int64) (LiveAgentPlanPromptDocument, error) {
	var out LiveAgentPlanPromptDocument
	if tenantID <= 0 || planID <= 0 {
		return out, ErrLiveAgentPlanNotFound
	}
	var sections, speech sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT prompt_md, sections_json, speech_prompt_md, updated_at FROM live_agent_plan_prompts WHERE tenant_id=? AND plan_id=?`, tenantID, planID).Scan(&out.PromptMD, &sections, &speech, &out.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	if sections.Valid && strings.TrimSpace(sections.String) != "" {
		if err := json.Unmarshal([]byte(sections.String), &out.Sections); err != nil {
			return out, fmt.Errorf("decode plan prompt sections: %w", err)
		}
		out.SpeechPromptMD = speech.String
	} else {
		// No page originals exist for a legacy document. Never reverse-parse its
		// compiled text into editable pages; expose that old text separately.
		out.LegacyPromptMD = out.PromptMD
		out.LegacyUnstructured = strings.TrimSpace(out.PromptMD) != ""
	}
	out.InteractionPromptMD = out.PromptMD
	return out, nil
}

// One statement persists all four editable sections and both generated prompts.
// Existing runtime readers consume prompt_md, which is now the interaction prompt.
func (s *Store) SaveLiveAgentPlanPromptSections(ctx context.Context, tenantID, planID, actorID int64, sections planprompt.Sections) (LiveAgentPlanPromptDocument, error) {
	var out LiveAgentPlanPromptDocument
	if tenantID <= 0 || planID <= 0 {
		return out, ErrLiveAgentPlanNotFound
	}
	if err := sections.Validate(); err != nil {
		return out, err
	}
	// Persist page originals verbatim. Interaction/Speech compile cleaned copies;
	// neither compilation nor later reads may mutate or repartition originals.
	raw, err := json.Marshal(sections)
	if err != nil {
		return out, err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO live_agent_plan_prompts (tenant_id, plan_id, prompt_md, sections_json, speech_prompt_md, updated_by_user_id)
		VALUES (?, ?, ?, ?, ?, ?) ON DUPLICATE KEY UPDATE prompt_md=VALUES(prompt_md), sections_json=VALUES(sections_json), speech_prompt_md=VALUES(speech_prompt_md), updated_by_user_id=VALUES(updated_by_user_id), updated_at=CURRENT_TIMESTAMP(3)`, tenantID, planID, sections.Interaction(), string(raw), sections.Speech(), actorID)
	if err != nil {
		return out, err
	}
	return s.GetLiveAgentPlanPromptDocument(ctx, tenantID, planID)
}

// GetLiveAgentPlanPrompt returns the editable plan-level prompt. A missing row
// is an empty prompt, not an error: every plan starts without a custom prompt.
func (s *Store) GetLiveAgentPlanPrompt(ctx context.Context, tenantID, planID int64) (string, time.Time, error) {
	if tenantID <= 0 || planID <= 0 {
		return "", time.Time{}, ErrLiveAgentPlanNotFound
	}
	var prompt string
	var updatedAt time.Time
	err := s.db.QueryRowContext(ctx, `
		SELECT prompt_md, updated_at
		FROM live_agent_plan_prompts
		WHERE tenant_id=? AND plan_id=?
	`, tenantID, planID).Scan(&prompt, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return "", time.Time{}, nil
	}
	if err != nil {
		return "", time.Time{}, err
	}
	return prompt, updatedAt, nil
}

// SaveLiveAgentPlanPrompt upserts the plan-level prompt. It is deliberately
// independent from plan versions: the prompt is an editable authoring asset.
func (s *Store) SaveLiveAgentPlanPrompt(ctx context.Context, tenantID, planID, actorID int64, prompt string) (time.Time, error) {
	if tenantID <= 0 || planID <= 0 {
		return time.Time{}, ErrLiveAgentPlanNotFound
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO live_agent_plan_prompts (tenant_id, plan_id, prompt_md, updated_by_user_id)
		VALUES (?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE
			prompt_md=VALUES(prompt_md),
			sections_json=NULL,
			speech_prompt_md=NULL,
			updated_by_user_id=VALUES(updated_by_user_id),
			updated_at=CURRENT_TIMESTAMP(3)
	`, tenantID, planID, strings.TrimSpace(prompt), actorID)
	if err != nil {
		return time.Time{}, err
	}
	_, updatedAt, err := s.GetLiveAgentPlanPrompt(ctx, tenantID, planID)
	return updatedAt, err
}
