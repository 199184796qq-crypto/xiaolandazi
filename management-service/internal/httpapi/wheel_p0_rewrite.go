package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/contentguard"
)

// 轮盘角度命中 P0 红线后的局部改写。
//
// 只替换违规的那一处，不做重写。这里刻意**不注入事实包**：
// 实测给事实时，模型会直接抄事实当角度（"最低价的6人餐"被改成"1号链接团购价337元"，
// 主题和商品全跑偏）；不给事实，它只能老实改那个词，10/10 保住了主题与商品链接。

const wheelP0RewriteSystem = `# 必说
下面这条直播讲解角度里有个词不能说。只把那个词换掉，其余字原样保留：
不改句子结构、不换主题、不加新信息、不换商品链接。换完要读得通顺，必要时调一两个字。
给 2 个改法，只输出 JSON：{"备选":["改法1","改法2"]}。
每条不超过 {{LIMIT}} 个字。

# 不提（这些词一个都不能出现在改法里）
{{NOT_SAY}}

# 改写（照着换）
- 最好 / 最佳 / 最优 → 很好 / 很不错 / 挺好
- 最低价 / 最便宜 / 最优惠 → 价格更低 / 更便宜 / 更划算
- 最高 / 最大 / 最多 → 很高 / 很大 / 很多
- 顶级 / 极致 / 顶尖 → 很好 / 不错 / 到位
- 第一 / 唯一 / 独家 → 少有的 / 比较特别
- 100% / 绝对 / 零风险 → 基本 / 通常 / 几乎
- 保证 / 确保 → 可以做到 / 一般都能
- 拿两个链接比 → 只讲本条角度自己的那个商品`

// renderWheelRewriteMessages 组装一次局部改写的消息（system + user）。
// hitTerms 明确告诉模型要改哪几个词；limit 是改写后的字数上限。
func renderWheelRewriteMessages(sellingPoint, angle string, hitTerms []string, notSay string, limit int) []agentgateway.Message {
	if limit <= 0 {
		limit = len([]rune(angle)) + 6
	}
	prompt := strings.Replace(wheelP0RewriteSystem, "{{NOT_SAY}}", strings.TrimSpace(notSay), 1)
	prompt = strings.Replace(prompt, "{{LIMIT}}", strconv.Itoa(limit), 1)
	user, _ := json.Marshal(map[string]any{"卖点": sellingPoint, "原角度": angle, "违规词": hitTerms})
	return []agentgateway.Message{{Role: "system", Content: prompt}, {Role: "user", Content: string(user)}}
}

// wheelRewriteCandidates 从模型输出里抠出备选角度。
func wheelRewriteCandidates(raw string) []string {
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start < 0 || end <= start {
		return nil
	}
	var doc struct {
		Candidates []string `json:"备选"`
	}
	if json.Unmarshal([]byte(raw[start:end+1]), &doc) != nil {
		return nil
	}
	out := make([]string, 0, len(doc.Candidates))
	for _, item := range doc.Candidates {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}

// pickWheelRewriteCandidate 选第一个仍不含红线词、且没有换商品的备选。
func pickWheelRewriteCandidate(candidates []string, original string, policy *contentguard.Policy) string {
	link := linkMention(original)
	for _, item := range candidates {
		if len(p0PolicyHits(item, policy)) != 0 {
			continue
		}
		// 原角度点名了某个链接，改写不许换成别的链接（实测会把 6人餐改成 1号链接）。
		if link != "" && linkMention(item) != "" && linkMention(item) != link {
			continue
		}
		return item
	}
	return ""
}

// linkMention 取出文本里提到的链接标记（如 "2号链接"），没提到返回空串。
func linkMention(text string) string {
	const suffix = "号链接"
	if idx := strings.Index(text, suffix); idx > 0 {
		start := idx
		for start > 0 {
			r := rune(text[start-1])
			if r < '0' || r > '9' {
				break
			}
			start--
		}
		if start < idx {
			return text[start : idx+len(suffix)]
		}
	}
	return ""
}

// p0PolicyHits 返回文本命中的 P0 词条（走策略自带的归一化与排除规则）。
func p0PolicyHits(text string, policy *contentguard.Policy) []contentguard.Hit {
	if policy == nil || strings.TrimSpace(text) == "" {
		return nil
	}
	return policy.Scan(text, "all").Hits
}

// rewriteWheelP0Angles 扫描轮盘里命中 P0 红线的角度，逐条让模型改写并挑一个干净的备选。
//
// 只改命中的那几条：命中通常 0~2 条，逐条改写比整轮重生成便宜得多。
// 改写失败不阻塞初始化——保留原角度，让它继续走后面的程序边界校验。
// 返回改写后的轮盘 JSON、改写条数、失败条数。
func (s *Server) rewriteWheelP0Angles(ctx context.Context, wheelJSON, _ string, p0MD, profileID string) (string, int, int, error) {
	policy, err := contentguard.Compile(strings.TrimSpace(p0MD))
	if err != nil || policy == nil {
		return wheelJSON, 0, 0, errors.New("P0策略不可用，跳过角度改写")
	}
	var wheel anchorStyleSellingPointWheel
	if err = json.Unmarshal([]byte(wheelJSON), &wheel); err != nil {
		return wheelJSON, 0, 0, err
	}
	notSay := renderWheelNotSay(p0MD)
	rewritten, failed := 0, 0
	for i := range wheel.Items {
		for j, angle := range wheel.Items[i].Angles {
			if len(p0PolicyHits(angle, policy)) == 0 {
				continue
			}
			hits := p0PolicyHits(angle, policy)
			terms := make([]string, 0, len(hits))
			for _, h := range hits {
				terms = append(terms, h.Text)
			}
			messages := renderWheelRewriteMessages(wheel.Items[i].Name, angle, terms, notSay, len([]rune(angle))+6)
			result, _, callErr := s.completeAnchorStyleWorkbench(ctx, profileID, "wheel_p0_rewrite", agentgateway.ResponseJSON, messages, 900, false)
			if callErr != nil {
				failed++
				continue
			}
			picked := pickWheelRewriteCandidate(wheelRewriteCandidates(result.Text), angle, policy)
			if picked == "" {
				failed++
				continue
			}
			wheel.Items[i].Angles[j] = picked
			rewritten++
		}
	}
	if rewritten == 0 {
		return wheelJSON, 0, failed, nil
	}
	encoded, err := json.Marshal(wheel)
	if err != nil {
		return wheelJSON, 0, failed, err
	}
	return string(encoded), rewritten, failed, nil
}
