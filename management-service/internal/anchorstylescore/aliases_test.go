package anchorstylescore

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestAliasFormatsCalibrateToSameRuler(t *testing.T) {
	sample := "哥哥姐姐们啊，哥哥姐们啊，哥哥姐们哟，老乡，我们家啊！"
	standard := strings.Replace(qualitativeResponse, `"称呼别名":{}`, `"称呼别名":{"哥哥姐姐们":["哥哥姐们"]}`, 1)
	expected, err := Calibrate(standard, sample, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, aliases := range []string{
		`{"哥哥姐姐们":"哥哥姐们"}`,
		`{"哥哥姐们":"哥哥姐姐们"}`,
		`{"哥哥姐们":"哥哥姐姐们的简称","老乡":"对观众的亲切称呼，与地域相关"}`,
		`{"哥哥姐姐们":["哥哥姐们"],"哥哥姐们":"哥哥姐姐们的简称"}`,
	} {
		t.Run(aliases, func(t *testing.T) {
			raw := strings.Replace(qualitativeResponse, `"称呼别名":{}`, `"称呼别名":`+aliases, 1)
			before := raw
			got, err := Calibrate(raw, sample, false)
			if err != nil {
				t.Fatal(err)
			}
			if got != expected || raw != before {
				t.Fatalf("normalization changed ruler or original return: %s", got)
			}
			b, _ := Parse(got)
			if Measure(sample, b).MainAudienceCount != 3 {
				t.Fatal("aliases not counted once in the full-address group")
			}
			again, err := Calibrate(got, sample, false)
			if err != nil || again != got {
				t.Fatalf("recalibration not idempotent: %v", err)
			}
		})
	}
}

func TestExplicitShortAddressExplanationMergesNonContraction(t *testing.T) {
	raw := strings.Replace(qualitativeResponse, `"候选称呼":["老乡","哥哥姐姐们","哥哥姐们"]`, `"候选称呼":["新粉丝们","新粉","老乡"]`, 1)
	raw = strings.Replace(raw, `"称呼别名":{}`, `"称呼别名":{"新粉":"新粉丝们的简称","老乡":"对观众的亲切称呼，与地域相关"}`, 1)
	got, err := Calibrate(raw, "新粉丝们啊，新粉哟，新粉嘛，老乡，我们家啊！", false)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := Parse(got)
	if b.Identity.Audience.Main != "新粉丝们" || !reflect.DeepEqual(b.Identity.Audience.Aliases["新粉丝们"], []string{"新粉"}) {
		t.Fatalf("wrong address group: %+v", b.Identity.Audience)
	}
	if _, exists := b.Identity.Audience.Aliases["老乡"]; exists {
		t.Fatal("explanation prose entered counted aliases")
	}
}

func TestMalformedAliasTypesStillFail(t *testing.T) {
	for _, aliases := range []string{`[]`, `"说明"`, `{"哥哥姐姐们":12}`, `{"哥哥姐姐们":true}`, `{"哥哥姐姐们":{}}`, `{"哥哥姐姐们":["哥哥姐们",3]}`} {
		t.Run(aliases, func(t *testing.T) {
			raw := strings.Replace(qualitativeResponse, `"称呼别名":{}`, `"称呼别名":`+aliases, 1)
			if _, err := Calibrate(raw, "哥哥姐姐们啊，我们家哟！", false); err == nil || !strings.Contains(err.Error(), "称呼别名") {
				t.Fatalf("invalid alias schema accepted: %v", err)
			}
		})
	}
}

func TestAliasNormalizationPreservesExistingArraysAndOtherFields(t *testing.T) {
	var audience map[string]json.RawMessage
	_ = json.Unmarshal([]byte(`{"候选称呼":["哥哥姐姐们","老乡"],"称呼别名":{"哥哥姐姐们":["哥哥姐们"],"老乡":null},"称呼使用场景":"原说明"}`), &audience)
	before := string(audience["称呼使用场景"])
	if err := normalizeAudienceAliases(audience); err != nil {
		t.Fatal(err)
	}
	var aliases map[string][]string
	_ = json.Unmarshal(audience["称呼别名"], &aliases)
	if !reflect.DeepEqual(aliases["哥哥姐姐们"], []string{"哥哥姐们"}) || string(audience["称呼使用场景"]) != before || len(aliases["老乡"]) != 0 {
		t.Fatalf("unexpected field mutation: %s", audience)
	}
}
