package httpapi

import (
	"context"
	"errors"
	"strings"
	"testing"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/anchorstylescore"
	lib "livecompanion/management/internal/speechlibrary"
)

type recordingMixLibrary struct {
	libraryMemory
	policies []lib.Policy
}

func (m *recordingMixLibrary) Select(_ context.Context, _ lib.Scope, p lib.Policy, _ string, track string) (lib.Choice, error) {
	m.policies = append(m.policies, p)
	return lib.Choice{TrackID: track}, nil
}

func TestFirstAndLaterOpeningLibraryEligibility(t *testing.T) {
	_, state, segment, _ := libraryFixture(t)
	state.Phase = "opening"
	state.OpeningCount = 0
	if libraryEligible(state) {
		t.Fatal("initial opening cannot select old text")
	}
	segment.Phase = "opening"
	segment.OpeningVersion = "完整版"
	if !initialOpeningSegment(state, segment) {
		t.Fatal("initial voice can hit cache")
	}
	state.OpeningCount = 1
	segment.OpeningVersion = "接新版"
	if !libraryEligible(state) || initialOpeningSegment(state, segment) {
		t.Fatal("later opening should participate")
	}
	state.Phase = "closing"
	if !libraryEligible(state) {
		t.Fatal("closing excluded from shared mix")
	}
	state.PendingFeedback = "用户本次纠正"
	if libraryEligible(state) {
		t.Fatal("current correction bypassed")
	}
}

func TestFirstOpeningWithoutAssetRepositoryDoesNotDereferenceNilStore(t *testing.T) {
	s := &Server{}
	state := &anchorcycle.State{Surface: "formal_live", Phase: "opening"}
	if e := s.loadFirstOpeningHistory(context.Background(), state); e != nil || !state.FirstOpeningHistoryLoaded {
		t.Fatal("optional history repository should not crash standalone use", e)
	}
	if e := s.rememberFirstLiveOpening(context.Background(), state, anchorcycle.Candidate{Text: "本场新的开场。"}); e != nil {
		t.Fatal(e)
	}
}
func TestMainlineMixPolicyIsNotMultipliedByAdaptiveProbability(t *testing.T) {
	s, state, _, _ := libraryFixture(t)
	m := &recordingMixLibrary{}
	s.speechLibrary = m
	state.OpeningCount = 1
	for i := 0; i < 10; i++ {
		want := state.ShouldReuse()
		_, _, _, _ = s.selectLibraryText(context.Background(), state, anchorstylescore.Baseline{})
		if len(m.policies) != i+1 {
			t.Fatal("selection not reached", i)
		}
		p := m.policies[i]
		rate := 0
		if want {
			rate = 100
		}
		if p.OneRate != rate || p.TwoRate != rate || p.FullRate != rate {
			t.Fatal("double probability", p, want)
		}
		origin := "fresh"
		if want {
			origin = "library"
		}
		state.Accepted = append(state.Accepted, anchorcycle.Segment{Phase: "body", Selected: anchorcycle.Candidate{ReuseOrigin: origin}})
	}
}
func TestFrozenLibraryPhaseSideDoNotPolluteRoomContext(t *testing.T) {
	s, state, segment, _ := libraryFixture(t)
	segment.Phase = "body"
	segment.Selected.FissionSide = "使用方式"
	a, e := s.libraryScope(state, segment)
	if e != nil {
		t.Fatal(e)
	}
	other := segment
	other.Phase = "closing"
	b, e := s.libraryScope(state, other)
	if e != nil {
		t.Fatal(e)
	}
	other = segment
	other.Selected.FissionSide = "地理位置"
	c, e := s.libraryScope(state, other)
	if e != nil {
		t.Fatal(e)
	}
	if a.TextContext == b.TextContext || a.Key() == b.Key() || a.TextContext == c.TextContext {
		t.Fatal("phase or side cache identity leaked")
	}
	if a.RoomContext != b.RoomContext || a.RoomContext != c.RoomContext {
		t.Fatal("phase change invalidated whole room library")
	}
	state.Phase = "opening"
	state.OpeningCount = 7
	again, e := s.libraryScope(state, segment)
	if e != nil || a != again {
		t.Fatal("future producer changed frozen audio scope", e, a, again)
	}
}
func TestDistinctFirstOpeningRetriesOnlyOnceAndKeepsFullGenerationContext(t *testing.T) {
	state := cycleTestState(t)
	state.Surface = "formal_live"
	state.Phase = "opening"
	state.BaselineJSON = textTestBaseline()
	state.FirstOpeningHistory = []string{"这款商品适合朋友们一起分享，按本次资料选择规格。"}
	state.PendingFeedback = "用户已确认的原始要求"
	calls := 0
	result, _, e := generateDistinctFirstOpening(&state, "历史只供避重，完成本次任务。", func() (agentgateway.Response, []agentgateway.Message, error) {
		calls++
		messages, err := renderCycleTextMessages(&state)
		if err != nil {
			return agentgateway.Response{}, nil, err
		}
		if !strings.Contains(messages[1].Content, "历史只供避重") || !strings.Contains(messages[0].Content, "有效P0") {
			t.Fatal("opening directive replaced complete generation rules")
		}
		if calls == 1 {
			return agentgateway.Response{Text: state.FirstOpeningHistory[0]}, messages, nil
		}
		return agentgateway.Response{Text: "想按人数选规格的话，先看看今天有哪些人一起用，再核对资料里的适用范围。"}, messages, nil
	})
	if e != nil || calls != 2 || firstOpeningRepeated(result.Text, state.FirstOpeningHistory) || state.PendingFeedback != "用户已确认的原始要求" {
		t.Fatal("fresh opening not distinct or context leaked", calls, e)
	}
	if len(state.Accepted) != 0 {
		t.Fatal("generation attempts spent acceptance quota")
	}
}
func TestDistinctOpeningCannotReturnCachedOldTextAfterTwoAttempts(t *testing.T) {
	state := cycleTestState(t)
	state.FirstOpeningHistory = []string{"本次开场旧内容。"}
	calls := 0
	_, _, e := generateDistinctFirstOpening(&state, "只生成新的开场。", func() (agentgateway.Response, []agentgateway.Message, error) {
		calls++
		return agentgateway.Response{Text: "本次开场旧内容！"}, nil, nil
	})
	if calls != 2 || !errors.Is(e, errFirstOpeningRepeated) {
		t.Fatal("unbounded retry or old opening accepted", calls, e)
	}
}
func TestFirstOpeningTinyEditsAreNotNewScripts(t *testing.T) {
	previous := strings.Repeat("今天根据本次资料选择适合自己的规格，先看使用人数和明确的适用条件。", 3)
	if !firstOpeningRepeated("哥哥姐姐们，"+previous, []string{previous}) {
		t.Fatal("only nickname changed")
	}
	if firstOpeningRepeated("先想想自己的用途，再逐项核对实际配置；不同规格要按各自的资料来比较，不能凭名字猜条件。", []string{previous}) {
		t.Fatal("different opening treated as same")
	}
}
