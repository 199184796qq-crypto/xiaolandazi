package anchorcycle

import "testing"

func TestCommonPoolIdentityAndShareAreGlobalAcrossProducts(t *testing.T) {
	point := FocusSellingPoint{Name: "公共停车", Weight: 1, Angles: []FocusSellingAngle{{Angle: "到店怎么停车", Weight: 1}, {Angle: "离店怎么取车", Weight: 1}}}
	own := FocusSellingPoint{Name: "套餐", Angles: []FocusSellingAngle{{Angle: "套餐内容"}}}
	focus := &ProductFocus{MaterialVersion: "product-common-v1", Policy: ProductPolicy{Version: "test"}, Products: []FocusProduct{
		{LinkKey: "1", SellingPoints: []FocusSellingPoint{own}, CommonPoints: []FocusSellingPoint{point}},
		{LinkKey: "2", SellingPoints: []FocusSellingPoint{own}, CommonPoints: []FocusSellingPoint{point}},
	}}
	tasks := ExpandProductSellingPoints(focus.Products, &focus.Policy)
	common := 0
	for _, task := range tasks {
		if task.Common {
			common++
			if task.Key != task.SlotKey {
				t.Fatal("common material identity changed", task)
			}
		}
	}
	if len(tasks) != 4 || common != 2 {
		t.Fatal("common pool multiplied or lost stable identity", tasks)
	}
	focus.Tasks = ProductTaskScopes(tasks)
	s, err := New(tasks, Config{Rolling: true, MaxChars: 300})
	if err != nil {
		t.Fatal(err)
	}
	s.ProductFocus = focus
	s.Phase = "body"
	played := 0
	for i := 0; i < 50; i++ {
		task, _ := simulateBody(&s)
		if task.Common {
			played++
			if task.ProductMode != "common" || len(task.ProductLinks()) != 2 {
				t.Fatal("shared task lost its original scope", task)
			}
		}
	}
	if played != 10 || s.ProductFocus.AdoptedSegments["1"]+s.ProductFocus.AdoptedSegments["2"] != 40 {
		t.Fatal("common copies amplified the global share", played, s.ProductFocus.ScheduleCursor)
	}
}
