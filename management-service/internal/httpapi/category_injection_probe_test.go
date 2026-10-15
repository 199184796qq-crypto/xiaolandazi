package httpapi

import (
	"context"
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/config"
	appdb "livecompanion/management/internal/db"
)

// 显式品类注入的本地验收：喂食品/建材两份事实包，验证自动判品类 + 显式注入后卖点挖掘
// 是否覆盖对应品类的核心维度。只读模型、不写任何方案/轮盘/声音。
func TestCategoryInjectionProbe(t *testing.T) {
	if os.Getenv("CATEGORY_INJECTION_PROBE") != "1" {
		t.Skip("explicit category injection probe required")
	}
	tenant, _ := strconv.ParseInt(os.Getenv("PROBE_TENANT_ID"), 10, 64)
	plan, _ := strconv.ParseInt(os.Getenv("PROBE_PLAN_ID"), 10, 64)
	store, err := appdb.Open(config.Load())
	if err != nil {
		t.Fatal("database unavailable")
	}
	defer store.Close()
	s := &Server{store: store}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	profile := strings.TrimSpace(os.Getenv("PROBE_PROFILE_ID"))
	if profile == "" {
		settings, err := store.AnchorGenerationSettings(ctx)
		if err != nil {
			t.Fatal(err)
		}
		profile = settings.SellingPointProfileID
	}
	if profile == "" {
		t.Fatal("selling-point profile required")
	}

	cases := []struct {
		name string
		fact string
	}{
		{"food", foodProbeFacts},
		{"building", buildingProbeFacts},
	}
	prompt, _, err := loadSellingPointExtractionPrompt()
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		cat, err := s.detectProductCategory(ctx, tenant, plan, profile, c.fact)
		if err != nil {
			t.Fatalf("[%s] detect failed: %v", c.name, err)
		}
		injected, err := injectCategorySellingDimensions(prompt, cat)
		if err != nil {
			t.Fatalf("[%s] inject failed: %v", c.name, err)
		}
		user, _ := json.Marshal(map[string]string{"fact_package": c.fact})
		result, _, err := s.completeAnchorStyleWorkbench(ctx, profile, "selling_point_extraction", agentgateway.ResponseJSON, []agentgateway.Message{{Role: "system", Content: strings.ReplaceAll(injected, "{{FACT_PACKAGE}}", "")}, {Role: "user", Content: string(user)}}, 7000, false)
		if err != nil {
			t.Fatalf("[%s] extraction failed: %v", c.name, err)
		}
		t.Logf("\n===== [%s] detected_category=%q =====\n%s", c.name, cat, result.Text)
	}
}

const foodProbeFacts = `# 正式事实包
方案：食品品类测试
- [商品卡] 链接=1；商品=杨鸭子招牌卤鸭整只；规格=900g；日常价=87.6；数量=1；适用=家庭聚餐
- [商品卡] 链接=2；商品=农香王26年新黄菜油；规格=5L；日常价=129.9；数量=1；适用=家庭做饭
- [craft/工艺] 老卤汤慢炖4小时，两遍油炸，裹秘制红油锁汁定型。
- [flavor/口感] 肉紧实有嚼头，油少皮薄无腥味，鲜甜酥脆。
- [ingredients/原料] 八个月土麻鸭，四川南充本地散养。
- [nutrition/营养] 蛋白质足、脂肪少。
- [brand/品牌] 五十多家实体门店，总店在红花街一号经营几十年。
- [price/价格] 1号链接日常价87.6，2号链接日常价129.9。
- [fulfillment/物流] 使用京东物流，川渝隔天到。`

const buildingProbeFacts = `# 正式事实包
方案：建材品类测试
- [商品卡] 链接=1；商品=中喆瓷砖胶C2型；规格=20kg/袋；日常价=68；数量=1；适用=贴墙地砖
- [商品卡] 链接=2；商品=涂润防水砂浆；规格=25kg/袋；日常价=75；数量=1；适用=厨卫防水
- [craft/工艺] 双组分按配比搅拌，齿形刮刀薄贴，降低空鼓。
- [functions/性能] 粘结强度不低于1.0MPa，抗滑移。
- [specification/规格] C2型20kg一袋，可铺贴约8到10平米。
- [usage/用法] 基层清理后按水灰比搅拌，刮涂施工。
- [safety_notice/认证] 通过GB/T 25181标准检测。`
