package httpapi

import (
	"livecompanion/management/internal/model"
	"reflect"
	"strings"
	"testing"
)

func TestRiskOriginsDoNotChangeFactsClaimsOrConfirmation(t *testing.T) {
	p0, p1, p2, err := loadRiskAnnotationMothers()
	if err != nil {
		t.Fatal(err)
	}
	facts := []model.LiveAgentPlanFact{{ID: 11, Category: "product", Key: "指标", Value: "低芥酸", Status: "active"}, {ID: 12, Category: "other", Key: "说明", Value: "第一行\n第二行", Status: "active"}}
	links := []model.LiveAgentPlanProductLink{{ID: 21, LinkKey: "1号链接", ProductName: "菜籽油", Status: "active", Attributes: []model.LiveAgentPlanProductAttribute{{ID: 31, Label: "健康/营养型卖点", Value: "低芥酸、高油酸", Status: "active"}}}}
	benefits := []model.LiveAgentPlanBenefit{{ID: 41, Key: "活动", Activity: "全网最低", ValidityMode: "permanent"}}
	plan := model.LiveAgentPlan{Name: "测试方案"}
	text, origins := renderAnchorStyleWorkbenchFactsWithOrigins(plan, facts, benefits, links)
	if text != renderAnchorStyleWorkbenchFacts(plan, facts, benefits, links) {
		t.Fatal("origin sidecar changed model facts")
	}
	wheel := `{"卖点列表":[{"卖点名":"原料","角度":["低芥酸菜籽油"]}]}`
	d, err := buildDerivedInput(text, wheel, p0, p1, p2)
	if err != nil {
		t.Fatal(err)
	}
	hashes := []string{}
	for _, r := range d.Reviews {
		hashes = append(hashes, r.ClaimSHA256)
	}
	fingerprint := d.fingerprint("test")
	attachRiskSourceOrigins(d, origins)
	after := []string{}
	for _, r := range d.Reviews {
		after = append(after, r.ClaimSHA256)
	}
	if !reflect.DeepEqual(hashes, after) || d.fingerprint("test") != fingerprint {
		t.Fatal("routing changed claim identity or source fingerprint")
	}
	found := map[string]bool{}
	for _, r := range d.Reviews {
		for _, s := range r.Sources {
			switch s.Area {
			case "products":
				if s.EntityID != 21 {
					t.Fatal(s)
				}
				if strings.Contains(s.Text, "高油酸") {
					if s.Field != "健康/营养型卖点" {
						t.Fatal(s)
					}
					found["high_oleic_product"] = true
				}
			case "knowledge":
				if s.EntityID != 11 && s.EntityID != 12 {
					t.Fatal(s)
				}
				found["fact"] = true
			case "benefits":
				if s.EntityID != 41 {
					t.Fatal(s)
				}
				found["benefit"] = true
			case "miner":
				found["wheel"] = true
			default:
				t.Fatalf("lost actual source: %+v", s)
			}
		}
	}
	for _, key := range []string{"high_oleic_product", "fact", "benefit", "wheel"} {
		if !found[key] {
			t.Fatal("missing origin", key)
		}
	}
}
