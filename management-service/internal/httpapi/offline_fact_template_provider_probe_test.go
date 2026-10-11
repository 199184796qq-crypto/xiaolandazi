package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/config"
	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/interactionpolicy"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

type factTemplateFixture struct{ Product, Category, Task, Identity, Facts string }

func factTemplateFixtures() []factTemplateFixture {
	fixtures := []factTemplateFixture{
		{"熟食A", "熟食", "规格价格", "即食卤炸鸭，开袋直接吃。", "1号链接900g整只87.6元；2号链接450g半只48.8元。"},
		{"熟食A", "熟食", "常温保存", "即食卤炸鸭，开袋直接吃。", "未开封常温抽真空6天，不抽真空3天。"},
		{"服装A", "服装", "胸围90选择尺码", "纯棉短袖129元。", "S胸围82—86厘米，M胸围87—91厘米，L胸围92—96厘米。按胸围选，不按体重推算。"},
		{"服装A", "服装", "能否机洗", "纯棉短袖129元。", "标签要求冷水手洗，不能使用漂白剂；没有确认可机洗。"},
		{"家电A", "家电", "内胆能否放在燃气灶上", "3L电饭煲399元，用于煮饭。", "内胆不支持放在明火上使用。"},
		{"家电A", "家电", "能否煎炸", "3L电饭煲399元，用于煮饭。", "没有确认煎炸功能。"},
		{"护肤品A", "护肤品", "首次使用", "50g外用保湿霜98元，只用在完整皮肤。", "敏感肌首次先局部测试，出现不适停止使用；未确认具体测试部位或测试时长。"},
		{"护肤品A", "护肤品", "能否治疗痘痘", "50g外用保湿霜98元，不是药品。", "没有确认治疗痘痘的功效。"},
		{"课程A", "课程", "回看是否永久", "线上摄影课6次，每次60分钟，总价199元。", "提供录播回看30天；未确认期限起算点或课程本体为直播还是录播。"},
		{"课程A", "课程", "上过一节后能否退款", "线上摄影课6次，每次60分钟，总价199元。", "未开课可退款，开课后不可退款。未承诺其他退费条件或平台统一规则。"},
		{"咖啡豆A", "咖啡豆", "是否加水即溶", "烘焙咖啡整豆200g39元。", "需研磨后用滤杯冲泡，不是速溶粉，不能把整豆说成加水即溶。"},
		{"咖啡豆A", "咖啡豆", "是否赠送磨豆机", "烘焙咖啡整豆200g39元。", "没有确认赠送磨豆机或滤杯。"},
	}
	// The same product and question receive a changed source, not a new identity.
	// This tests fact updates as well as different tasks for one product.
	for _, change := range []struct {
		index int
		facts string
	}{
		{1, "未开封常温抽真空6天，不抽真空2天。"},
		{5, "已确认支持煎炸功能；煎炸时必须开盖。"},
		{9, "未开课可退款；开课后第一节课结束24小时内可申请退款，超过该期限不可退款。"},
		{11, "购买这袋咖啡豆赠送手摇磨豆机一台，不赠送滤杯。"},
	} {
		f := fixtures[change.index]
		f.Facts = change.facts
		fixtures = append(fixtures, f)
	}
	return fixtures
}

func TestFactTemplateCrossProductAndMultiFactProvider(t *testing.T) {
	if os.Getenv("FACT_TEMPLATE_PROVIDER_PROBE") != "1" {
		t.Skip("explicit paid offline template experiment")
	}
	store, err := appdb.Open(config.Load())
	if err != nil {
		t.Fatal("configuration unavailable")
	}
	defer store.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 18*time.Minute)
	defer cancel()
	models, err := store.SpeechModels(ctx)
	if err != nil {
		t.Fatal(err)
	}
	system, err := store.ResolveSpeechModel(ctx, models.SystemDefaultID)
	if err != nil || system == nil {
		t.Fatal("system model unavailable")
	}
	writer, err := store.ResolveSpeechModel(ctx, models.DefaultID)
	if err != nil || writer == nil {
		t.Fatal("writer unavailable")
	}
	p0, _, err := loadP0Mother()
	if err != nil {
		t.Fatal(err)
	}
	templateMother, err := interactionpolicy.Read("fact-expression-template.md")
	if err != nil {
		t.Fatal(err)
	}
	writeMother, err := interactionpolicy.Read("fact-template-write.md")
	if err != nil {
		t.Fatal(err)
	}
	composedMode := os.Getenv("FACT_TEMPLATE_PROGRAM_COMPOSE") == "1"
	if composedMode {
		templateMother, err = interactionpolicy.Read("fact-expression-template-single.md")
		if err != nil {
			t.Fatal(err)
		}
		writeMother, err = interactionpolicy.Read("fact-template-companion.md")
		if err != nil {
			t.Fatal(err)
		}
	}
	reviewMother, err := interactionpolicy.Read("final-fact-boundary-review.md")
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{store: store}
	call := func(profile *agentgateway.SpeechModel, stage, mother string, packet any, temperature *float64) (agentgateway.Response, error) {
		data, e := json.Marshal(packet)
		if e != nil {
			return agentgateway.Response{}, e
		}
		input := string(data)
		if plain, ok := packet.(string); ok {
			input = plain
		}
		r := agentgateway.Request{Stage: stage, Model: profile.Model, Messages: []agentgateway.Message{{Role: "system", Content: p0 + "\n\n" + mother}, {Role: "user", Content: input}}, ResponseFormat: agentgateway.ResponseText, MaxTokens: 850, Timeout: 20 * time.Second, EnableThinking: false, ThinkingOverride: new(false), Temperature: temperature}
		started := time.Now()
		id, e := s.beginModelExchange(ctx, stage, profile.ID, r)
		if e != nil {
			return agentgateway.Response{}, e
		}
		c, stop := context.WithTimeout(ctx, 20*time.Second)
		defer stop()
		res, e := agentgateway.CompleteSpeechEndpoint(c, *profile, r)
		res.Model = profile.Model
		res.Provider = "anchor:" + profile.ID
		e = s.finishModelExchange(ctx, id, res, res.Text, started, res.InputTokens, res.OutputTokens, e)
		return res, e
	}
	fixedTemperature := 0.2
	temperatures := []float64{0.7}
	if os.Getenv("FACT_TEMPLATE_TEMPERATURE_GRID") == "1" {
		temperatures = []float64{0.2, 0.7, 1.1}
	}
	review := func(f factTemplateFixture, text string) (string, agentgateway.Response, error) {
		var packet any = map[string]any{"商品类别": f.Category, "当前任务": f.Task, "商品必要事实": f.Identity, "确认资料": f.Facts, "正文": text}
		if composedMode {
			packet = fmt.Sprintf("当前商品类别：%s\n当前问题：%s\n商品必要事实：%s\n本次确认资料：%s\n\n只核对下面这一段正文：\n%s", f.Category, f.Task, f.Identity, f.Facts, text)
		}
		r, e := call(system, "offline_fact_template_review", string(reviewMother), packet, &fixedTemperature)
		return factBoundaryVerdict(r.Text), r, e
	}
	var mu sync.Mutex
	rows := []map[string]any{}
	archive := func() {
		mu.Lock()
		defer mu.Unlock()
		b, e := json.MarshalIndent(map[string]any{"rows": rows, "program_composed": composedMode, "template_model": system.Model, "review_model": system.Model, "writer_model": writer.Model, "p0_sha": derivedHash(p0), "writer_temperatures": temperatures, "fixed_plan_review_temperature": fixedTemperature, "repetitions": 2, "identical_messages_across_temperatures": true, "prompts": map[string]string{"p0": p0, "template": string(templateMother), "writer": string(writeMother), "review": string(reviewMother)}, "offline": true, "adopted": false, "tts": false, "played": false}, "", "  ")
		if e == nil {
			e = os.WriteFile(os.Getenv("FACT_TEMPLATE_PROVIDER_OUTPUT"), b, 0600)
		}
		if e != nil {
			t.Errorf("archive: %v", e)
		}
	}
	defer archive()
	slots := make(chan struct{}, 3)
	var wg sync.WaitGroup
	for _, fixture := range factTemplateFixtures() {
		f := fixture
		wg.Add(1)
		go func() {
			defer wg.Done()
			slots <- struct{}{}
			defer func() { <-slots }()
			row := map[string]any{"fixture": f, "source_sha": anchorcycle.ProductDigest(f)}
			finish := func() {
				mu.Lock()
				rows = append(rows, row)
				mu.Unlock()
				archive()
				t.Logf("completed %s/%s", f.Product, f.Task)
			}
			defer finish()
			binding := anchorcycle.SpeechTemplateBinding{OwnerKey: anchorcycle.ProductDigest(f.Product + "/" + f.Task), SourceSHA: anchorcycle.ProductDigest(f), P0SHA: derivedHash(p0), MotherSHA: derivedHash(string(templateMother) + string(reviewMother) + system.ID + system.Model)}
			var gen agentgateway.Response
			var e error
			if !composedMode {
				gen, e = call(system, "offline_fact_template_plan", string(templateMother), map[string]any{"商品类别": f.Category, "本次任务": f.Task, "商品必要事实": f.Identity, "本次确认事实": f.Facts}, &fixedTemperature)
				row["template_generation"] = gen
				if e != nil {
					row["error"] = "template request incomplete"
					t.Errorf("%s template request incomplete", f.Product)
					return
				}
			}
			if composedMode {
				responses := []agentgateway.Response{}
				clauses := []string{}
				for attempt := 0; attempt < 3; attempt++ {
					packet := fmt.Sprintf("当前商品类别：%s\n商品必要事实：%s\n本次确认事实：%s\n\n现在只回答这个观众问题：%s", f.Category, f.Identity, f.Facts, f.Task)
					one, e := call(system, "offline_fact_template_single", string(templateMother), packet, &fixedTemperature)
					responses = append(responses, one)
					if e != nil {
						t.Errorf("single template request incomplete")
						continue
					}
					clauses = append(clauses, fmt.Sprintf("%d. %s", len(clauses)+1, strings.TrimSpace(one.Text)))
				}
				row["single_template_responses"] = responses
				gen.Text = strings.Join(clauses, "\n")
				row["program_template_aggregation"] = gen.Text
			}
			asset, e := anchorcycle.NewSpeechTemplateAsset(gen.Text, anchorcycle.ID(), binding)
			if e != nil {
				row["error"] = e.Error()
				t.Errorf("template content incomplete")
				return
			}
			row["asset"] = asset
			for _, c := range asset.Cards {
				verdict, res, e := review(f, c.Action)
				if e != nil {
					t.Errorf("template review incomplete")
				}
				receipt := anchorcycle.SpeechTemplateReceipt{TextSHA: anchorcycle.ProductDigest(c.Action), BindingSHA: anchorcycle.ProductDigest(binding), Accepted: e == nil && verdict == "通过", RawFeedback: res.Text}
				if e = asset.RecordReview(c.ID, receipt); e != nil {
					t.Error(e)
				}
			}
			outputs := []map[string]any{}
			modes := []string{"direct", "template"}
			if composedMode {
				modes = []string{"composed"}
			}
			for _, mode := range modes {
				for _, temperature := range temperatures {
					recent := []string{}
					for repeat := 0; repeat < 2; repeat++ {
						locked := ""
						if mode != "direct" {
							// Keep the exact same selected sentence at all temperatures.
							locked, e = asset.Select(binding, 0)
							if e != nil {
								outputs = append(outputs, map[string]any{"mode": mode, "temperature": temperature, "repeat": repeat, "skipped": "no_verified_template"})
								continue
							}
						}
						// Recent outputs stay out of the request so only temperature changes.
						var packet any = map[string]any{"商品类别": f.Category, "当前任务": f.Task, "商品必要事实": f.Identity, "确认资料": f.Facts, "近期正文": []string{}, "必须保留的风险事实句": locked}
						if composedMode {
							packet = fmt.Sprintf("商品必要事实：%s\n当前问题：%s\n本次确认事实：%s\n已有风险事实答句：%s\n\n只返回一句其余衔接，程序保留已有答句。", f.Identity, f.Task, f.Facts, locked)
						}
						res, e := call(writer, "offline_fact_template_write", string(writeMother), packet, &temperature)
						o := map[string]any{"mode": mode, "temperature": temperature, "repeat": repeat, "locked_sentence": locked, "response": res, "request_content": packet, "completed": e == nil, "request_sha": anchorcycle.ProductDigest(packet)}
						outputs = append(outputs, o)
						if e != nil {
							t.Errorf("writer incomplete")
							continue
						}
						body := res.Text
						if composedMode {
							body, e = anchorcycle.ComposeSpeechTemplate(locked, res.Text)
							if e != nil {
								t.Error(e)
								continue
							}
						}
						o["final_body"] = body
						verdict, reviewRes, e := review(f, body)
						o["review"] = reviewRes
						o["verdict"] = verdict
						o["template_retained"] = mode == "direct" || anchorcycle.SpeechTemplateRetained(body, locked)
						duplicate := len(recent) > 0 && strings.TrimSpace(recent[len(recent)-1]) == strings.TrimSpace(body)
						o["exact_duplicate"] = duplicate
						o["eligible_by_probe_rules"] = e == nil && verdict == "通过" && !duplicate && o["template_retained"] == true
						if e != nil {
							t.Errorf("final review incomplete")
						}
						recent = append(recent, body)
					}
				}
			}
			row["outputs"] = outputs
		}()
	}
	wg.Wait()
	if len(rows) != len(factTemplateFixtures()) {
		t.Fatal("incomplete product/fact matrix")
	}
}
