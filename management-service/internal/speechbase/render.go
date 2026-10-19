// Package speechbase 是三类话术（开场/主线/互动）共享的「外围底座」渲染层。
//
// 这里只放纯函数：输入明确的数据（主播打分基线、P0 母版、事实），输出渲染结果，
// 不依赖任何具体的状态机或业务对象。主线/开场由 anchorcycle.State 提供数据，
// 互动由适配器提供数据，双方都调用这里的同一份实现，避免三套渲染各写各的。
package speechbase

import (
	"fmt"
	"math/rand/v2"
	"strings"

	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/anchorstylescore"
	"livecompanion/management/internal/contentguard"
)

// FourLayerNotMentionText 是四层结构「不提」层的统一文案：事实未提供或未确认的
// 未知项正反都不说。主线/开场/互动共用同一句，避免三处同义措辞漂移。
const FourLayerNotMentionText = "事实未提供、未确认的未知项，正反都不说：既不说“能/有/支持”，也不说“不能/没有/不支持”；必要时只说“这点需要帮您确认”。"

// P0Rule 是「生成红线」精简 JSON 里的一条：一条红线对应一份裸词表（不提）和一句
// 专属改说（改写）。这样一个大红线 ↔ 一个改说一一对应，以后要加新红线就往 JSON
// 里补一条，不动代码。程序启动时加载进内存，提示词工厂组装时把「不提」词表合并进
// 「不提」层、把「改说」合并进「改写」层。
type P0Rule struct {
	Redline   string   `json:"红线"`
	Forbidden []string `json:"不提"`
	Rewrite   string   `json:"改说"`
}

// RenderP0ForbiddenWordList 把 N 条红线的「不提」词表合并成一句裸词清单，供四层
// 结构的「不提」层使用。格式：红线名（词1、词2、…）；红线名（…）。
func RenderP0ForbiddenWordList(rules []P0Rule) string {
	parts := make([]string, 0, len(rules))
	for _, r := range rules {
		if len(r.Forbidden) == 0 {
			continue
		}
		parts = append(parts, r.Redline+"（"+strings.Join(r.Forbidden, "、")+"）")
	}
	return strings.Join(parts, "；")
}

// RenderP0RewriteGuidance 把 N 条红线的「改说」合并成带序号的映射清单，供四层结构
// 的「改写」层使用。格式：1. 红线名 → 改说；2. …
func RenderP0RewriteGuidance(rules []P0Rule) string {
	lines := make([]string, 0, len(rules))
	for i, r := range rules {
		if strings.TrimSpace(r.Rewrite) == "" {
			continue
		}
		lines = append(lines, fmt.Sprintf("%d. %s → %s", i+1, r.Redline, r.Rewrite))
	}
	return strings.Join(lines, "\n")
}

// RenderAnchorExpressionView 从主播打分基线 JSON 渲染「主播表达标尺」。
// 主线/开场/互动三类话术共用同一份渲染，避免主播腔调（称呼/语气词/自指/数字习惯）三套各写各的。
func RenderAnchorExpressionView(baselineJSON string) (any, error) {
	b, err := anchorstylescore.Parse(baselineJSON)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"语气词": b.Identity.Particles, "称呼": b.Identity.Audience, "自指": b.Identity.Self,
		"节奏":     map[string]any{"平均分句长度": b.Identity.Rhythm.Length, "长度区间": b.Identity.Rhythm.LengthRange, "问句频率": b.Identity.Rhythm.QuestionFrequency},
		"数字表达习惯": map[string]any{"价格表述方式": b.Identity.Numbers.Style, "自然帮观众算账": b.Identity.Numbers.LikesArithmetic},
		"量化基准":   b.Quant, "表达偏好": b.Forbidden,
	}, nil
}

// RenderAnchorVoiceCard 把「主播风格母版（StyleMD，定性腔调，带例句的"味"）」与
// 「打分基线里的量化基准（Quant，程序统计的硬数据）」合并成一张「主播腔调卡」。
//
// 背景：StyleMD 与 Baseline.Identity（称呼/语气词/自指/方言等结构化词表）讲的是
// 同一批腔调，二者高度重叠。之前渲染时把「主播风格母版」和「主播表达标尺」两份
// 并排塞进 system，重复且费 token。合并后只产出一份：定性描述保留 StyleMD 原文
// （腔调的"味"和例句不能丢，丢了会退回「风格没味」），量化基准保留 Quant（打分器
// 与生成频率控制的硬依据），其余结构化词表不再重复输出。
//
// 风格字段经 pureStyle 净化：剥掉 StyleMD 里混入的「逼单导向词」和「商品专属词」。
// 实测发现 StyleMD 原文里带「互动压迫感 / 紧迫氛围 / 推进节奏确认订单 / 买得早发得早」
// 这类催单意图，以及「清油味 / 四川重庆物流时效」这类具体商品信息，直接喂给生成模型
// 会「有腔调地逼单 + 编商品卖点」。净化只删这些污染句/词，保留语气词、称呼、自指、
// 句式节奏、口头禅等纯说话腔调（腔调的"味"不能丢）。
//
// 仅主线话术的「主播腔调」字段使用本函数；打分/审核等只吃量化基准的路径仍走
// RenderAnchorExpressionView，不受影响。
func RenderAnchorVoiceCard(styleMD, baselineJSON string) (any, error) {
	b, err := anchorstylescore.Parse(baselineJSON)
	if err != nil {
		return nil, err
	}
	style := pureStyle(styleMD)
	if strings.TrimSpace(style) == "" {
		style = "（未提供主播风格描述）"
	}
	return map[string]any{
		"风格":   style,
		"量化基准": b.Quant,
		"禁用表达": b.Forbidden,
	}, nil
}

// pureStyle 把 StyleMD 剥成「纯腔调」：去掉逼单导向和商品专属信息，只留说话习惯。
// 策略保守——只删明确命中的污染短语和含污染词的句子，不重写正文，避免误删腔调。
func pureStyle(styleMD string) string {
	// 逼单/紧迫导向短语：StyleMD 里把这些写进「节奏/句式」，会让模型「有腔调地催单」。
	// 逐个删除这些短语（含其所在分句），保留句子的其余部分。
	pushPhrases := []string{
		"互动压迫感", "压迫感", "即时反馈期待", "紧迫", "紧迫且热烈", "推进节奏与确认订单",
		"推进节奏", "确认订单", "买得早发得早", "营造出紧迫", "催促", "催单",
	}
	s := styleMD
	for _, p := range pushPhrases {
		s = strings.ReplaceAll(s, p, "")
	}
	// 商品专属词：具体商品的卖点/物流信息，不属于通用主播腔调，换商品就失效。
	productPhrases := []string{
		"清油味", "四川重庆老乡", "家乡味", "物流时效", "物流",
	}
	for _, p := range productPhrases {
		s = strings.ReplaceAll(s, p, "")
	}
	// 清理替换后留下的空括号、多余空格、空行。
	s = strings.ReplaceAll(s, "（）", "")
	s = strings.ReplaceAll(s, "()", "")
	s = strings.ReplaceAll(s, "，。", "。")
	s = strings.ReplaceAll(s, "。。", "。")
	s = strings.ReplaceAll(s, "  ", " ")
	// 压缩连续空行，去掉首尾空白。
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			out = append(out, l)
		}
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

// Program provenance remains archived in Task.FactScope. The model receives
// only the content job and human-facing product labels, never its relation keys.
func GenerationTaskView(task anchorcycle.Task) map[string]any {
	return map[string]any{"卖点": task.SellingPoint, "角度": task.Angle, "观众顾虑": task.PainPoint, "参考人群": task.TargetAudience}
}

// Execution/logging documentation and rule identifiers belong to the program.
// Generation gets all applicable rule content, including contextual exceptions.
// Unrecognized user-authored policy text is preserved, never silently discarded.
func GenerationP0View(md, scope string) any {
	if strings.TrimSpace(md) == "" {
		return ""
	}
	p, err := contentguard.Compile(md)
	if err != nil {
		return md
	}
	if scope == "" {
		scope = p.Data.Execution.DefaultScope
	}
	// 精简输出：只保留生成模型真正需要的字段。
	// 适用范围/适用条件（all/always 等）与匹配表达（正则）是程序扫描用的元信息，
	// 排除表述多为空，模型读它们没有意义，反而撑大每条请求的输入 token。
	// 相关表述词表必须保留——它是模型主动回避极限词的依据，砍掉会拉高退回率。
	rules := []map[string]any{}
	for _, r := range p.Data.Rules {
		if scope != "*" && r.Scope != "all" && r.Scope != scope {
			continue
		}
		entry := map[string]any{"类别": r.Category, "处理方式": r.ReviewMode, "相关表述": r.Terms, "规则说明": r.Note}
		if len(r.RequiredBasis) > 0 {
			entry["所需依据"] = r.RequiredBasis
		}
		rules = append(rules, entry)
	}
	// 说明：给生成模型看的规则总则。关键是把「处理方式」讲成生成动作指令，
	// 而非「可以语境豁免」——否则模型会把 semantic 当成放行口令，反复产出极限词，
	// 再被程序扫描退回，形成生成↔审核死循环。
	return map[string]any{
		"适用规则": rules,
		"说明":   "以下是本次口播禁止出现的表述清单，生成正文时从一开始就避开，不要先写出来再改。处理方式为 hard 的：任何语境、任何句子都不得出现。处理方式为 semantic 的：默认同样不得出现，只有当本次「可说」事实中明确写有该表述的原话、数字或依据时才可原样引用，否则改用可核实、不绝对的说法；不要自行判断“这个语境下不算极限词”而照用。背书数据（销量、榜单、回购、年限、门店数）只有在「可说」事实里明确写了才能报，没有就别编；纯称呼（新粉、老粉、家人们）不带具体数字可正常使用。",
	}
}

// The existing fact renderer encodes operator guidance with reserved delimiters.
// Keep its exact fact text separate from guidance and its example numbers.
func GenerationFactView(facts []anchorcycle.HookFact) []map[string]any {
	result := []map[string]any{}
	for _, f := range facts {
		data := f.Data
		cut := len(data)
		guides := append([]map[string]string{}, f.ExpressionGuidance...)
		for _, marker := range []string{"；禁说=", "；安全改写="} {
			at := strings.Index(data, marker)
			if at < 0 {
				continue
			}
			if at < cut {
				cut = at
			}
			text := data[at+len(marker):]
			end := len(text)
			for _, next := range []string{"；禁说=", "；安全改写="} {
				if i := strings.Index(text, next); i >= 0 && i < end {
					end = i
				}
			}
			guides = append(guides, map[string]string{"要求": strings.Trim(marker, "；="), "内容": text[:end]})
		}
		record := map[string]any{"事实内容": data[:cut], "适用商品链接": f.Products}
		if len(guides) > 0 {
			record["表达指导（不作为商品事实）"] = guides
		}
		result = append(result, record)
	}
	return result
}

// Facts and writing controls occupy separate model-input blocks. This also
// separates guidance from legacy delimiter-rendered facts without editing them.
func GenerationMaterialViews(facts []anchorcycle.HookFact) ([]map[string]any, []map[string]any) {
	content := GenerationFactView(facts)
	guidance := []map[string]any{}
	for _, record := range content {
		if rules, exists := record["表达指导（不作为商品事实）"]; exists {
			if typed, ok := rules.([]map[string]string); ok {
				copy := append([]map[string]string{}, typed...)
				positions := []int{}
				for i, g := range copy {
					if strings.Contains(g["要求"], "正向举例") {
						positions = append(positions, i)
					}
				}
				rand.Shuffle(len(positions), func(i, j int) { a, b := positions[i], positions[j]; copy[a], copy[b] = copy[b], copy[a] })
				rules = copy
			}
			guidance = append(guidance, map[string]any{"对应事实": record["事实内容"], "适用商品链接": record["适用商品链接"], "负向约束及正向举例（不作为新增事实，不直接照念指导）": rules})
			delete(record, "表达指导（不作为商品事实）")
		}
	}
	return content, guidance
}

// RenderPUserForbiddenList 把「用户已确认的长期纠正规则」里的「表达限制」抽出来，
// 合并成四层「不提」层用的一段清单。格式与 P0 的裸词表不同：纠正规则是完整语义
// 禁令（如「不要拿早餐举例」），按原句保留，不做抽词，避免丢失用户限定的范围。
// 空时返回空串，调用方自行决定是否拼接。
func RenderPUserForbiddenList(corrections []map[string]any) string {
	parts := make([]string, 0, len(corrections))
	for _, row := range corrections {
		for _, key := range []string{"表达限制", "Forbidden"} {
			if text, ok := row[key].(string); ok && strings.TrimSpace(text) != "" {
				parts = append(parts, strings.TrimSpace(text))
				break
			}
		}
	}
	return strings.Join(parts, "\n")
}

// RenderPUserRewriteGuidance 把「用户已确认的长期纠正规则」里的「展开指导」抽出来，
// 合并成四层「改写」层用的一段安全替换说法。展开指导是用户明确给出的正向表达安排
// （如「说到分量时改说一桌人刚好吃饱」），按原句保留，不自行扩写。
// 空时返回空串。
func RenderPUserRewriteGuidance(corrections []map[string]any) string {
	parts := make([]string, 0, len(corrections))
	for _, row := range corrections {
		for _, key := range []string{"展开指导", "Guidance"} {
			if text, ok := row[key].(string); ok && strings.TrimSpace(text) != "" {
				parts = append(parts, strings.TrimSpace(text))
				break
			}
		}
	}
	return strings.Join(parts, "\n")
}
