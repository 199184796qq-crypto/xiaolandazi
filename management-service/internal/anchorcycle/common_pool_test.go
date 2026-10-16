package anchorcycle

import "testing"

func TestCommonPoolIdentityAndAngleUseAreSharedAcrossProducts(t *testing.T) {
	point := FocusSellingPoint{Name: "公共停车", Weight: 1, Angles: []FocusSellingAngle{{Angle: "到店怎么停车", Weight: 1}, {Angle: "离店怎么取车", Weight: 1}}}
	focus := &ProductFocus{MaterialVersion: "product-common-v1", Policy: ProductPolicy{Version: "test"}, Products: []FocusProduct{
		{LinkKey: "1", CommonPoints: []FocusSellingPoint{point}},
		{LinkKey: "2", CommonPoints: []FocusSellingPoint{point}},
	}}
	tasks := ExpandProductSellingPoints(focus.Products, &focus.Policy)
	if len(tasks) != 2 || !tasks[0].Common || tasks[0].Key != tasks[0].SlotKey {
		t.Fatal("common pool multiplied or lost stable identity", tasks)
	}
	focus.Tasks = ProductTaskScopes(tasks)
	s, err := New(tasks, Config{Rolling: true, MaxChars: 300})
	if err != nil {
		t.Fatal(err)
	}
	s.ProductFocus = focus
	s.Phase = "body"
	focus.CurrentLink = "1"
	s.SelectProductTask()
	first := s.Task()
	if first.ProductMode != "common" {
		t.Fatal(first)
	}
	s.recordScheduledAdoption(first)
	focus.CurrentLink = "2"
	focus.CurrentSlot = focus.ScheduleCursor
	s.SelectProductTask()
	second := s.Task()
	if first.Key == second.Key {
		t.Fatal("second product forgot common angle use", first, second)
	}
	if s.ScheduledAngleUses[s.schedulingAngleKey(first, "2")] != 1 {
		t.Fatal("common angle history split by product")
	}
	if s.ProductFocus.ScheduleCursor != 1 {
		t.Fatal("one adopted common body must consume one product slot")
	}
}
