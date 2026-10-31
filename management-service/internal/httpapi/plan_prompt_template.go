package httpapi

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"livecompanion/management/internal/interactionpolicy"
	"livecompanion/management/internal/model"
)

// planPromptDefaults 是母版里那些“没有数据也能成立”的占位符兜底值。
// 有品类包的方案可以覆盖它们，这里只保证渲染结果不会留下 {{…}}。
var planPromptDefaults = map[string]string{
	"品类要点":   "使用方法",
	"品类禁忌":   "不讲功效，不说养生、不上火、滋补、零添加、纯天然这类没有依据的话。",
	"行业表达限制": "只讲体验和使用感受，不做健康或功效承诺",
	"没问不提清单": "门店、总店、包邮、吃法、保存时间",
	"禁止罗列项":  "配料表、门店数量、工艺步骤",
	"称呼":     "“朋友们”“老乡们”“各位”",
	"互动问句示例": "有没有朋友平时就爱吃这口的？有没有跟我一样的？",
	"默认置信度":  "85",
	"口号":     "想看好物，咱们直播见。",
	"核心标签":   "靠谱、实在、不套路、讲真话。",
	"核心价值":   "帮粉丝挑性价比好货，讲清楚产品优缺点，不夸大功效；优先站在买家角度讲痛点，而不是硬推。",
	"底色":     "真诚稳重，带一点生活化松弛感，热情但不聒噪。",
	"说话风格":   "大白话为主，短句多，少书面词；偶尔口语助词（哎、对吧、你看哈），密度适中，不堆砌。",
	"禁止项":    "大喊大叫、夸张咆哮、刻意煽情卖惨、低俗玩梗。",
	"视觉形象":   "亲和耐看，表情自然，语速舒服，像本地实体店的主理人，不浮夸、不嘶吼。",
	"主播名":    "小林",
	"赛道":     "本地生活",
}

var planFactCategoryLabels = map[string]string{
	"ingredients": "原料", "craft": "工艺", "storage": "保存", "flavor": "口味口感",
	"usage": "吃法", "scenario": "适用场景", "shelf_life": "保质期", "shipping_time": "发货时效",
	"delivery": "快递", "promotion": "活动", "price": "价格", "other": "其它",
	"audience": "适用人群", "business_history": "品牌历史", "origin": "产地",
	"functions": "营养", "brand": "品牌", "return_policy": "售后", "pickup": "自提",
	"product": "商品", "shipping_fee": "运费",
}

type planPromptRenderData struct {
	PlanName string
	Links    []model.LiveAgentPlanProductLink
	Benefits []model.LiveAgentPlanBenefit
	Facts    []model.LiveAgentPlanFact
}

// liveAgentPlanPromptRender 按母版渲染一份方案提示词初稿。它只读不写：
// 用户看到结果后自己编辑，保存仍走原来的 PUT。
func (s *Server) liveAgentPlanPromptRender(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	planID, ok := liveAgentPlanPathID(w, r)
	if !ok {
		return
	}
	tenantID, ok := s.resolveLiveAgentPlanTenant(w, r, actor, 0, true)
	if !ok {
		return
	}
	ctx := r.Context()
	plan, err := s.store.GetLiveAgentPlan(ctx, tenantID, planID)
	if err != nil {
		writeError(w, http.StatusNotFound, "方案不存在")
		return
	}
	links, err := s.store.ListLiveAgentPlanProductLinks(ctx, tenantID, planID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取商品卡失败")
		return
	}
	benefits, err := s.store.ListLiveAgentPlanBenefits(ctx, tenantID, planID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取活动福利失败")
		return
	}
	facts, err := s.store.ListLiveAgentPlanFacts(ctx, tenantID, planID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取事实依据失败")
		return
	}

	master, err := interactionpolicy.Read("plan-prompt-master.md")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取提示词母版失败")
		return
	}
	data := planPromptRenderData{PlanName: plan.Name, Links: links, Benefits: benefits, Facts: facts}
	text := renderPlanPrompt(string(master), data)
	writeJSON(w, http.StatusOK, map[string]any{"prompt_md": text, "facts_md": renderPlanPromptFacts(data), "master_sha256": fmt.Sprintf("%x", sha256.Sum256(master))})
}

func renderPlanPromptFacts(data planPromptRenderData) string {
	var blocks []string
	for _, block := range []struct{ title, text string }{
		{"商品卡", planCardBlock(data.Links)},
		{"活动福利", planBenefitBlock(data.Benefits)},
		{"已确认事实", planFactBlock(data.Facts)},
	} {
		if text := strings.TrimSpace(block.text); text != "" {
			blocks = append(blocks, "【"+block.title+"】\n"+text)
		}
	}
	return strings.Join(blocks, "\n\n")
}

func renderPlanPrompt(master string, data planPromptRenderData) string {
	master = replaceTemplateBlock(master, "商品", planCardBlock(data.Links))
	master = replaceTemplateBlock(master, "福利", planBenefitBlock(data.Benefits))
	// 事实依据和用词消歧由用户自己在提示词里手填，这里不做任何填充。
	master = replaceTemplateBlock(master, "额外禁忌", "")

	values := make(map[string]string, len(planPromptDefaults)+2)
	for k, v := range planPromptDefaults {
		values[k] = v
	}
	if strings.TrimSpace(data.PlanName) != "" {
		values["直播间名"] = strings.TrimSpace(data.PlanName)
	}
	for key, value := range values {
		master = strings.ReplaceAll(master, "{{"+key+"}}", value)
	}

	lines := make([]string, 0, 256)
	for _, line := range strings.Split(master, "\n") {
		if strings.Contains(line, "{{") || strings.Contains(line, "}}") {
			continue // 没有数据来源的占位符整行丢弃，绝不留进提示词
		}
		lines = append(lines, strings.TrimRight(line, " \t"))
	}
	lines = dropEmptySections(lines)

	out := make([]string, 0, len(lines))
	blank := false
	for _, line := range lines {
		isBlank := strings.TrimSpace(line) == ""
		if isBlank && blank {
			continue
		}
		out = append(out, line)
		blank = isBlank
	}
	text := strings.TrimSpace(strings.Join(out, "\n"))
	return strings.ReplaceAll(text, "**", "") + "\n"
}

func planCardBlock(links []model.LiveAgentPlanProductLink) string {
	blocks := make([]string, 0, len(links))
	for _, link := range links {
		if !strings.EqualFold(link.Status, "active") {
			continue
		}
		rows := []struct{ label, value string }{
			{"商品名", link.ProductName},
			{"规格", link.Spec},
			{"价格", link.DailyPrice},
			{"数量", link.Quantity},
			{"适用", link.Audience},
		}
		lines := []string{link.LinkKey + "号链接："}
		for _, row := range rows {
			value := strings.TrimSpace(row.value)
			if value == "" || value == "0" || (row.label == "数量" && value == "1") {
				continue
			}
			lines = append(lines, row.label+"："+value)
		}
		blocks = append(blocks, strings.Join(lines, "\n")+"\n")
	}
	return strings.Join(blocks, "\n")
}

func planBenefitBlock(benefits []model.LiveAgentPlanBenefit) string {
	lines := make([]string, 0, len(benefits))
	for _, benefit := range benefits {
		if !strings.EqualFold(benefit.Status, "active") {
			continue
		}
		parts := make([]string, 0, 3)
		for _, raw := range []string{benefit.Activity, benefit.ActivityPrice, benefit.Gift} {
			value := strings.TrimSpace(raw)
			if value == "" || planContainsString(parts, value) {
				continue
			}
			parts = append(parts, value)
		}
		if len(parts) == 0 {
			continue
		}
		lines = append(lines, strings.Join(parts, "，"))
	}
	if len(lines) == 0 {
		return ""
	}
	numbered := make([]string, 0, len(lines))
	for i, line := range lines {
		numbered = append(numbered, strconv.Itoa(i+1)+". "+line)
	}
	return strings.Join(numbered, "\n")
}

func planFactBlock(facts []model.LiveAgentPlanFact) string {
	seen := map[string]bool{}
	type row struct {
		order int
		text  string
	}
	rows := make([]row, 0, len(facts))
	for _, fact := range facts {
		if fact.Status != "" && !strings.EqualFold(fact.Status, "active") {
			continue
		}
		value := strings.TrimSpace(fact.Value)
		identity := value + "\x00" + fact.ForbiddenWording + "\x00" + fact.SafeRewrite
		if value == "" || seen[identity] {
			continue
		}
		seen[identity] = true
		label := planFactCategoryLabels[strings.TrimSpace(fact.Category)]
		if label == "" {
			label = "其它"
		}
		text := label + "：" + value
		if wording := strings.TrimSpace(fact.ForbiddenWording); wording != "" {
			text += "\n表达限制：" + wording
		}
		if wording := strings.TrimSpace(fact.SafeRewrite); wording != "" {
			text += "\n参考表达：" + wording
		}
		rows = append(rows, row{order: categoryOrder(label), text: text})
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].order < rows[j].order })
	texts := make([]string, 0, len(rows))
	for _, item := range rows {
		texts = append(texts, item.text)
	}
	return strings.Join(texts, "\n")
}

func categoryOrder(label string) int {
	order := []string{"品牌", "品牌历史", "门店与历史综合", "原料", "产地", "工艺", "口味口感", "营养",
		"吃法", "适用场景", "适用人群", "保存", "保质期", "发货时效", "快递", "运费", "自提",
		"价格", "活动", "售后", "商品", "其它"}
	for i, name := range order {
		if name == label {
			return i
		}
	}
	return len(order)
}

// replaceTemplateBlock 把 {{#key}}…{{/key}} 整段替换成 body；body 为空时
// 连段一起删掉，避免留下空标题让模型自己补内容。
func replaceTemplateBlock(text, key, body string) string {
	open := "{{#" + key + "}}"
	closeTag := "{{/" + key + "}}"
	for {
		start := strings.Index(text, open)
		if start < 0 {
			return text
		}
		end := strings.Index(text[start:], closeTag)
		if end < 0 {
			return text
		}
		end += start + len(closeTag)
		text = text[:start] + body + text[end:]
	}
}

func dropEmptySections(lines []string) []string {
	headings := map[string]bool{"【商品卡】": true, "【活动福利】": true}
	out := make([]string, 0, len(lines))
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		if headings[strings.TrimSpace(line)] {
			j := i + 1
			for j < len(lines) && strings.TrimSpace(lines[j]) == "" {
				j++
			}
			if j >= len(lines) || strings.HasPrefix(strings.TrimSpace(lines[j]), "【") {
				i = j - 1
				continue
			}
		}
		out = append(out, line)
	}
	return out
}

func planContainsString(list []string, target string) bool {
	for _, item := range list {
		if item == target {
			return true
		}
	}
	return false
}

// 供测试与调用方复用，避免后再各自实现一遍。
func renderPlanPromptForTest(master string, data planPromptRenderData) string {
	return renderPlanPrompt(master, data)
}
