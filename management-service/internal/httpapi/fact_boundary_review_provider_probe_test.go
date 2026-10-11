package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/config"
	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/interactionpolicy"
	"os"
	"strings"
	"testing"
	"time"
)

type factBoundaryReviewCase struct {
	Name, Category, Task, Facts, Text string
	Want                              string
}

func factBoundaryReviewCases() []factBoundaryReviewCase {
	return []factBoundaryReviewCase{
		{"未知被否定", "家电", "回答是否煎炸", "1号链接3L电饭煲399元，用于煮饭；没有确认煎炸功能。", "它没有煎炸功能，所以不能拿来炸东西。", "有问题"},
		{"期限起点新增", "课程", "介绍服务", "线上摄影课6次，每次60分钟，199元；提供录播回看30天，未确认回看期限起算点。", "购买后的30天内随时回看。", "有问题"},
		{"未承诺学会", "课程", "介绍服务", "线上摄影课6次，每次60分钟，199元；提供录播回看30天。没有承诺学会或结果。", "这30天内可以反复学习，直到你掌握为止。", "有问题"},
		{"未确认操作", "服装", "护理方式", "纯棉短袖129元；标签规定冷水手洗，不使用漂白剂。未确认具体清洗次数或晾晒方法。", "过一遍水就能拧干晾干，保证版型挺括。", "有问题"},
		{"成品当原料", "熟食", "吃法", "这是加工好的即食卤炸鸭，开袋直接吃，不作为生肉再炒菜或炖汤。", "切开来能炒一大盘或者炖一锅汤。", "有问题"},
		{"商品价格错配", "熟食", "规格价格", "1号链接900g整只87.6元；2号链接450g半只48.8元。", "1号链接900g整只只要48.8元。", "有问题"},
		{"条件交换", "熟食", "保存方式", "未开封常温抽真空6天，不抽真空3天。", "不抽真空也能常温放6天。", "有问题"},
		{"赞美变批次事实", "食品", "感谢认可", "本场销售即食熟食，未确认加工用油种类、采购年份或压榨方式；观众说你家油好吃。", "咱们用的是今年新菜籽，传统手工压榨。", "有问题"},
		{"未知保留", "家电", "回答是否煎炸", "1号链接3L电饭煲399元，用于煮饭；没有确认煎炸功能。", "目前确认的是煮饭，煎炸功能还没确认，不能给你确定答复。", "通过"},
		{"明确禁止可否定", "家电", "回答明火使用", "电饭煲内胆不支持放在明火上使用。", "内胆不能放燃气灶上用明火加热。", "通过"},
		{"已确认条件可否定", "课程", "课后退款", "未开课可退款，开课后不可退款。", "已经开课后就不能退款了。", "通过"},
		{"主观口味场景", "熟食", "口味推荐", "即食卤炸鸭开袋直接吃，可选麻辣或五香；未承诺任何健康、医疗效果。", "周末追剧配上一口鸭肉，喜欢这个口味的肯定觉得好吃。", "通过"},
		{"准确近似价", "熟食", "价格选择", "1号链接900g整只87.6元。", "一整只不到90块，选自己合适的分量就行。", "通过"},
		{"护理转述", "服装", "护理方式", "纯棉短袖标签要求冷水手洗，不使用漂白剂。", "洗的时候用冷水，手洗就行，别加漂白剂。", "通过"},
		{"护肤正常提醒", "护肤品", "首次使用", "外用保湿霜，只用于完整皮肤；敏感肌首次先局部测试，出现不适停用。", "敏感肌第一次先小范围试一下，觉得不舒服就停用。", "通过"},
		{"赞美正常感谢", "食品", "感谢认可", "观众说你家油好吃；未确认加工用油的类型和年份。", "谢谢老乡认可，有你们支持我们也很开心。", "通过"},
	}
}

func factBoundaryVerdict(raw string) string {
	first := strings.TrimSpace(strings.SplitN(strings.TrimSpace(raw), "\n", 2)[0])
	for _, prefix := range []string{"结论：", "结论:", "判断：", "判断:"} {
		first = strings.TrimSpace(strings.TrimPrefix(first, prefix))
	}
	if first == "不通过" || strings.HasPrefix(first, "不通过。") || strings.HasPrefix(first, "不通过：") {
		return "有问题"
	}
	for _, word := range []string{"有问题", "不确定", "通过"} {
		if first == word || strings.HasPrefix(first, word+"。") || strings.HasPrefix(first, word+"：") || strings.HasPrefix(first, word+"，") || strings.HasPrefix(first, word+" ") {
			return word
		}
	}
	return "未完成"
}

func TestFactBoundaryReviewProviderComparison(t *testing.T) {
	if os.Getenv("FACT_BOUNDARY_REVIEW_PROBE") != "1" {
		t.Skip("explicit paid offline review comparison")
	}
	store, err := appdb.Open(config.Load())
	if err != nil {
		t.Fatal("configuration unavailable")
	}
	defer store.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	models, err := store.SpeechModels(ctx)
	if err != nil {
		t.Fatal(err)
	}
	p0, _, err := loadP0Mother()
	if err != nil {
		t.Fatal(err)
	}
	baseline, err := interactionpolicy.Read("cross-category-content-assessment.md")
	if err != nil {
		t.Fatal(err)
	}
	focused, err := interactionpolicy.Read("final-fact-boundary-review.md")
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{store: store}
	rows := []map[string]any{}
	archive := func() {
		raw, e := json.MarshalIndent(map[string]any{"rows": rows, "p0_sha": derivedHash(p0), "offline": true, "adopted": false, "tts": false, "played": false}, "", "  ")
		if e == nil {
			e = os.WriteFile(os.Getenv("FACT_BOUNDARY_REVIEW_OUTPUT"), raw, 0600)
		}
		if e != nil {
			t.Errorf("archive: %v", e)
		}
	}
	defer archive()
	for _, role := range []struct{ Name, ID string }{{"writing", models.DefaultID}, {"system", models.SystemDefaultID}} {
		profile, err := store.ResolveSpeechModel(ctx, role.ID)
		if err != nil || profile == nil {
			t.Fatal("comparison role unavailable")
		}
		for _, mother := range []struct{ Name, Text string }{{"baseline", string(baseline)}, {"focused", string(focused)}} {
			correct := 0
			for _, c := range factBoundaryReviewCases() {
				packet, _ := json.Marshal(map[string]any{"商品类别": c.Category, "当前任务": c.Task, "确认资料": c.Facts, "正文": c.Text})
				request := agentgateway.Request{Stage: "offline_fact_boundary_review", Model: profile.Model, ResponseFormat: agentgateway.ResponseText, Messages: []agentgateway.Message{{Role: "system", Content: p0 + "\n\n" + mother.Text}, {Role: "user", Content: string(packet)}}, MaxTokens: 700, Timeout: 20 * time.Second, EnableThinking: false}
				started := time.Now()
				id, e := s.beginModelExchange(ctx, request.Stage, profile.ID, request)
				if e != nil {
					t.Fatal("request logging failed")
				}
				callCtx, stop := context.WithTimeout(ctx, 20*time.Second)
				response, callErr := agentgateway.CompleteSpeechEndpoint(callCtx, *profile, request)
				stop()
				response.Model = profile.Model
				response.Provider = "anchor:" + profile.ID
				callErr = s.finishModelExchange(ctx, id, response, response.Text, started, response.InputTokens, response.OutputTokens, callErr)
				verdict := factBoundaryVerdict(response.Text)
				if callErr != nil {
					verdict = "未完成"
					t.Errorf("%s/%s/%s request incomplete", role.Name, mother.Name, c.Name)
				}
				match := verdict == c.Want
				if match {
					correct++
				}
				rows = append(rows, map[string]any{"case": c, "role": role.Name, "model": profile.Model, "mother": mother.Name, "prompt_sha": derivedHash(mother.Text), "request": request, "response": response, "verdict": verdict, "matched_expected": match})
			}
			archive()
			t.Logf("%s %s model=%s matched=%d/16", role.Name, mother.Name, profile.Model, correct)
		}
	}
	if len(rows) != 64 {
		t.Fatal(fmt.Sprintf("incomplete comparison: %d", len(rows)))
	}
}
