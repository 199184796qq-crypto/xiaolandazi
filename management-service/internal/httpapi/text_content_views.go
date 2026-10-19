package httpapi

import (
	"encoding/json"
	"fmt"
	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/contentguard"
	"livecompanion/management/internal/speechbase"
	"strings"
)

// generationP0Rules 缓存「生成侧」P0 精简 JSON 解析后的规则列表，进程内只加载一次。
var generationP0Rules []speechbase.P0Rule

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

// loadGenerationP0 读取「生成侧」P0 精简 JSON（红线 + 裸词表 + 专属改说，一一对应），
// 解析成 []speechbase.P0Rule 并缓存在内存（程序启动后只加载一次，之后每次组装直接
// 复用，不再现读文件）。供主线/开场/互动生成时，由提示词工厂把「不提」词表合并进
// 四层「不提」、把「改说」合并进四层「改写」。审核侧继续用 p0-hard-block.md 的词表
// 做程序扫描，两边解耦。
func loadGenerationP0() ([]speechbase.P0Rule, error) {
	if generationP0Rules != nil {
		return generationP0Rules, nil
	}
	raw, _, err := loadReplaceablePromptAsset("ANCHOR_STYLE_P0_GENERATION_PATH", "configs/prompts/anchor-style/compliance/L1/p0-generation.json", "P0生成侧JSON")
	if err != nil {
		return nil, err
	}
	var doc struct {
		Rules []speechbase.P0Rule `json:"rules"`
	}
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		return nil, fmt.Errorf("解析P0生成侧JSON失败: %w", err)
	}
	if len(doc.Rules) == 0 {
		return nil, fmt.Errorf("P0生成侧JSON为空或缺少rules")
	}
	// 非空校验：每条红线必须同时带「红线」名和至少一个「不提」词，否则视为
	// 解析失败（导出字段被忽略/文件残缺），不能当成空规则缓存后整场漏拦截。
	for i, r := range doc.Rules {
		if strings.TrimSpace(r.Redline) == "" || len(r.Forbidden) == 0 {
			return nil, fmt.Errorf("P0生成侧JSON第%d条缺少红线名或不提词表", i+1)
		}
	}
	generationP0Rules = doc.Rules
	return generationP0Rules, nil
}

// generationFactView 委托共享包 speechbase 渲染事实（三类话术共用）。
func generationFactView(facts []anchorcycle.HookFact) []map[string]any {
	return speechbase.GenerationFactView(facts)
}

// generationMaterialViews 委托共享包 speechbase 渲染事实与表达约束（三类话术共用）。
func generationMaterialViews(facts []anchorcycle.HookFact) ([]map[string]any, []map[string]any) {
	return speechbase.GenerationMaterialViews(facts)
}
