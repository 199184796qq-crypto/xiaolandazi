package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"livecompanion/management/internal/db"
	"livecompanion/management/internal/sharedfilelock"
	"livecompanion/management/internal/storage"
)

type scriptHumanUploadTicket struct {
	Tenant int64
	Plan   int64
	Actor  int64
	Piece  string
	OldID  int64
	Name   string
	Size   int64
}

func scriptHumanUploadDirectory() string {
	return filepath.Join(recordingCacheMediaRoot(), "script-human-recording-upload-tickets")
}

func (s *Server) startScriptHumanUpload(w http.ResponseWriter, r *http.Request, tenant, plan int64, piece string, actor int64) {
	var request struct {
		Name    string `json:"name"`
		Size    int64  `json:"size"`
		Replace int64  `json:"replace_id"`
	}
	if err := readJSONWithLimit(w, r, &request, 8<<10); err != nil {
		writeError(w, http.StatusBadRequest, "录音上传请求无效")
		return
	}
	name, format := scriptHumanUploadFile(request.Name, request.Size)
	if format == "" || request.Replace < 0 {
		writeError(w, http.StatusBadRequest, scriptHumanUploadFileError)
		return
	}
	ctx := r.Context()
	if request.Replace > 0 {
		if _, err := s.store.ScriptHumanVoice(ctx, tenant, plan, piece, request.Replace); err != nil {
			writeScriptHumanVoiceError(w, err)
			return
		}
	} else {
		assets, err := s.store.ListScriptHumanVoices(ctx, tenant, plan, piece)
		if err != nil {
			writeScriptHumanVoiceError(w, err)
			return
		}
		if len(assets) >= db.MaxScriptHumanVoices {
			writeScriptHumanVoiceError(w, db.ErrScriptHumanVoiceLimit)
			return
		}
	}
	oss, err := s.directAudioStore()
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "录音存储暂不可用")
		return
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		writeScriptHumanVoiceError(w, err)
		return
	}
	// Leave one sweep interval between signature expiration and the hour mark.
	expires := time.Now().UTC().Add(time.Hour - time.Minute).Truncate(time.Minute)
	id := fmt.Sprintf("%010d-%s", expires.Unix(), hex.EncodeToString(nonce[:]))
	form, err := oss.ScriptHumanUploadForm(id, request.Size)
	if err != nil {
		writeScriptHumanVoiceError(w, err)
		return
	}
	ticket := scriptHumanUploadTicket{Tenant: tenant, Plan: plan, Actor: actor, Piece: piece,
		OldID: request.Replace, Name: name, Size: request.Size}
	if err := writeVolcengineJSON(filepath.Join(scriptHumanUploadDirectory(), id+".json"), ticket); err != nil {
		writeScriptHumanVoiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"upload_id": id, "form": form})
}

func (s *Server) completeScriptHumanUpload(w http.ResponseWriter, r *http.Request, tenant, plan int64, piece string, actor int64) {
	var request struct {
		ID string `json:"upload_id"`
	}
	if err := readJSONWithLimit(w, r, &request, 8<<10); err != nil {
		writeError(w, http.StatusBadRequest, "录音上传请求无效")
		return
	}
	expires := storage.ScriptHumanUploadExpiry(request.ID)
	if !expires.After(time.Now()) {
		writeError(w, http.StatusGone, "录音上传已过期，请重新选择文件")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
	defer cancel()
	dir := scriptHumanUploadDirectory()
	// A fixed set of locks serializes retries across processes sharing media.
	release, err := sharedfilelock.Lock(ctx, filepath.Join(dir, "locks", request.ID[11:13]))
	if err != nil {
		writeScriptHumanVoiceError(w, err)
		return
	}
	defer release()
	var ticket scriptHumanUploadTicket
	err = readVolcengineJSON(filepath.Join(dir, request.ID+".json"), &ticket)
	if errors.Is(err, os.ErrNotExist) || err == nil && (ticket.Tenant != tenant || ticket.Plan != plan || ticket.Actor != actor || ticket.Piece != piece) {
		writeError(w, http.StatusNotFound, "录音上传记录不存在，请重新选择文件")
		return
	}
	if err != nil {
		writeScriptHumanVoiceError(w, err)
		return
	}
	if !expires.After(time.Now()) {
		writeError(w, http.StatusGone, "录音上传已过期，请重新选择文件")
		return
	}
	key := db.ScriptHumanVoicePrefix(tenant, plan, piece) + request.ID[11:] + ".wav"
	saved, err := s.store.ScriptHumanVoiceObjectRegistered(ctx, tenant, key)
	if err != nil {
		writeScriptHumanVoiceError(w, err)
		return
	}
	if saved {
		s.writeScriptHumanVoiceResult(w, ctx, tenant, plan, piece, key)
		return
	}
	select {
	case scriptHumanUploadSlots <- struct{}{}:
		defer func() { <-scriptHumanUploadSlots }()
	default:
		writeError(w, http.StatusTooManyRequests, "录音正在处理中，请稍后重试")
		return
	}
	oss, err := s.directAudioStore()
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "录音存储暂不可用")
		return
	}
	name, format := scriptHumanUploadFile(ticket.Name, ticket.Size)
	if format == "" {
		writeError(w, http.StatusBadRequest, scriptHumanUploadFileError)
		return
	}
	audio, err := oss.OpenScriptHumanUpload(ctx, request.ID, ticket.Size)
	if err != nil {
		writeError(w, http.StatusBadGateway, "OSS 上的原录音暂时无法读取，请重试")
		return
	}
	src, err := copyAudioReaderToTemp(io.LimitReader(audio, ticket.Size+1), filepath.Ext(name))
	_ = audio.Close()
	if err != nil {
		writeScriptHumanVoiceError(w, err)
		return
	}
	defer os.Remove(src)
	if info, err := os.Stat(src); err != nil || info.Size() != ticket.Size {
		writeError(w, http.StatusBadGateway, "原录音大小不一致，请重新上传")
		return
	}
	if s.saveScriptHumanVoice(w, ctx, oss, tenant, plan, piece, ticket.OldID, actor, key, name, src, format) {
		if piece == db.PlanHumanScriptScope {
			wakePlanHumanScriptASR()
		}
		s.writeScriptHumanVoiceResult(w, ctx, tenant, plan, piece, key)
		cleanup, done := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer done()
		_ = oss.DeleteScriptHumanUpload(cleanup, request.ID)
	}
}

func (s *Server) RunScriptHumanUploadCleanup(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		if s.leader == nil || s.leader.IsLeader() {
			s.sweepScriptHumanUploads(ctx)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Server) sweepScriptHumanUploads(ctx context.Context) {
	dir := scriptHumanUploadDirectory()
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err != nil {
		log.Printf("script human upload tickets: %v", err)
		return
	}
	now := time.Now()
	expired := make([]string, 0)
	due := false
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		id := entry.Name()[:len(entry.Name())-len(".json")]
		expiry := storage.ScriptHumanUploadExpiry(id)
		if expiry.IsZero() || expiry.After(now) {
			continue
		}
		due = true
		// In-flight requests have exited before the durable ticket is erased.
		if expiry.Add(5 * time.Minute).Before(now) {
			expired = append(expired, entry.Name())
		}
	}
	if !due {
		return
	}
	oss, err := s.directAudioStore()
	if err != nil {
		log.Printf("script human upload cleanup: OSS unavailable")
		return
	}
	cleanup, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	if err := oss.DeleteExpiredScriptHumanUploads(cleanup, now); err != nil {
		log.Printf("script human upload cleanup failed")
		return
	}
	for _, name := range expired {
		if err := os.Remove(filepath.Join(dir, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			log.Printf("script human upload ticket cleanup: %v", err)
		}
	}
}
