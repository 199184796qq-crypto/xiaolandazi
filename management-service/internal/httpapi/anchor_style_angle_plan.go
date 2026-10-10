package httpapi

import (
	"context"
	"encoding/json"
	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/anchorcycle"
	appdb "livecompanion/management/internal/db"
	"time"
)

func (s *Server) ensureCycleAnglePlan(ctx context.Context, state *anchorcycle.State, profile agentgateway.SpeechModel) {
	roleID, roleErr := s.store.ResolveTextRole(ctx, "mainline_angle_plan")
	if roleErr != nil {
		state.Warn("角度规划模型配置暂不可用，按原任务继续生成")
		return
	}
	if roleID != "" {
		selected, err := s.store.ResolveSpeechModel(ctx, roleID)
		if err != nil || selected == nil || !selected.Enabled {
			state.Warn("角度规划模型暂不可用，按原任务继续生成")
			return
		}
		profile = *selected
	}
	mother, _, err := loadReplaceablePromptAsset("ANCHOR_STYLE_ANGLE_HOOK_PLAN_PROMPT_PATH", "configs/prompts/anchor-style/angle-hook-plan.md", "角度钩子规划母版")
	if err != nil {
		state.AnglePlanMotherSHA = ""
		state.Warn("角度规划母版暂不可用，按原任务继续生成")
		return
	}
	state.AnglePlanMotherSHA = derivedHash(mother)
	if roleID != "" {
		state.AnglePlanMotherSHA = derivedHash(mother + "\n" + profile.ID + ":" + profile.Model + ":" + profile.BaseURL + ":" + profile.SystemPrompt)
	}
	if state.CurrentAnglePlan() != nil {
		return
	}
	input := map[string]any{"task": state.Task(), "phase": state.Phase, "confirmed_facts": state.ProductFacts(state.Task()), "effective_p0": state.P0PolicyMD}
	if strategy, e := state.ContentStrategy.Render("主线", state.ProductFacts(state.Task())); e == nil && strategy != "" {
		input["candidate_directions"] = strategy
	}
	packet, _ := json.Marshal(input)
	request := agentgateway.Request{Stage: "mainline_angle_plan", Model: profile.Model, Messages: []agentgateway.Message{{Role: "system", Content: mother}, {Role: "user", Content: string(packet)}}, MaxTokens: 1100, ResponseFormat: agentgateway.ResponseJSON, EnableThinking: false, Timeout: 15 * time.Second}
	callCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	callCtx = withExchangeScope(callCtx, appdb.ModelExchangeScope{TenantID: state.TenantID, PlanID: state.PlanID, RoomID: state.RoomID, RunID: state.RunID})
	id, err := s.beginModelExchange(callCtx, request.Stage, profile.ID, request)
	if err != nil {
		state.Warn("角度规划记录暂不可用，按原任务继续生成")
		return
	}
	started := time.Now()
	response, callErr := agentgateway.CompleteSpeechEndpoint(callCtx, profile, request)
	response.Provider, response.Model = "anchor:"+profile.ID, profile.Model
	callErr = s.finishModelExchange(callCtx, id, response, response.Text, started, response.InputTokens, response.OutputTokens, callErr)
	if callErr != nil {
		state.Warn("角度规划未完成，按原任务继续生成")
		return
	}
	plan, err := anchorcycle.ParseAnglePlan(response.Text)
	if err != nil {
		state.Warn("角度规划结构不兼容，原始返回已记录，按原任务继续生成")
		return
	}
	plan.SourceSHA, plan.PromptSHA, plan.Model = state.AnglePlanKey(), state.AnglePlanMotherSHA, profile.Model
	plan.InputTokens, plan.OutputTokens = response.InputTokens, response.OutputTokens
	if state.AnglePlans == nil {
		state.AnglePlans = map[string]*anchorcycle.AnglePlan{}
	}
	state.AnglePlans[plan.SourceSHA] = plan
}

func applyCycleAnglePlanPacket(state *anchorcycle.State, packet map[string]any) {
	plan := state.CurrentAnglePlan()
	if plan == nil {
		return
	}
	task := state.Task()
	// Keep the original angle in run state and the planning exchange, rather than
	// giving the speech generator a ready-made paragraph to recite.
	hook := plan.Hooks[(state.Used[state.Task().Key].Count+len(state.Attempts))%len(plan.Hooks)]
	task.Angle = hook
	task.PainPoint = plan.AudienceConcern
	packet["本次任务"] = task
	packet["角度钩子意图"] = map[string]any{"要接住的顾虑": plan.AudienceConcern, "本段切入方法": hook}
	packet["表达展开建议"] = hook
	delete(packet, "内容方向与采用条件") // The candidate bank is consumed by planning, not recited by speech generation.
}
