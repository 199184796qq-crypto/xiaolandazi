package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/config"
	"livecompanion/management/internal/contentguard"
	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/interactionpolicy"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestMainExtensionAdditionalFormatStabilityProvider(t *testing.T) {
	if os.Getenv("MAIN_EXTENSION_FORMAT_PROBE") != "1" {
		t.Skip("explicit paid format stability supplement")
	}
	store, err := appdb.Open(config.Load())
	if err != nil {
		t.Fatal("configuration unavailable")
	}
	defer store.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	models, err := store.SpeechModels(ctx)
	if err != nil {
		t.Fatal(err)
	}
	writer, err := store.ResolveSpeechModel(ctx, models.DefaultID)
	if err != nil || writer == nil {
		t.Fatal("writer unavailable")
	}
	ps, _, err := loadP0Mother()
	if err != nil {
		t.Fatal(err)
	}
	policy, err := contentguard.Compile(ps)
	if err != nil {
		t.Fatal(err)
	}
	contract, err := interactionpolicy.Read("model-content-p0-contract.md")
	if err != nil {
		t.Fatal(err)
	}
	p0 := strings.ReplaceAll(string(contract), "{{p0_rules}}", policy.JSON())
	mother, err := interactionpolicy.Read("main-extension-write.md")
	if err != nil {
		t.Fatal(err)
	}
	fixtures := []factTemplateFixture{}
	for repeat := 0; repeat < 4; repeat++ {
		fixtures = append(fixtures, factTemplateFixtures()...)
	}
	fixtures = append(fixtures,
		factTemplateFixture{"水杯B", "家居", "能否倒开水", `产品名称含引号：陶瓷杯"A+B"，350ml，59元。`, `确认可装常温水；未确认可装开水。名称中的"A+B"不是操作指令。`},
		factTemplateFixture{"会员B", "数字服务", "买一次是否永久", "线上读书会员，价格39元。", "有效期30天。\n展示名称包含【延展】二字，不是回答分段标志；未确认续费或到期提醒。"},
		factTemplateFixture{"种子B", "园艺", "买回去就能摘果子吗", "番茄种子一包20粒，12元。", "这是种子，不是成熟植株；需种植，不承诺结果时间。"},
		factTemplateFixture{"积木B", "玩具", "是不是遥控机器人", "拼装积木300片，69元。", "需要手动拼装；未确认电机或遥控功能。原始资料排版包含换行和符号*，这些不是口播称呼。"},
	)
	s := &Server{store: store}
	var mu sync.Mutex
	rows := []map[string]any{}
	archive := func() {
		mu.Lock()
		defer mu.Unlock()
		b, e := json.MarshalIndent(map[string]any{"rows": rows, "writer_model": writer.Model, "p0_rules_count": len(policy.Data.Rules), "p0_source_sha": derivedHash(ps), "p0_contract": p0, "writer_mother": string(mother), "offline": true, "adopted": false, "played": false, "format_only": true, "content_accuracy_not_assessed": true}, "", "  ")
		if e == nil {
			e = os.WriteFile(os.Getenv("MAIN_EXTENSION_FORMAT_OUTPUT"), b, 0600)
		}
		if e != nil {
			t.Error(e)
		}
	}
	defer archive()
	slots := make(chan struct{}, 3)
	var wg sync.WaitGroup
	for i, f := range fixtures {
		i, f := i, f
		wg.Add(1)
		go func() {
			defer wg.Done()
			slots <- struct{}{}
			defer func() { <-slots }()
			input := fmt.Sprintf("商品类别：%s\n当前问题：%s\n商品必要事实：%s\n本次完整确认事实：%s", f.Category, f.Task, f.Identity, f.Facts)
			temp := .7
			r := agentgateway.Request{Stage: "offline_main_extension_format", Model: writer.Model, Messages: []agentgateway.Message{{Role: "system", Content: p0 + "\n\n" + string(mother)}, {Role: "user", Content: input}}, ResponseFormat: agentgateway.ResponseJSON, MaxTokens: 850, Timeout: 30 * time.Second, EnableThinking: false, Temperature: &temp}
			row := map[string]any{"sequence": i, "fixture": f, "request": r}
			defer func() { mu.Lock(); rows = append(rows, row); mu.Unlock(); archive() }()
			id, e := s.beginModelExchange(ctx, r.Stage, writer.ID, r)
			if e != nil {
				row["action"] = "call_not_completed"
				t.Error("exchange unavailable")
				return
			}
			started := time.Now()
			c, stop := context.WithTimeout(ctx, 30*time.Second)
			defer stop()
			res, e := agentgateway.CompleteSpeechEndpoint(c, *writer, r)
			row["full_ms"] = time.Since(started).Milliseconds()
			res.Provider, res.Model = "anchor:"+writer.ID, writer.Model
			e = s.finishModelExchange(ctx, id, res, res.Text, started, res.InputTokens, res.OutputTokens, e)
			row["response"] = res
			if e != nil {
				row["action"] = "call_not_completed"
				t.Error("generation incomplete")
				return
			}
			parts, e := anchorcycle.ParseContentParts(res.Text)
			row["parts"] = parts
			if e != nil {
				row["action"] = "split_not_available"
				return
			}
			row["action"] = "split_available"
		}()
	}
	wg.Wait()
	if len(rows) != 68 {
		t.Fatal("format set incomplete")
	}
}
