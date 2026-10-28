package addressing

import (
	"strings"
	"testing"
)

func TestTermFallbackChain(t *testing.T) {
	cases := []struct {
		name string
		src  AddressTermSources
		want string
		from string
	}{
		{"room wins", AddressTermSources{Room: []string{"老乡"}, Style: []string{"宝宝"}}, "老乡", "room"},
		{"style when room empty", AddressTermSources{Style: []string{"宝宝"}}, "宝宝", "style"},
		{"defaults when both empty", AddressTermSources{}, "老乡", "default"},
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

func TestGroupLabelsFallBackToDefaults(t *testing.T) {
	labels, from := AddressTermSources{}.ResolveGroupLabels()
	if from != "default" || len(labels) == 0 {
		t.Fatalf("labels=%v from=%s", labels, from)
	}
}

func TestGroupLabelsFollowRoomTerms(t *testing.T) {
	labels := GroupLabelsFrom([]string{"老乡", "家人们"})
	joined := strings.Join(labels, ",")
	if !strings.Contains(joined, "老乡们") || !strings.Contains(joined, "家人们") {
		t.Fatalf("labels=%v", labels)
	}
	if strings.Contains(joined, "几位朋友") {
		t.Fatalf("default label leaked: %v", labels)
	}
	if fallback := GroupLabelsFrom(nil); len(fallback) == 0 {
		t.Fatal("missing fallback labels")
	}
}
