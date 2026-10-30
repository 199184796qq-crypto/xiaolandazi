package httpapi

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"livecompanion/management/internal/model"
	assetstorage "livecompanion/management/internal/storage"
)

type customUploadRequest struct {
	Files []struct {
		Name string `json:"name"`
		Size int64  `json:"size"`
	} `json:"files"`
	DurationMS int64 `json:"duration_ms"`
}

func validateCustomUploadRequest(input customUploadRequest) error {
	if len(input.Files) == 0 || len(input.Files) > maxCustomMainlineFiles {
		return errors.New("请选择1至20段录音")
	}
	for _, file := range input.Files {
		if file.Size <= 0 || file.Size > assetstorage.MaxDirectAudioBytes {
			return errors.New("每段录音必须大于0且不超过2GB")
		}
		if len(file.Name) > 255 || strings.TrimSpace(file.Name) == "" {
			return errors.New("录音文件名无效或过长")
		}
		if _, ok := speechAnalysisUploadContentTypes[strings.ToLower(filepath.Ext(file.Name))]; !ok {
			return errors.New("仅支持 WAV、MP3、M4A、AAC、FLAC、OGG、WEBM 音频")
		}
	}
	return nil
}

func (s *Server) customUploadAccess(w http.ResponseWriter, r *http.Request) (tenantID, planID, userID int64, ok bool) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return 0, 0, 0, false
	}
	planID, ok = liveAgentPlanPathID(w, r)
	if !ok {
		return 0, 0, 0, false
	}
	tenantID, ok = s.resolveLiveAgentPlanTenant(w, r, actor, requestTenantID(r), true)
	if !ok {
		return 0, 0, 0, false
	}
	if _, err := s.store.GetLiveAgentPlan(r.Context(), tenantID, planID); err != nil {
		writeError(w, 404, "直播智能体方案不存在")
		return 0, 0, 0, false
	}
	return tenantID, planID, actor.UserID, true
}

func (s *Server) directAudioStore() (*assetstorage.OSS, error) {
	if s.assetStorage == nil {
		return nil, errors.New("OSS unavailable")
	}
	store, err := s.assetStorage.For("oss")
	if err != nil {
		return nil, err
	}
	oss, ok := store.(*assetstorage.OSS)
	if !ok {
		return nil, errors.New("OSS unavailable")
	}
	return oss, nil
}

func (s *Server) customMainlineUploadInit(w http.ResponseWriter, r *http.Request) {
	tenantID, planID, userID, ok := s.customUploadAccess(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 32<<10)
	var input customUploadRequest
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, 400, "上传文件信息无效")
		return
	}
	if err := validateCustomUploadRequest(input); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	store, err := s.directAudioStore()
	if err != nil {
		writeError(w, 503, "原始声音直传需要配置OSS存储")
		return
	}
	random := make([]byte, 16)
	if _, err = rand.Read(random); err != nil {
		writeError(w, 500, "生成上传编号失败")
		return
	}
	job := model.CustomMainlineUpload{ID: hex.EncodeToString(random), TenantID: tenantID, PlanID: planID, UserID: userID, DurationMS: max(input.DurationMS, 0)}
	forms := make([]assetstorage.DirectUploadForm, 0, len(input.Files))
	// All file policies share a fixed expiry, so a later re-upload cannot extend retention.
	expires := time.Now().UTC().Add(2*time.Hour - time.Minute)
	for i, file := range input.Files {
		ext := strings.ToLower(filepath.Ext(file.Name))
		prefix := fmt.Sprintf("custom-mainline-uploads/tenant-%d/plan-%d/%s/", tenantID, planID, job.ID)
		item := model.CustomMainlineUploadFile{Name: filepath.Base(strings.ReplaceAll(file.Name, "\\", "/")), Size: file.Size, ContentType: speechAnalysisUploadContentTypes[ext], Key: fmt.Sprintf("%ssource-%02d%s", prefix, i, ext), SnapshotKey: fmt.Sprintf("%sverified-%02d%s", prefix, i, ext)}
		form, err := store.DirectUploadForm(item.Key, item.ContentType, item.Size, expires)
		if err != nil {
			writeError(w, 503, "OSS直传配置不可用，请检查公网HTTPS端点")
			return
		}
		job.Files = append(job.Files, item)
		forms = append(forms, form)
	}
	if err = s.store.CreateCustomMainlineUpload(r.Context(), job); err != nil {
		writeError(w, 500, "保存上传任务失败")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 201, map[string]any{"id": job.ID, "uploads": forms, "expires_at": expires, "max_file_bytes": assetstorage.MaxDirectAudioBytes})
}

func (s *Server) scopedCustomUpload(w http.ResponseWriter, r *http.Request) (model.CustomMainlineUpload, bool) {
	tenantID, planID, userID, ok := s.customUploadAccess(w, r)
	if !ok {
		return model.CustomMainlineUpload{}, false
	}
	job, err := s.store.GetCustomMainlineUpload(r.Context(), tenantID, planID, userID, r.PathValue("uploadID"))
	if err != nil {
		writeError(w, 404, "上传任务不存在")
		return job, false
	}
	return job, true
}

func (s *Server) customMainlineUploadComplete(w http.ResponseWriter, r *http.Request) {
	job, ok := s.scopedCustomUpload(w, r)
	if !ok {
		return
	}
	if job.Status == "uploading" {
		if time.Since(job.CreatedAt) >= 2*time.Hour {
			writeError(w, 409, "上传授权已过期，请重新上传")
			return
		}
		if _, err := s.store.TransitionCustomMainlineUpload(r.Context(), job, "uploading", "queued", "", nil); err != nil {
			writeError(w, 500, "提交音频处理任务失败")
			return
		}
		job, _ = s.store.GetCustomMainlineUpload(r.Context(), job.TenantID, job.PlanID, job.UserID, job.ID)
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 202, job)
}

func (s *Server) customMainlineUploadStatus(w http.ResponseWriter, r *http.Request) {
	job, ok := s.scopedCustomUpload(w, r)
	if !ok {
		return
	}
	if r.Method == http.MethodDelete {
		for _, from := range []string{"uploading", "queued", "processing"} {
			if _, err := s.store.TransitionCustomMainlineUpload(r.Context(), job, from, "cancelled", "上传处理已取消", nil); err != nil {
				writeError(w, 500, "取消上传任务失败")
				return
			}
		}
		job, _ = s.store.GetCustomMainlineUpload(r.Context(), job.TenantID, job.PlanID, job.UserID, job.ID)
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, job)
}

// One bounded worker, durable CAS claims, and no automatic replay of interrupted
// ASR/voice cloning. Status requests never execute paid operations.
func (s *Server) RunCustomMainlineUploads(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			s.maintainCustomUploads(ctx)
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		if s.leader == nil || s.leader.IsLeader() {
			job, err := s.store.NextCustomMainlineUpload(ctx)
			if err == nil {
				claimed, err := s.store.TransitionCustomMainlineUpload(ctx, job, "queued", "processing", "", nil)
				if err == nil && claimed {
					s.processDirectCustomUpload(ctx, job)
					continue
				}
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

type customUploadResponse struct {
	header http.Header
	code   int
	body   bytes.Buffer
}

func (w *customUploadResponse) Header() http.Header { return w.header }
func (w *customUploadResponse) WriteHeader(code int) {
	if w.code == 0 {
		w.code = code
	}
}
func (w *customUploadResponse) Write(p []byte) (int, error) {
	if w.code == 0 {
		w.code = 200
	}
	return w.body.Write(p)
}

func (s *Server) processDirectCustomUpload(parent context.Context, job model.CustomMainlineUpload) {
	ctx, cancel := context.WithTimeout(parent, 6*time.Hour)
	defer cancel()
	done := make(chan struct{})
	defer close(done)
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				current, err := s.store.GetCustomMainlineUpload(ctx, job.TenantID, job.PlanID, job.UserID, job.ID)
				if err != nil || current.Status != "processing" || (s.leader != nil && !s.leader.IsLeader()) {
					cancel()
					return
				}
				if s.store.HeartbeatCustomMainlineUpload(ctx, job.ID) != nil {
					cancel()
					return
				}
			}
		}
	}()
	fail := func(message string) {
		dbctx, c := context.WithTimeout(context.Background(), 10*time.Second)
		defer c()
		_, _ = s.store.TransitionCustomMainlineUpload(dbctx, job, "processing", "failed", message, nil)
	}
	defer func() {
		if recover() != nil {
			fail("录音处理异常中断，请重新上传；不会自动重复调用识别或声音复刻")
			log.Printf("custom mainline processing interrupted, job=%s", job.ID)
		}
	}()
	store, err := s.directAudioStore()
	if err != nil {
		fail("OSS存储暂不可用，请重新上传")
		return
	}
	if _, err = s.store.GetLiveAgentPlan(ctx, job.TenantID, job.PlanID); err != nil {
		fail("方案不存在，已停止处理录音")
		return
	}
	sources := []customMainlineSource{}
	names := []string{}
	contentTypes := []string{}
	var total int64
	for _, file := range job.Files {
		if err = store.PinDirectUpload(ctx, file.Key, file.SnapshotKey, file.Size); err != nil {
			fail("OSS文件未上传完整或文件已变化，请重新上传")
			return
		}
		sources = append(sources, customMainlineSource{Name: file.Name, Size: file.Size, Open: func(ctx context.Context) (io.ReadCloser, error) { return store.Open(ctx, file.SnapshotKey) }})
		names = append(names, file.Name)
		contentTypes = append(contentTypes, file.ContentType)
		total += file.Size
	}
	response := &customUploadResponse{header: make(http.Header)}
	prefix := fmt.Sprintf("custom-mainline-uploads/tenant-%d/plan-%d/%s/", job.TenantID, job.PlanID, job.ID)
	request, _ := http.NewRequestWithContext(context.WithValue(ctx, customASRPrefixKey{}, prefix), http.MethodPost, "http://internal/custom-mainline", nil)
	s.processCustomMainlineSources(response, request, job.TenantID, job.PlanID, job.UserID, sources, names, contentTypes, total, job.DurationMS, nil, store)
	if response.code != http.StatusCreated {
		var payload struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(response.body.Bytes(), &payload)
		if payload.Error == "" {
			payload.Error = "原音处理失败，请重新上传"
		}
		fail(payload.Error)
		return
	}
	dbctx, c := context.WithTimeout(context.Background(), 10*time.Second)
	defer c()
	if _, err = s.store.TransitionCustomMainlineUpload(dbctx, job, "processing", "ready", "", json.RawMessage(response.body.Bytes())); err != nil {
		log.Printf("custom mainline result persistence failed, job=%s", job.ID)
	}
}

func (s *Server) maintainCustomUploads(ctx context.Context) {
	if ctx.Err() != nil || (s.leader != nil && !s.leader.IsLeader()) {
		return
	}
	if err := s.store.MaintainCustomMainlineUploads(ctx); err != nil {
		return
	}
	store, err := s.directAudioStore()
	if err != nil {
		return
	}
	items, err := s.store.CustomMainlineUploadsToClean(ctx)
	if err != nil {
		return
	}
	for _, job := range items {
		all := true
		prefix := fmt.Sprintf("custom-mainline-uploads/tenant-%d/plan-%d/%s/", job.TenantID, job.PlanID, job.ID)
		for _, file := range job.Files {
			for _, key := range []string{file.Key, file.SnapshotKey} {
				if !strings.HasPrefix(key, prefix) || strings.Contains(key, "..") {
					all = false
					continue
				}
				if err = store.Delete(ctx, key); err != nil {
					all = false
				}
			}
		}
		// RIFF playback assets are below 4GB: at most five six-hour ASR chunks.
		for i := 0; i < 5; i++ {
			if err = store.Delete(ctx, fmt.Sprintf("%sasr-%06d.wav", prefix, i)); err != nil {
				all = false
			}
		}
		if all {
			_ = s.store.MarkCustomMainlineUploadCleaned(ctx, job.ID)
		}
	}
}
