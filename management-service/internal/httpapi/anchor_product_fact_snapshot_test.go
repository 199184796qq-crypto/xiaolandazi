package httpapi

import (
	"context"
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"

	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/speechbase"
)

const couponFactBlock = "- [use_conditions/核销限制] 每桌限用1张券，不与店内优惠同享；安全改写=【必说】一桌用一张券\n【可说】不需要预约，高峰期可能排队\n；禁说=任意叠加"

func independentFactSnapshotFixture() (*anchorcycle.State, []model.LiveAgentPlanProductLink) {
	var links []model.LiveAgentPlanProductLink
	lines := []string{"# 正式事实包", "方案：多商品测试", couponFactBlock,
		"- [identity_location/门店] 门店有停车位",
		"- [other/专属配置] 链接=2号链接；二号专属甜点",
		"- [有效福利] 链接=1号链接、2号链接；活动=指定套餐赠饮品"}
	for i, key := range []string{"1号链接", "2号链接", "3号链接"} {
		links = append(links, model.LiveAgentPlanProductLink{LinkKey: key, ProductName: key + "套餐", Status: "active", FrequencyWeight: 110,
			RoomRoles: []string{"ordinary"}, SellingPoints: []model.LiveAgentPlanProductSellingPoint{
				{ID: int64(10 + i), SellingPoint: "核销说明", Status: "active", PointWeight: 7,
					Angles: []model.LiveAgentPlanProductSellingAngle{{Angle: "每桌限用一张券", Weight: 2}}},
			}})
		lines = append(lines, "- [商品卡] 链接="+key+"；商品="+key+"套餐")
	}
	lines = append(lines, anchorWorkbenchFactsBoundary)
	return &anchorcycle.State{Facts: strings.Join(lines, "\n"), Phase: "opening", CycleNumber: 1}, links
}

// Exercise the real builder, JSON version boundary and shared save/start
// preparation, then inspect the actual material views used before generation.
func TestIndependentProductFactSnapshotSurvivesSaveAndStartup(t *testing.T) {
	ctx := context.Background()
	input, links := independentFactSnapshotFixture()
	beforeLinks, _ := json.Marshal(links)
	server := &Server{} // independent pools must not require a model/database
	focus, err := independentSnapshotFocus(input, links)
	if err != nil {
		t.Fatal(err)
	}
	if len(focus.Facts) != 7 || focus.FactsSHA != derivedHash(input.Facts) {
		t.Fatalf("save lost source facts: %+v", focus.Facts)
	}
	scheme := liveAnchorScheme{Version: 1, ProductFocus: focus, Config: anchorcycle.Config{Rolling: true, MaxChars: 300}}
	saved, _ := json.Marshal(scheme)
	var reloaded liveAnchorScheme
	if err = json.Unmarshal(saved, &reloaded); err != nil {
		t.Fatal(err)
	}
	safety := model.LiveDerivedSafetyAsset{SafeFacts: input.Facts, SafeWheelJSON: `{"卖点列表":[{"卖点名":"旧池","角度":["旧角度"]}]}`}
	state, err := server.prepareSavedAnchorTaskState(ctx, 14, 5, nil, reloaded, safety)
	if err != nil || len(state.Queue) != 3 || state.Status != "ready" {
		t.Fatal("saved snapshot cannot start", err)
	}
	for _, task := range state.Queue {
		if task.PointWeight != 7 || task.AngleWeight != 2 {
			t.Fatal("save changed independent pool weights", task)
		}
		state.Phase = "body"
		state.ProductFocus.CurrentLink = task.LinkKey
		state.SelectProductTask()
		active := state.Task()
		material := state.ProductFacts(active)
		if !strings.Contains(material, couponFactBlock) || !strings.Contains(material, "门店有停车位") {
			t.Fatal("shared facts or multiline guidance lost", material)
		}
		if strings.Contains(material, "二号专属甜点") != (task.LinkKey == "2号链接") || strings.Contains(material, "指定套餐赠饮品") != (task.LinkKey != "3号链接") {
			t.Fatal("explicit fact or multi-link benefit leaked/dropped", task.LinkKey, material)
		}
		want, got := productFactBlocks(productFactPackage(task.LinkKey, input.Facts)), productFactBlocks(material)
		sort.Strings(want)
		sort.Strings(got)
		if !reflect.DeepEqual(want, got) {
			t.Fatal("initialization and saved generation use different facts", task.LinkKey, want, got)
		}
		content, guidance := speechbase.GenerationMaterialViews(state.HookFacts())
		contents, _ := json.Marshal(content)
		guides, _ := json.Marshal(guidance)
		if !strings.Contains(string(contents), "每桌限用1张券") || strings.Contains(string(contents), "任意叠加") || !strings.Contains(string(guides), "一桌用一张券") || !strings.Contains(string(guides), "高峰期可能排队") || !strings.Contains(string(guides), "任意叠加") {
			t.Fatal("four-layer fact/guidance material lost", string(contents), string(guides))
		}
	}
	after, _ := json.Marshal(reloaded)
	afterLinks, _ := json.Marshal(links)
	if string(after) != string(saved) || string(beforeLinks) != string(afterLinks) {
		t.Fatal("startup rewrote the immutable scheme or product pools")
	}
}

func TestIndependentProductFactSnapshotRejectsOmissionsAndInvalidOwnership(t *testing.T) {
	input, links := independentFactSnapshotFixture()
	for _, invalid := range []string{
		strings.Replace(input.Facts, "链接=1号链接、2号链接", "链接=1号链接、未知链接", 1), // same count must not imply shared
		input.Facts + "\n- [other/专属] 链接=未知链接；错误资料",
	} {
		probe := *input
		probe.Facts = invalid
		if _, err := independentSnapshotFocus(&probe, links); err == nil {
			t.Fatal("unknown product scope accepted")
		}
	}
	focus, err := independentSnapshotFocus(input, links)
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*anchorcycle.ProductFocus){
		func(f *anchorcycle.ProductFocus) { f.Facts = f.Facts[:len(f.Facts)-1] },
		func(f *anchorcycle.ProductFocus) { f.Facts = append(f.Facts, f.Facts[0]) },
		func(f *anchorcycle.ProductFocus) {
			for i := range f.Facts {
				f.Facts[i].Text = strings.ReplaceAll(f.Facts[i].Text, "\n【可说】不需要预约，高峰期可能排队", "")
			}
		},
		func(f *anchorcycle.ProductFocus) {
			for i := range f.Facts {
				if len(f.Facts[i].Links) > 0 {
					f.Facts[i].Shared, f.Facts[i].Links = true, nil
					break
				}
			}
		},
	} {
		raw, _ := json.Marshal(focus)
		var damaged anchorcycle.ProductFocus
		_ = json.Unmarshal(raw, &damaged)
		change(&damaged)
		if err = validateProductFactSnapshot(input.Facts, &damaged); err == nil {
			t.Fatal("incomplete/changed fact snapshot accepted despite matching source hash")
		}
	}
}

func TestProductFactBlocksKeepContinuationAndExcludeDocumentBoundary(t *testing.T) {
	complete := couponFactBlock + "\n边界：周末不适用"
	facts := "# 标题\r\n方案：测试\r\n" + strings.ReplaceAll(complete, "\n", "\r\n") + "\r\n\r\n" + anchorWorkbenchFactsBoundary
	if got := productFactBlocks(facts); len(got) != 1 || got[0] != complete {
		t.Fatal("multiline fact was split or document text became a fact", got)
	}
}

func TestSavedProductFocusMultilineLabelRebindKeepsConditionsImmutable(t *testing.T) {
	old := couponFactBlock
	changedLabel := strings.Replace(old, "/核销限制]", "/使用规则]", 1)
	saved := &anchorcycle.ProductFocus{FactsSHA: derivedHash(old), Facts: []anchorcycle.ScopedFact{{Text: old, Shared: true}}}
	bound, err := savedProductFocusForFacts(saved, changedLabel)
	if err != nil || len(bound.Facts) != 1 || bound.Facts[0].Text != changedLabel || saved.Facts[0].Text != old {
		t.Fatal("label rebind lost multiline conditions or mutated original", bound, err)
	}
	changedCondition := strings.Replace(changedLabel, "高峰期可能排队", "无需排队", 1)
	bound, err = savedProductFocusForFacts(saved, changedCondition)
	if err != nil || len(bound.Facts) != 0 {
		t.Fatal("changed continuation inherited saved applicability", bound, err)
	}
	legacy := &anchorcycle.ProductFocus{FactsSHA: derivedHash(old), Facts: []anchorcycle.ScopedFact{}}
	bound, err = savedProductFocusForFacts(legacy, old)
	if err != nil || len(bound.Facts) != 0 {
		t.Fatal("legacy snapshot was filled retroactively", bound, err)
	}
}

func independentSnapshotFocus(state *anchorcycle.State, links []model.LiveAgentPlanProductLink) (*anchorcycle.ProductFocus, error) {
	facts, err := classifyProductCommonFacts(context.Background(), state.Facts, links, func(context.Context, map[string]any) (string, error) { return `{"归属":"公共"}`, nil })
	if err != nil {
		return nil, err
	}
	return buildClassifiedProductFocus(state, links, facts, anchorStyleSellingPointWheel{Items: []anchorStyleSellingPointWheelItem{{Name: "商品", Scope: "商品", Angles: []string{"商品角度"}}}})
}
