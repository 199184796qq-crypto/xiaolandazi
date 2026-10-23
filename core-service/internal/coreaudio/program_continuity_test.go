package coreaudio

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"livecompanion/core/internal/audiohub"
	"livecompanion/core/internal/audioout"
	"livecompanion/core/internal/roomaudio"
)

func seedCompactionQueue(t *testing.T, index int) (*Client, *programState, audioout.RoomProgramSnapshot, string) {
	t.Helper()
	c, p, start, url := sequentialFixture(t)
	c.mu.Lock()
	template := p.Tracks[0]
	p.Tracks = make([]programTrack, index+2)
	for i := range p.Tracks {
		track := template
		track.ID, track.Text = fmt.Sprintf("stable-%d", i), fmt.Sprintf("第%d段完整内容。", i)
		track.AllowConsecutiveRepeat = true
		track.Timeline = []audioout.ProgramTimelineSegment{{SegmentID: "before", EndMS: 5419, Text: "切点前内容。", SafeCut: true}, {SegmentID: "after", StartMS: 5419, EndMS: 60000, Text: "切点后内容。", SafeCut: true}}
		p.Tracks[i] = track
	}
	p.TrackIndex = index
	p.MainlinePosition = programPositionAt(p, index)
	c.mu.Unlock()
	return c, p, start, url
}

func TestAcceptedAppendRetryDoesNotProbeUnavailableOrigin(t *testing.T) {
	c, _, start, _ := sequentialFixture(t)
	var unavailable atomic.Bool
	var requests atomic.Int32
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if unavailable.Load() {
			http.Error(w, "origin offline", 503)
			return
		}
		w.Header().Set("Content-Type", "audio/wav")
		_, _ = w.Write(testWAV(1000))
	}))
	defer source.Close()
	input := appendFixtureInput(start, source.URL)
	accepted, err := c.AppendProgram(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	before := requests.Load()
	unavailable.Store(true)
	retried, err := c.AppendProgram(context.Background(), input)
	if err != nil || retried.TrackCount != accepted.TrackCount || requests.Load() != before {
		t.Fatal("lost ack retry re-probed or duplicated accepted audio", retried, err, requests.Load(), before)
	}
	input.Tracks[0].Text = "不能复用编号覆盖另一段内容。"
	if _, err = c.AppendProgram(context.Background(), input); err == nil {
		t.Fatal("idempotence accepted conflicting digest")
	}
}

func TestCompactionKeepsReservedAndPlayingInteractionIdentity(t *testing.T) {
	for _, index := range []int{63, 64, 65} {
		for _, playing := range []bool{false, true} {
			t.Run(fmt.Sprintf("index%d_playing%t", index, playing), func(t *testing.T) {
				c, p, start, url := seedCompactionQueue(t, index)
				c.mu.Lock()
				state := buildInteractionReturn(p, "answer", "实际回答。", 5419, 4000, true)
				p.InteractionReturn = state
				p.Suspended = playing
				p.PlannedNextTrack, p.PrefetchedTrack = index+1, index+1
				p.PrefetchedAudio = []byte("verified future audio")
				token := state.snapshot.Token
				c.mu.Unlock()
				input := appendFixtureInput(start, url)
				appended, err := c.AppendProgram(context.Background(), input)
				if err != nil {
					t.Fatal(err)
				}
				wantIndex := index
				if index >= 64 {
					wantIndex = 4
				}
				if appended.TrackIndex != wantIndex || appended.TrackID != fmt.Sprintf("stable-%d", index) {
					t.Fatalf("wrong current identity after compaction: %+v", appended)
				}
				c.mu.Lock()
				if p.PlannedNextTrack != wantIndex+1 || p.PrefetchedTrack != wantIndex+1 || state.snapshot.Token != token || state.snapshot.Units[0].TrackIndex != wantIndex {
					t.Fatal("pending references were not migrated together")
				}
				p.CurrentTaskID, p.Suspended, state.finished = "answer", true, true
				c.mu.Unlock()
				c.hub.Expire(start.Task.ID)
				c.resumeAfterInteraction(p, "answer")
				after, _ := c.ProgramSnapshot(context.Background(), start.RoomID)
				published, _ := c.hub.Snapshot(after.Task.ID)
				if after.TrackID != fmt.Sprintf("stable-%d", index) || after.TrackIndex != wantIndex || published.Task.StartMS != 5419 || after.Suspended || !after.Running {
					t.Fatalf("return changed the semantic position: %+v", after)
				}
				repeated, err := c.AppendProgram(context.Background(), input)
				if err != nil || repeated.TrackCount != appended.TrackCount {
					t.Fatal("retry duplicated a previously accepted unit", repeated, err)
				}
				input.Tracks[0].ID = "new-after-return"
				if _, err = c.AppendProgram(context.Background(), input); err != nil {
					t.Fatal("next append failed after the return", err)
				}
			})
		}
	}
}

func TestRepeatedCompactionKeepsTrackAndInvalidatesRetiredPrefetch(t *testing.T) {
	c, p, start, url := seedCompactionQueue(t, 64)
	for cycle := 0; cycle < 3; cycle++ {
		c.mu.Lock()
		for len(p.Tracks) < 66 {
			track := p.Tracks[len(p.Tracks)-1]
			track.ID = fmt.Sprintf("future-%d-%d", cycle, len(p.Tracks))
			p.Tracks = append(p.Tracks, track)
		}
		p.TrackIndex = 64
		want := p.Tracks[64].ID
		p.MainlinePosition = programPositionAt(p, 64)
		p.PlannedNextTrack, p.PrefetchedTrack = 1, 1
		p.PrefetchedAudio = []byte("retired")
		c.mu.Unlock()
		input := appendFixtureInput(start, url)
		input.Tracks[0].ID = fmt.Sprintf("append-cycle-%d", cycle)
		input.Tracks[0].AllowConsecutiveRepeat = true
		current, err := c.AppendProgram(context.Background(), input)
		if err != nil || current.TrackIndex != 4 || current.TrackID != want || current.TrackCount != 7 {
			t.Fatalf("cycle%d: %+v %v", cycle, current, err)
		}
		c.mu.RLock()
		stale := p.PrefetchedTrack != -1 || len(p.PrefetchedAudio) != 0 || p.PlannedNextTrack != -1
		c.mu.RUnlock()
		if stale {
			t.Fatal("retired references remained usable")
		}
	}
}

func TestInvalidCursorUsesOnlyPinnedIdentityAndRejectsChangedGeneration(t *testing.T) {
	c, p, start, url := seedCompactionQueue(t, 64)
	c.mu.Lock()
	p.TrackIndex = 400
	c.mu.Unlock()
	if restored, err := c.AppendProgram(context.Background(), appendFixtureInput(start, url)); err != nil || restored.TrackID != "stable-64" {
		t.Fatal("lost recoverable stable cursor", restored, err)
	}
	c.mu.Lock()
	p.TrackIndex = 400
	p.MainlinePosition.TrackID = "missing"
	c.mu.Unlock()
	if _, err := c.AppendProgram(context.Background(), appendFixtureInput(start, url)); err == nil {
		t.Fatal("unknown cursor was silently reset to the first track")
	}
	c.mu.Lock()
	p.TrackIndex = 4
	p.InteractionReturn = buildInteractionReturn(p, "answer", "实际回答。", 5419, 4000, true)
	p.Suspended, p.CurrentTaskID = true, "answer"
	p.Generation++
	c.mu.Unlock()
	if _, err := c.RecoverProgramInteraction(context.Background(), p.RoomID, "answer"); err == nil {
		t.Fatal("stale generation authorized resuming old audio")
	}
}

func waitContinuity(t *testing.T, f func() bool) {
	t.Helper()
	until := time.Now().Add(3 * time.Second)
	for time.Now().Before(until) {
		if f() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("continuity condition did not become true")
}

func TestPublishFailureRetriesAcceptedTrackAndStopCancelsRecovery(t *testing.T) {
	for _, stop := range []bool{false, true} {
		t.Run(fmt.Sprint("stop=", stop), func(t *testing.T) {
			c, p, start, _ := sequentialFixture(t)
			c.hub.Expire(start.Task.ID)
			_, err := c.hub.Publish(audiohub.Task{ID: "temporary-conflict", RoomID: p.RoomID, Kind: "mainline_program", AudioURL: start.Task.AudioURL, DurationMS: 60000, StartedAt: time.Now()})
			if err != nil {
				t.Fatal(err)
			}
			c.advanceMainline(p, start.Task.ID)
			c.mu.RLock()
			recovering := p.Recovery != nil
			c.mu.RUnlock()
			if !recovering {
				t.Fatal("failed publish has no recovery owner")
			}
			if stop {
				c.StopProgram(context.Background(), p.RoomID)
			}
			c.hub.Expire("temporary-conflict")
			if stop {
				time.Sleep(350 * time.Millisecond)
				current, _ := c.ProgramSnapshot(context.Background(), p.RoomID)
				if current.Running || c.hub.ActiveTask(p.RoomID) != nil {
					t.Fatal("recovery restarted a stopped show")
				}
			} else {
				waitContinuity(t, func() bool {
					current, _ := c.ProgramSnapshot(context.Background(), p.RoomID)
					return current.TrackID == "B" && current.Task.ID != start.Task.ID
				})
			}
		})
	}
}

func TestMainlineSourceRetryResumesPCMAndPermanentFailureSkipsIncomplete(t *testing.T) {
	for _, kind := range []string{"http_once", "partial_once", "permanent"} {
		t.Run(kind, func(t *testing.T) {
			c, _ := newTestClient(t, 400)
			var requests atomic.Int32
			wav := testWAV(400)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "audio/wav")
				if r.URL.Path == "/bad" {
					n := requests.Add(1)
					if kind == "permanent" || (kind == "http_once" && n == 1) {
						w.WriteHeader(503)
						return
					}
					if kind == "partial_once" && n == 1 {
						w.Header().Set("Content-Length", strconv.Itoa(len(wav)))
						w.Write(wav[:44+roomaudio.PCMBytesPerFrame*4])
						return
					}
				}
				w.Write(wav)
			}))
			defer server.Close()
			engine := roomaudio.New()
			c.SetRoomAudioEngine(engine)
			_, unsubscribe, err := engine.Subscribe(21)
			if err != nil {
				t.Fatal(err)
			}
			defer unsubscribe()
			_, err = c.startPreparedProgram(21, "owned", "continuity", 10, 1, []programTrack{{ID: "bad", AudioURL: server.URL + "/bad", DurationMS: 400, Text: "已验收的原文。"}, {ID: "good", AudioURL: server.URL + "/good", DurationMS: 400, Text: "下一段已验收内容。"}}, true)
			if err != nil {
				t.Fatal(err)
			}
			defer c.StopProgram(context.Background(), 21)
			waitContinuity(t, func() bool {
				current, _ := c.ProgramSnapshot(context.Background(), 21)
				return current.Running && current.WaitingForContent
			})
			current, _ := c.ProgramSnapshot(context.Background(), 21)
			if kind == "permanent" {
				if requests.Load() != 3 || len(current.PlaybackReceipts) != 2 || current.PlaybackReceipts[0].Complete || !current.PlaybackReceipts[0].Terminal || current.PlaybackReceipts[0].PlayedMS != 0 {
					t.Fatalf("failed asset was counted as played: requests=%d receipts=%+v", requests.Load(), current.PlaybackReceipts)
				}
			} else if requests.Load() != 2 || current.MainlinePlayedMS != 800 || len(current.PlaybackReceipts) != 2 || !current.PlaybackReceipts[0].Complete {
				t.Fatalf("retry lost or replayed PCM: requests=%d snapshot=%+v", requests.Load(), current)
			}
		})
	}
}

func TestInteractionFailureRecoveryRespectsManualPauseAndTaskOwnership(t *testing.T) {
	for _, condition := range []string{"failed", "manual_pause", "stopped", "changed_task"} {
		t.Run(condition, func(t *testing.T) {
			c, p, start, _ := sequentialFixture(t)
			c.hub.Expire(start.Task.ID)
			c.mu.Lock()
			p.InteractionReturn = buildInteractionReturn(p, "answer", "实际回答。", 0, 4000, true)
			p.Suspended, p.CurrentTaskID = true, "answer"
			switch condition {
			case "manual_pause":
				p.ManualPaused = true
			case "stopped":
				p.Running = false
			case "changed_task":
				p.CurrentTaskID = "different-answer"
			}
			c.mu.Unlock()
			current, err := c.RecoverProgramInteraction(context.Background(), p.RoomID, "answer")
			if err != nil {
				t.Fatal(err)
			}
			if condition == "failed" {
				if current.Suspended || current.Task == nil || current.Task.Kind == "interaction_audio" {
					t.Fatal("failed interaction left the mainline suspended", current)
				}
			} else if c.hub.ActiveTask(p.RoomID) != nil {
				t.Fatal("stale recovery published audio", current)
			}
		})
	}
}

func TestFailedRefreshPublishRetriesNewPlaylistWithoutReplayingOldTrack(t *testing.T) {
	c, p, start, url := sequentialFixture(t)
	c.mu.Lock()
	p.TrackIndex = len(p.Tracks) - 1
	p.MainlinePosition = programPositionAt(p, p.TrackIndex)
	c.mu.Unlock()
	input := refreshFixtureInput(start, url)
	input.Tracks = []audioout.ProgramTrack{{ID: "new-generation", AudioURL: url, DurationMS: 60000}}
	if _, err := c.RefreshProgram(context.Background(), input); err != nil {
		t.Fatal(err)
	}
	c.hub.Expire(start.Task.ID)
	if _, err := c.hub.Publish(audiohub.Task{ID: "temporary-conflict", RoomID: p.RoomID, Kind: "mainline_program", AudioURL: start.Task.AudioURL, DurationMS: 60000, StartedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	c.advanceMainline(p, start.Task.ID)
	c.hub.Expire("temporary-conflict")
	waitContinuity(t, func() bool {
		current, _ := c.ProgramSnapshot(context.Background(), p.RoomID)
		return current.Generation == 1 && current.TrackID == "new-generation" && current.VersionID == 11
	})
	c.mu.RLock()
	replayed := p.TrackPlayCount["B"]
	c.mu.RUnlock()
	if replayed != 0 {
		t.Fatal("refresh recovery replayed the consumed old track")
	}
}

func TestSourceRetryStopsImmediatelyOnManualControl(t *testing.T) {
	for _, stop := range []bool{false, true} {
		t.Run(fmt.Sprint("stop=", stop), func(t *testing.T) {
			c, _ := newTestClient(t, 400)
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				w.WriteHeader(503)
			}))
			defer server.Close()
			c.SetRoomAudioEngine(roomaudio.New())
			if _, err := c.startPreparedProgram(21, "owned", "continuity", 10, 1, []programTrack{{ID: "temporarily-unavailable", AudioURL: server.URL, DurationMS: 400}}, true); err != nil {
				t.Fatal(err)
			}
			waitContinuity(t, func() bool { return requests.Load() == 1 })
			if stop {
				c.StopProgram(context.Background(), 21)
			} else {
				c.PauseProgram(context.Background(), 21)
				defer c.StopProgram(context.Background(), 21)
			}
			time.Sleep(350 * time.Millisecond)
			if requests.Load() != 1 || c.hub.ActiveTask(21) != nil {
				t.Fatal("old mirror retried after manual control")
			}
		})
	}
}

func TestLatePrefetchIsResolvedByTrackIdentityAfterCompaction(t *testing.T) {
	c, p, start, url := seedCompactionQueue(t, 64)
	requested, release := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(requested)
		<-release
		w.Write([]byte("future-65-audio"))
	}))
	defer server.Close()
	c.mu.Lock()
	p.PlannedNextTrack, p.PlannedForTaskID = 65, start.Task.ID
	p.Tracks[65].AudioURL = server.URL
	c.mu.Unlock()
	c.prefetchMainlineTrack(p, start.Task.ID, 65)
	<-requested
	if _, err := c.AppendProgram(context.Background(), appendFixtureInput(start, url)); err != nil {
		close(release)
		t.Fatal(err)
	}
	close(release)
	waitContinuity(t, func() bool {
		c.mu.RLock()
		defer c.mu.RUnlock()
		return p.PrefetchedTrack == 5 && string(p.PrefetchedAudio) == "future-65-audio"
	})
}

func TestStopFencesSlowStartBeforeAnyProgramExists(t *testing.T) {
	c, _ := newTestClient(t, 400)
	requested, release := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(requested)
		<-release
		w.Header().Set("Content-Type", "audio/wav")
		w.Write(testWAV(400))
	}))
	defer server.Close()
	done := make(chan error, 1)
	go func() {
		_, err := c.StartProgram(context.Background(), audioout.StartProgramInput{RoomID: 21, SessionID: "old-start", VersionID: 10, Tracks: []audioout.ProgramTrack{{ID: "a", AudioURL: server.URL, DurationMS: 400}}})
		done <- err
	}()
	<-requested
	c.StopProgram(context.Background(), 21)
	close(release)
	if err := <-done; !errors.Is(err, ErrProgramStartCancelled) {
		t.Fatal("late start escaped the stop fence", err)
	}
	current, _ := c.ProgramSnapshot(context.Background(), 21)
	if current.Running || c.hub.ActiveTask(21) != nil {
		t.Fatal("stopped room was revived by late prepared audio", current)
	}
}

func TestNewerStartFencesOlderPreparedAudio(t *testing.T) {
	c, _ := newTestClient(t, 400)
	requested, release := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/old" {
			close(requested)
			<-release
		}
		w.Header().Set("Content-Type", "audio/wav")
		w.Write(testWAV(400))
	}))
	defer server.Close()
	done := make(chan error, 1)
	go func() {
		_, err := c.StartProgram(context.Background(), audioout.StartProgramInput{RoomID: 21, SessionID: "old-start", VersionID: 10, Tracks: []audioout.ProgramTrack{{ID: "old", AudioURL: server.URL + "/old", DurationMS: 400}}})
		done <- err
	}()
	<-requested
	newer, err := c.StartProgram(context.Background(), audioout.StartProgramInput{RoomID: 21, SessionID: "new-start", VersionID: 11, Tracks: []audioout.ProgramTrack{{ID: "new", AudioURL: server.URL + "/new", DurationMS: 400}}})
	if err != nil {
		close(release)
		t.Fatal(err)
	}
	defer c.StopProgram(context.Background(), 21)
	close(release)
	if err = <-done; !errors.Is(err, ErrProgramStartCancelled) {
		t.Fatal("older start took ownership after newer start", err)
	}
	current, _ := c.ProgramSnapshot(context.Background(), 21)
	if current.ProgramID != newer.ProgramID || current.TrackID != "new" || current.Task.SessionID != "new-start" {
		t.Fatal("older request replaced the new session", current)
	}
}

func TestQueueFullPreservesPendingUnitAndRetryIdentity(t *testing.T) {
	c, p, start, url := seedCompactionQueue(t, 0)
	c.mu.Lock()
	for len(p.Tracks) < 256 {
		track := p.Tracks[0]
		track.ID = fmt.Sprintf("future-%d", len(p.Tracks))
		p.Tracks = append(p.Tracks, track)
	}
	c.mu.Unlock()
	input := appendFixtureInput(start, url)
	if _, err := c.AppendProgram(context.Background(), input); !errors.Is(err, ErrProgramQueueFull) {
		t.Fatal("queue capacity is not reported as retryable", err)
	}
	c.mu.Lock()
	_, seen := p.AppendedUnits[input.Tracks[0].ID]
	p.TrackIndex = 1
	c.mu.Unlock()
	if seen {
		t.Fatal("rejected unit consumed its append receipt")
	}
	if _, err := c.AppendProgram(context.Background(), input); err != nil {
		t.Fatal("pending unit cannot retry once queue capacity is available", err)
	}
}
