package purposelib

import "testing"

func TestBuildGroupsCategoriesAndSources(t *testing.T) {
	specs := Build(
		[]string{"price", "shipping_fee", "storage", "shipping_fee", "custom_9e01ffdd"},
		[]string{"线上当前售价", "偏远地区空运补运费", "常温保存期限", "空运补运费", "配送时效与包装建议"},
		true, true,
	)
	byKey := map[string]Spec{}
	for _, spec := range specs {
		byKey[spec.Key] = spec
	}
	if _, ok := byKey["price"]; !ok {
		t.Fatal("price purpose missing")
	}
	fee, ok := byKey["shipping_fee"]
	if !ok || !fee.InOut {
		t.Fatalf("shipping_fee should be in/out: %#v", fee)
	}
	if _, ok := byKey["storage"]; !ok {
		t.Fatal("storage purpose missing")
	}
	if link, ok := byKey["link"]; !ok || len(link.Sources) == 0 {
		t.Fatal("link purpose missing")
	}
	if benefit, ok := byKey["benefit"]; !ok || len(benefit.Sources) == 0 {
		t.Fatal("benefit purpose missing")
	}
	if len(fee.Keywords) == 0 {
		t.Fatal("keywords missing")
	}
}
