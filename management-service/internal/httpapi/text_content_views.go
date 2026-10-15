package httpapi

import (
	"fmt"
	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/contentguard"
	"livecompanion/management/internal/speechbase"
	"strings"
)

// Enforced before every owned generation/planning call, including offline
// probes. An absent or invalid policy must never become an unconstrained call.
func requireCycleP0(state *anchorcycle.State) error {
	if strings.TrimSpace(state.P0PolicyMD) == "" {
		return fmt.Errorf("本次有效P0缺失，本次不发送模型请求")
	}
	if _, err := contentguard.Compile(state.P0PolicyMD); err != nil {
		return fmt.Errorf("本次有效P0无效，本次不发送模型请求: %w", err)
	}
	return nil
}

// generationTaskView 委托共享包 speechbase 渲染任务视图（三类话术共用）。
func generationTaskView(task anchorcycle.Task) map[string]any {
	return speechbase.GenerationTaskView(task)
}

// generationP0View 委托共享包 speechbase 渲染有效 P0（三类话术共用）。
func generationP0View(md, scope string) any {
	return speechbase.GenerationP0View(md, scope)
}

// generationFactView 委托共享包 speechbase 渲染事实（三类话术共用）。
func generationFactView(facts []anchorcycle.HookFact) []map[string]any {
	return speechbase.GenerationFactView(facts)
}

// generationMaterialViews 委托共享包 speechbase 渲染事实与表达约束（三类话术共用）。
func generationMaterialViews(facts []anchorcycle.HookFact) ([]map[string]any, []map[string]any) {
	return speechbase.GenerationMaterialViews(facts)
}
