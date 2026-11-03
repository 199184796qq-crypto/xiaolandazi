package httpapi

import (
	"fmt"
	"path/filepath"
	"testing"

	"livecompanion/management/internal/model"
	"livecompanion/management/internal/scriptplay"
)

func TestFullPoolRefreshIsFiniteThirtyPercentBatch(t *testing.T) {
	root := t.TempDir()
	v := model.LiveAgentPlanVersion{PlanID: 7, VoiceIdentity: playbackIdentity("A")}
	u := volcengineScriptUnit{Pool: "entry-1", Text: "original"}
	pool := scriptplay.Pool{}
	for i := 0; i < 10; i++ {
		pool.Versions = append(pool.Versions, scriptplay.Version{AudioRef: fmt.Sprint(i), Kind: scriptplay.KindRewrite, PlayCount: 4, CompleteCount: 4})
	}
	pool.Versions[0].Kind = scriptplay.KindOriginal
	if _, ok, err := scriptRefreshRetire(root, v, u, scriptplay.Pool{Versions: pool.Versions[:9]}, 10); err != nil || ok {
		t.Fatal("partial pool refreshed", ok, err)
	}
	pool.Versions[2].CompleteCount = 3
	if _, ok, err := scriptRefreshRetire(root, v, u, pool, 10); err != nil || ok {
		t.Fatal("partial playback counted", ok, err)
	}
	pool.Versions[2].CompleteCount = 4
	retired := map[string]bool{}
	for i := 0; i < 3; i++ {
		ref, ok, err := scriptRefreshRetire(root, v, u, pool, 10)
		if err != nil || !ok || ref == "0" || retired[ref] {
			t.Fatal(ref, ok, err)
		}
		// A budget deferral retains this exact replacement, rather than rolling again.
		again, ok, err := scriptRefreshRetire(root, v, u, pool, 10)
		if err != nil || !ok || again != ref {
			t.Fatal(again, ref, ok, err)
		}
		retired[ref] = true
		for j := range pool.Versions {
			if pool.Versions[j].AudioRef == ref {
				pool.Versions[j] = scriptplay.Version{AudioRef: fmt.Sprint(100 + i), Kind: scriptplay.KindRewrite}
			}
		}
	}
	if _, ok, err := scriptRefreshRetire(root, v, u, pool, 10); err != nil || ok {
		t.Fatal("more than 30% replaced before new members played", ok, err)
	}
}

func TestRetiredSharedTextDoesNotRebuySameVoiceButNewRateCanReuseText(t *testing.T) {
	root := t.TempDir()
	v := withScriptRewriteSnapshot(model.LiveAgentPlanVersion{PlanID: 8, VoiceIdentity: playbackIdentity("A")}, scriptRewriteSnapshot{Prompt: "existing", Facts: "stable"})
	u := volcengineScriptUnit{Pool: "entry-2", Text: "original"}
	text := "approved legacy rewrite"
	key := scriptFirstParagraphKey(v, text)
	if err := addScriptVoiceVersion(root, v.PlanID, u.poolID(), scriptplay.Version{Text: text, Fingerprint: scriptplay.Fingerprint(text), FactsKey: scriptRewriteFactsKey(v), Kind: scriptplay.KindRewrite, VoiceID: "A", Model: v.VoiceIdentity.Model, AudioRef: "1", SourcePath: filepath.Join(root, key)}); err != nil {
		t.Fatal(err)
	}
	if err := saveScriptSharedText(root, v, u, text); err != nil {
		t.Fatal(err)
	}
	// Simulate retirement from A's audio slots; its approved text stays shared.
	scriptVoicePoolMu.Lock()
	err := saveScriptVoicePoolLocked(root, v.PlanID, u.poolID(), scriptplay.Pool{})
	scriptVoicePoolMu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if _, found, err := scriptMissingSharedText(root, v, u); err != nil || found {
		t.Fatal("retired text re-entered same audio pool", found, err)
	}
	v.VoiceIdentity.Rate = 1.05
	if shared, found, err := scriptMissingSharedText(root, v, u); err != nil || !found || shared.Text != text {
		t.Fatal("changed synthesis settings could not reuse text", shared, found, err)
	}
	v.VoiceIdentity.VoiceID = "B"
	if shared, found, err := scriptMissingSharedText(root, v, u); err != nil || !found || shared.Text != text {
		t.Fatal(shared, found, err)
	}
}
