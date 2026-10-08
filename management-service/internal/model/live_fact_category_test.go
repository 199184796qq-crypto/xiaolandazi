package model

import (
	"strings"
	"testing"
)

func TestCustomFactCategoryIdentifierAndValidation(t *testing.T) {
	a, err := NewLiveFactCategory(" 算账口径 ", "product", "")
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewLiveFactCategory("算账口径", "product", "价格核算")
	if err != nil || a.Value != b.Value || !strings.HasPrefix(a.Value, "custom_") {
		t.Fatal(a, b, err)
	}
	for _, input := range []struct{ label, family string }{{"", "other"}, {"a\nb", "product"}, {strings.Repeat("字", 41), "other"}, {"分类", "invented"}} {
		if _, err = NewLiveFactCategory(input.label, input.family, ""); err == nil {
			t.Fatal(input)
		}
	}
}
