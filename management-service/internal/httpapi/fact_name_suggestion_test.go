package httpapi

import "testing"

func TestParseFactNameSuggestion(t *testing.T) {
	for _, raw := range []string{`{"name":"开袋后保存建议"}`, "```json\n{\"name\":\" 开袋后保存建议 \"}\n```"} {
		name, err := parseFactNameSuggestion(raw)
		if err != nil || name != "开袋后保存建议" {
			t.Fatal(name, err)
		}
	}
	for _, raw := range []string{`{}`, `{"name":""}`, `{"name":42}`, `{"name":"发货时效","value":"明天发货"}`, `{"name":"发货时效"} {}`, `{"name":"发货\n时效"}`, `{"name":"这个名字超过二十四个字所以不能作为简短的事实名称保存到输入框里"}`} {
		if _, err := parseFactNameSuggestion(raw); err == nil {
			t.Fatalf("invalid response accepted: %s", raw)
		}
	}
}
