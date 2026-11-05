package httpapi

import "sort"

// The editor advertises only slots consumed by the current six-template paths.
// Nested legacy loop fields and slots from other prompt assets are not standalone slots.
type planPromptPlaceholder struct {
	Key         string   `json:"key"`
	Label       string   `json:"label"`
	InsertText  string   `json:"insert_text"`
	Description string   `json:"description"`
	Group       string   `json:"group"`
	Templates   []string `json:"templates"`
}

func planPromptPlaceholderCatalog() []planPromptPlaceholder {
	standard := []string{"persona_md", "facts_md", "red_lines_md", "speech_requirements_md", "interaction_requirements_md"}
	items := []planPromptPlaceholder{{Key: "直播间名", Label: "直播间名", InsertText: "{{直播间名}}", Description: "拉取时填入当前方案名称。", Group: "方案资料", Templates: standard}}
	keys := make([]string, 0, len(planPromptDefaults))
	for key := range planPromptDefaults {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		items = append(items, planPromptPlaceholder{Key: key, Label: key, InsertText: "{{" + key + "}}", Description: "拉取时使用系统默认值：" + planPromptDefaults[key], Group: "系统默认表达", Templates: standard})
	}
	items = append(items,
		planPromptPlaceholder{Key: "商品", Label: "商品卡", InsertText: "{{#商品}}\n{{/商品}}", Description: "拉取时整块填入当前方案的有效商品卡，无数据时留空。", Group: "事实数据块", Templates: []string{"facts_md"}},
		planPromptPlaceholder{Key: "福利", Label: "活动福利", InsertText: "{{#福利}}\n{{/福利}}", Description: "拉取时整块填入当前方案的有效活动福利，无数据时留空。", Group: "事实数据块", Templates: []string{"facts_md"}},
	)
	for _, entry := range []struct{ key, description string }{
		{"样本话术", "执行改写时填入本条待改写原文；系统也会自动附加样本。"},
		{"已有开场", "后台改写时填入这个片段已经使用的开场，首次可能为空。"},
		{"开场方式", "后台改写时填入本轮选择的开场方式，手动改写时可能为空。"},
		{"近期写作记录", "改写时填入上次骨架和写作上下文；没有记录时明确说明暂无记录。"},
	} {
		items = append(items, planPromptPlaceholder{Key: entry.key, Label: entry.key, InsertText: "{{" + entry.key + "}}", Description: entry.description, Group: "改写运行数据", Templates: []string{"rewrite_planning_md"}})
	}
	return items
}
