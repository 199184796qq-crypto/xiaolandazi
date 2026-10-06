package agentunderstanding

import (
	"testing"
	"time"

	"livecompanion/management/internal/model"
)

func TestUseModelModes(t *testing.T) {
	program := DefaultPolicy()
	program.Mode = model.AgentUnderstandingModeProgram
	if UseModel(program, false, 0) {
		t.Fatal("program mode must never call model")
	}
	modelPolicy := DefaultPolicy()
	modelPolicy.Mode = model.AgentUnderstandingModeModel
	if !UseModel(modelPolicy, true, 1) {
		t.Fatal("model mode must call model")
	}
	auto := DefaultPolicy()
	auto.Mode = model.AgentUnderstandingModeAuto
	auto.MinConfidence = 0.8
	if UseModel(auto, true, 0.95) {
		t.Fatal("auto should accept high-confidence program result")
	}
	if !UseModel(auto, false, 0.2) {
		t.Fatal("auto should escalate unresolved result")
	}
}

func TestProgramLiveStrategyBenefitStartNow(t *testing.T) {
	now := time.Date(2026, 9, 28, 9, 30, 0, 0, time.FixedZone("CST", 8*3600))
	out := ProgramInterpretLiveStrategy("修改1号链接活动 开始时间：现在", LiveStrategyProgramContext{
		CurrentMode: "benefits",
		Now:         now,
		Benefits: []LiveStrategyBenefit{{
			Key: "current-benefit", LinkKey: "1号链接", ProductName: "试用装",
		}},
	})
	if out.Kind != KindCommand || out.Intent != "benefit.update" {
		t.Fatalf("unexpected intent: %#v", out)
	}
	if got := out.Changes["starts_at"]; got != "2026-09-28 09:30" {
		t.Fatalf("starts_at=%v", got)
	}
	if got := out.Target["benefit_key"]; got != "current-benefit" {
		t.Fatalf("benefit target=%v", got)
	}
}

func TestProgramLiveStrategyBenefitProductNameRename(t *testing.T) {
	ctx := LiveStrategyProgramContext{
		CurrentMode: "benefits",
		Benefits:    []LiveStrategyBenefit{{Key: "1号链接:current-benefit", LinkKey: "1号链接", ProductName: "试用装2斤"}},
	}
	out := ProgramInterpretLiveStrategy("把1号链接的试用装2斤改成试用装5斤", ctx)
	if out.Kind != KindCommand || out.Intent != "benefit.update" {
		t.Fatalf("unexpected intent: %#v", out)
	}
	if got := out.Changes["product_name"]; got != "试用装5斤" {
		t.Fatalf("product_name=%v", got)
	}
	if got := out.Target["benefit_key"]; got != "1号链接:current-benefit" {
		t.Fatalf("benefit target=%v", got)
	}
}

func TestProgramLiveStrategyAmbiguousLinkUpdate(t *testing.T) {
	out := ProgramInterpretLiveStrategy("修改1号链接", LiveStrategyProgramContext{
		CurrentMode: "benefits",
		Products:    []LiveStrategyProduct{{LinkKey: "1号链接"}},
		Benefits:    []LiveStrategyBenefit{{Key: "b1", LinkKey: "1号链接"}},
	})
	if out.Kind != KindClarify || out.Intent != "unknown" {
		t.Fatalf("ambiguous update must clarify: %#v", out)
	}
}

func TestProgramLiveStrategyProductSpec(t *testing.T) {
	out := ProgramInterpretLiveStrategy("修改1号链接 规格：5L", LiveStrategyProgramContext{
		CurrentMode: "products",
		Products:    []LiveStrategyProduct{{LinkKey: "1号链接", Spec: "2L"}},
	})
	if out.Kind != KindCommand || out.Intent != "product.update" {
		t.Fatalf("unexpected: %#v", out)
	}
	if got := out.Changes["spec"]; got != "5L" {
		t.Fatalf("spec=%v", got)
	}
}

func TestProgramLiveStrategyProductAttributeCRUD(t *testing.T) {
	ctx := LiveStrategyProgramContext{CurrentMode: "products", Products: []LiveStrategyProduct{{LinkKey: "1号链接", Attributes: []LiveStrategyProductAttribute{{ID: 7, Code: "pressing_process", Label: "压榨工艺", Value: "传统熟榨"}}}}}
	add := ProgramInterpretLiveStrategy("给1号链接添加个性属性：原料=非转基因菜籽", ctx)
	if add.Intent != "product_attribute.add" || add.Changes["attribute_label"] != "原料" || add.Changes["attribute_value"] != "非转基因菜籽" {
		t.Fatalf("add attribute intent=%+v", add)
	}
	update := ProgramInterpretLiveStrategy("修改1号链接个性属性压榨工艺为小榨熟香", ctx)
	if update.Intent != "product_attribute.update" || update.Changes["attribute_value"] != "小榨熟香" {
		t.Fatalf("update attribute intent=%+v", update)
	}
	remove := ProgramInterpretLiveStrategy("删除1号链接个性属性压榨工艺", ctx)
	if remove.Intent != "product_attribute.disable" || remove.Target["attribute_label"] != "压榨工艺" {
		t.Fatalf("disable attribute intent=%+v", remove)
	}
}

func TestProgramLiveStrategyFactAdd(t *testing.T) {
	out := ProgramInterpretLiveStrategy("添加事实 发货物流-快递方式：顺丰", LiveStrategyProgramContext{CurrentMode: "knowledge"})
	if out.Kind != KindCommand || out.Intent != "fact.add" {
		t.Fatalf("unexpected: %#v", out)
	}
	if out.Target["fact_category"] != "发货物流" || out.Target["fact_key"] != "快递方式" || out.Changes["fact_value"] != "顺丰" {
		t.Fatalf("bad fact parse: %#v", out)
	}
}

func TestProgramLiveStrategyAnswersCourierFromFormalFact(t *testing.T) {
	out := ProgramInterpretLiveStrategy("你们发什么快递？", LiveStrategyProgramContext{
		CurrentMode: "plan",
		Facts: []LiveStrategyFact{{
			Category: "发货物流",
			Key:      "快递方式",
			Value:    "顺丰",
		}},
	})
	if out.Kind != KindChat || out.Intent != "chat" {
		t.Fatalf("courier query must be read-only chat: %#v", out)
	}
	if out.Reply != "快递方式：顺丰。" {
		t.Fatalf("unexpected courier reply: %q", out.Reply)
	}
}

func TestProgramLiveStrategyCourierQueryNeverGuesses(t *testing.T) {
	out := ProgramInterpretLiveStrategy("你们发什么快递？", LiveStrategyProgramContext{CurrentMode: "plan"})
	if out.Kind != KindChat || out.Intent != "chat" {
		t.Fatalf("missing courier fact must remain read-only chat: %#v", out)
	}
	if out.Reply == "" || !containsAnyProgram(out.Reply, "没有已确认", "不会猜") {
		t.Fatalf("missing courier fact must explicitly avoid guessing: %#v", out)
	}
}

func TestProgramLiveStrategyNaturalCourierCorrection(t *testing.T) {
	out := ProgramInterpretLiveStrategy("快递改成圆通", LiveStrategyProgramContext{
		CurrentMode: "plan",
		Facts: []LiveStrategyFact{{
			Category: "发货物流",
			Key:      "快递方式",
			Value:    "顺丰",
		}},
	})
	if out.Kind != KindCommand || out.Intent != "fact.update" {
		t.Fatalf("natural courier correction must update formal fact: %#v", out)
	}
	if out.Target["fact_category"] != "发货物流" || out.Target["fact_key"] != "快递方式" {
		t.Fatalf("unexpected courier correction target: %#v", out.Target)
	}
	if out.Changes["fact_value"] != "圆通" {
		t.Fatalf("unexpected courier correction value: %#v", out.Changes)
	}
}

func TestProgramLiveStrategyAnswersProductPrice(t *testing.T) {
	out := ProgramInterpretLiveStrategy("1号链接多少钱？", LiveStrategyProgramContext{
		CurrentMode: "products",
		Products: []LiveStrategyProduct{{
			LinkKey: "1号链接", ProductName: "菜籽油", DailyPrice: "99元",
		}},
	})
	if out.Kind != KindChat || out.Intent != "chat" || out.Reply != "1号链接：日常价：99元。" {
		t.Fatalf("unexpected product price answer: %#v", out)
	}
}

func TestProgramLiveStrategyUpdatesStableRoomPosition(t *testing.T) {
	out := ProgramInterpretLiveStrategy("把1号链接的直播间定位设为主推款和利润款", LiveStrategyProgramContext{
		CurrentMode: "products",
		Products:    []LiveStrategyProduct{{LinkKey: "1号链接", ProductName: "菜籽油", RoomRoles: []string{"ordinary"}}},
	})
	if out.Kind != KindCommand || out.Intent != "product.update" {
		t.Fatalf("room positioning must be a product update: %#v", out)
	}
	roles, ok := out.Changes["room_roles"].([]string)
	if !ok || len(roles) != 2 || roles[0] != "main" || roles[1] != "profit" {
		t.Fatalf("unexpected room roles: %#v", out.Changes)
	}
}

func TestProgramLiveStrategyRemovesOneRoomPosition(t *testing.T) {
	out := ProgramInterpretLiveStrategy("去掉1号链接的福利定位", LiveStrategyProgramContext{
		CurrentMode: "products",
		Products:    []LiveStrategyProduct{{LinkKey: "1号链接", ProductName: "菜籽油", RoomRoles: []string{"main", "benefit"}}},
	})
	roles, ok := out.Changes["room_roles"].([]string)
	if out.Kind != KindCommand || !ok || len(roles) != 1 || roles[0] != "main" {
		t.Fatalf("removal must return the complete remaining positioning: %#v", out)
	}
}

func TestProgramLiveStrategyAnswersSelectedPlan(t *testing.T) {
	out := ProgramInterpretLiveStrategy("现在用哪个方案？", LiveStrategyProgramContext{
		Plans: []LiveStrategyPlan{
			{ID: 1, Name: "默认方案", Bound: true},
			{ID: 2, Name: "菜籽油直播方案", Bound: true, Selected: true},
		},
	})
	if out.Kind != KindChat || out.Reply != "当前直播间正在使用方案“菜籽油直播方案”。" {
		t.Fatalf("unexpected selected plan answer: %#v", out)
	}
}

func TestProgramLiveStrategyPlanSwitch(t *testing.T) {
	out := ProgramInterpretLiveStrategy("切换到菜籽油直播方案", LiveStrategyProgramContext{
		CurrentMode: "plan",
		Plans:       []LiveStrategyPlan{{ID: 2, Name: "菜籽油直播方案", Bound: true}},
	})
	if out.Kind != KindCommand || out.Intent != "plan.switch" || out.Target["plan_id"] != int64(2) {
		t.Fatalf("unexpected: %#v", out)
	}
}

func TestProgramLiveStrategyCancelNeverMutates(t *testing.T) {
	out := ProgramInterpretLiveStrategy("不用了", LiveStrategyProgramContext{CurrentMode: "products"})
	if out.Kind != KindChat || out.Intent != "chat" {
		t.Fatalf("cancel should be chat: %#v", out)
	}
}

func TestProgramModeImageClarifiesWithoutModel(t *testing.T) {
	out := ProgramInterpretLiveStrategy("把这张图加进去", LiveStrategyProgramContext{HasImages: true})
	if out.Kind != KindClarify || len(out.Missing) == 0 {
		t.Fatalf("image should clarify in program engine: %#v", out)
	}
}

func TestProgramSystemNavigation(t *testing.T) {
	out := ProgramInterpretSystem("打开智能体理解配置", SystemProgramContext{
		Navigation: []SystemNavigation{{Title: "智能体理解配置", To: "/system/settings/agent-routing"}},
	})
	if out.Kind != KindCommand || out.Intent != "NAVIGATE_PAGE" || out.Target["to"] != "/system/settings/agent-routing" {
		t.Fatalf("unexpected navigation: %#v", out)
	}
}

func TestProgramSystemStaffQuery(t *testing.T) {
	out := ProgramInterpretSystem("查看销售组员工名单", SystemProgramContext{Groups: []string{"销售组"}})
	if out.Kind != KindCommand || out.Intent != "QUERY_STAFF" || out.Changes["group_name"] != "销售组" {
		t.Fatalf("unexpected staff query: %#v", out)
	}
}

func TestProgramSystemCreateEmployeeClarifiesMissing(t *testing.T) {
	out := ProgramInterpretSystem("新增员工 张三", SystemProgramContext{Groups: []string{"销售组"}, Roles: []string{"销售"}})
	if out.Kind != KindClarify || out.Intent != "CREATE_STAFF_EMPLOYEE" || len(out.Missing) == 0 {
		t.Fatalf("missing fields should clarify: %#v", out)
	}
}
