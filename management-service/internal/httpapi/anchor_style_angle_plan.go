package httpapi

import (
	"context"
	"encoding/json"
	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/anchorcycle"
	appdb "livecompanion/management/internal/db"
	"os"
	"time"
)

func cycleAnglePlanEnabled() bool { return os.Getenv("ANCHOR_STYLE_ANGLE_HOOK_PLAN_ENABLED") != "0" }

func (s *Server) ensureCycleAnglePlan(ctx context.Context, state *anchorcycle.State, profile agentgateway.SpeechModel) {
	if err := requireCycleP0(state); err != nil {
		state.Warn(err.Error())
		return
	}
	if state.Task().ProductMode == "lecture" {
		return
	}
	if !cycleAnglePlanEnabled() {
		return
	}
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
	key := state.AnglePlanKey()
	usage := state.AngleUsage(key)
	if state.AnglePlans == nil {
		state.AnglePlans = map[string]*anchorcycle.AnglePlan{}
	}
	persistent := state.TenantID > 0 && state.PlanID > 0
	if state.CurrentAnglePlan() == nil && persistent {
		saved, loadErr := s.store.LoadAngleDirections(ctx, state.TenantID, state.PlanID, key)
		if loadErr != nil {
			state.Warn("推演池读取暂不可用，本次独立推演，继续供料")
		}
		if saved != nil {
			state.AnglePlans[key] = saved
			usage.LastSupplementAt = usage.Plays
			usage.LastAttemptAt = usage.Plays
		}
	}
	if state.CurrentAnglePlan() != nil && !state.AngleRefreshDue(key) {
		return
	}
	previous := state.CurrentAnglePlan()
	usage.LastAttemptAt = usage.Plays
	facts := state.HookFacts()
	// The wire payload contains content and business link labels only. Program
	// identifiers and source relationships stay in the archived source binding.
	contentFacts := generationFactView(facts)
	task := state.Task()
	input := map[string]any{"卖点": task.SellingPoint, "角度": task.Angle, "观众顾虑": task.PainPoint, "本次商品链接": state.HookProducts(), "当前有效事实": contentFacts, "有效P0": generationP0View(state.P0PolicyMD, state.Config.ComplianceScope), "方向数量上限": 4}
	if task.FactScope != nil {
		input["用户已确认的长期纠正规则"] = task.FactScope.Corrections
	}
	if previous != nil {
		old := []string{}
		// Long-lived assets keep every generation. Only the latest directions
		// are needed as a bounded avoid-duplicate context for the next supplement.
		for _, h := range previous.Cards[max(0, len(previous.Cards)-12):] {
			old = append(old, h.Action)
		}
		input["已有方向，仅供避免新增完全相同方向"] = old
	}
	if strategy, e := state.ContentStrategy.Render("主线", state.GenerationFacts()); e == nil && strategy != "" {
		input["候选方向资料"] = strategy
	}
	packet, _ := json.Marshal(input)
	source := map[string]any{"task": task, "facts": facts, "products": state.HookProducts(), "source_sha": key, "prompt_sha": state.AnglePlanMotherSHA}
	request := agentgateway.Request{Stage: "mainline_angle_plan", Model: profile.Model, Messages: []agentgateway.Message{{Role: "system", Content: mother}, {Role: "user", Content: string(packet)}}, MaxTokens: 1400, ResponseFormat: agentgateway.ResponseText, EnableThinking: false, Timeout: 15 * time.Second}
	if profile.SystemPrompt != "" {
		request.Messages = append([]agentgateway.Message{{Role: "system", Content: profile.SystemPrompt}}, request.Messages...)
	}
	// A failed plan is a bounded, observable fallback for this immutable task,
	// not an endless series of planning requests while the mainline waits.
	fallback := func(message string) {
		state.Warn(message + "，继续生成，不停止直播")
		if previous != nil {
			return
		} // Failed supplements retain all existing directions.
		state.AnglePlans[key] = &anchorcycle.AnglePlan{SourceSHA: key, PromptSHA: state.AnglePlanMotherSHA, Model: profile.Model, PlanError: message}

	}
	callCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	callCtx = withExchangeScope(callCtx, appdb.ModelExchangeScope{TenantID: state.TenantID, PlanID: state.PlanID, RoomID: state.RoomID, RunID: state.RunID})
	id, err := s.beginModelExchange(callCtx, request.Stage, profile.ID, request)
	if err != nil {
		fallback("角度规划记录暂不可用")
		return
	}
	started := time.Now()
	response, callErr := agentgateway.CompleteSpeechEndpoint(callCtx, profile, request)
	response.Provider, response.Model = "anchor:"+profile.ID, profile.Model
	callErr = s.finishModelExchange(callCtx, id, response, response.Text, started, response.InputTokens, response.OutputTokens, callErr)
	if callErr != nil {
		fallback("角度规划未完成")
		return
	}
	plan := anchorcycle.ParseDirectionContent(response.Text, anchorcycle.ID(), facts, state.HookProducts(), task.SellingPoint)
	if len(plan.Cards) == 0 {
		fallback("角度推演返回为空，原始返回已记录")
		return
	}
	plan.SourceSHA, plan.PromptSHA, plan.Model = key, state.AnglePlanMotherSHA, profile.Model
	plan.InputTokens, plan.OutputTokens = response.InputTokens, response.OutputTokens
	plan.GenerationCount = 1
	plan.GeneratedCards = append([]anchorcycle.AngleHook(nil), plan.Cards...)
	if persistent {
		if err := s.store.AppendAngleDirections(ctx, state.TenantID, state.PlanID, key, source, plan); err != nil {
			state.Warn("新方向暂未持久保存，本场仍可使用，继续供料")
		}
	}
	if previous != nil {
		plan.Cards = anchorcycle.DistinctDirectionCards(previous.Cards, plan.Cards)
		plan.GenerationCount = previous.GenerationCount + 1
		plan.LifetimePlays = previous.LifetimePlays
	} else {
		plan.Cards = anchorcycle.DistinctDirectionCards(plan.Cards)
	}
	state.AnglePlans[key] = plan
	usage.LastSupplementAt = usage.Plays

}

func applyCycleAnglePlanPacket(state *anchorcycle.State, packet map[string]any) {
	plan := state.CurrentAnglePlan()
	if plan == nil {
		return
	}
	task := state.Task()
	task.Key = derivedHash(task.Key) // Legacy keys may embed the old speech example.
	// Keep the original angle in run state and the planning exchange, rather than
	// giving the speech generator a ready-made paragraph to recite.
	if hook := state.CurrentAngleHook(); hook != nil {
		task.ID, task.Key = "", ""
		task.Angle, task.PainPoint, task.TargetAudience = "", hook.Concern, ""
		packet["本次任务"] = generationTaskView(task)
		packet["角度钩子工作卡"] = map[string]any{"本段要完成的沟通任务": hook.Action}
		packet["讲述目的"] = plan.Goal
		calculated, warnings := state.CurrentHookCalculations()
		if len(calculated) > 0 {
			packet["程序计算的数值资料"] = calculated
		}
		for _, warning := range warnings {
			state.Warn("计算辅助未采用: " + warning)
		}
		delete(packet, "内容方向与采用条件")
		return
	}
	if len(plan.Hooks) == 0 {
		task.Angle, task.TargetAudience = "", ""
		packet["本次任务"] = generationTaskView(task)
		delete(packet, "内容方向与采用条件")
		return
	}
	hook := plan.Hooks[(state.Used[state.Task().Key].Count+len(state.Attempts))%len(plan.Hooks)]
	task.Angle = hook
	task.PainPoint = plan.AudienceConcern
	packet["本次任务"] = generationTaskView(task)
	packet["角度钩子意图"] = map[string]any{"要接住的顾虑": plan.AudienceConcern, "本段切入方法": hook}
	packet["表达展开建议"] = hook
	delete(packet, "内容方向与采用条件") // The candidate bank is consumed by planning, not recited by speech generation.
}
