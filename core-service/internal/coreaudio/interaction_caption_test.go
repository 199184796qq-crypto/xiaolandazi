package coreaudio

import (
	"context"
	"livecompanion/core/internal/audioout"
	"livecompanion/core/internal/roomaudio"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestUntimedInteractionShowsCurrentTTSBodyAndClearsOnMainlineReturn(t *testing.T) {
	main := acousticTestWAV(1600, 700, 900, 5000)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/reply" {
			_, _ = w.Write(testWAV(400))
			return
		}
		_, _ = w.Write(main)
	}))
	defer server.Close()
	c, _ := newTestClient(t, 1600)
	e := roomaudio.New()
	c.SetRoomAudioEngine(e)
	start, err := c.StartProgram(t.Context(), audioout.StartProgramInput{RoomID: 903, Sequential: true, Tracks: []audioout.ProgramTrack{{ID: "untimed-main", Text: "这是没有字幕的主线原文，不应当显示。", AudioURL: server.URL, DurationMS: 1600, UntimedTranscript: true}, {ID: "next", AudioURL: server.URL, DurationMS: 1600, UntimedTranscript: true}}})
	if err != nil {
		t.Fatal(err)
	}
	defer c.StopProgram(context.Background(), 903)
	until := time.Now().Add(time.Second)
	for {
		p, _ := c.ProgramSnapshot(t.Context(), 903)
		if len(p.AcousticCuts) > 0 {
			break
		}
		if time.Now().After(until) {
			t.Fatal("acoustic scan not ready")
		}
		time.Sleep(time.Millisecond)
	}
	if f := e.Snapshot(903).SpeechFeed; f.Current != nil {
		t.Fatal("untimed mainline text leaked", f)
	}
	cut := 800
	text := "欢迎新朋友。这个问题我来回答。"
	reply, err := c.InsertTestProgramInteraction(t.Context(), audioout.InsertInteractionInput{RoomID: 903, ExpectedTaskID: start.Task.ID, AudioURL: server.URL + "/reply", Text: text, AcousticCut: true, SwitchAtMS: &cut})
	if err != nil {
		t.Fatal(err)
	}
	until = time.Now().Add(2 * time.Second)
	for {
		f := e.Snapshot(903).SpeechFeed
		if f.Current != nil {
			if f.Current.Text != text {
				t.Fatal("only full current TTS body should display", f)
			}
			break
		}
		if time.Now().After(until) {
			t.Fatal("current interaction body was hidden")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if _, err = c.CompleteProgramInteraction(t.Context(), 903, reply.Task.ID); err != nil {
		t.Fatal(err)
	}
	until = time.Now().Add(time.Second)
	for {
		state := e.Snapshot(903)
		if state.ActiveSource == roomaudio.SourceMainline {
			f := state.SpeechFeed
			if f.Current != nil || f.Previous != nil || f.Next != nil {
				t.Fatal("reply caption survived mainline return", f)
			}
			break
		}
		if time.Now().After(until) {
			t.Fatal("mainline did not resume")
		}
		time.Sleep(5 * time.Millisecond)
	}
}
