package anchorcycle

import (
	"os"
	"testing"
)

func installP0Fixture(t *testing.T, s *State) {
	t.Helper()
	raw, err := os.ReadFile("../../../configs/prompts/anchor-style/compliance/L1/p0-hard-block.md")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.InstallP0(string(raw), "fixture"); err != nil {
		t.Fatal(err)
	}
}

// SkipReview 开启后，即使正文含「最好」「第一」等违禁词，CheckP0Candidate 也直接
// 签发「通过」回执，CheckApprovedText / FinalContentCandidateApproved /
// LectureCandidateApproved 一律放行，实现「生成后跳过全部审核直接送 TTS」。
func TestSkipReviewBypassesAllChecks(t *testing.T) {
	s := &State{Config: Config{SkipReview: true, MaxChars: 300, Threshold: 0}}
	installP0Fixture(t, s)

	c := &Candidate{Text: "这是全网最好的菜籽油，销量第一", OutputText: "这是全网最好的菜籽油，销量第一"}

	// 1) CheckP0Candidate 直接通过，且签发 Passed 回执。
	if err := s.CheckP0Candidate(c); err != nil {
		t.Fatalf("SkipReview 下 CheckP0Candidate 不应报错，got %v", err)
	}
	if c.P0Report == nil || !c.P0Report.Passed {
		t.Fatalf("SkipReview 下应签发 Passed 回执，got %+v", c.P0Report)
	}

	// 2) CheckApprovedText 直接放行。
	if err := s.CheckApprovedText(*c, c.OutputText); err != nil {
		t.Fatalf("SkipReview 下 CheckApprovedText 应放行，got %v", err)
	}

	// 3) CheckP0Voice 直接放行（声音出口）。
	if err := s.CheckP0Voice(c.OutputText); err != nil {
		t.Fatalf("SkipReview 下 CheckP0Voice 应放行，got %v", err)
	}

	// 4) 终审与讲解复核直接通过。
	s.FinalContentReviewRequired = true
	if !s.FinalContentCandidateApproved(*c) {
		t.Fatalf("SkipReview 下 FinalContentCandidateApproved 应为 true")
	}
	if !s.LectureCandidateApproved(*c) {
		t.Fatalf("SkipReview 下 LectureCandidateApproved 应为 true")
	}
}

// 未开启 SkipReview 时，含违禁词的正文必须被 CheckP0Candidate 拦下（对照：开关
// 关闭则审核照常生效）。
func TestSkipReviewOffStillBlocks(t *testing.T) {
	s := &State{Config: Config{SkipReview: false, MaxChars: 300}}
	installP0Fixture(t, s)

	c := &Candidate{Text: "这是全网最好的菜籽油", OutputText: "这是全网最好的菜籽油"}
	_ = s.CheckP0Candidate(c)
	if c.P0Report == nil || c.P0Report.Passed {
		t.Fatalf("未开 SkipReview 时含「最好」应被拦下，got %+v", c.P0Report)
	}
}
