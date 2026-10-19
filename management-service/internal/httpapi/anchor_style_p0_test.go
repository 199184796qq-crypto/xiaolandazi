package httpapi

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/anchorstylescore"
	"livecompanion/management/internal/anchorvoicecache"
	"livecompanion/management/internal/contentguard"
)

func TestP0NestedMotherLatestNewRunAndFrozenCurrentRun(t *testing.T) {
	md, _, err := loadP0Mother()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	path := filepath.Join(root, "compliance", "L1", "p0-hard-block.md")
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, []byte(md), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ANCHOR_STYLE_PROMPT_DIR", root)
	t.Setenv("ANCHOR_STYLE_P0_POLICY_PATH", "")
	first := cycleTestState(t)
	if err = installP0Mother(&first); err != nil {
		t.Fatal(err)
	}
	revised := strings.Replace(md, `"max_rewrites": 3`, `"max_rewrites": 2`, 1)
	if err = os.WriteFile(path, []byte(revised), 0600); err != nil {
		t.Fatal(err)
	}
	if err = installP0Mother(&first); err != nil || first.P0MaxRewrites != 3 {
		t.Fatal("active run mother overwritten", err)
	}
	next := cycleTestState(t)
	if err = installP0Mother(&next); err != nil || next.P0MaxRewrites != 2 || next.P0PolicySource == first.P0PolicySource {
		t.Fatal("new mother not loaded", err)
	}
	t.Setenv("ANCHOR_STYLE_P0_POLICY_PATH", filepath.Join(root, "missing.md"))
	next = cycleTestState(t)
	if installP0Mother(&next) == nil {
		t.Fatal("missing policy silently bypassed")
	}
}

func TestP0PromptBootstrapRepairAndNormalOutputReset(t *testing.T) {
	s := cycleTestState(t)
	if err := installP0Mother(&s); err != nil {
		t.Fatal(err)
	}
	first, err := renderAnchorCyclePrompt(&s)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(first, s.StyleMD) || !strings.Contains(first, s.Facts) || !strings.Contains(first, "l1.p0.absolute_claim") {
		t.Fatal("bootstrap incomplete")
	}
	s.BootstrapSent = true
	follow, err := renderAnchorCyclePrompt(&s)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(follow, s.StyleMD) || strings.Contains(follow, "l1.p0.absolute_claim") || !strings.Contains(follow, "恢复纯话术") {
		t.Fatal("bootstrap repeated or JSON mode not reset", follow)
	}
}

func TestP0VoiceAndCacheGateBeforeAnyOSSWork(t *testing.T) {
	calls := 0
	s, state, segment, cache := voiceCacheFixture(t, func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(200) })
	group := cycleVoiceCacheGroup(state, segment)
	e := cache.NewEntry(state.TenantID, group, "unsafe-cache", "fixture-voice-cache", 5*time.Hour)
	e.Text = "我们家零风险。"
	e.OutputText = e.Text
	e.Bundle.Text = e.Text
	e.Bundle.AudioDurationMS = 1000
	cache.entries = []anchorvoicecache.Entry{e}
	cached := s.cachedCycleCandidate(context.Background(), state, anchorstylescore.Baseline{})
	if cached == nil {
		t.Fatal("cached text must reach final text review")
	}
	cachedCandidate := anchorcycle.Candidate{Text: cached.Text, OutputText: cached.OutputText}
	if err := state.CheckP0Candidate(&cachedCandidate); err != nil || cachedCandidate.P0Report == nil || cachedCandidate.P0Report.Passed {
		t.Fatal("cached unsafe text escaped final review", err)
	}
	segment.Selected.Text = e.Text
	if _, err := s.cachedCycleVoice(context.Background(), state, segment); err == nil {
		t.Fatal("unsafe voice cache queried")
	}
	if _, err := s.archiveCycleVoiceCache(context.Background(), state, segment, []byte("fixture"), nil, 1000); err == nil {
		t.Fatal("unsafe audio archived")
	}
	if checkCycleVoiceBinding(state, segment) == nil || calls != 0 || len(cache.reserved) != 0 {
		t.Fatal("unsafe voice performed external work")
	}
	segment.Selected.Text = "我们家在这个镇上做油啊。"
	segment.Voice = &anchorcycle.VoiceAsset{Status: "ready"}
	segment.Voice.Bundle.Text = e.Text
	if checkCycleVoiceBinding(state, segment) == nil {
		t.Fatal("ready bundle bypassed guard")
	}
	state.P0PolicyMD = ""
	if checkCycleVoiceBinding(state, segment) == nil {
		t.Fatal("legacy run bypassed guard")
	}
}

func TestP0AuditDecisionKeepsRawResponse(t *testing.T) {
	s := cycleTestState(t)
	if err := installP0Mother(&s); err != nil {
		t.Fatal(err)
	}
	c := anchorcycle.Candidate{ID: "fixture", Text: "零风险。", RawText: "raw response", Prompt: "actual prompt"}
	if err := s.CheckP0Candidate(&c); err != nil {
		t.Fatal(err)
	}
	e := p0AuditEvent(&s, c)
	if e.Decision != "rewrite_required" || e.RawResponse != c.RawText || e.Prompt != c.Prompt || len(e.RuleSHA256) != 64 {
		t.Fatal(e)
	}
	s.P0RewritesUsed = 2
	c.P0Patch = &contentguard.Repair{RewriteNumber: 3}
	c.P0RewriteNumber = 3
	if e = p0AuditEvent(&s, c); e.Decision != "skipped" || e.RewriteNumber != 3 {
		t.Fatal(e)
	}
}
