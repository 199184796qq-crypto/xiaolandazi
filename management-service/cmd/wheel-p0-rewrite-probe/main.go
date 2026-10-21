// 轮盘 P0 局部改写探测：用真实模型跑多轮「命中 → 出 2 个备选 → 程序挑一个」，
// 统计改写成功率与剩余命中率。只读配置与母版，不写方案、轮盘、话术数据。
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/config"
	"livecompanion/management/internal/contentguard"
	"livecompanion/management/internal/db"
)

const rewriteSystem = `# 必说
下面这条直播讲解角度里有个词不能说。只把那个词换掉，其余字原样保留：
不改句子结构、不换主题、不加新信息、不换商品链接。
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



const unusedFacts = `- [用户指定商品="1号链接"；market/价格] 1号链接团购价337元，原价547元；10人餐
- [用户指定商品="2号链接"；market/价格] 2号链接团购价187元，原价245元；适合3-6人
- [用户指定公共；promotion/优惠券] 领取优惠券可再减30元`

type probeCase struct {
	SellingPoint string
	Angle        string
}

func main() {
	profileID := flag.String("profile", "", "encrypted credential profile id")
	p0File := flag.String("p0", "", "path to p0-hard-block.md")
	output := flag.String("output", "", "result archive")
	flag.Parse()
	if *profileID == "" || *p0File == "" || *output == "" {
		panic("explicit probe arguments required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	store, err := db.Open(config.Load())
	if err != nil {
		panic("probe database unavailable")
	}
	defer store.Close()
	profile, err := store.ResolveSpeechModel(ctx, *profileID)
	if err != nil || profile == nil || profile.APIKey == "" || !profile.Enabled {
		panic("probe credential unavailable")
	}
	raw, err := os.ReadFile(*p0File)
	if err != nil {
		panic("p0 policy unavailable")
	}
	policy, err := contentguard.Compile(string(raw))
	if err != nil {
		panic("p0 policy compile failed: " + err.Error())
	}
	notSay := renderNotSay(policy)

	cases := buildCases()
	rows := []map[string]any{}
	hit, called, fixed, stuck, callFail := 0, 0, 0, 0, 0
	var totalLatency int64
	for _, c := range cases {
		row := map[string]any{"selling_point": c.SellingPoint, "angle": c.Angle}
		row["hits"] = hitTerms(c.Angle, policy)
		if len(row["hits"].([]string)) == 0 {
			row["action"] = "clean"
			rows = append(rows, row)
			continue
		}
		hit++
		called++
		limit := len([]rune(c.Angle)) + 6
		prompt := strings.Replace(rewriteSystem, "{{NOT_SAY}}", notSay, 1)
		prompt = strings.Replace(prompt, "{{LIMIT}}", fmt.Sprint(limit), 1)
		user, _ := json.Marshal(map[string]any{"卖点": c.SellingPoint, "原角度": c.Angle, "违规词": row["hits"]})
		req := agentgateway.Request{
			Stage:          "wheel_p0_rewrite_probe",
			Model:          profile.Model,
			Messages:       []agentgateway.Message{{Role: "system", Content: prompt}, {Role: "user", Content: string(user)}},
			ResponseFormat: agentgateway.ResponseJSON,
			MaxTokens:      600,
			EnableThinking: false,
			Timeout:        45 * time.Second,
		}
		req, err = agentgateway.PrepareTextRequest(*profile, req)
		if err != nil {
			row["action"] = "prepare_failed"
			rows = append(rows, row)
			callFail++
			continue
		}
		started := time.Now()
		result, callErr := agentgateway.CompleteSpeechEndpoint(ctx, *profile, req)
		latency := time.Since(started).Milliseconds()
		totalLatency += latency
		row["latency_ms"] = latency
		if callErr != nil {
			row["action"] = "call_failed"
			rows = append(rows, row)
			callFail++
			continue
		}
		candidates := parseCandidates(result.Text)
		row["candidates"] = candidates
		row["raw"] = result.Text
		picked := ""
		for _, item := range candidates {
			if len(hitTerms(item, policy)) == 0 {
				picked = item
				break
			}
		}
		row["picked"] = picked
		if picked == "" {
			row["action"] = "all_candidates_hit"
			row["picked_hits"] = hitTerms(strings.Join(candidates, " / "), policy)
			stuck++
		} else {
			row["action"] = "rewritten"
			row["picked_hits"] = []string{}
			fixed++
		}
		rows = append(rows, row)
		raw, _ := json.MarshalIndent(rows, "", "  ")
		_ = os.WriteFile(*output, raw, 0o600)
		fmt.Printf("case=%d hits=%d picked=%q latency=%dms\n", len(rows), len(row["hits"].([]string)), picked, latency)
	}
	summary := map[string]any{
		"cases": len(cases), "hits": hit, "calls": called, "rewritten": fixed,
		"all_candidates_hit": stuck, "call_failed": callFail,
		"rewrite_rate":      pct(fixed, called),
		"avg_latency_ms":    avg(totalLatency, int64(called-callFail)),
		"p0_rules_rendered": len(notSay),
	}
	out, _ := json.MarshalIndent(map[string]any{"summary": summary, "rows": rows}, "", "  ")
	if err := os.WriteFile(*output, out, 0o600); err != nil {
		panic("probe archive failed")
	}
	pretty, _ := json.MarshalIndent(summary, "", "  ")
	fmt.Println(string(pretty))
}

func buildCases() []probeCase {
	p := "价格算账与优惠"
	c := "双规格差异对比"
	e := "内容亮点"
	return []probeCase{
		{p, "对比两档套餐的绝对节省金额"},
		{p, "这是全城最好吃的6人餐"},
		{p, "团购价是全市最低价"},
		{p, "这个套餐性价比最高"},
		{p, "2号链接是同类第一名的选择"},
		{p, "100%值得回购的套餐"},
		{p, "零风险尝试这份6人餐"},
		{p, "顶级品质的10人餐"},
		{p, "纯手工制作的锅底"},
		{p, "极致性价比的团购组合"},
		{c, "对比1号链接与2号链接的荤素菜数量差异"},
		{c, "1号链接和2号链接都包含锅底油碟"},
		{c, "两个套餐配置完全一致"},
		{c, "按人数选1号或2号链接"},
		{c, "1号链接比2号链接更划算"},
		{e, "这个套餐能养胃滋补"},
		{e, "吃完能调理肠胃"},
		{e, "适合孕妇和婴儿食用"},
		{p, "1号链接团购价337元，原价547元"},
		{e, "包含22道荤素菜、10道凉菜小吃"},
	}
}

func renderNotSay(policy *contentguard.Policy) string {
	var b strings.Builder
	for _, rule := range policy.Data.Rules {
		terms := make([]string, 0, len(rule.Terms))
		for _, term := range rule.Terms {
			if term = strings.TrimSpace(term); term != "" {
				terms = append(terms, term)
			}
		}
		if len(terms) == 0 {
			continue
		}
		b.WriteString("- " + strings.TrimSpace(rule.Category) + "：" + strings.Join(terms, "、") + "\n")
	}
	return b.String()
}

func hitTerms(text string, policy *contentguard.Policy) []string {
	hits := policy.Scan(text, "all").Hits
	out := make([]string, 0, len(hits))
	for _, h := range hits {
		out = append(out, h.Text)
	}
	return out
}

func parseCandidates(raw string) []string {
	start, end := strings.Index(raw, "{"), strings.LastIndex(raw, "}")
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

func pct(part, total int) float64 {
	if total == 0 {
		return 0
	}
	return float64(part) * 100 / float64(total)
}

func avg(sum, n int64) int64 {
	if n == 0 {
		return 0
	}
	return sum / n
}
