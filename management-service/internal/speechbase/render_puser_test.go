package speechbase

import "testing"

func TestRenderPUserForbiddenList(t *testing.T) {
	cases := []struct {
		name string
		in   []map[string]any
		want string
	}{
		{
			name: "表达限制中文字段",
			in: []map[string]any{
				{"表达限制": "不要把10人餐说成3-4人", "展开指导": "说到分量改说一桌人刚好吃饱"},
			},
			want: "不要把10人餐说成3-4人",
		},
		{
			name: "多条拼接",
			in: []map[string]any{
				{"表达限制": "不用早餐举例"},
				{"表达限制": "不承诺当天发货"},
			},
			want: "不用早餐举例\n不承诺当天发货",
		},
		{
			name: "空值跳过",
			in: []map[string]any{
				{"表达限制": "   "},
				{"表达限制": "不承诺当天发货"},
			},
			want: "不承诺当天发货",
		},
		{
			name: "全部为空",
			in:   []map[string]any{{"表达限制": ""}},
			want: "",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := RenderPUserForbiddenList(c.in); got != c.want {
				t.Fatalf("got %q want %q", got, c.want)
			}
		})
	}
}

func TestRenderPUserRewriteGuidance(t *testing.T) {
	cases := []struct {
		name string
		in   []map[string]any
		want string
	}{
		{
			name: "展开指导中文字段",
			in: []map[string]any{
				{"表达限制": "不用早餐举例", "展开指导": "说到分量改说一桌人刚好吃饱"},
			},
			want: "说到分量改说一桌人刚好吃饱",
		},
		{
			name: "全部为空",
			in:   []map[string]any{{"表达限制": "不用早餐举例"}},
			want: "",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := RenderPUserRewriteGuidance(c.in); got != c.want {
				t.Fatalf("got %q want %q", got, c.want)
			}
		})
	}
}
