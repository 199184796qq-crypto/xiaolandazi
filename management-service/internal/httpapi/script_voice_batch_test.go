package httpapi

import (
	"context"
	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/coreclient"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/scriptplay"
	"livecompanion/management/internal/voicebatch"
	"testing"
	"time"
)

func TestHostQueueRespectsBatchAndManualCommand(t *testing.T) {
	a, b := playbackIdentity("A"), playbackIdentity("B")
	v := model.LiveAgentPlanVersion{VoiceIdentity: a}
	voices := []recordingScriptVoice{{VoiceID: "A", Model: a.Model, Identity: &a}, {VoiceID: "B", Model: b.Model, Identity: &b}}
	s := voicebatch.State{Active: a, Batch: 1}
	if _, ok := scriptVoiceBatchTarget(s, voices, v, v); ok {
		t.Fatal("changed person after a piece/round")
	}
	s.ElapsedMS = int64((2 * time.Hour) / time.Millisecond)
	if target, ok := scriptVoiceBatchTarget(s, voices, v, v); !ok || target.VoiceIdentity.VoiceID != "B" {
		t.Fatal(target, ok)
	}
	s.ElapsedMS = 0
	s.Requested = &b
	if target, ok := scriptVoiceBatchTarget(s, voices, v, v); !ok || target.VoiceIdentity.VoiceID != "B" {
		t.Fatal("manual switch must bypass the clock")
	}
	c := playbackIdentity("unselected")
	s.Requested = &c
	if _, ok := scriptVoiceBatchTarget(s, voices, v, v); ok {
		t.Fatal("unselected voice accepted")
	}
}

func TestHostCommandMadeDuringTTSWinsBeforeQueue(t *testing.T) {
	a, b, c := playbackIdentity("A"), playbackIdentity("B"), playbackIdentity("C")
	v := model.LiveAgentPlanVersion{VoiceIdentity: a}
	voices := []recordingScriptVoice{{VoiceID: "A", Model: a.Model, Identity: &a}, {VoiceID: "B", Model: b.Model, Identity: &b}, {VoiceID: "C", Model: c.Model, Identity: &c}}
	prepared := scriptVersionForVoice(v, voices[1])
	s := voicebatch.State{Active: a, Requested: &c, Batch: 1}
	if stageScriptHostVoice(&s, voices, v, v, prepared, "next") || s.Pending != nil || s.Requested.VoiceID != "C" {
		t.Fatal("obsolete B superseded newer C", s)
	}
	prepared = scriptVersionForVoice(v, voices[2])
	if !stageScriptHostVoice(&s, voices, v, v, prepared, "next") || s.Active.VoiceID != "A" || s.Pending.VoiceID != "C" {
		t.Fatal(s)
	}
	if stageScriptHostVoice(&s, voices, v, v, scriptVersionForVoice(v, voices[1]), "other") || s.PendingInstance != "next" {
		t.Fatal("queued pending instance overwritten", s)
	}
}

func TestFullPieceRequiresEveryBlockComplete(t *testing.T) {
	t.Setenv("MEDIA_ROOT", t.TempDir())
	v := withScriptRewriteSnapshot(model.LiveAgentPlanVersion{PlanID: 9, VoiceIdentity: playbackIdentity("A")}, scriptRewriteSnapshot{})
	u := volcengineScriptUnit{Pool: "p", Text: "original"}
	addReadyScriptFixture(t, recordingCacheMediaRoot(), v, u, "A", 123)
	cursor := volcengineScriptCursor{Delivered: []scriptDeliveredTrack{
		{Unit: 0, TrackID: "one-0", InstanceID: "one", AssetID: 123, VoiceID: "A", TotalBlocks: 2, Confirmed: true},
		{Unit: 0, TrackID: "one-1", InstanceID: "one", AssetID: 123, VoiceID: "A", TotalBlocks: 2, Confirmed: true},
	}}
	s := &Server{}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	p := coreclient.ProgramRefreshSnapshot{PlaybackReceipts: []anchorcycle.PlaybackReceipt{{TrackID: "one-0", Complete: true}, {TrackID: "one-1", PlayedMS: 100}}}
	for i := 0; i < 2; i++ {
		if err := s.confirmScriptPlayback(ctx, 14, 29, 99, v, []volcengineScriptUnit{u}, &cursor, p, func() error { return nil }); err != nil {
			t.Fatal(err)
		}
		pool, _ := loadScriptVoicePool(recordingCacheMediaRoot(), 9, "p")
		if pool.Versions[0].CompleteCount != 0 {
			t.Fatal("partial multi-block paragraph counted complete")
		}
	}
	p.PlaybackReceipts = []anchorcycle.PlaybackReceipt{{TrackID: "one-1", Complete: true}}
	for i := 0; i < 2; i++ {
		if err := s.confirmScriptPlayback(ctx, 14, 29, 99, v, []volcengineScriptUnit{u}, &cursor, p, func() error { return nil }); err != nil {
			t.Fatal(err)
		}
	}
	pool, _ := loadScriptVoicePool(recordingCacheMediaRoot(), 9, "p")
	if pool.Versions[0].CompleteCount != 1 {
		t.Fatal(pool)
	}
}

func TestSharedRewriteTextCrossesVoicesButNotFacts(t *testing.T) {
	root := t.TempDir()
	a, b := playbackIdentity("A"), playbackIdentity("B")
	v := withScriptRewriteSnapshot(model.LiveAgentPlanVersion{PlanID: 7, VoiceIdentity: a}, scriptRewriteSnapshot{Prompt: "existing prompt", Facts: "price 87.6"})
	u := volcengineScriptUnit{Pool: "piece-1", Text: "original"}
	if err := saveScriptSharedText(root, v, u, "already approved rewrite"); err != nil {
		t.Fatal(err)
	}
	v.VoiceIdentity = b
	text, ok, err := scriptMissingSharedText(root, v, u)
	if err != nil || !ok || text.Text != "already approved rewrite" {
		t.Fatal(text, ok, err)
	}
	// No TTS/model call is required to discover/reuse an existing paid rewrite.
	if err := addScriptVoiceVersion(root, v.PlanID, u.poolID(), scriptplay.Version{VoiceID: "A", Model: a.Model, Kind: scriptplay.KindRewrite, Text: "legacy approved rewrite", Fingerprint: scriptplay.Fingerprint("legacy approved rewrite"), FactsKey: scriptRewriteFactsKey(v), AudioRef: "1"}); err != nil {
		t.Fatal(err)
	}
	scriptVoicePoolMu.Lock()
	all, err := loadScriptSharedTextsLocked(root, v, u)
	scriptVoicePoolMu.Unlock()
	if err != nil || len(all.Versions) != 2 {
		t.Fatal(all, err)
	}
	v = withScriptRewriteSnapshot(v, scriptRewriteSnapshot{Prompt: "existing prompt", Facts: "price 99"})
	if _, ok, err := scriptMissingSharedText(root, v, u); err != nil || ok {
		t.Fatal("obsolete facts reused", ok, err)
	}
}

func TestCompleteReceiptRetirementCountIsIdempotent(t *testing.T) {
	root := t.TempDir()
	v := scriptplay.Version{VoiceID: "A", Model: "m", AudioRef: "10", Kind: scriptplay.KindOriginal, Text: "original", Fingerprint: scriptplay.Fingerprint("original")}
	if err := addScriptVoiceVersion(root, 1, "p", v); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if err := markScriptVersionCompletedOnce(root, 1, "p", "10", "instance-1"); err != nil {
			t.Fatal(err)
		}
	}
	pool, err := loadScriptVoicePool(root, 1, "p")
	if err != nil || pool.Versions[0].CompleteCount != 1 || pool.Versions[0].PlayCount != 0 {
		t.Fatal(pool, err)
	}
}
