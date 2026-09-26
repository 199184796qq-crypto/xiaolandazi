package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"
	"unicode"

	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
	assetstorage "livecompanion/management/internal/storage"
)

type speechAnalysisStatusResponse struct {
	Configured          bool                      `json:"configured"`
	ConfigurationReason string                    `json:"configuration_reason,omitempty"`
	Task                *model.SpeechAnalysisTask `json:"task,omitempty"`
}

type speechAnalysisCaptureSnapshot struct {
	Recording *struct {
		ID        string    `json:"id"`
		Status    string    `json:"status"`
		StartedAt time.Time `json:"started_at"`
	} `json:"recording,omitempty"`
}

type speechAnalysisRoom struct {
	Name           string `json:"name"`
	ExternalRoomID string `json:"external_room_id"`
}

type coreSpeechAnalysisSnapshot struct {
	TaskID         int64     `json:"task_id"`
	TenantID       int64     `json:"tenant_id"`
	RoomID         int64     `json:"room_id"`
	Status         string    `json:"status"`
	Stage          string    `json:"stage"`
	Progress       int       `json:"progress"`
	ErrorMessage   string    `json:"error_message,omitempty"`
	ASRTaskID      string    `json:"asr_task_id,omitempty"`
	TranscriptText string    `json:"transcript_text,omitempty"`
	ReportText     string    `json:"report_text,omitempty"`
	UpdatedAt      time.Time `json:"updated_at"`
}

var speechAnalysisUploadContentTypes = map[string]string{
	".wav":  "audio/wav",
	".mp3":  "audio/mpeg",
	".m4a":  "audio/mp4",
	".aac":  "audio/aac",
	".flac": "audio/flac",
	".ogg":  "audio/ogg",
	".webm": "audio/webm",
}

const (
	speechAnalysisResultPrefix    = "live-analysis"
	speechAnalysisTempAudioPrefix = "live-analysis-temp-audio"
	speechAnalysisAudioMaxAge     = 12 * time.Hour
)

func (s *Server) roomSpeechAnalysisStatus(w http.ResponseWriter, r *http.Request) {
	tenantID, roomID, ok := s.roomCaptureScope(w, r)
	if !ok {
		return
	}
	configured, reason, _ := s.speechAnalysisConfigured()
	response := speechAnalysisStatusResponse{
		Configured:          configured,
		ConfigurationReason: reason,
	}
	task, err := s.store.GetLatestSpeechAnalysisTask(r.Context(), tenantID, roomID)
	if err == nil {
		response.Task = &task
	} else if !errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusInternalServerError, "读取智能话术分析状态失败")
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) roomSpeechAnalysisStart(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	roomID, ok := pathID(w, r)
	if !ok {
		return
	}
	tenantID, ok := s.tenantForRoom(w, r, actor, roomID)
	if !ok {
		return
	}
	configured, reason, _ := s.speechAnalysisConfigured()
	if !configured {
		writeError(w, http.StatusServiceUnavailable, reason)
		return
	}

	captureSnapshot, err := s.loadSpeechAnalysisCapture(r.Context(), tenantID, roomID)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	if captureSnapshot.Recording == nil || !strings.EqualFold(captureSnapshot.Recording.Status, "ready") {
		writeError(w, http.StatusConflict, "请先完成声音录制并生成完整 WAV")
		return
	}
	recordingID := strings.TrimSpace(captureSnapshot.Recording.ID)
	if recordingID == "" {
		writeError(w, http.StatusConflict, "当前录音缺少录制编号，请重新录制")
		return
	}

	if existing, err := s.store.LatestSpeechAnalysisForRecording(r.Context(), tenantID, roomID, recordingID); err == nil {
		switch existing.Status {
		case model.SpeechAnalysisQueued, model.SpeechAnalysisUploading, model.SpeechAnalysisASR,
			model.SpeechAnalysisAnalyzing, model.SpeechAnalysisRendering, model.SpeechAnalysisReady:
			writeJSON(w, http.StatusOK, speechAnalysisStatusResponse{Configured: true, Task: &existing})
			return
		}
	}

	roomInfo := s.loadSpeechAnalysisRoom(r.Context(), tenantID, roomID)
	reportName := speechAnalysisReportFileName(roomInfo, roomID, captureSnapshot.Recording.StartedAt)
	task, err := s.store.CreateSpeechAnalysisTask(
		r.Context(),
		tenantID,
		roomID,
		actor.UserID,
		recordingID,
		captureSnapshot.Recording.StartedAt,
		reportName,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "创建智能话术分析任务失败")
		return
	}
	go s.runSpeechAnalysisTask(task, roomInfo)
	writeJSON(w, http.StatusAccepted, speechAnalysisStatusResponse{Configured: true, Task: &task})
}

func (s *Server) roomSpeechAnalysisUpload(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	roomID, ok := pathID(w, r)
	if !ok {
		return
	}
	tenantID, ok := s.tenantForRoom(w, r, actor, roomID)
	if !ok {
		return
	}
	configured, reason, ossStore := s.speechAnalysisConfigured()
	if !configured || ossStore == nil {
		writeError(w, http.StatusServiceUnavailable, reason)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxMediaAssetBytes+4*1024*1024)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "录音文件过大或上传格式错误")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "请选择要分析的录音文件")
		return
	}
	defer file.Close()
	if header.Size <= 0 || header.Size > maxMediaAssetBytes {
		writeError(w, http.StatusBadRequest, "录音文件大小不合法")
		return
	}
	extension := strings.ToLower(path.Ext(strings.TrimSpace(header.Filename)))
	contentType, allowed := speechAnalysisUploadContentTypes[extension]
	if !allowed {
		writeError(w, http.StatusBadRequest, "仅支持 WAV、MP3、M4A、AAC、FLAC、OGG、WEBM 录音")
		return
	}

	now := time.Now().UTC()
	recordingID := fmt.Sprintf("upload-%d-%d", now.UnixMilli(), actor.UserID)
	audioPrefix := path.Join(
		speechAnalysisTempAudioPrefix,
		fmt.Sprintf("tenant-%d", tenantID),
		fmt.Sprintf("room-%d", roomID),
		recordingID,
	)
	audioObjectKey := path.Join(audioPrefix, "recording"+extension)
	if err := ossStore.Put(r.Context(), audioObjectKey, file, contentType); err != nil {
		writeError(w, http.StatusBadGateway, "上传录音失败，请稍后重试")
		return
	}

	roomInfo := s.loadSpeechAnalysisRoom(r.Context(), tenantID, roomID)
	reportName := speechAnalysisReportFileName(roomInfo, roomID, now)
	task, err := s.store.CreateSpeechAnalysisTask(
		r.Context(),
		tenantID,
		roomID,
		actor.UserID,
		recordingID,
		now,
		reportName,
	)
	if err != nil {
		_ = ossStore.Delete(context.Background(), audioObjectKey)
		writeError(w, http.StatusInternalServerError, "创建智能话术分析任务失败")
		return
	}
	task.AudioObjectKey = audioObjectKey
	task.Stage = "分析语音"
	task.Progress = 20
	if err := s.store.SaveSpeechAnalysisTask(r.Context(), task); err != nil {
		_ = ossStore.Delete(context.Background(), audioObjectKey)
		writeError(w, http.StatusInternalServerError, "保存录音分析任务失败")
		return
	}

	go s.runSpeechAnalysisTask(task, roomInfo)
	writeJSON(w, http.StatusAccepted, speechAnalysisStatusResponse{Configured: true, Task: &task})
}

func (s *Server) roomSpeechAnalysisTranscript(w http.ResponseWriter, r *http.Request) {
	tenantID, roomID, ok := s.roomCaptureScope(w, r)
	if !ok {
		return
	}
	task, err := s.store.GetLatestSpeechAnalysisTask(r.Context(), tenantID, roomID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "当前直播间还没有文字稿")
			return
		}
		writeError(w, http.StatusInternalServerError, "读取文字稿失败")
		return
	}
	if strings.TrimSpace(task.TranscriptObjectKey) == "" {
		writeError(w, http.StatusConflict, "文字稿尚未生成")
		return
	}
	_, _, ossStore := s.speechAnalysisConfigured()
	if ossStore == nil {
		writeError(w, http.StatusServiceUnavailable, "文件服务暂不可用")
		return
	}
	reader, err := ossStore.Open(r.Context(), task.TranscriptObjectKey)
	if err != nil {
		writeError(w, http.StatusBadGateway, "文字稿暂不可下载")
		return
	}
	defer reader.Close()

	roomInfo := s.loadSpeechAnalysisRoom(r.Context(), tenantID, roomID)
	name := speechAnalysisTranscriptFileName(roomInfo, roomID, task.RecordingStartedAt)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set(
		"Content-Disposition",
		"attachment; filename=\"transcript.txt\"; filename*=UTF-8''"+url.PathEscape(name),
	)
	w.Header().Set("Cache-Control", "no-store")
	_, _ = io.Copy(w, reader)
}

func (s *Server) roomSpeechAnalysisReport(w http.ResponseWriter, r *http.Request) {
	tenantID, roomID, ok := s.roomCaptureScope(w, r)
	if !ok {
		return
	}
	task, err := s.store.GetLatestSpeechAnalysisTask(r.Context(), tenantID, roomID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "当前直播间还没有话术分析报告")
			return
		}
		writeError(w, http.StatusInternalServerError, "读取话术分析报告失败")
		return
	}
	if task.Status != model.SpeechAnalysisReady || strings.TrimSpace(task.ReportObjectKey) == "" {
		writeError(w, http.StatusConflict, "话术分析报告尚未生成")
		return
	}
	_, _, ossStore := s.speechAnalysisConfigured()
	if ossStore == nil {
		writeError(w, http.StatusServiceUnavailable, "文件服务暂不可用")
		return
	}
	reader, err := ossStore.Open(r.Context(), task.ReportObjectKey)
	if err != nil {
		writeError(w, http.StatusBadGateway, "话术分析报告暂不可下载")
		return
	}
	defer reader.Close()

	name := safeSpeechAnalysisFileName(task.ReportFileName)
	if name == "" {
		name = "智能话术分析.md"
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set(
		"Content-Disposition",
		"attachment; filename=\"speech-analysis.md\"; filename*=UTF-8''"+url.PathEscape(name),
	)
	w.Header().Set("Cache-Control", "no-store")
	_, _ = io.Copy(w, reader)
}

func (s *Server) speechAnalysisConfigured() (bool, string, assetstorage.Store) {
	if s.assetStorage == nil {
		return false, "分析服务暂不可用，请联系管理员", nil
	}
	ossStore, err := s.assetStorage.For("oss")
	if err != nil {
		return false, "分析服务暂不可用，请联系管理员", nil
	}
	return true, "", ossStore
}

func (s *Server) loadSpeechAnalysisCapture(ctx context.Context, tenantID, roomID int64) (speechAnalysisCaptureSnapshot, error) {
	query := url.Values{}
	query.Set("tenant_id", strconv.FormatInt(tenantID, 10))
	resp, err := s.core.DoRoom(
		ctx,
		tenantID,
		roomID,
		http.MethodGet,
		fmt.Sprintf("/internal/v1/rooms/%d/capture", roomID),
		query,
		nil,
	)
	if err != nil {
		return speechAnalysisCaptureSnapshot{}, fmt.Errorf("核心服务采集状态暂不可用")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return speechAnalysisCaptureSnapshot{}, fmt.Errorf("读取录音状态失败 HTTP %d", resp.StatusCode)
	}
	var snapshot speechAnalysisCaptureSnapshot
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&snapshot); err != nil {
		return speechAnalysisCaptureSnapshot{}, fmt.Errorf("解析录音状态失败")
	}
	return snapshot, nil
}

func (s *Server) loadSpeechAnalysisRoom(ctx context.Context, tenantID, roomID int64) speechAnalysisRoom {
	query := url.Values{}
	query.Set("tenant_id", strconv.FormatInt(tenantID, 10))
	resp, err := s.core.DoRoom(
		ctx,
		tenantID,
		roomID,
		http.MethodGet,
		fmt.Sprintf("/internal/v1/rooms/%d", roomID),
		query,
		nil,
	)
	if err != nil {
		return speechAnalysisRoom{}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return speechAnalysisRoom{}
	}
	var room speechAnalysisRoom
	_ = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&room)
	return room
}

func (s *Server) runSpeechAnalysisTask(task model.SpeechAnalysisTask, roomInfo speechAnalysisRoom) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Minute)
	defer cancel()

	fail := func(err error) {
		now := time.Now().UTC()
		task.Status = model.SpeechAnalysisFailed
		task.Stage = "分析失败"
		task.Progress = maxSpeechProgress(task.Progress, 1)
		task.ErrorMessage = compactSpeechAnalysisError(err)
		task.FinishedAt = &now
		saveCtx, saveCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer saveCancel()
		_ = s.store.SaveSpeechAnalysisTask(saveCtx, task)
	}
	configured, reason, ossStore := s.speechAnalysisConfigured()
	if !configured || ossStore == nil {
		fail(errors.New(reason))
		return
	}
	prefix := path.Join(
		speechAnalysisResultPrefix,
		fmt.Sprintf("tenant-%d", task.TenantID),
		fmt.Sprintf("room-%d", task.RoomID),
		safeSpeechAnalysisObjectPart(task.RecordingID),
	)
	if strings.TrimSpace(task.AudioObjectKey) == "" {
		task.Status = model.SpeechAnalysisUploading
		task.Stage = "上传录音"
		task.Progress = 10
		task.ErrorMessage = ""
		if err := s.store.SaveSpeechAnalysisTask(ctx, task); err != nil {
			fail(err)
			return
		}
		task.AudioObjectKey = path.Join(
			speechAnalysisTempAudioPrefix,
			fmt.Sprintf("tenant-%d", task.TenantID),
			fmt.Sprintf("room-%d", task.RoomID),
			safeSpeechAnalysisObjectPart(task.RecordingID),
			"recording.wav",
		)
		if err := s.uploadSpeechAnalysisRecording(ctx, ossStore, task); err != nil {
			fail(err)
			return
		}
		if err := s.store.SaveSpeechAnalysisTask(ctx, task); err != nil {
			fail(err)
			return
		}
	}

	audioURL, err := ossStore.SignedURL(ctx, task.AudioObjectKey, 6*time.Hour)
	if err != nil {
		fail(fmt.Errorf("prepare recording access: %w", err))
		return
	}

	query := url.Values{}
	query.Set("tenant_id", strconv.FormatInt(task.TenantID, 10))
	profile, profileErr := s.store.GetActiveSpeechAnalysisProfile(ctx)
	if profileErr != nil {
		profile = appdb.DefaultSpeechAnalysisProfile()
	}
	dispatchBody := map[string]any{
		"task_id":              task.ID,
		"tenant_id":            task.TenantID,
		"room_id":              task.RoomID,
		"room_name":            roomInfo.Name,
		"recording_id":         task.RecordingID,
		"recording_started_at": task.RecordingStartedAt,
		"audio_url":            audioURL,
		"analysis_config": map[string]any{
			"profile_id":              profile.ID,
			"profile_version":         profile.Version,
			"profile_name":            profile.Name,
			"provider":                profile.Provider,
			"model":                   profile.Model,
			"segment_system_prompt":   profile.SegmentSystemPrompt,
			"segment_prompt_template": profile.SegmentPromptTemplate,
			"summary_system_prompt":   profile.SummarySystemPrompt,
			"summary_prompt_template": profile.SummaryPromptTemplate,
		},
	}
	resp, err := s.core.DoRoom(
		ctx,
		task.TenantID,
		task.RoomID,
		http.MethodPost,
		fmt.Sprintf("/internal/v1/rooms/%d/speech-analysis/jobs", task.RoomID),
		query,
		dispatchBody,
	)
	if err != nil {
		fail(fmt.Errorf("dispatch analysis job: %w", err))
		return
	}
	var initial coreSpeechAnalysisSnapshot
	decodeErr := json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&initial)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusOK {
		fail(fmt.Errorf("core analysis job http %d", resp.StatusCode))
		return
	}
	if decodeErr != nil {
		fail(fmt.Errorf("decode core analysis job: %w", decodeErr))
		return
	}

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		if err := s.applyCoreSpeechAnalysisSnapshot(ctx, ossStore, prefix, &task, initial); err != nil {
			fail(err)
			return
		}
		if initial.Status == model.SpeechAnalysisReady || initial.Status == model.SpeechAnalysisFailed {
			return
		}

		select {
		case <-ctx.Done():
			fail(ctx.Err())
			return
		case <-ticker.C:
		}

		resp, err := s.core.DoRoom(
			ctx,
			task.TenantID,
			task.RoomID,
			http.MethodGet,
			fmt.Sprintf("/internal/v1/rooms/%d/speech-analysis/jobs/%d", task.RoomID, task.ID),
			query,
			nil,
		)
		if err != nil {
			fail(fmt.Errorf("read core analysis job: %w", err))
			return
		}
		var next coreSpeechAnalysisSnapshot
		decodeErr = json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&next)
		statusCode := resp.StatusCode
		_ = resp.Body.Close()
		if statusCode != http.StatusOK {
			fail(fmt.Errorf("core analysis status http %d", statusCode))
			return
		}
		if decodeErr != nil {
			fail(fmt.Errorf("decode core analysis status: %w", decodeErr))
			return
		}
		initial = next
	}
}

func (s *Server) applyCoreSpeechAnalysisSnapshot(
	ctx context.Context,
	ossStore assetstorage.Store,
	prefix string,
	task *model.SpeechAnalysisTask,
	snapshot coreSpeechAnalysisSnapshot,
) error {
	if task == nil {
		return errors.New("speech analysis task is nil")
	}
	if strings.TrimSpace(snapshot.Status) != "" {
		task.Status = snapshot.Status
	}
	if strings.TrimSpace(snapshot.Stage) != "" {
		task.Stage = snapshot.Stage
	}
	if snapshot.Progress > 0 {
		task.Progress = snapshot.Progress
	}
	if strings.TrimSpace(snapshot.ASRTaskID) != "" {
		task.ASRTaskID = snapshot.ASRTaskID
	}
	task.ErrorMessage = snapshot.ErrorMessage

	if strings.TrimSpace(snapshot.TranscriptText) != "" && strings.TrimSpace(task.TranscriptObjectKey) == "" {
		task.TranscriptObjectKey = path.Join(prefix, "transcript.txt")
		if err := ossStore.Put(
			ctx,
			task.TranscriptObjectKey,
			strings.NewReader(snapshot.TranscriptText),
			"text/plain; charset=utf-8",
		); err != nil {
			return fmt.Errorf("save transcript: %w", err)
		}
	}

	if strings.TrimSpace(task.TranscriptObjectKey) != "" && strings.TrimSpace(task.AudioObjectKey) != "" {
		audioObjectKey := task.AudioObjectKey
		if err := ossStore.Delete(ctx, audioObjectKey); err != nil {
			log.Printf("speech analysis task=%d delete temporary audio %q: %v", task.ID, audioObjectKey, err)
		} else {
			task.AudioObjectKey = ""
		}
	}

	if strings.TrimSpace(snapshot.ReportText) != "" && strings.TrimSpace(task.ReportObjectKey) == "" {
		task.ReportObjectKey = path.Join(prefix, "analysis.md")
		if err := ossStore.Put(
			ctx,
			task.ReportObjectKey,
			strings.NewReader(snapshot.ReportText),
			"text/markdown; charset=utf-8",
		); err != nil {
			return fmt.Errorf("save analysis report: %w", err)
		}
	}

	if task.Status == model.SpeechAnalysisReady || task.Status == model.SpeechAnalysisFailed {
		now := time.Now().UTC()
		task.FinishedAt = &now
	}
	return s.store.SaveSpeechAnalysisTask(ctx, *task)
}

func (s *Server) RunSpeechAnalysisAudioCleanup(ctx context.Context) {
	if s == nil || s.store == nil || s.assetStorage == nil {
		return
	}
	ossStore, err := s.assetStorage.For("oss")
	if err != nil {
		log.Printf("speech analysis audio cleanup disabled: %v", err)
		return
	}

	cleanup := func() {
		if s.leader != nil && !s.leader.IsLeader() {
			return
		}
		cleanupCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
		defer cancel()
		before := time.Now().UTC().Add(-speechAnalysisAudioMaxAge)
		tasks, err := s.store.ListSpeechAnalysisTasksNeedingAudioCleanup(cleanupCtx, before, 200)
		if err != nil {
			log.Printf("list stale speech analysis audio: %v", err)
			return
		}
		for _, task := range tasks {
			objectKey := strings.TrimSpace(task.AudioObjectKey)
			if objectKey == "" {
				continue
			}
			if err := ossStore.Delete(cleanupCtx, objectKey); err != nil {
				log.Printf("cleanup stale speech analysis audio task=%d object=%q: %v", task.ID, objectKey, err)
				continue
			}
			if err := s.store.ClearSpeechAnalysisAudioObjectKey(cleanupCtx, task.ID, task.TenantID, task.RoomID, objectKey); err != nil {
				log.Printf("clear stale speech analysis audio key task=%d: %v", task.ID, err)
			}
		}
	}

	cleanup()
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cleanup()
		}
	}
}

func (s *Server) uploadSpeechAnalysisRecording(
	ctx context.Context,
	ossStore assetstorage.Store,
	task model.SpeechAnalysisTask,
) error {
	query := url.Values{}
	query.Set("tenant_id", strconv.FormatInt(task.TenantID, 10))
	resp, err := s.core.StreamRoom(
		ctx,
		task.TenantID,
		task.RoomID,
		fmt.Sprintf("/internal/v1/rooms/%d/capture/audio/file", task.RoomID),
		query,
	)
	if err != nil {
		return fmt.Errorf("读取 Core 录音: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		message, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
		return fmt.Errorf("Core 录音下载失败 HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(message)))
	}
	if err := ossStore.Put(ctx, task.AudioObjectKey, resp.Body, "audio/wav"); err != nil {
		return fmt.Errorf("上传录音到 OSS: %w", err)
	}
	return nil
}

func speechAnalysisReportFileName(room speechAnalysisRoom, roomID int64, startedAt time.Time) string {
	base := safeSpeechAnalysisFileName(room.Name)
	if base == "" {
		externalID := safeSpeechAnalysisFileName(room.ExternalRoomID)
		if externalID != "" {
			base = "直播间" + externalID
		} else {
			base = fmt.Sprintf("直播间%d", roomID)
		}
	}
	if startedAt.IsZero() {
		startedAt = time.Now().UTC()
	}
	return fmt.Sprintf(
		"%s_%s_智能话术分析.md",
		base,
		startedAt.In(time.FixedZone("CST", 8*60*60)).Format("20060102_150405"),
	)
}

func speechAnalysisTranscriptFileName(room speechAnalysisRoom, roomID int64, startedAt time.Time) string {
	base := safeSpeechAnalysisFileName(room.Name)
	if base == "" {
		externalID := safeSpeechAnalysisFileName(room.ExternalRoomID)
		if externalID != "" {
			base = "直播间" + externalID
		} else {
			base = fmt.Sprintf("直播间%d", roomID)
		}
	}
	if startedAt.IsZero() {
		startedAt = time.Now().UTC()
	}
	return fmt.Sprintf(
		"%s_%s_逐字稿.txt",
		base,
		startedAt.In(time.FixedZone("CST", 8*60*60)).Format("20060102_150405"),
	)
}

func safeSpeechAnalysisFileName(value string) string {
	value = strings.TrimSpace(value)
	value = strings.NewReplacer(
		"\\", "_", "/", "_", ":", "_", "*", "_", "?", "_", "\"", "_",
		"<", "_", ">", "_", "|", "_",
	).Replace(value)
	value = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, value)
	return strings.TrimRight(strings.TrimSpace(value), ". ")
}

func safeSpeechAnalysisObjectPart(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Sprintf("recording-%d", time.Now().UTC().UnixMilli())
	}
	var builder strings.Builder
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' || r == '.' {
			builder.WriteRune(r)
		} else {
			builder.WriteRune('_')
		}
	}
	return strings.Trim(builder.String(), "._")
}

func compactSpeechAnalysisError(err error) string {
	if err == nil {
		return "未知错误"
	}
	value := strings.TrimSpace(err.Error())
	runes := []rune(value)
	if len(runes) > 1200 {
		value = string(runes[:1200])
	}
	return value
}

func maxSpeechProgress(current, fallback int) int {
	if current > fallback {
		return current
	}
	return fallback
}
