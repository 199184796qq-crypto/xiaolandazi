package coreaudio

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"livecompanion/core/internal/audioout"
	"livecompanion/core/internal/roomaudio"
)

func acousticTestWAV(duration, pauseStart, pauseEnd int, amplitude float64) []byte {
	w := testWAV(duration)
	for i := 0; i < (len(w)-44)/2; i++ {
		ms := i / 24
		v := int16(math.Sin(float64(i)*.1) * amplitude)
		if ms >= pauseStart && ms < pauseEnd {
			v = 0
		}
		binary.LittleEndian.PutUint16(w[44+i*2:], uint16(v))
	}
	return w
}

func TestAcousticScanRequiresSustainedPauseAndScalesAcrossVoices(t *testing.T) {
	for _, amp := range []float64{30, 200, 5000, 30000} {
		wav := acousticTestWAV(1600, 700, 900, amp)
		table := scanAcousticPCM(wav[44:])
		if len(table.Points) != 1 || table.Points[0].CutMS != 800 || table.Points[0].Grade != "A" {
			t.Fatalf("amp=%v points=%+v", amp, table.Points)
		}
	}
	if points := scanAcousticPCM(acousticTestWAV(1600, 700, 720, 5000)[44:]).Points; len(points) != 0 {
		t.Fatal("isolated crossing treated as pause", points)
	}
	if points := scanAcousticPCM(acousticTestWAV(1600, -1, -1, 5000)[44:]).Points; len(points) != 0 {
		t.Fatal("voiced audio treated as pause", points)
	}
}

func TestAcousticInteractionRestoresExactPCMForTimedAndUntimedTracks(t *testing.T) {
	for _, untimed := range []bool{false, true} {
		t.Run(map[bool]string{false: "volcengine_with_captions", true: "ali_without_captions"}[untimed], func(t *testing.T) {
			wav := acousticTestWAV(1600, 700, 900, 5000)
			audio := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/reply" {
					w.Write(testWAV(200))
					return
				}
				w.Write(wav)
			}))
			defer audio.Close()
			client, _ := newTestClient(t, 1600)
			engine := roomaudio.New()
			client.SetRoomAudioEngine(engine)
			frames, unsub, _ := engine.Subscribe(99)
			defer unsub()
			start, err := client.StartProgram(context.Background(), audioout.StartProgramInput{RoomID: 99, Sequential: true, Tracks: []audioout.ProgramTrack{{ID: "main", AudioURL: audio.URL, Text: "字幕的句号不能决定切点。", UntimedTranscript: untimed, DurationMS: 1600, Timeline: []audioout.ProgramTimelineSegment{{SegmentID: "sentence", Index: 1, EndMS: 1600, Text: "字幕的句号不能决定切点。", SafeCut: true}}}, {ID: "next", AudioURL: audio.URL, DurationMS: 1600}}})
			if err != nil {
				t.Fatal(err)
			}
			defer client.StopProgram(context.Background(), 99)
			until := time.Now().Add(time.Second)
			for {
				s, _ := client.ProgramSnapshot(context.Background(), 99)
				if len(s.AcousticCuts) > 0 {
					break
				}
				if time.Now().After(until) {
					t.Fatal("PCM scan was not connected to program")
				}
				time.Sleep(time.Millisecond)
			}
			cut, bogusResume := 800, 1500
			result, err := client.InsertTestProgramInteraction(context.Background(), audioout.InsertInteractionInput{RoomID: 99, ExpectedTaskID: start.Task.ID, AudioURL: audio.URL + "/reply", AcousticCut: true, SwitchAtMS: &cut, ResumeOffsetMS: &bogusResume, DeferredReturn: true, SkipCurrentSentence: true})
			if err != nil {
				t.Fatal(err)
			}
			if !result.Suspended || result.ResumeOffsetMS != cut {
				t.Fatal("cut was changed by legacy return policy", result)
			}
			client.mu.RLock()
			legacy := client.programs[99].InteractionReturn
			client.mu.RUnlock()
			if legacy != nil {
				t.Fatal("exact mode created sentence-unit return")
			}
			time.Sleep(260 * time.Millisecond)
			_, err = client.CompleteProgramInteraction(context.Background(), 99, result.Task.ID)
			if err != nil {
				t.Fatal(err)
			}
			var got []byte
			deadline := time.After(3 * time.Second)
			for len(got) < len(wav)-44 {
				select {
				case f := <-frames:
					if f.Source == roomaudio.SourceMainline {
						got = append(got, f.PCM...)
					}
				case <-deadline:
					t.Fatal("mainline did not resume", len(got))
				}
			}
			if !bytes.Equal(got[:len(wav)-44], wav[44:]) {
				t.Fatal("source samples lost or replayed across acoustic insert")
			}
			current, _ := client.ProgramSnapshot(context.Background(), 99)
			if current.Suspended {
				t.Fatal("interaction never returned")
			}
		})
	}
}

func TestAcousticCutCannotBypassPCMValidation(t *testing.T) {
	client, _ := newTestClient(t, 1600)
	client.SetRoomAudioEngine(roomaudio.New())
	audio := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(acousticTestWAV(1600, 700, 900, 5000)) }))
	defer audio.Close()
	start, err := client.StartProgram(context.Background(), audioout.StartProgramInput{RoomID: 99, Sequential: true, Tracks: []audioout.ProgramTrack{{ID: "bound", AudioURL: audio.URL, DurationMS: 1600}}})
	if err != nil {
		t.Fatal(err)
	}
	defer client.StopProgram(context.Background(), 99)
	bad := 400
	_, err = client.InsertTestProgramInteraction(context.Background(), audioout.InsertInteractionInput{RoomID: 99, ExpectedTaskID: start.Task.ID, AudioURL: audio.URL, AcousticCut: true, SwitchAtMS: &bad})
	if !errors.Is(err, audioout.ErrInteractionSwitchStale) {
		t.Fatal("unverified acoustic cut accepted", err)
	}
}

func TestCancelledAcousticReservationDoesNotConsumeOrStopMainline(t *testing.T) {
	wav := acousticTestWAV(1600, 700, 900, 5000)
	audio := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(wav) }))
	defer audio.Close()
	client, _ := newTestClient(t, 1600)
	engine := roomaudio.New()
	client.SetRoomAudioEngine(engine)
	frames, unsub, _ := engine.Subscribe(99)
	defer unsub()
	start, err := client.StartProgram(context.Background(), audioout.StartProgramInput{RoomID: 99, Sequential: true, Tracks: []audioout.ProgramTrack{{ID: "cancel", AudioURL: audio.URL, DurationMS: 1600}}})
	if err != nil {
		t.Fatal(err)
	}
	defer client.StopProgram(context.Background(), 99)
	until := time.Now().Add(time.Second)
	for {
		p, _ := client.ProgramSnapshot(context.Background(), 99)
		if len(p.AcousticCuts) > 0 {
			break
		}
		if time.Now().After(until) {
			t.Fatal("scan not ready")
		}
		time.Sleep(time.Millisecond)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	cut := 800
	_, err = client.InsertTestProgramInteraction(ctx, audioout.InsertInteractionInput{RoomID: 99, ExpectedTaskID: start.Task.ID, AudioURL: audio.URL, AcousticCut: true, SwitchAtMS: &cut})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("cancel did not stop reservation", err)
	}
	var got []byte
	deadline := time.After(3 * time.Second)
	for len(got) < len(wav)-44 {
		select {
		case f := <-frames:
			if f.Source != roomaudio.SourceMainline {
				t.Fatal("canceled interaction emitted PCM")
			}
			got = append(got, f.PCM...)
		case <-deadline:
			t.Fatal("mainline stuck after cancellation")
		}
	}
	if !bytes.Equal(got, wav[44:]) {
		t.Fatal("cancel consumed or replayed original samples")
	}
}

func TestAcousticBPauseAndSourceIdentity(t *testing.T) {
	wav := acousticTestWAV(1600, 700, 900, 5000)
	for i := 44 + 700*48; i < 44+900*48; i += 2 {
		binary.LittleEndian.PutUint16(wav[i:], 200)
	}
	table := scanAcousticPCM(wav[44:])
	if len(table.Points) != 1 || table.Points[0].Grade != "B" {
		t.Fatal("low valley did not produce B", table.Points)
	}
	copyPCM := append([]byte(nil), wav[44:]...)
	copyPCM[1000] ^= 1
	if table.Hash == scanAcousticPCM(copyPCM).Hash {
		t.Fatal("source identity ignored changed PCM")
	}
}
