package httpapi

import (
	"context"
	"log"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"livecompanion/management/internal/speechasr"
)

const maxMobileAgentVoiceBytes int64 = 8 << 20

func (s *Server) transcribeRoomAgentVoice(w http.ResponseWriter, r *http.Request) {
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

	r.Body = http.MaxBytesReader(w, r.Body, maxMobileAgentVoiceBytes+(1<<20))
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "语音文件过大或格式无效")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "没有收到语音文件")
		return
	}
	defer file.Close()
	if header.Size <= 0 {
		writeError(w, http.StatusBadRequest, "语音内容为空")
		return
	}
	if header.Size > maxMobileAgentVoiceBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "单次语音不能超过 8MB")
		return
	}

	extension, contentType := mobileAgentVoiceFormat(header)
	if extension == "" || contentType == "" {
		writeError(w, http.StatusBadRequest, "当前手机录音格式暂不支持")
		return
	}
	if s.assetStorage == nil {
		writeError(w, http.StatusServiceUnavailable, "语音识别存储尚未初始化")
		return
	}
	ossStore, err := s.assetStorage.For("oss")
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "语音识别存储暂不可用")
		return
	}
	objectKey, err := newMediaObjectKey(tenantID, "mobile-agent-voice", "voice"+extension)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "生成语音对象编号失败")
		return
	}
	if err := ossStore.Put(r.Context(), objectKey, file, contentType); err != nil {
		writeError(w, http.StatusBadGateway, "上传语音失败")
		return
	}
	defer func() {
		_ = ossStore.Delete(context.Background(), objectKey)
	}()

	signedURL, err := ossStore.SignedURL(r.Context(), objectKey, 30*time.Minute)
	if err != nil || strings.TrimSpace(signedURL) == "" {
		writeError(w, http.StatusBadGateway, "无法生成语音识别地址")
		return
	}
	client := speechasr.NewFromEnv()
	result, err := client.Transcribe(r.Context(), signedURL)
	if err != nil {
		log.Printf("mobile agent voice asr failed tenant=%d room=%d bytes=%d mime=%s: %v", tenantID, roomID, header.Size, contentType, err)
		writeError(w, http.StatusBadGateway, "语音转文字失败，请再说一次")
		return
	}
	text := strings.TrimSpace(result.Text)
	if text == "" {
		writeError(w, http.StatusUnprocessableEntity, "没有识别到有效语音")
		return
	}
	log.Printf("mobile agent voice transcribed tenant=%d room=%d bytes=%d mime=%s chars=%d task=%s", tenantID, roomID, header.Size, contentType, len([]rune(text)), result.TaskID)
	writeJSON(w, http.StatusOK, map[string]any{
		"text":    text,
		"task_id": result.TaskID,
	})
}

func mobileAgentVoiceFormat(header *multipart.FileHeader) (string, string) {
	if header == nil {
		return "", ""
	}
	ext := strings.ToLower(filepath.Ext(strings.TrimSpace(header.Filename)))
	if contentType, ok := speechAnalysisUploadContentTypes[ext]; ok {
		return ext, contentType
	}
	mimeType := strings.ToLower(strings.TrimSpace(header.Header.Get("Content-Type")))
	if separator := strings.IndexByte(mimeType, ';'); separator >= 0 {
		mimeType = strings.TrimSpace(mimeType[:separator])
	}
	switch mimeType {
	case "audio/webm":
		return ".webm", "audio/webm"
	case "audio/mp4", "video/mp4":
		return ".m4a", "audio/mp4"
	case "audio/aac":
		return ".aac", "audio/aac"
	case "audio/ogg":
		return ".ogg", "audio/ogg"
	case "audio/wav", "audio/x-wav":
		return ".wav", "audio/wav"
	case "audio/mpeg", "audio/mp3":
		return ".mp3", "audio/mpeg"
	default:
		return "", ""
	}
}
