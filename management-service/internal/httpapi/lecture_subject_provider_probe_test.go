package httpapi

import (
	"context"
	"encoding/json"
	"os"
	"strconv"
	"testing"
	"time"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/config"
	appdb "livecompanion/management/internal/db"
)

// Explicit text-only replay of selected lectures. No adoption, TTS or playback.
func TestLectureSubjectProviderProbe(t *testing.T) {
	if os.Getenv("LECTURE_SUBJECT_PROVIDER_PROBE") != "1" {
		t.Skip("explicit paid lecture test required")
	}
	number := func(name string) int64 {
		v, e := strconv.ParseInt(os.Getenv(name), 10, 64)
		if e != nil || v <= 0 {
			t.Fatalf("invalid probe scope: %s", name)
		}
		return v
	}
	tenant, plan, taskID := number("LECTURE_SUBJECT_TENANT"), number("LECTURE_SUBJECT_PLAN"), number("LECTURE_SUBJECT_TASK")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	store, e := appdb.Open(config.Load())
	if e != nil {
		t.Fatal("database unavailable")
	}
	defer store.Close()
	lib, e := store.LectureLibrary(ctx, tenant, plan, false)
	if e != nil {
		t.Fatal(e)
	}
	settings, e := store.AnchorGenerationSettings(ctx)
	if e != nil {
		t.Fatal(e)
	}
	speech, e := store.ResolveSpeechModel(ctx, settings.Cycle.TextProfileID)
	if e != nil || speech == nil {
		t.Fatal("text model unavailable")
	}
	policy, source, e := loadP0Mother()
	if e != nil {
		t.Fatal(e)
	}
	server := &Server{store: store}
	rows := []map[string]any{}
	for _, task := range lib.Tasks {
		if task.ID != taskID || !task.Valid || task.RootFact == nil {
			continue
		}
		for _, angle := range task.Angles {
			for round := 0; round < 2; round++ {
				active := anchorcycle.Task{ProductMode: "lecture", LectureTopic: task.ID, SellingPoint: task.Title, Angle: angle.Title + "：" + angle.Goal, LecturePurpose: task.Purpose, LectureFacts: task.Evidence()}
				state := anchorcycle.State{Config: anchorcycle.Config{MaxChars: 220, TextProfileID: speech.ID, FeedbackID: speech.ID}, TextModel: speech, Lectures: &anchorcycle.LectureRuntime{Active: &active}}
				if card, err := store.GetActiveLiveAnchorStyleScorecard(ctx, tenant, plan); err == nil {
					state.BaselineJSON = card.EffectiveScorecardJSON()
				}
				if e = state.InstallP0(policy, source); e != nil {
					t.Fatal(e)
				}
				messages, e := renderLectureTaskMessages(&state)
				if e != nil {
					t.Fatal(e)
				}
				cctx := context.WithValue(ctx, pinnedSpeechProfileKey{}, speech.ID)
				response, actualMessages, err := server.completeAnchorStyleWorkbench(cctx, speech.ID, "lecture_task_preview", agentgateway.ResponseText, messages, 1200, false)
				row := map[string]any{"angle": angle, "round": round + 1, "messages": actualMessages, "response": response, "request_success": err == nil}
				rows = append(rows, row)
				if err != nil {
					row["error"] = "model request failed"
					t.Errorf("lecture request failed")
					continue
				}
				c := lectureTextCandidate(response.Text)
				if err = state.CheckP0Candidate(&c); err == nil {
					server.reviewCycleP0Context(cctx, &state, &c)
					server.auditLectureCandidate(cctx, &state, &c)
				}
				row["preview"] = lecturePreviewContent(&state, c, "")
				row["p0_report"], row["lecture_audit"] = c.P0Report, c.LectureAudit
				t.Logf("angle=%s round=%d generated=true", angle.Title, round+1)
			}
		}
	}
	if len(rows) == 0 {
		t.Fatal("selected two-level lecture unavailable")
	}
	raw, _ := json.MarshalIndent(map[string]any{"configured_speech_model": speech.Model, "p0_sha": derivedHash(policy), "rows": rows, "tts_generated": false, "played": false, "plan_assets_unchanged": true}, "", "  ")
	if e = os.WriteFile(os.Getenv("LECTURE_SUBJECT_OUTPUT"), raw, 0600); e != nil {
		t.Fatal("archive failed")
	}
}
