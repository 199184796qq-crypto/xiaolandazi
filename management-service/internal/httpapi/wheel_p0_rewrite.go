package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/contentguard"
)

// 轮盘角度命中 P0 红线后的局部重写。
//
// 不删角度（删了卖点就少一个讲解任务），也不整轮重生成（代价太大）。
// 做法：把命中的那一条单独喂回模型，按四层结构要求它改写，出 2 个备选，
// 程序挑第一个仍然干净的。提示词刻意写短——这里只是换一句话，
// 塞进全量母版会让模型跑题去重写整个轮盘。

const wheelP0RewriteSystem = `# 必说
把下面这条直播讲解角度改写成 2 条备选角度。只输出 JSON：{"备选":["角度1","角度2"]}。
每条都要保留原角度的沟通任务与事实，不新增数字、不换卖点、不解释过程。

# 可说（事实包）
{{FACTS}}

# 不提（以下说法一律不许出现）
{{NOT_SAY}}

# 改写
1. 绝对化说法 → 换成「很 / 非常 / 特别 / 相当」，或直接写事实。
2. 跨链接比较 → 只讲本条角度自己的那个商品；没有事实依据不许拿两个链接对比。
3. 每条不超过 30 字，只写这条要讲什么，不写正文。`

// renderWheelRewriteMessages 组装一次局部改写的消息（system + user）。
func renderWheelRewriteMessages(sellingPoint, angle, facts, notSay string) []agentgateway.Message {
	prompt := strings.Replace(wheelP0RewriteSystem, "{{FACTS}}", strings.TrimSpace(facts), 1)
	prompt = strings.Replace(prompt, "{{NOT_SAY}}", strings.TrimSpace(notSay), 1)
	user, _ := json.Marshal(map[string]string{"卖点": sellingPoint, "原角度": angle})
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

// pickWheelRewriteCandidate 选第一个仍不含红线词的备选；都不行返回空串。
func pickWheelRewriteCandidate(candidates []string, policy *contentguard.Policy) string {
	for _, item := range candidates {
		if len(p0PolicyHits(item, policy)) == 0 {
			return item
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
func (s *Server) rewriteWheelP0Angles(ctx context.Context, wheelJSON, facts, p0MD, profileID string) (string, int, int, error) {
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
			messages := renderWheelRewriteMessages(wheel.Items[i].Name, angle, facts, notSay)
			result, _, callErr := s.completeAnchorStyleWorkbench(ctx, profileID, "wheel_p0_rewrite", agentgateway.ResponseJSON, messages, 900, false)
			if callErr != nil {
				failed++
				continue
			}
			picked := pickWheelRewriteCandidate(wheelRewriteCandidates(result.Text), policy)
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
