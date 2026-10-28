package addressing

import "testing"

func TestTermFallbackChain(t *testing.T) {
	cases := []struct {
		name string
		src  AddressTermSources
		want string
		from string
	}{
		{"room wins", AddressTermSources{Room: []string{"老乡"}, Style: []string{"宝宝"}, Script: []string{"朋友"}}, "老乡", "room"},
		{"style when room empty", AddressTermSources{Style: []string{"宝宝"}, Script: []string{"朋友"}}, "宝宝", "style"},
		{"script when both empty", AddressTermSources{Script: []string{"朋友"}}, "朋友", "script"},
		{"nothing", AddressTermSources{}, "", ""},
	}
	for _, tc := range cases {
		got, from := tc.src.Resolve()
		joined := ""
		if len(got) > 0 {
			joined = got[0]
		}
		if joined != tc.want || from != tc.from {
			t.Fatalf("%s: got %q from %q", tc.name, joined, from)
		}
	}
}
