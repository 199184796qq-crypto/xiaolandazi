package anchorcycle

import "testing"

// 角色权重：辅推商品按 RoleWeights 展开成交错序列，权重高的出现次数多且同类不相邻。
func TestAuxiliarySequenceWeighted(t *testing.T) {
	p := &ProductFocus{
		Policy: ProductPolicy{RoleWeights: map[string]int{"traffic": 3, "benefit": 2, "bundle": 1}},
		Products: []FocusProduct{
			{LinkKey: "1", Roles: []string{"main"}},
			{LinkKey: "2", Roles: []string{"traffic"}},
			{LinkKey: "3", Roles: []string{"benefit"}},
			{LinkKey: "4", Roles: []string{"bundle"}},
		},
	}
	seq := p.auxiliarySequence()
	counts := map[string]int{}
	for _, item := range seq {
		counts[item.LinkKey]++
	}
	if len(seq) != 6 {
		t.Fatalf("序列长度应为 6，得到 %d", len(seq))
	}
	if counts["2"] != 3 || counts["3"] != 2 || counts["4"] != 1 {
		t.Fatalf("角色权重展开错误：traffic=%d benefit=%d bundle=%d", counts["2"], counts["3"], counts["4"])
	}
	for i := 1; i < len(seq); i++ {
		if seq[i].LinkKey == seq[i-1].LinkKey {
			t.Fatalf("同类商品不应相邻：%v", seq)
		}
	}
}

// 未配置角色权重时，退化为现有顺序（traffic 优先，不展开）。
func TestAuxiliarySequenceDefaultsToOriginal(t *testing.T) {
	p := &ProductFocus{
		Policy:   ProductPolicy{},
		Products: []FocusProduct{{LinkKey: "1", Roles: []string{"main"}}, {LinkKey: "2", Roles: []string{"traffic"}}, {LinkKey: "3", Roles: []string{"bundle"}}},
	}
	seq := p.auxiliarySequence()
	if len(seq) != 2 {
		t.Fatalf("无权重时应返回去重后的辅推列表，得到 %d", len(seq))
	}
	if seq[0].LinkKey != "2" { // traffic 优先
		t.Fatalf("traffic 应排在前，得到 %s", seq[0].LinkKey)
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
	for i := 0; i < 4; i++ {
		s.chooseNext()
		tk := s.Queue[s.TaskIndex]
		counts[tk.SellingPoint]++
		u := s.Used[tk.Key]
		u.Count++
		u.LastIndex = s.TotalAccepted()
		u.LastCycle = s.CycleNumber
		s.Used[tk.Key] = u
		s.Accepted = append(s.Accepted, Segment{Phase: "body", Task: tk, CycleNumber: s.CycleNumber})
	}
	// 缺省等频：口感（3 角度）与工艺（1 角度）应各出现 2 次，而不是口感 3 次霸屏。
	if counts["口感"] != 2 || counts["工艺"] != 2 {
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
	counts := map[string]int{}
	for i := 0; i < 8; i++ {
		s.chooseNext()
		tk := s.Queue[s.TaskIndex]
		counts[tk.SellingPoint]++
		u := s.Used[tk.Key]
		u.Count++
		u.LastIndex = s.TotalAccepted()
		u.LastCycle = s.CycleNumber
		s.Used[tk.Key] = u
		s.Accepted = append(s.Accepted, Segment{Phase: "body", Task: tk, CycleNumber: s.CycleNumber})
	}
	// 权重 3:1，8 次应约为 6:2，口感明显多于工艺。
	if counts["口感"] <= counts["工艺"] {
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
	counts := map[string]int{}
	for i := 0; i < 6; i++ {
		s.chooseNext()
		tk := s.Queue[s.TaskIndex]
		counts[tk.Angle]++
		u := s.Used[tk.Key]
		u.Count++
		u.LastIndex = s.TotalAccepted()
		u.LastCycle = s.CycleNumber
		s.Used[tk.Key] = u
		s.Accepted = append(s.Accepted, Segment{Phase: "body", Task: tk, CycleNumber: s.CycleNumber})
	}
	// 权重 2:1，6 次应约为 4:2，鲜嫩明显多于多汁。
	if counts["鲜嫩"] <= counts["多汁"] {
		t.Fatalf("权重高的角度应出现更多，得到 鲜嫩=%d 多汁=%d", counts["鲜嫩"], counts["多汁"])
	}
}

// 轮级冷却：同一角度至少隔 MinReappearCycles 轮才再出现。
func TestMinReappearCycles(t *testing.T) {
	tasks := []Task{{SellingPoint: "口感", Angle: "鲜嫩"}, {SellingPoint: "工艺", Angle: "慢炖"}}
	s, err := New(tasks, Config{Rolling: true, BodyMin: 2, BodyMax: 6, MaxChars: 200, Threshold: 80, MaxRetries: 1, PointGap: 0, CombinationGap: 0, MinReappearCycles: 2})
	if err != nil {
		t.Fatal(err)
	}
	// 第 1 轮采纳「口感」
	s.CycleNumber = 1
	s.chooseNext()
	first := s.Queue[s.TaskIndex]
	u := s.Used[first.Key]
	u.Count++
	u.LastIndex = s.TotalAccepted()
	u.LastCycle = s.CycleNumber
	s.Used[first.Key] = u
	s.Accepted = append(s.Accepted, Segment{Phase: "body", Task: first, CycleNumber: s.CycleNumber})

	// 第 2 轮：口感距离上次仅 1 轮，应被冷却，只能选工艺
	s.CycleNumber = 2
	s.chooseNext()
	second := s.Queue[s.TaskIndex]
	if second.SellingPoint == first.SellingPoint {
		t.Fatalf("同一角度应被轮级冷却，第 2 轮不应再选 %s", second.SellingPoint)
	}
}
