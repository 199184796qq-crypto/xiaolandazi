package decisionexecutor

import (
	"testing"

	"livecompanion/management/internal/model"
)

func TestMatchLibraryEntryRespectsExclusions(t *testing.T) {
	entry := model.QuestionLibraryEntry{
		Question:     "砍、切块、剁",
		Answer:       "可以砍块，切好再发",
		ExcludeWords: []string{"价", "便宜", "优惠"},
	}
	if _, ok := matchLibraryEntry(entry, normalizeQuestion("可以砍吗")); !ok {
		t.Fatal("砍 should hit the chopping entry")
	}
	if _, ok := matchLibraryEntry(entry, normalizeQuestion("能砍价吗")); ok {
		t.Fatal("砍价 must be excluded")
	}
	empty := model.QuestionLibraryEntry{Question: "砍", Answer: ""}
	if _, ok := matchLibraryEntry(empty, "砍吗"); ok {
		t.Fatal("empty answer must not match")
	}
}
