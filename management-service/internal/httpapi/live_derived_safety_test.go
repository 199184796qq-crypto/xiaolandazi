package httpapi

import (
	"encoding/json"
	"strings"
	"testing"
)

func safetyFixture(t *testing.T) (*derivedInput, string, string, string) {
	t.Helper()
	p0, p1, p2, err := loadRiskAnnotationMothers()
	if err != nil {
		t.Fatal(err)
	}
	facts := "# 正式事实包\n方案：测试方案\n- [product/安全性] 零胆固醇\n- [商品卡] 规格=5L；数量=1桶\n边界：只能使用正式材料。"
	wheel := `{"卖点列表":[{"卖点名":"健康品质","目标人群":"家庭做饭人群","痛点":"了解选油","角度":["零胆固醇","5L规格说明"]}]}`
	d, err := buildDerivedInput(facts, wheel, p0, p1, p2)
	if err != nil {
		t.Fatal(err)
	}
	return d, p0, p1, p2
}
func replacementsFor(d *derivedInput, text string) string {
	rows := []map[string]string{}
	for _, f := range d.Fields {
		if d.needsRewrite(f) {
			rows = append(rows, map[string]string{"id": f.ID, "text": text})
		}
	}
	raw, _ := json.Marshal(map[string]any{"replacements": rows})
	return string(raw)
}
func TestDerivedSafetyChangesOnlyDerivedFields(t *testing.T) {
	d, p0, p1, p2 := safetyFixture(t)
	originalFacts, originalWheel := d.Facts, d.WheelJSON
	raw := replacementsFor(d, "查看商品说明")
	changes, err := applyDerivedReplacements(d, raw, "5L规格说明", p0, p1, p2)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 2 {
		t.Fatalf("expected fact + angle changes, got %d", len(changes))
	}
	if d.Facts != originalFacts || d.WheelJSON != originalWheel {
		t.Fatal("original snapshots mutated")
	}
	if strings.Contains(strings.Join(d.lines, "\n"), "零胆固醇") || d.wheel.Items[0].Angles[0] != "查看商品说明" {
		t.Fatal("unsafe derived field retained")
	}
	if d.wheel.Items[0].Angles[1] != "5L规格说明" {
		t.Fatal("untargeted angle changed")
	}
	if d.lines[3] != "- [商品卡] 规格=5L；数量=1桶" {
		t.Fatal("untargeted fact changed")
	}
}
func TestDerivedSafetyDeduplicatesSameProblemAcrossSources(t *testing.T) {
	d, _, _, _ := safetyFixture(t)
	matching := 0
	for _, review := range d.Reviews {
		if review.RuleID == "food.p1.cholesterol_zero" {
			matching++
		}
	}
	if matching != 1 {
		t.Fatalf("same business problem must have one review, got %d: %+v", matching, d.Reviews)
	}
	var hashes []string
	for _, field := range d.Fields {
		if field.Text != "零胆固醇" {
			continue
		}
		for _, hit := range field.Hits {
			hashes = append(hashes, derivedClaimHash(field, hit, d.Hashes))
		}
	}
	if len(hashes) != 2 || hashes[0] != hashes[1] {
		t.Fatalf("equivalent problems from different sources must share a fingerprint: %v", hashes)
	}
}
func TestDerivedSafetyRejectsNewRiskAndNumbers(t *testing.T) {
	for _, text := range []string{"绝对安全", "有机产品", "一天销量9000单", "保证治愈"} {
		t.Run(text, func(t *testing.T) {
			d, p0, p1, p2 := safetyFixture(t)
			raw := replacementsFor(d, text)
			if _, err := applyDerivedReplacements(d, raw, "5L规格说明", p0, p1, p2); err == nil {
				t.Fatal("unsafe replacement accepted")
			}
			if d.lines[2] != "- [product/安全性] 零胆固醇" || d.wheel.Items[0].Angles[0] != "零胆固醇" {
				t.Fatal("rejected patch mutated fields")
			}
		})
	}
}
func TestDerivedSafetyProofDoesNotApproveP0AndSourceChangeInvalidates(t *testing.T) {
	d, _, _, _ := safetyFixture(t)
	for _, r := range d.Reviews {
		if r.Severity == "P1" {
			r.Status = "confirmed"
			r.Evidence = "商品标签与批次检验报告"
			d.Statuses[r.ClaimSHA256] = r
		}
	}
	for _, f := range d.Fields {
		if len(f.Hits) > 0 && d.needsRewrite(f) {
			t.Fatal("verified exact P1 claim not honored")
		}
	}
	old := d.fingerprint("mother")
	d.Facts += "\nchanged"
	if old == d.fingerprint("mother") {
		t.Fatal("stale safety hash")
	}
	// Mere wording changes no longer invalidate a same-scope atomic attribute.
	changed, p0, p1, p2 := safetyFixture(t)
	changed, err := buildDerivedInput(changed.Facts+"\n- [批次/批号] 20261008", changed.WheelJSON, p0, p1, p2)
	if err != nil {
		t.Fatal(err)
	}
	changed.Statuses = d.Statuses
	for _, f := range changed.Fields {
		if len(f.Hits) > 0 && !changed.needsRewrite(f) {
			t.Fatal("old confirmation reused for changed batch")
		}
	}
}
func TestDerivedSafetyP2ConfirmationReleasesButP0NeverDoes(t *testing.T) {
	p0, p1, p2, err := loadRiskAnnotationMothers()
	if err != nil {
		t.Fatal(err)
	}
	p2Input, err := buildDerivedInput("# 正式事实包\n- [表达] 买到就是赚到", "", p0, p1, p2)
	if err != nil {
		t.Fatal(err)
	}
	for _, review := range p2Input.Reviews {
		if review.Severity == "P2" {
			review.Status = "confirmed"
			review.Evidence = "用户已确认该项P2表达可以使用"
			p2Input.Statuses[review.ClaimSHA256] = review
		}
	}
	for _, field := range p2Input.Fields {
		if len(field.Hits) > 0 && p2Input.needsRewrite(field) {
			t.Fatal("confirmed P2 expression was not released")
		}
	}

	p0Input, err := buildDerivedInput("# 正式事实包\n- [表达] 加微信购买", "", p0, p1, p2)
	if err != nil {
		t.Fatal(err)
	}
	for _, review := range p0Input.Reviews {
		if review.Severity != "P0" {
			continue
		}
		if !strings.Contains(review.Reason, "程序命中规则") || !strings.Contains(review.Reason, review.RuleID) {
			t.Fatal("P0 handling reason is missing", review.Reason)
		}
		review.Status = "confirmed"
		review.Evidence = "伪造的人工确认"
		p0Input.Statuses[review.ClaimSHA256] = review
	}
	foundBlocked := false
	for _, field := range p0Input.Fields {
		if len(field.Hits) > 0 && p0Input.needsRewrite(field) {
			foundBlocked = true
		}
	}
	if !foundBlocked {
		t.Fatal("P0 was released by a forged confirmation")
	}
}

func TestDerivedSafetyContextualP0NeedsSemanticAndCustomerDecision(t *testing.T) {
	p0, p1, p2, err := loadRiskAnnotationMothers()
	if err != nil {
		t.Fatal(err)
	}
	input, err := buildDerivedInput("# 正式事实包\n- [identity_location/经营说明] 这项记录写的是老字号年数70年", "", p0, p1, p2)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, review := range input.Reviews {
		if review.RuleID != "l1.p0.absolute_claim" {
			continue
		}
		found = true
		if review.SemanticDecision != "pending" {
			t.Fatalf("semantic decision=%q", review.SemanticDecision)
		}
		input.Statuses[review.ClaimSHA256] = review
		for _, field := range input.Fields {
			if !input.needsRewrite(field) {
				continue
			}
			approved := review
			approved.SemanticDecision = "pass"
			approved.Status = "confirmed"
			approved.Evidence = "用户确认当前语境仅为记录说明"
			input.Statuses[review.ClaimSHA256] = approved
			if input.needsRewrite(field) {
				t.Fatal("semantic pass plus user decision was not released")
			}
		}
	}
	if !found {
		t.Fatal("老字号 contextual review was not registered")
	}
}

func TestDerivedSafetyP1EvidenceRuleAbsorbsOverlappingContextualP0(t *testing.T) {
	p0, p1, p2, err := loadRiskAnnotationMothers()
	if err != nil {
		t.Fatal(err)
	}
	input, err := buildDerivedInput("# 正式事实包\n- [identity_location/品牌说明] 品牌为70年老字号", "", p0, p1, p2)
	if err != nil {
		t.Fatal(err)
	}
	p0Count, p1Count := 0, 0
	for _, review := range input.Reviews {
		if review.Severity == "P0" {
			p0Count++
		}
		if review.RuleID == "food.p1.business_years" {
			p1Count++
		}
	}
	if p0Count != 0 || p1Count != 1 {
		t.Fatalf("overlapping contextual P0 was not absorbed by one P1 evidence card: p0=%d p1=%d reviews=%+v", p0Count, p1Count, input.Reviews)
	}
}
func TestDerivedSafetyRemovesUnsafeFactLines(t *testing.T) {
	d, _, _, _ := safetyFixture(t)
	// 未改写且仍命中风险的 facts/* 字段，其所在整行应从安全事实包剔除。
	removed, risks := d.removeUnsafeFactLines()
	if removed == 0 {
		t.Fatal("expected unsafe fact line to be removed")
	}
	if len(risks) != removed {
		t.Fatalf("removed count %d != risk details %d", removed, len(risks))
	}
	for _, risk := range risks {
		if risk.Area != "knowledge" {
			t.Fatalf("removed risk area=%q, want knowledge", risk.Area)
		}
		if risk.ClaimSHA256 == "" || risk.Text == "" || risk.Reason == "" {
			t.Fatalf("removed risk missing fields: %+v", risk)
		}
	}
	if strings.Contains(strings.Join(d.lines, "\n"), "零胆固醇") {
		t.Fatal("unsafe fact line retained after removal")
	}
	// 已被确认释放（不再 needsRewrite）的事实行不应被剔除。
	released, _, _, _ := safetyFixture(t)
	for _, review := range released.Reviews {
		if review.Severity != "P1" {
			continue
		}
		review.Status = "confirmed"
		review.Evidence = "商品标签与批次检验报告"
		released.Statuses[review.ClaimSHA256] = review
	}
	keptBefore := len(released.lines)
	if got, _ := released.removeUnsafeFactLines(); got != 0 {
		t.Fatalf("confirmed-safe facts were removed: %d", got)
	}
	if len(released.lines) != keptBefore {
		t.Fatal("released fact line should be preserved")
	}
}

func TestDerivedSafetyRemovesUnsafeFactLinesSkipsWheel(t *testing.T) {
	d, _, _, _ := safetyFixture(t)
	// 只应剔除 facts/* 字段所在行；wheel/* 卖点字段保持结构化，不按行删除。
	removed, _ := d.removeUnsafeFactLines()
	if removed == 0 {
		t.Fatal("expected facts to be removed")
	}
	if len(d.wheel.Items) == 0 {
		t.Fatal("wheel items should remain intact")
	}
}

func TestDerivedSafetyRejectsMissingOrDuplicateTargets(t *testing.T) {
	for _, raw := range []string{`{"replacements":[]}`, `{"replacements":[{"id":"facts/2","text":"查看说明"},{"id":"facts/2","text":"查看说明"}]}`, `{"replacements":[{"id":"not-a-target","text":"查看说明"}]}`, `{"replacements":[],"approval":true}`} {
		d, p0, p1, p2 := safetyFixture(t)
		if _, err := applyDerivedReplacements(d, raw, "", p0, p1, p2); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
}
