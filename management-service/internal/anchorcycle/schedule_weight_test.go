package anchorcycle

import "testing"

// 商品频率轮换：所有商品按「角色基准比例 × 手动系数」归一化后加权交错展开。
// 主推不再独占，所有商品平等参与轮换。
func TestProductSequenceNormalized(t *testing.T) {
	p := &ProductFocus{
		Policy: ProductPolicy{},
		Products: []FocusProduct{
			{LinkKey: "1", Roles: []string{"main"}},
			{LinkKey: "2", Roles: []string{"traffic"}},
			{LinkKey: "3", Roles: []string{"benefit"}},
		},
	}
	// 新的轻微偏好：主推、引流、福利；最终商品频率差距不超过20%。
	weights := p.normalizedProductWeights()
	if weights["1"] <= weights["2"] || weights["2"] <= weights["3"] {
		t.Fatalf("角色基准应 main>traffic>benefit，得到 %v", weights)
	}
	total := 0.0
	for _, w := range weights {
		total += w
	}
	if total < 0.9999 || total > 1.0001 {
		t.Fatalf("归一化占比总和应=1，得到 %f", total)
	}
	if weights["1"] > weights["3"]*ProductFrequencyRatio+1e-9 {
		t.Fatal("商品差距越界", weights)
	}
}

// 归一化总和恒等于 1：不管手动系数怎么调，占比总和始终是 1（有增必有减）。
func TestProductWeightsAlwaysNormalizeToOne(t *testing.T) {
	p := &ProductFocus{
		Policy: ProductPolicy{},
		Products: []FocusProduct{
			{LinkKey: "1", Roles: []string{"main"}},
			{LinkKey: "2", Roles: []string{"traffic"}, FrequencyWeight: 200}, // 系数 2.0
			{LinkKey: "3", Roles: []string{"benefit"}, FrequencyWeight: 50},  // 系数 0.5
		},
	}
	weights := p.normalizedProductWeights()
	total := 0.0
	for _, w := range weights {
		total += w
	}
	if total < 0.9999 || total > 1.0001 {
		t.Fatalf("归一化总和应恒=1，得到 %f", total)
	}
	// 系数 2.0 的 traffic 应超过默认 1.0 时的占比；系数 0.5 的 benefit 应被压低。
	base := (&ProductFocus{Policy: ProductPolicy{}, Products: []FocusProduct{
		{LinkKey: "1", Roles: []string{"main"}},
		{LinkKey: "2", Roles: []string{"traffic"}},
		{LinkKey: "3", Roles: []string{"benefit"}},
	}}).normalizedProductWeights()
	if weights["2"] <= base["2"] {
		t.Fatalf("系数 2.0 应放大 traffic 占比：%v vs %v", weights["2"], base["2"])
	}
	if weights["3"] >= base["3"] {
		t.Fatalf("系数 0.5 应缩小 benefit 占比：%v vs %v", weights["3"], base["3"])
	}
}

// Legacy weights remain readable as history, but cannot drive selection.
func TestProductLegacyRoleWeightsDoNotDriveScheduling(t *testing.T) {
	p := &ProductFocus{Policy: ProductPolicy{RoleWeights: map[string]int{"main": 400, "ordinary": 1}}, Products: []FocusProduct{{LinkKey: "1", Roles: []string{"main"}}, {LinkKey: "2", Roles: []string{"ordinary"}}}}
	weights := p.normalizedProductWeights()
	if weights["1"] > weights["2"]*1.2+1e-9 {
		t.Fatal(weights)
	}
	p.Policy.RoleWeights = nil
	if p.normalizedProductWeights()["1"] != weights["1"] {
		t.Fatal("historical ratio still affects new scheduling")
	}
}

// 卖点级权重：角度多的卖点不再霸屏，各卖点按权重（缺省等频）分布。
func TestPointWeightedScheduling(t *testing.T) {
	tasks := []Task{
		{SellingPoint: "口感", Angle: "鲜嫩"},
		{SellingPoint: "口感", Angle: "多汁"},
		{SellingPoint: "口感", Angle: "酥脆"},
		{SellingPoint: "工艺", Angle: "慢炖"},
	}
	s, err := New(tasks, Config{Rolling: true, BodyMin: 2, BodyMax: 6, MaxChars: 200, Threshold: 80, MaxRetries: 1, PointGap: 0, CombinationGap: 0})
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for i := 0; i < 240; i++ {
		s.chooseNext()
		tk := s.Queue[s.TaskIndex]
		s.recordScheduledAdoption(tk)
		counts[tk.SellingPoint]++
		u := s.Used[tk.Key]
		u.Count++
		u.LastIndex = s.TotalAccepted()
		u.LastCycle = s.CycleNumber
		s.Used[tk.Key] = u
		s.Accepted = append(s.Accepted, Segment{Phase: "body", Task: tk, CycleNumber: s.CycleNumber})
	}
	if counts["口感"] != 120 || counts["工艺"] != 120 {
		t.Fatalf("卖点应等频分布，得到 口感=%d 工艺=%d", counts["口感"], counts["工艺"])
	}
}

// 卖点权重：配置了 PointWeights 后，权重高的卖点出现次数按比例增多。
func TestPointWeightedExplicit(t *testing.T) {
	tasks := []Task{
		{SellingPoint: "口感", Angle: "鲜嫩"},
		{SellingPoint: "口感", Angle: "多汁"},
		{SellingPoint: "工艺", Angle: "慢炖"},
		{SellingPoint: "工艺", Angle: "油炸"},
	}
	s, err := New(tasks, Config{Rolling: true, BodyMin: 2, BodyMax: 6, MaxChars: 200, Threshold: 80, MaxRetries: 1, PointGap: 0, CombinationGap: 0})
	if err != nil {
		t.Fatal(err)
	}
	s.ProductFocus = &ProductFocus{Policy: ProductPolicy{PointWeights: map[string]int{"口感": 3, "工艺": 1}}}
	s.ProductFocus.Tasks = map[string]ScopedTask{}
	for _, task := range s.Queue {
		s.ProductFocus.Tasks[task.Key] = ScopedTask{Shared: true}
	}
	counts := map[string]int{}
	for i := 0; i < 2400; i++ {
		s.chooseNext()
		tk := s.Queue[s.TaskIndex]
		s.recordScheduledAdoption(tk)
		counts[tk.SellingPoint]++
		u := s.Used[tk.Key]
		u.Count++
		u.LastIndex = s.TotalAccepted()
		u.LastCycle = s.CycleNumber
		s.Used[tk.Key] = u
		s.Accepted = append(s.Accepted, Segment{Phase: "body", Task: tk, CycleNumber: s.CycleNumber})
	}
	if counts["口感"] <= counts["工艺"] || float64(counts["口感"])/float64(counts["工艺"]) > 1.31 {
		t.Fatalf("权重高的卖点应出现更多，得到 口感=%d 工艺=%d", counts["口感"], counts["工艺"])
	}
}

// 角度权重：同一卖点下，权重高的角度出现次数按比例增多。
func TestAngleWeighted(t *testing.T) {
	tasks := []Task{
		{SellingPoint: "口感", Angle: "鲜嫩"},
		{SellingPoint: "口感", Angle: "多汁"},
	}
	s, err := New(tasks, Config{Rolling: true, BodyMin: 2, BodyMax: 6, MaxChars: 200, Threshold: 80, MaxRetries: 1, PointGap: 0, CombinationGap: 0})
	if err != nil {
		t.Fatal(err)
	}
	s.ProductFocus = &ProductFocus{Policy: ProductPolicy{AngleWeights: map[string]int{"口感×鲜嫩": 2, "口感×多汁": 1}}}
	s.ProductFocus.Tasks = map[string]ScopedTask{}
	for _, task := range s.Queue {
		s.ProductFocus.Tasks[task.Key] = ScopedTask{Shared: true}
	}
	counts := map[string]int{}
	for i := 0; i < 6; i++ {
		s.chooseNext()
		tk := s.Queue[s.TaskIndex]
		s.recordScheduledAdoption(tk)
		counts[tk.Angle]++
		u := s.Used[tk.Key]
		u.Count++
		u.LastIndex = s.TotalAccepted()
		u.LastCycle = s.CycleNumber
		s.Used[tk.Key] = u
		s.Accepted = append(s.Accepted, Segment{Phase: "body", Task: tk, CycleNumber: s.CycleNumber})
	}
	if counts["鲜嫩"] <= counts["多汁"] {
		t.Fatalf("权重高的角度应出现更多，得到 鲜嫩=%d 多汁=%d", counts["鲜嫩"], counts["多汁"])
	}
}

// Within a selected point, prefer an angle which is outside its cooldown.
func TestMinReappearCycles(t *testing.T) {
	s, err := New([]Task{{SellingPoint: "同一卖点", Angle: "A"}, {SellingPoint: "同一卖点", Angle: "B"}}, Config{Rolling: true, BodyMin: 2, BodyMax: 6, MaxChars: 200, Threshold: 80, MinReappearCycles: 2})
	if err != nil {
		t.Fatal(err)
	}
	s.chooseNext()
	first := s.Queue[s.TaskIndex]
	s.Used[first.Key] = Use{Count: 1, LastCycle: 1, LastIndex: 1}
	s.CycleNumber = 2
	s.chooseNext()
	if s.Queue[s.TaskIndex].Key == first.Key {
		t.Fatal("an unused alternative angle was ignored")
	}
}
