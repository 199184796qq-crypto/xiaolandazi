// 临时探针：对比「整句素材」与「短属性/值 + 福利」两种欢迎输入的话术变化。
// 只读数据库与模型池，不写任何业务数据。
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/config"
	appdb "livecompanion/management/internal/db"
)

const writerPrompt = `你是正在直播的主播。写之前先读完输入。
输出严格 JSON：{"body":"欢迎正文","used_claim_ids":[1,2]}
规则：
1. 只使用输入里的事实，不增加输入没有的事实。
2. 数字、价格、规格必须与输入原文一致，不能改、不能自己算。
3. 不输出称呼。
4. 一到两句，40 到 90 字，适合直接 TTS。
5. 不要每次都用"欢迎来到+商品名+规格+价格+包邮"的固定全套。
`

type material struct {
	ClaimID int64  `json:"claim_id"`
	Kind    string `json:"kind"`
	Topic   string `json:"topic"`
	Text    string `json:"text"`
}

func callModel(ctx context.Context, gw *agentgateway.Gateway, payload map[string]any) (string, error) {
	raw, _ := json.Marshal(payload)
	req := agentgateway.Request{
		Stage:           "welcome_probe",
		Messages:        []agentgateway.Message{{Role: "system", Content: writerPrompt}, {Role: "user", Content: string(raw)}},
		MaxTokens:       300,
		ResponseFormat:  agentgateway.ResponseJSON,
		EnableThinking:  false,
		Timeout:         20 * time.Second,
	}
	resp, err := gw.Complete(ctx, req)
	if err != nil {
		return "", err
	}
	var out struct {
		Body string `json:"body"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(resp.Text)), &out); err != nil {
		return strings.TrimSpace(resp.Text), nil
	}
	return out.Body, nil
}

func main() {
	cfg := config.Load()
	store, err := appdb.Open(cfg)
	if err != nil {
		fmt.Println("open db:", err)
		os.Exit(1)
	}
	defer store.Close()
	gw := agentgateway.NewFromEnv().WithSpeechModels(store)
	ctx := context.Background()

	// 整句素材（现状）：claim 原文
	long := []material{
		{1, "product", "品牌资历", "门店在南充红花街一号，开了几十年了，实体门店五十多家"},
		{2, "product", "核心工艺", "老卤汤慢炖四个小时，两遍油炸，出锅裹上秘制红油高温锁汁定型"},
		{3, "product", "口感", "外皮酥脆有嚼劲，肉质鲜嫩多汁，开袋可以直接吃"},
		{4, "offer", "价格", "1号链接87.6元，2号链接92.5元，3号链接48.8元"},
	}
	// 短值（候选）：属性/值
	short := []material{
		{1, "product", "门店数量", "五十多家"},
		{2, "product", "地址", "南充红花街一号"},
		{3, "product", "工艺", "老卤4小时、两遍油炸"},
		{4, "product", "口感", "外皮酥脆、肉质鲜嫩"},
		{5, "offer", "价格", "1号87.6 / 2号92.5 / 3号48.8"},
	}
	intro := "四川绸都杨鸭子官方直播间，主播小林，实在直爽，像跟熟客唠家常。"

	for round := 1; round <= 3; round++ {
		a, errA := callModel(ctx, gw, map[string]any{"room_intro": intro, "materials": long, "recent_welcome": []string{}})
		b, errB := callModel(ctx, gw, map[string]any{"room_intro": intro, "materials": short, "recent_welcome": []string{}})
		fmt.Printf("[%d] LONG err=%v\n%s\n", round, errA, a)
		fmt.Printf("[%d] SHORT err=%v\n%s\n", round, errB, b)
		fmt.Println(strings.Repeat("-", 40))
	}
}
