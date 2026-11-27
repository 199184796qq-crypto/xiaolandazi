package httpapi

import (
	"context"
	"strings"
	"time"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/planprompt"
)

// planPromptDraftTimeout 是单页生成的上限。五页串行，整次拉取控制在两分钟内。
const planPromptDraftTimeout = 45 * time.Second

// draftPlanPromptSections 把六份母版当生成器用：每一页的母版填好方案数据后
// 单独交给模型，模型返回这一页的提示词正文。
//
// 规则：
//   - 一页一次调用，互不影响；
//   - 模型输出经审核，不合格或调用失败时保留母版渲染结果；
//   - 事实依据永远不交给模型，由程序渲染。
func (s *Server) draftPlanPromptSections(
	ctx context.Context,
	plan model.LiveAgentPlan,
	profile db.PlanBusinessProfile,
	links []model.LiveAgentPlanProductLink,
	benefits []model.LiveAgentPlanBenefit,
	facts []model.LiveAgentPlanFact,
	rendered map[string]string,
	masters map[string]string,
	only map[string]bool,
) (sections map[string]string, warnings map[string][]string, sources map[string]string) {
	sections = map[string]string{}
	for k, v := range rendered {
		sections[k] = v
	}
	warnings = map[string][]string{}
	sources = map[string]string{}
	for k := range rendered {
		sources[k] = "template"
	}

	values := planPromptWriterValues(plan, profile, links, benefits, facts, sections)
	for _, page := range planprompt.WriterPages {
		if len(only) > 0 && !only[page.Section] {
			continue
		}
		master := strings.TrimSpace(masters[page.Section])
		if master == "" {
			continue
		}
		system := planprompt.FillWriter(master, values) + "\n\n" + planprompt.WriterOutputRule(page.Title)
		text, err := s.completePlanPromptDraft(ctx, page, system)
		if err != nil {
			warnings[page.Section] = append(warnings[page.Section], "模型调用失败，已保留母版内容")
			continue
		}
		text = planprompt.NormalizeWriterOutput(text)
		if strings.TrimSpace(text) == "" {
			warnings[page.Section] = append(warnings[page.Section], "模型返回空内容，已保留母版内容")
			continue
		}
		cleaned, cleanNotes, fatal := planprompt.CleanWriterOutput(page.Key, text)
		if len(cleanNotes) > 0 {
			warnings[page.Section] = append(warnings[page.Section], cleanNotes...)
		}
		if fatal {
			continue
		}
		text = cleaned
		fatal, notes := planprompt.AuditWriterOutput(page.Key, text)
		if fatal {
			warnings[page.Section] = append(warnings[page.Section], notes...)
			continue
		}
		sections[page.Section] = text
		// 渲染接口对外既有 persona/redlines/speech/interaction 短键，
		// 也有 *_md 长键，两套都要写，避免一端取不到。
		if short := planPromptShortSectionKey(page.Key); short != "" {
			sections[short] = text
			sources[short] = "model"
		}
		sources[page.Section] = "model"
		if len(notes) > 0 {
			warnings[page.Section] = append(warnings[page.Section], notes...)
		}
	}
	return sections, warnings, sources
}

func (s *Server) completePlanPromptDraft(ctx context.Context, page planprompt.WriterPage, system string) (string, error) {
	callCtx, cancel := context.WithTimeout(ctx, planPromptDraftTimeout)
	defer cancel()
	resp, err := s.completePaidScriptModel(callCtx, agentgateway.Request{
		Stage:          "plan_prompt_draft_" + page.Key,
		ResponseFormat: agentgateway.ResponseText,
		MaxTokens:      900,
		Timeout:        planPromptDraftTimeout,
		Messages: []agentgateway.Message{
			{Role: "system", Content: system},
		},
	})
	if err != nil {
		return "", err
	}
	return resp.Text, nil
}

// planPromptShortSectionKey 把页 key 映射到渲染接口的短键。
func planPromptShortSectionKey(page string) string {
	switch page {
	case "persona":
		return "persona"
	case "red_lines":
		return "redlines"
	case "speech":
		return "speech"
	case "interaction":
		return "interaction"
	}
	return ""
}

// planPromptWriterValues 是母版占位符的填充数据，全部来自方案本身或平台默认。
func planPromptWriterValues(
	plan model.LiveAgentPlan,
	profile db.PlanBusinessProfile,
	links []model.LiveAgentPlanProductLink,
	benefits []model.LiveAgentPlanBenefit,
	facts []model.LiveAgentPlanFact,
	sections map[string]string,
) map[string]string {
	anchor := strings.TrimSpace(planPromptDefaults["主播名"])
	if anchor == "" {
		anchor = "主播"
	}
	return map[string]string{
		"方案名称":   plan.Name,
		"主播名":    anchor,
		"主播身份":   "店主本人，店里的自己人",
		"直播赛道":   planPromptTrack(profile),
		"业务模式":   planPromptBusinessMode(profile.BizDomain),
		"商品卡":    planCardBlock(links),
		"活动福利":   planBenefitBlock(benefits),
		"有效事实":   planFactBlock(facts),
		"本行业事实类": planprompt.IndustryFactFields(profile.BizDomain, profile.EcommerceIndustry),
		"现有提示词":  sections["persona_md"],
	}
}

func planPromptBusinessMode(domain string) string {
	switch strings.ToLower(strings.TrimSpace(domain)) {
	case "ecommerce":
		return "电商带货"
	case "local_service":
		return "本地生活"
	default:
		return "直播带货"
	}
}

// planPromptTrack 把行业代码翻译成一句赛道描述。认不出时退回业务模式。
func planPromptTrack(profile db.PlanBusinessProfile) string {
	labels := map[string]string{
		"food": "食品-卤味熟食-地方特产", "apparel": "服饰鞋包", "beauty": "美妆个护",
		"digital": "数码家电", "baby": "母婴亲子", "home": "家居日用",
	}
	for _, key := range []string{profile.EcommerceIndustry, profile.LocalServiceIndustry} {
		if v, ok := labels[strings.ToLower(strings.TrimSpace(key))]; ok {
			return v
		}
	}
	return planPromptBusinessMode(profile.BizDomain)
}