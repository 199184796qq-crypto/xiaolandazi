package anchorcycle

import (
	"encoding/json"
	"testing"
)

func TestProductTaskIdentitySurvivesRunInitialization(t *testing.T) {
	products := []FocusProduct{
		{LinkKey: "1号链接", Roles: []string{"ordinary"}, SellingPoints: []FocusSellingPoint{{ID: 11, Name: "套餐内容", Angles: []FocusSellingAngle{{Angle: "餐品配置"}}}}},
		{LinkKey: "2号链接", Roles: []string{"main"}, SellingPoints: []FocusSellingPoint{{ID: 12, Name: "套餐内容", Angles: []FocusSellingAngle{{Angle: "餐品配置"}}}}},
	}
	focus := &ProductFocus{Products: products, Policy: ProductPolicy{Version: "product-focus-v3"}, Tasks: map[string]ScopedTask{}}
	tasks := ExpandProductSellingPoints(products, &focus.Policy)
	for _, task := range tasks {
		focus.Tasks[task.Key] = ScopedTask{Links: []string{task.LinkKey}}
	}
	state, err := New(tasks, Config{Rolling: true, MaxChars: 300, PointGap: 2, CombinationGap: 8})
	if err != nil {
		t.Fatalf("separate products with the same point and angle must remain separate: %v", err)
	}
	for i, task := range state.Queue {
		if task.Key != tasks[i].Key || task.SlotKey != tasks[i].SlotKey {
			t.Fatalf("initialization changed program-owned task identity: before=%+v after=%+v", tasks[i], task)
		}
		if focus.Policy.AngleWeights[task.Key] != 1 {
			t.Fatalf("task no longer matches its angle weight: %q", task.Key)
		}
	}
	state.Surface, state.ProductFocus = "formal_live", focus
	for _, link := range []string{"1号链接", "2号链接"} {
		state.ProductFocus.CurrentLink = link
		state.SelectProductTask()
		if state.Status != "ready" || state.Queue[state.TaskIndex].LinkKey != link {
			t.Fatalf("product %q cannot select its own task: status=%s error=%s", link, state.Status, state.Error)
		}
		for _, task := range state.Queue {
			if state.productTaskEligible(task) != (task.LinkKey == link) {
				t.Fatalf("cross-product eligibility changed for %+v", task)
			}
		}
	}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	var restored State
	if err = json.Unmarshal(raw, &restored); err != nil {
		t.Fatal(err)
	}
	restored.SelectProductTask()
	if restored.Status != "ready" || restored.Queue[restored.TaskIndex].LinkKey != "2号链接" {
		t.Fatal("persisted run lost product task identity")
	}
}

func TestNewTaskIdentityKeepsLegacyAndExplicitIDs(t *testing.T) {
	for _, tc := range []struct {
		name string
		task Task
		want string
	}{
		{"legacy", Task{SellingPoint: "工艺", Angle: "制作", Key: "ignored-legacy-key"}, "工艺×制作"},
		{"product", Task{LinkKey: "2", SellingPoint: "工艺", Angle: "制作", SlotKey: "2×#4×#1"}, "2×工艺×制作"},
		{"explicit", Task{ID: "program-id", LinkKey: "2", SellingPoint: "工艺", Angle: "制作"}, "program-id"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, err := New([]Task{tc.task}, Config{Rolling: true, MaxChars: 300})
			if err != nil {
				t.Fatal(err)
			}
			if s.Queue[0].Key != tc.want {
				t.Fatalf("key=%q want %q", s.Queue[0].Key, tc.want)
			}
			if s.Queue[0].SlotKey != tc.task.SlotKey {
				t.Fatal("cache slot changed")
			}
		})
	}
}
