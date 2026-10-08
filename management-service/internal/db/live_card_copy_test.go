package db

import (
	"errors"
	"strings"
	"testing"
)

func TestCopiedCardsStayAfterSourceAcrossReloadWithoutHidingOrphans(t *testing.T) {
	type card struct {
		id  int64
		ref string
	}
	rows := []card{{1, ""}, {10, "copy:product:2:v1"}, {11, "copy:product:2:v1"}, {12, "copy:product:11:v1"}, {2, ""}, {13, "copy:product:999:v1"}, {14, "copy:product:15:v1"}, {15, "copy:product:14:v1"}}
	got := orderCopiedCards(rows, "product", func(c card) int64 { return c.id }, func(c card) string { return c.ref })
	want := []int64{1, 2, 11, 12, 10, 13, 14, 15}
	if len(got) != len(rows) {
		t.Fatal("copy lineage hid a card")
	}
	for i, c := range got {
		if c.id != want[i] {
			t.Fatal("copy position not preserved", got)
		}
	}
}

func TestCopyCardLinkUsesDatabaseMaximumAndPreservesFormat(t *testing.T) {
	for _, tc := range []struct {
		source string
		keys   []string
		want   string
	}{
		{"1", []string{"1", "2", "10"}, "11"},
		{"1号链接", []string{"1号链接", "9号链接", "02", "无效"}, "10号链接"},
		{"链接1", []string{"1", "链接9", "20号链接"}, "链接21"},
		{"自定义链接", []string{"5"}, "6号链接"},
	} {
		got, err := nextCopiedCardLink(tc.source, tc.keys)
		if err != nil || got != tc.want {
			t.Fatal(tc, got, err)
		}
	}
	if _, err := nextCopiedCardLink("1", []string{"9223372036854775807"}); !errors.Is(err, ErrLiveCardCopyInvalid) {
		t.Fatal("overflow must reject")
	}
}
func TestCopyCardNameSuffixDoesNotChangeOriginal(t *testing.T) {
	original := "杨鸭子"
	got, err := copiedCardName(original, []string{original, original + "01", original + "02"})
	if err != nil || got != "杨鸭子03" || original != "杨鸭子" {
		t.Fatal(got, err)
	}
	got, err = copiedCardName(original, []string{original})
	if err != nil || got != "杨鸭子01" {
		t.Fatal(got, err)
	}
	if _, err = copiedCardName(strings.Repeat("鸭", 254), nil); !errors.Is(err, ErrLiveCardCopyInvalid) {
		t.Fatal("must not silently truncate real product name")
	}
}

func TestBenefitCopyUsesNextRealActiveProductNotSyntheticNumber(t *testing.T) {
	key, err := nextExistingCardLink("1号链接", []string{"20号链接", "7", "1号链接", "2号链接"})
	if err != nil || key != "2号链接" {
		t.Fatal(key, err)
	}
	key, err = nextExistingCardLink("2号链接", []string{"1号链接", "7"})
	if err != nil || key != "7" {
		t.Fatal(key, err)
	}
	key, err = nextExistingCardLink("", []string{"5", "9"})
	if err != nil || key != "5" {
		t.Fatal(key, err)
	}
	for _, source := range []string{"9", "非数字"} {
		if _, err = nextExistingCardLink(source, []string{"1", "9"}); !errors.Is(err, ErrLiveCardCopyInvalid) {
			t.Fatal("must not invent/wrap real product link", source)
		}
	}
}
