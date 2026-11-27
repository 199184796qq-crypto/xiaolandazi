package httpapi

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"livecompanion/management/internal/db"
	"livecompanion/management/internal/speechasr"
)

const maxPlanHumanScriptText = 96000
const planHumanScriptASRTimeout = 8 * time.Minute

var planHumanScriptASRWake = make(chan struct{}, 2)

var planHumanSRTTimestamp = regexp.MustCompile(`^([0-9]{2}:[0-5][0-9]:[0-5][0-9],[0-9]{3}) --> ([0-9]{2}:[0-5][0-9]:[0-5][0-9],[0-9]{3})$`)
var planHumanSRTIndex = regexp.MustCompile(`^[1-9][0-9]*$`)

func (s *Server) registerPlanHumanScriptRoutes(mux *http.ServeMux) {
	const root = "/api/v1/live-agent-plans/{planID}/human-scripts"
	for _, pattern := range []string{"GET " + root, "PATCH " + root + "/{assetID}", "DELETE " + root + "/{assetID}", "GET " + root + "/{assetID}/playback", "POST " + root + "/uploads/start", "POST " + root + "/uploads/complete", "PUT " + root + "/order", "GET " + root + "/{assetID}/transcript", "PUT " + root + "/{assetID}/transcript", "POST " + root + "/{assetID}/transcript", "GET " + root + "/{assetID}/fission", "POST " + root + "/{assetID}/fission", "POST " + root + "/{assetID}/fission/render",
		"GET " + root + "/{assetID}/fission/parts", "GET " + root + "/{assetID}/fission/parts/{part}/playback",
		"POST " + root + "/{assetID}/fission/parts/{part}/split",
		"POST " + root + "/{assetID}/fission/parts/merge", "POST " + root + "/{assetID}/fission/names", "PATCH " + root + "/{assetID}/fission/names", "GET " + root + "/{assetID}/fission/clear", "DELETE " + root + "/{assetID}/fission"} {
		mux.HandleFunc(pattern, s.handleScriptHumanVoices)
	}
}

func (s *Server) handlePlanHumanScriptExtra(w http.ResponseWriter, r *http.Request, tenant, plan, id, actor int64) bool {
	if strings.Contains(r.Pattern, "/{assetID}/fission") {
		s.handleHumanFission(w, r, tenant, plan, id, actor)
		return true
	}
	if strings.HasSuffix(r.Pattern, "/order") {
		var input struct {
			IDs []int64 `json:"ids"`
		}
		if readJSONWithLimit(w, r, &input, 8<<10) != nil || len(input.IDs) > db.MaxScriptHumanVoices {
			writeError(w, 400, "录音排序请求无效")
			return true
		}
		if err := s.store.ReorderPlanHumanScripts(r.Context(), tenant, plan, input.IDs); err != nil {
			if errors.Is(err, db.ErrPlanHumanScriptOrder) {
				writeError(w, 409, err.Error())
			} else {
				writeScriptHumanVoiceError(w, err)
			}
			return true
		}
		s.writeScriptHumanVoices(w, r.Context(), tenant, plan, db.PlanHumanScriptScope)
		return true
	}
	if !strings.HasSuffix(r.Pattern, "/transcript") {
		return false
	}
	asset, err := s.store.ScriptHumanVoice(r.Context(), tenant, plan, db.PlanHumanScriptScope, id)
	if err != nil {
		writeScriptHumanVoiceError(w, err)
		return true
	}
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, 200, planHumanScriptTranscript(asset.Metadata))
	case http.MethodPut:
		var input struct {
			Text string `json:"text"`
		}
		if readJSONWithLimit(w, r, &input, 640<<10) != nil || utf8.RuneCountInString(input.Text) > maxPlanHumanScriptText {
			writeError(w, 400, "文稿最多 96000 字")
			return true
		}
		input.Text = strings.TrimSpace(strings.ReplaceAll(input.Text, "\r\n", "\n"))
		existing := planHumanScriptTranscript(asset.Metadata)
		if !validPlanHumanScriptSRT(input.Text) && (existing["text"] == "" || existing["format"] != "text") {
			writeError(w, 400, "请保留 SRT 的序号、时间轴和字幕内容")
			return true
		}
		if err := s.store.SavePlanHumanScriptTranscript(r.Context(), tenant, plan, id, input.Text); err != nil {
			writeScriptHumanVoiceError(w, err)
			return true
		}
		writeJSON(w, 200, planHumanScriptTranscript(map[string]any{"script_transcript": input.Text, "script_asr_status": "ready"}))
	case http.MethodPost:
		if err := s.store.QueuePlanHumanScriptASR(r.Context(), tenant, plan, id); err != nil {
			writeScriptHumanVoiceError(w, err)
			return true
		}
		wakePlanHumanScriptASR()
		asset, err = s.store.ScriptHumanVoice(r.Context(), tenant, plan, db.PlanHumanScriptScope, id)
		if err != nil {
			writeScriptHumanVoiceError(w, err)
			return true
		}
		writeJSON(w, 202, planHumanScriptTranscript(asset.Metadata))
	}
	return true
}

func planHumanScriptTranscript(metadata map[string]any) map[string]string {
	text, _ := metadata["script_transcript"].(string)
	format := "srt"
	if text != "" && !validPlanHumanScriptSRT(text) {
		format = "text"
	}
	return map[string]string{"text": text, "status": planHumanScriptASRStatus(metadata), "format": format}
}

func validPlanHumanScriptSRT(text string) bool {
	for cue := range strings.SplitSeq(strings.TrimSpace(strings.ReplaceAll(text, "\r\n", "\n")), "\n\n") {
		lines := strings.SplitN(cue, "\n", 3)
		if len(lines) != 3 || !planHumanSRTIndex.MatchString(lines[0]) || strings.TrimSpace(lines[2]) == "" {
			return false
		}
		timecode := planHumanSRTTimestamp.FindStringSubmatch(lines[1])
		if timecode == nil || timecode[1] >= timecode[2] {
			return false
		}
	}
	return true
}

func wakePlanHumanScriptASR() {
	select {
	case planHumanScriptASRWake <- struct{}{}:
	default:
	}
}

func (s *Server) RunPlanHumanScriptASR(ctx context.Context) {
	// The queue is committed with the recording, so uploads and queued work
	// survive a closed browser or service restart.
	for range cap(planHumanScriptASRWake) {
		go s.consumePlanHumanScriptASR(ctx)
	}
}

func (s *Server) consumePlanHumanScriptASR(ctx context.Context) {
	ticker := time.NewTicker(8 * time.Second)
	defer ticker.Stop()
	for ctx.Err() == nil {
		if s.leader == nil || s.leader.IsLeader() {
			query, cancel := context.WithTimeout(ctx, 5*time.Second)
			job, err := s.store.NextPlanHumanScriptASR(query)
			cancel()
			if err == nil {
				if s.transcribePlanHumanScript(ctx, job) {
					continue
				}
			} else if !errors.Is(err, sql.ErrNoRows) && ctx.Err() == nil {
				log.Printf("read plan script ASR queue: %v", err)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-planHumanScriptASRWake:
		case <-ticker.C:
		}
	}
}

func (s *Server) transcribePlanHumanScript(parent context.Context, job db.PlanHumanScriptASRJob) bool {
	ctx, cancel := context.WithTimeout(parent, planHumanScriptASRTimeout)
	defer cancel()
	token := rand.Text()
	claimed, err := s.store.ClaimPlanHumanScriptASR(ctx, job.TenantID, job.PlanID, job.AssetID, token, time.Now().Add(planHumanScriptASRTimeout+time.Minute))
	if err != nil {
		log.Printf("claim plan script ASR asset=%d: %v", job.AssetID, err)
		return false
	}
	if !claimed {
		return true
	}
	text, err := s.generatePlanHumanScriptSRT(ctx, job)
	if err != nil {
		log.Printf("transcribe plan script asset=%d: failed to generate SRT", job.AssetID)
	}
	save, cancelSave := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelSave()
	if err := s.store.FinishPlanHumanScriptASR(save, job.TenantID, job.PlanID, job.AssetID, token, text); err != nil {
		log.Printf("save plan script transcript asset=%d: %v", job.AssetID, err)
	}
	return true
}

func (s *Server) generatePlanHumanScriptSRT(ctx context.Context, job db.PlanHumanScriptASRJob) (string, error) {
	asset, err := s.store.ScriptHumanVoice(ctx, job.TenantID, job.PlanID, db.PlanHumanScriptScope, job.AssetID)
	if err != nil {
		return "", err
	}
	client := speechasr.NewFromEnv()
	if err := client.Configured(); err != nil {
		return "", err
	}
	playback, err := s.scriptHumanVoicePlayback(ctx, asset)
	if err != nil {
		return "", err
	}
	result, err := client.Transcribe(ctx, playback.URL)
	if err != nil {
		return "", err
	}
	var duration int64
	if asset.DurationMS != nil {
		duration = int64(*asset.DurationMS)
	}
	return planHumanScriptSRT(result, duration)
}

func planHumanScriptSRT(result speechasr.Result, duration int64) (string, error) {
	timeline, _, _, err := customMainlineTimelineFromASR(result, duration)
	if err != nil {
		return "", err
	}
	text := strings.TrimSpace(liveAgentTimelineSRT(timeline))
	if utf8.RuneCountInString(text) > maxPlanHumanScriptText || !validPlanHumanScriptSRT(text) {
		return "", errors.New("录音转写结果无法保存为 SRT")
	}
	return text, nil
}

func planHumanScriptASRStatus(metadata map[string]any) string {
	status, _ := metadata["script_asr_status"].(string)
	if status == "processing" {
		expiry, _ := metadata["script_asr_expires"].(float64)
		if int64(expiry) <= time.Now().Unix() {
			return "failed"
		}
	}
	return status
}
