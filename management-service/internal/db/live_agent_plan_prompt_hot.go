package db

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
	"unicode/utf8"

	"livecompanion/management/internal/planprompt"
)

var ErrPlanPromptChanged = errors.New("方案提示词已被其他保存更新，请重新读取后再试，当前草稿请先保留")
var ErrPlanPromptHotUnavailable = errors.New("当前方案缺少完整的分区提示词，请结束直播后保存一次完整方案")
var ErrRuntimePromptInvalid = errors.New("三项提示词内容不合法")

type RuntimePromptUpdate struct {
	ExpectedRevision          string `json:"expected_revision"`
	SpeechRequirementsMD      string `json:"speech_requirements_md"`
	InteractionRequirementsMD string `json:"interaction_requirements_md"`
	RewritePlanningMD         string `json:"rewrite_planning_md"`
}

func livePlanPromptRevision(prompt, originals, rewrite string) string {
	raw, _ := json.Marshal([]string{prompt, originals, rewrite})
	return fmt.Sprintf("%x", sha256.Sum256(raw))
}

func (s *Store) GetLiveAgentPlanRuntimePrompts(ctx context.Context, tenant, plan int64) (planprompt.RuntimePrompts, error) {
	doc, err := s.GetLiveAgentPlanPromptDocument(ctx, tenant, plan)
	if err != nil {
		return planprompt.RuntimePrompts{}, err
	}
	p := planprompt.RuntimePrompts{Sections: doc.Sections, Revision: doc.Revision, Enabled: doc.RuntimeHotUpdate, Structured: !doc.LegacyUnstructured && doc.Revision != ""}
	if doc.RewritePlanningMD != nil {
		p.Planning = *doc.RewritePlanningMD
	} else {
		p.Enabled = false
	}
	return p, nil
}

// SaveLiveAgentPlanRuntimePrompts updates three page originals and their compiled
// uses in one transaction. It never publishes a version or modifies other pages.
func (s *Store) SaveLiveAgentPlanRuntimePrompts(ctx context.Context, tenant, plan, actor int64, input RuntimePromptUpdate) (LiveAgentPlanPromptDocument, error) {
	if tenant <= 0 || plan <= 0 {
		return LiveAgentPlanPromptDocument{}, ErrLiveAgentPlanNotFound
	}
	if input.ExpectedRevision == "" {
		return LiveAgentPlanPromptDocument{}, ErrPlanPromptChanged
	}
	if utf8.RuneCountInString(input.RewritePlanningMD) > planprompt.MaxPromptRunes {
		return LiveAgentPlanPromptDocument{}, fmt.Errorf("%w：话术规划不能超过20000字", ErrRuntimePromptInvalid)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return LiveAgentPlanPromptDocument{}, err
	}
	defer tx.Rollback()
	// Serialize with version saves and stream starts using their existing plan lock.
	var id int64
	if err = tx.QueryRowContext(ctx, `SELECT id FROM live_agent_plans WHERE tenant_id=? AND id=? FOR UPDATE`, tenant, plan).Scan(&id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			err = ErrLiveAgentPlanNotFound
		}
		return LiveAgentPlanPromptDocument{}, err
	}
	var prompt string
	var originals, rewrite sql.NullString
	if err = tx.QueryRowContext(ctx, `SELECT prompt_md, sections_json, rewrite_prompt_md FROM live_agent_plan_prompts WHERE tenant_id=? AND plan_id=? FOR UPDATE`, tenant, plan).Scan(&prompt, &originals, &rewrite); err != nil {
		return LiveAgentPlanPromptDocument{}, err
	}
	if input.ExpectedRevision != livePlanPromptRevision(prompt, originals.String, rewrite.String) {
		return LiveAgentPlanPromptDocument{}, ErrPlanPromptChanged
	}
	var sections planprompt.Sections
	var raw map[string]json.RawMessage
	if !originals.Valid || json.Unmarshal([]byte(originals.String), &raw) != nil || raw == nil || json.Unmarshal([]byte(originals.String), &sections) != nil {
		return LiveAgentPlanPromptDocument{}, ErrPlanPromptHotUnavailable
	}
	if sections.Interaction() != prompt {
		return LiveAgentPlanPromptDocument{}, ErrPlanPromptHotUnavailable
	}
	sections.SpeechRequirementsMD = input.SpeechRequirementsMD
	sections.InteractionRequirementsMD = input.InteractionRequirementsMD
	if err = sections.Validate(); err != nil {
		return LiveAgentPlanPromptDocument{}, fmt.Errorf("%w：%v", ErrRuntimePromptInvalid, err)
	}
	compiledRewrite := planprompt.CompileRewritePlanning(sections, input.RewritePlanningMD)
	if utf8.RuneCountInString(compiledRewrite) > planprompt.MaxPromptRunes {
		return LiveAgentPlanPromptDocument{}, fmt.Errorf("%w：合成后的话术修改元提示词不能超过20000字", ErrRuntimePromptInvalid)
	}
	for key, value := range map[string]any{"speech_requirements_md": input.SpeechRequirementsMD, "interaction_requirements_md": input.InteractionRequirementsMD, "rewrite_planning_md": input.RewritePlanningMD, "runtime_hot_update": true} {
		raw[key], err = json.Marshal(value)
		if err != nil {
			return LiveAgentPlanPromptDocument{}, err
		}
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return LiveAgentPlanPromptDocument{}, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE live_agent_plan_prompts SET prompt_md=?, sections_json=?, speech_prompt_md=?, rewrite_prompt_md=?, updated_by_user_id=?, updated_at=CURRENT_TIMESTAMP(3) WHERE tenant_id=? AND plan_id=?`, sections.Interaction(), string(encoded), sections.Speech(), compiledRewrite, actor, tenant, plan)
	if err != nil {
		return LiveAgentPlanPromptDocument{}, err
	}
	if err = tx.Commit(); err != nil {
		return LiveAgentPlanPromptDocument{}, err
	}
	// Return the exact committed revision, avoiding a post-commit read that could
	// fail or return another editor's later save as this request's own receipt.
	planning := input.RewritePlanningMD
	return LiveAgentPlanPromptDocument{Sections: sections, PromptMD: sections.Interaction(), InteractionPromptMD: sections.Interaction(), SpeechPromptMD: sections.Speech(), RewritePromptMD: compiledRewrite, RewritePlanningMD: &planning, Revision: livePlanPromptRevision(sections.Interaction(), string(encoded), compiledRewrite), RuntimeHotUpdate: true, UpdatedAt: time.Now().UTC()}, nil
}
