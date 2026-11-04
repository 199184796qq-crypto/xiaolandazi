package httpapi

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/coreclient"
	"livecompanion/management/internal/scriptplay"
)

func TestGenerationPlayRequiresPCMAndDeduplicatesWholeInstance(t *testing.T) {
	t.Setenv("MEDIA_ROOT", t.TempDir())
	root := recordingCacheMediaRoot()
	v := policyVersion()
	u := volcengineScriptUnit{Pool: "first", Text: "四百字原文"}
	policyFixture(t, root, v, u, 1, u.Text, 75000, scriptplay.KindOriginal)
	cursor := volcengineScriptCursor{Delivered: []scriptDeliveredTrack{
		{Unit: 0, TrackID: "long-99-1-0", InstanceID: "long-99-1", AssetID: 1, VoiceID: "A"},
		{Unit: 0, TrackID: "long-99-1-1", InstanceID: "long-99-1", AssetID: 1, VoiceID: "A"},
	}}
	s := &Server{}
	p := coreclient.ProgramRefreshSnapshot{TrackID: cursor.Delivered[0].TrackID, CurrentMS: 1234, MainlinePlaybackReceipts: []anchorcycle.AnglePlaybackReceipt{{TrackID: cursor.Delivered[0].TrackID, StartedAt: time.Now().Format(time.RFC3339)}}}
	confirm := func() {
		t.Helper()
		if err := s.confirmScriptPlayback(t.Context(), 14, 29, 99, v, []volcengineScriptUnit{u}, &cursor, p, func() error { return nil }); err != nil {
			t.Fatal(err)
		}
	}
	confirm()
	if pending, err := pendingScriptGenerationPlay(root, 14, 29, 99, v); err != nil || pending != nil {
		t.Fatal("queue position/start label granted a purchase", pending, err)
	}
	p.PlaybackReceipts = []anchorcycle.PlaybackReceipt{{TrackID: cursor.Delivered[0].TrackID, PlayedMS: 20}}
	confirm() // Confirmed was already true: PCM must still grant its one opportunity.
	play, err := pendingScriptGenerationPlay(root, 14, 29, 99, v)
	if err != nil || play == nil || play.Piece != u.poolID() {
		t.Fatal(play, err)
	}
	release, ok, err := claimScriptGenerationPlay(t.Context(), root, 14, 29, 99, v, u, play)
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	release()
	for i := 0; i < 100; i++ {
		p.PlaybackReceipts = []anchorcycle.PlaybackReceipt{{TrackID: cursor.Delivered[i%2].TrackID, PlayedMS: int64(i + 30)}}
		confirm()
	}
	if pending, err := pendingScriptGenerationPlay(root, 14, 29, 99, v); err != nil || pending != nil {
		t.Fatal("blocks/progress bought another rewrite", pending, err)
	}
}

func TestGenerationPlayCoalescesDropsInFlightAndDoesNotRetryPaidFailure(t *testing.T) {
	root := t.TempDir()
	v := policyVersion()
	u := volcengineScriptUnit{Pool: "p", Text: "原文"}
	policyFixture(t, root, v, u, 1, u.Text, 75000, scriptplay.KindOriginal)
	for n := 1; n <= 3; n++ {
		if err := recordScriptGenerationPlay(root, 14, 29, 99, v, u, fmt.Sprintf("long-99-%d", n)); err != nil {
			t.Fatal(err)
		}
	}
	play, _ := pendingScriptGenerationPlay(root, 14, 29, 99, v)
	if play == nil || play.Instance != "long-99-3" {
		t.Fatal("backlog retained", play)
	}
	release, ok, err := claimScriptGenerationPlay(t.Context(), root, 14, 29, 99, v, u, play)
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	defer release()
	if err := recordScriptGenerationPlay(root, 14, 29, 99, v, u, "long-99-4"); err != nil {
		t.Fatal(err)
	}
	if pending, _ := pendingScriptGenerationPlay(root, 14, 29, 99, v); pending != nil {
		t.Fatal("play during generation left a queued purchase")
	}
	if _, again, err := claimScriptGenerationPlay(t.Context(), root, 14, 29, 99, v, u, play); again || err != nil {
		t.Fatal("same paid attempt retried", again, err)
	}
}

func TestGenerationPlayConcurrentClaimAndLongCursorReplay(t *testing.T) {
	root := t.TempDir()
	v := policyVersion()
	u := volcengineScriptUnit{Pool: "p", Text: "原文"}
	policyFixture(t, root, v, u, 1, u.Text, 75000, scriptplay.KindOriginal)
	if err := recordScriptGenerationPlay(root, 14, 29, 99, v, u, "long-99-1000"); err != nil {
		t.Fatal(err)
	}
	play, _ := pendingScriptGenerationPlay(root, 14, 29, 99, v)
	var count atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Go(func() {
			release, ok, err := claimScriptGenerationPlay(t.Context(), root, 14, 29, 99, v, u, play)
			if err != nil {
				t.Error(err)
			}
			if ok {
				count.Add(1)
				release()
			}
		})
	}
	wg.Wait()
	if count.Load() != 1 {
		t.Fatal("concurrent purchases", count.Load())
	}
	for i := 1; i <= 1000; i++ {
		if err := recordScriptGenerationPlay(root, 14, 29, 99, v, u, fmt.Sprintf("long-99-%d", i)); err != nil {
			t.Fatal(err)
		}
	}
	if pending, _ := pendingScriptGenerationPlay(root, 14, 29, 99, v); pending != nil {
		t.Fatal("historical receipts minted credit after dedupe horizon")
	}
}

func TestGenerationPlayScopeCancellationAndSharedCache(t *testing.T) {
	root := t.TempDir()
	v := policyVersion()
	u := volcengineScriptUnit{Pool: "p", Text: "原文"}
	policyFixture(t, root, v, u, 1, u.Text, 75000, scriptplay.KindOriginal)
	for _, room := range []int64{29, 30} {
		if err := recordScriptGenerationPlay(root, 14, room, 99, v, u, "long-99-1"); err != nil {
			t.Fatal(err)
		}
	}
	play, _ := pendingScriptGenerationPlay(root, 14, 29, 99, v)
	changed := v
	changed.VoiceIdentity.Rate = 1.05
	if pending, _ := pendingScriptGenerationPlay(root, 14, 29, 99, changed); pending != nil {
		t.Fatal("rate change inherited opportunity")
	}
	changed = v
	changed.ID++
	if pending, _ := pendingScriptGenerationPlay(root, 14, 29, 99, changed); pending != nil {
		t.Fatal("plan version inherited opportunity")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, ok, _ := claimScriptGenerationPlay(ctx, root, 14, 29, 99, v, u, play); ok {
		t.Fatal("stopped room bought a rewrite")
	}
	policyFixture(t, root, v, u, 2, "另一个房间已生成", 75000, scriptplay.KindRewrite)
	other, _ := pendingScriptGenerationPlay(root, 14, 30, 99, v)
	if _, ok, err := claimScriptGenerationPlay(t.Context(), root, 14, 30, 99, v, u, other); ok || err != nil {
		t.Fatal("same shared fill was bought again", ok, err)
	}
	if err := recordScriptGenerationPlay(root, 14, 29, 100, v, u, "long-100-1"); err != nil {
		t.Fatal(err)
	}
	_ = recordScriptGenerationPlay(root, 14, 29, 99, v, u, "long-99-2000")
	if pending, _ := pendingScriptGenerationPlay(root, 14, 29, 99, v); pending != nil {
		t.Fatal("old session resurrected")
	}
	if pending, _ := pendingScriptGenerationPlay(root, 14, 29, 100, v); pending == nil {
		t.Fatal("late old receipt damaged new session")
	}
}

func TestGenerationPolicyOnlyFillsHeardPieceAndNoPurchaseWithoutPlay(t *testing.T) {
	t.Setenv("MEDIA_ROOT", t.TempDir())
	root := recordingCacheMediaRoot()
	v := policyVersion()
	units := []volcengineScriptUnit{{Pool: "heard", Text: "读过的原文"}, {Pool: "unheard", Text: "未读的原文"}}
	for i, u := range units {
		policyFixture(t, root, v, u, int64(i+1), u.Text, 75000, scriptplay.KindOriginal)
	}
	policyFixture(t, root, v, units[0], 3, "已经有一版", 75000, scriptplay.KindRewrite)
	index, _, _, err := scriptPolicyGenerationForPiece(root, v, units, "heard", time.Now().UTC(), false)
	if err != nil || index != 0 {
		t.Fatal("selected unplayed shallower piece", index, err)
	}
	// The public worker is independently fenced; calling it cannot bypass PCM.
	units[0].Dir = filepath.Join(root, "units", "heard")
	ctx := agentgateway.WithRoomModelScope(t.Context(), agentgateway.RoomModelScope{TenantID: 14, RoomID: 29, RuntimeID: 99})
	(&Server{}).maybeGenerateRewriteVersion(ctx, 14, 29, v, units[0])
	if pending, _ := pendingScriptGenerationPlay(root, 14, 29, 99, v); pending != nil {
		t.Fatal("worker fabricated play evidence")
	}
}

func TestGenerationCold400CharactersAndFivePiecesBuyOnlyAfterTheirPlay(t *testing.T) {
	for _, pieces := range []int{1, 5} {
		t.Run(fmt.Sprint(pieces), func(t *testing.T) {
			root := t.TempDir()
			v := policyVersion()
			var units []volcengineScriptUnit
			for i := 0; i < pieces; i++ {
				u := volcengineScriptUnit{Pool: fmt.Sprint(i), Text: fmt.Sprint(i) + strings.Repeat("字", 399)}
				units = append(units, u)
				policyFixture(t, root, v, u, int64(i+1), u.Text, 75000, scriptplay.KindOriginal)
			}
			purchases := 0
			for i, u := range units {
				instance := fmt.Sprintf("long-99-%d", i+1)
				if err := recordScriptGenerationPlay(root, 14, 29, 99, v, u, instance); err != nil {
					t.Fatal(err)
				}
				for poll := 0; poll < 50; poll++ {
					play, err := pendingScriptGenerationPlay(root, 14, 29, 99, v)
					if err != nil {
						t.Fatal(err)
					}
					if play == nil {
						continue
					}
					index, _, _, err := scriptPolicyGenerationForPiece(root, v, units, play.Piece, time.Now().UTC(), false)
					if err != nil || index != i {
						t.Fatal("unheard piece purchased", index, err)
					}
					release, ok, err := claimScriptGenerationPlay(t.Context(), root, 14, 29, 99, v, units[index], play)
					if err != nil || !ok {
						t.Fatal(ok, err)
					}
					purchases++
					policyFixture(t, root, v, u, int64(100+i), "第一次使用后才准备的新版"+u.Text, 75000, scriptplay.KindRewrite)
					release()
				}
				if purchases != i+1 {
					t.Fatal("one play filled several versions", purchases)
				}
				for j, piece := range units {
					pool := scriptCompatiblePool(root, v, piece)
					want := 1
					if j <= i {
						want++
					}
					if len(pool.Versions) != want {
						t.Fatal("unplayed piece filled in advance", j, len(pool.Versions), want)
					}
				}
			}
		})
	}
}

func TestGenerationMatureBatchWaitsForEachRetirementPieceToBeHeard(t *testing.T) {
	root := t.TempDir()
	v := policyVersion()
	units := []volcengineScriptUnit{{Pool: "a", Text: "原文A"}, {Pool: "b", Text: "原文B"}}
	for i, u := range units {
		for n := 0; n < 4; n++ {
			kind, text := scriptplay.KindRewrite, fmt.Sprintf("片段%d变化%d", i, n)
			if n == 0 {
				kind, text = scriptplay.KindOriginal, u.Text
			}
			policyFixture(t, root, v, u, int64(i*10+n+1), text, 10*60000, kind)
		}
		pool, _ := loadScriptVoicePool(root, v.PlanID, u.poolID())
		for n := range pool.Versions {
			pool.Versions[n].CompleteCount = 4
		}
		if err := saveScriptVoicePool(root, v.PlanID, u.poolID(), pool); err != nil {
			t.Fatal(err)
		}
	}
	st := scriptPoolPolicyState{FirstObserved: time.Now().UTC(), Retire: []scriptPolicyRetirement{{Piece: "b", Ref: "12"}, {Piece: "a", Ref: "2"}}}
	if err := writeVolcengineJSON(scriptPoolPolicyPath(root, v), st); err != nil {
		t.Fatal(err)
	}
	i, _, ref, err := scriptPolicyGenerationForPiece(root, v, units, "a", time.Now().UTC(), false)
	if err != nil || i != 0 || ref != "2" {
		t.Fatal("batch forced generation for unplayed head item", i, ref, err)
	}
	// A frozen batch alone is not a playback opportunity and cannot be claimed.
	if pending, _ := pendingScriptGenerationPlay(root, 14, 29, 99, v); pending != nil {
		t.Fatal("retirement batch fabricated a play")
	}
}
