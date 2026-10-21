package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/config"
	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/decisionexecutor"
	"livecompanion/management/internal/interactionpolicy"
	"livecompanion/management/internal/model"
	"os"
	"strings"
	"testing"
	"time"
)

// Synthetic rooms and products exist only in this test. The real provider,
// planner, writer, question workflow and P0 are exercised; no asset adoption.
type crossCategoryFixture struct {
	Name, Point, Angle         string
	Topic, Boundary, Questions []string
}

func loadCrossCategoryFixtures() ([]crossCategoryFixture, error) {
	fixtures := crossCategoryFixtures()
	if path := os.Getenv("CROSS_CATEGORY_CONTENT_INPUT"); path != "" {
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		if err = json.Unmarshal(raw, &fixtures); err != nil {
			return nil, err
		}
	}
	if len(fixtures) == 0 || len(fixtures) > 30 {
		return nil, fmt.Errorf("cross-category fixture count invalid")
	}
	seen := map[string]bool{}
	for _, fixture := range fixtures {
		if strings.TrimSpace(fixture.Name) == "" || seen[fixture.Name] || fixture.Point == "" || fixture.Angle == "" || len(fixture.Topic) == 0 || len(fixture.Boundary) == 0 || len(fixture.Questions) == 0 {
			return nil, fmt.Errorf("cross-category fixture incomplete or duplicate")
		}
		seen[fixture.Name] = true
	}
	return fixtures, nil
}

func crossCategoryFixtures() []crossCategoryFixture {
	return []crossCategoryFixture{
		{"熟食", "分量与价格", "解释整只与半只怎样选", []string{"1号链接即食卤炸鸭900g整只，售价87.6元", "2号链接即食卤炸鸭450g半只，售价48.8元"}, []string{"这是已经加工好的熟食，开袋直接吃，不作为生肉炒菜或炖汤", "两种规格均可选抽真空或不抽真空；未开封常温抽真空6天、不抽真空3天", "确认使用京东物流；没有确认到货日期"}, []string{"半只多少克多少钱？", "今天买明天能收到吗？"}},
		{"服装", "穿着与清洗", "说明怎样按已确认标签护理", []string{"1号链接纯棉短袖，售价129元，S/M/L可选", "标签护理：冷水手洗，不能使用漂白剂；没有确认可机洗"}, []string{"这是成衣，不是食品或加工原料", "S适用胸围82—86厘米，M适用87—91厘米，L适用92—96厘米；按胸围选择，不按体重猜尺码", "款式有白色和蓝色；没有确认保暖、防晒或医疗效果"}, []string{"胸围90选哪个码？", "能不能机洗？"}},
		{"家电", "容量与用途", "帮助判断3升是否适合已确认的用途", []string{"1号链接电饭煲容量3L，售价399元，用于煮饭", "不支持压力烹饪，不支持把内胆放在明火上；没有确认煎炸功能"}, []string{"这是家用电器，容量不是食物重量，不把3升说成3公斤", "包装含主机和内胆；没有确认另送配件", "按随附说明书操作，没有确认替代医疗或营养功效"}, []string{"可以拿内胆上燃气灶吗？", "能不能拿来炸东西？"}},
		{"护肤品", "使用方式", "说明外用范围与试用注意", []string{"1号链接保湿霜50g，售价98元，仅用于完整皮肤外用", "敏感肌首次使用先局部测试；出现不适停止使用"}, []string{"这是外用护肤品，不是食品或药品", "没有确认治疗痘痘、消炎、祛斑、所有肤质适用或永久效果", "没有确认孕婴适用、口服或医用资格"}, []string{"能治痘痘吗？", "敏感肌能直接整脸用吗？"}},
		{"课程", "课时与服务范围", "区分课程内容与未承诺的结果", []string{"1号链接线上摄影课程6次课，总价199元，每次60分钟", "课程提供录播回看30天；未开课可退款，开课后不可退款"}, []string{"这是线上学习服务，不是实物商品，不涉及快递", "没有确认包就业、保收入、包过考试或终身回看", "6次课是课程总量，不是每天6次或6个月"}, []string{"买了能一直回看吗？", "上过一节不喜欢可以退吗？"}},
		{"咖啡豆", "商品形态与冲泡", "区分整豆和速溶饮料，说明已有使用步骤", []string{"1号链接烘焙咖啡整豆200g，售价39元", "需研磨后用滤杯冲泡，不是速溶粉，不能把整豆说成加水即溶"}, []string{"这是烘焙咖啡整豆，规格200g不是200杯", "没有确认糖含量、添加剂、减肥或提神时长承诺", "没有确认赠送磨豆机或滤杯"}, []string{"直接兑水就能喝吗？", "是不是会送磨豆机？"}},
	}
}

type crossCategoryProbeStore struct {
	*appdb.Store
	facts, name, productContext string
	oldIndustry                 bool
}

func (s *crossCategoryProbeStore) LoadLivePolicyLayers(ctx context.Context, tenant, room int64) (string, *model.LivePolicyVersion, *model.LivePolicyVersion, *model.LivePolicyVersion, error) {
	if s.oldIndustry {
		return s.Store.LoadLivePolicyLayers(ctx, tenant, room)
	}
	l1, err := s.Store.GetActiveLivePolicyVersion(ctx, model.LivePolicyLayerL1, "", 0, 0)
	if err != nil {
		return "", nil, nil, nil, err
	}
	l2, err := s.Store.GetActiveLivePolicyVersion(ctx, model.LivePolicyLayerL2, "general", 0, 0)
	return "general", l1, l2, nil, err
}

func (s *crossCategoryProbeStore) GetLiveAgentPlanForRoom(context.Context, int64, int64) (model.LiveAgentPlan, error) {
	return model.LiveAgentPlan{ID: 4, TenantID: 14, Name: "离线模拟" + s.name, Status: "active"}, nil
}
func (s *crossCategoryProbeStore) ListLiveAgentPlanFacts(context.Context, int64, int64) ([]model.LiveAgentPlanFact, error) {
	return nil, nil
}
func (s *crossCategoryProbeStore) CurrentInteractionFacts(context.Context, int64, int64, int64) (string, error) {
	return s.facts, nil
}
func (s *crossCategoryProbeStore) CurrentInteractionProductContext(context.Context, int64, int64, int64) (string, error) {
	return s.productContext, nil
}
func (s *crossCategoryProbeStore) CurrentInteractionLectureFacts(context.Context, int64, int64, int64, string) (string, error) {
	return "", nil
}
func (s *crossCategoryProbeStore) GetLatestPlanContentStrategy(context.Context, int64, int64, string, time.Time) (string, error) {
	return "", appdb.ErrPlanAnalysisAssetNotFound
}
func (s *crossCategoryProbeStore) ListActiveAgentMemories(context.Context, int64, int64) ([]model.AgentMemoryItem, error) {
	return nil, nil
}

type crossCategoryProbeGateway struct {
	gateway *agentgateway.Gateway
	p0      string
	calls   []map[string]any
}

func (g *crossCategoryProbeGateway) ModelRoleIdentity(ctx context.Context, stage string) (string, error) {
	return g.gateway.ModelRoleIdentity(ctx, stage)
}
func (g *crossCategoryProbeGateway) TextGenerationIdentity(ctx context.Context) (string, error) {
	return g.gateway.TextGenerationIdentity(ctx)
}
func (g *crossCategoryProbeGateway) Complete(ctx context.Context, r agentgateway.Request) (agentgateway.Response, error) {
	r.Messages = append([]agentgateway.Message{{Role: "system", Content: g.p0}}, r.Messages...)
	result, err := g.gateway.Complete(ctx, r)
	g.calls = append(g.calls, map[string]any{"request": r, "response": result, "completed": err == nil})
	return result, err
}

func TestCrossCategoryContentProviderProbe(t *testing.T) {
	if os.Getenv("CROSS_CATEGORY_CONTENT_PROBE") != "1" {
		t.Skip("explicit paid cross-category replay")
	}
	fixtures, err := loadCrossCategoryFixtures()
	if err != nil {
		t.Fatal(err)
	}
	store, err := appdb.Open(config.Load())
	if err != nil {
		t.Fatal("configuration unavailable")
	}
	defer store.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()
	settings, err := store.AnchorGenerationSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	speech, err := store.ResolveSpeechModel(ctx, settings.Cycle.TextProfileID)
	if err != nil || speech == nil {
		t.Fatal("writer unavailable")
	}
	p0, source, err := loadP0Mother()
	if err != nil {
		t.Fatal(err)
	}
	mother, err := interactionpolicy.Read("cross-category-content-assessment.md")
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{store: store}
	rows := []map[string]any{}
	archive := func(complete bool) {
		raw, err := json.MarshalIndent(map[string]any{"rows": rows, "writer_model": speech.Model, "p0_sha": derivedHash(p0), "workflow_passed": complete && !t.Failed(), "complete": complete, "synthetic": true, "tts": false, "played": false, "adopted": false}, "", "  ")
		if err == nil {
			err = os.WriteFile(os.Getenv("CROSS_CATEGORY_CONTENT_OUTPUT"), raw, 0600)
		}
		if err != nil {
			t.Errorf("archive failed: %v", err)
		}
	}
	defer func() { archive(true) }()
	assess := func(f crossCategoryFixture, text, task string) string {
		data, _ := json.Marshal(map[string]any{"类目": f.Name, "本次任务": task, "确认事实": append(append([]string{}, f.Topic...), f.Boundary...), "正文": text, "有效P0": generationP0View(p0, "")})
		result, _, err := server.completeAnchorStyleWorkbench(ctx, settings.Cycle.TextProfileID, "mainline_p0_context", agentgateway.ResponseText, []agentgateway.Message{{Role: "system", Content: string(mother)}, {Role: "user", Content: string(data)}}, 900, false)
		if err != nil {
			t.Errorf("%s content assessment unavailable", f.Name)
			return "未完成"
		}
		return result.Text
	}
	for _, fixture := range fixtures {
		facts, boundary := []anchorcycle.HookFact{}, []anchorcycle.HookFact{}
		for i, text := range fixture.Topic {
			facts = append(facts, anchorcycle.HookFact{ID: fmt.Sprint("topic-", i), Products: []string{"1", "2"}, Data: text})
		}
		for i, text := range fixture.Boundary {
			boundary = append(boundary, anchorcycle.HookFact{ID: fmt.Sprint("boundary-", i), Products: []string{"1", "2"}, Data: text})
		}
		task := anchorcycle.Task{Key: "synthetic-" + fixture.Name, SellingPoint: fixture.Point, Angle: fixture.Angle, AllowedLinksJSON: `["1","2"]`, FactScope: &anchorcycle.TaskFactScope{Facts: facts, ProductBoundary: boundary}}
		if fixture.Name != "熟食" {
			task.AllowedLinksJSON = `["1"]`
			for i := range facts {
				facts[i].Products = []string{"1"}
			}
			for i := range boundary {
				boundary[i].Products = []string{"1"}
			}
		}
		state, err := anchorcycle.New([]anchorcycle.Task{task}, anchorcycle.Config{Rolling: true, MaxChars: 220, BodySegments: 3, TextProfileID: speech.ID, FeedbackID: speech.ID})
		if err != nil {
			t.Fatal(err)
		}
		state.Surface = "offline"
		state.Phase = "body"
		state.TextModel = speech
		state.BaselineJSON = textTestBaseline()
		state.Facts = strings.Join(append(append([]string{}, fixture.Topic...), fixture.Boundary...), "\n")
		if err = state.InstallP0(p0, source); err != nil {
			t.Fatal(err)
		}
		for repeat := 1; repeat <= 3; repeat++ {
			response, messages, err := server.completeCycleText(ctx, &state)
			row := map[string]any{"category": fixture.Name, "kind": "mainline", "repeat": repeat, "fixture": fixture, "messages": messages, "response": response, "plan": state.CurrentAnglePlan()}
			rows = append(rows, row)
			if err != nil {
				t.Errorf("%s mainline request failed", fixture.Name)
				continue
			}
			candidate := anchorcycle.Candidate{ID: anchorcycle.ID(), Text: response.Text, OutputText: response.Text}
			cleanCycleOpeningInstruction(&state, &candidate, "")
			cleanCycleConsecutiveSpeech(&state, &candidate)
			row["final_text"], row["instruction_edits"], row["duplicate_removals"] = candidate.OutputText, candidate.OpeningInstructionEdits, candidate.ExactDuplicateRemovals
			if candidate.OutputText == "" {
				row["skipped"] = "consecutive_exact_duplicate"
				continue
			}
			if err = state.CheckP0Candidate(&candidate); err != nil {
				t.Fatal(err)
			}
			row["p0"] = candidate.P0Report
			row["content_assessment"] = assess(fixture, candidate.OutputText, fixture.Point+"："+fixture.Angle)
			if candidate.P0Report == nil || candidate.P0Report.Error != "" {
				t.Errorf("%s review unavailable", fixture.Name)
			} else if candidate.P0Report.Passed {
				state.Accepted = append(state.Accepted, anchorcycle.Segment{Task: state.Task(), Selected: candidate})
			} else {
				row["skipped"] = "p0_blocked"
			}
		}
		factsText := ""
		for _, text := range append(append([]string{}, fixture.Topic...), fixture.Boundary...) {
			factsText += "- [已确认事实] " + text + "\n"
		}
		gateway := &crossCategoryProbeGateway{gateway: server.speechGateway(), p0: p0}
		worker := decisionexecutor.New(&crossCategoryProbeStore{Store: store, facts: factsText, name: fixture.Name, productContext: fixture.Topic[0] + "\n" + fixture.Boundary[0]}, nil, gateway, nil)
		for _, question := range fixture.Questions {
			result, err := worker.SimulateInteraction(ctx, 14, 15, question, "", nil)
			row := map[string]any{"category": fixture.Name, "kind": "interaction", "fixture": fixture, "question": question, "result": result, "completed": err == nil}
			rows = append(rows, row)
			if err != nil {
				t.Errorf("%s interaction failed", fixture.Name)
				continue
			}
			row["content_assessment"] = assess(fixture, result.Reply, question)
		}
		rows = append(rows, map[string]any{"category": fixture.Name, "kind": "interaction_requests", "calls": gateway.calls})
		archive(false)
		t.Logf("completed category %s", fixture.Name)
	}
	// A room previously configured for another industry is a separate regression.
	fixture := crossCategoryFixtures()[2]
	factsText := ""
	for _, text := range append(append([]string{}, fixture.Topic...), fixture.Boundary...) {
		factsText += "- [已确认事实] " + text + "\n"
	}
	gateway := &crossCategoryProbeGateway{gateway: server.speechGateway(), p0: p0}
	worker := decisionexecutor.New(&crossCategoryProbeStore{Store: store, facts: factsText, name: fixture.Name, oldIndustry: true, productContext: fixture.Topic[0] + "\n" + fixture.Boundary[0]}, nil, gateway, nil)
	for _, question := range fixture.Questions {
		result, err := worker.SimulateInteraction(ctx, 14, 15, question, "", nil)
		row := map[string]any{"category": "旧行业切换到家电", "kind": "interaction", "fixture": fixture, "question": question, "result": result, "completed": err == nil}
		rows = append(rows, row)
		if err != nil {
			t.Errorf("old industry transition failed")
			continue
		}
		row["content_assessment"] = assess(fixture, result.Reply, question)
	}
	rows = append(rows, map[string]any{"category": "旧行业切换到家电", "kind": "interaction_requests", "calls": gateway.calls})
}
