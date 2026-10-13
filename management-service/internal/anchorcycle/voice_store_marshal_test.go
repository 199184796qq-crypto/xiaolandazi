package anchorcycle

import (
	"encoding/json"
	"testing"

	"livecompanion/management/internal/anchorstylescore"
)

// TestMarshalRunKeepsOutputTextForReceipt 验证 marshalRun 剥离调试字段时保留 OutputText，
// 使文字审核回执在 Redis 保存/读取后身份一致，声音阶段 CheckApprovedText 不被误判。
func TestMarshalRunKeepsOutputTextForReceipt(t *testing.T) {
	s := p0Fixture(t, 2)
	c := candidate("开袋后建议尽快吃完。", 100)
	c.OutputText = "开袋后建议尽快吃完，口感更好。"
	if err := s.CheckP0Candidate(&c); err != nil || !c.P0Report.Passed {
		t.Fatal("candidate 未通过 P0 审核", err)
	}
	if ok, err := s.AddCandidate(c, anchorstylescore.Baseline{}); !ok || err != nil {
		t.Fatal("candidate 未采纳", ok, err)
	}

	raw, err := marshalRun(&s)
	if err != nil {
		t.Fatal(err)
	}
	var restored State
	if err := json.Unmarshal(raw, &restored); err != nil {
		t.Fatal(err)
	}

	adopted := restored.Accepted[0].Selected
	if adopted.OutputText == "" {
		t.Fatal("marshalRun 误清空了 OutputText，声音阶段回执将失配")
	}
	if restored.CheckApprovedText(adopted, adopted.OutputText) != nil {
		t.Fatal("保存/读取后文字审核回执失配，会被误判跳过")
	}
	// 调试大字段应仍被剥离（防 Redis 撑爆）
	if adopted.Prompt != "" || adopted.RawText != "" {
		t.Fatal("Prompt/RawText 调试字段未剥离")
	}
	if len(restored.Accepted[0].Attempts) != 0 {
		t.Fatal("Attempts 未剥离")
	}
}
