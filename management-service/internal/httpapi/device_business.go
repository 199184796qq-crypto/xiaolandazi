package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image/jpeg"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/speechasr"
	"livecompanion/management/internal/storage"
)

const maxDeviceBusinessBytes int64 = 2 << 20

// The gateway waits 30s. Bound all request work to 29s and reserve the last
// second for the persisted result; optional acoustic analysis must not consume
// that reserve. Private-object cleanup has its own bounded defer after Flush.
const deviceVoiceResponseTimeout = 29 * time.Second
const deviceVoiceCommitReserve = time.Second

var deviceRequestIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{7,95}$`)

type deviceBusinessLimiter struct {
	mu      sync.Mutex
	windows map[string]deviceClaimWindow
}

func (l *deviceBusinessLimiter) allow(key string, max int) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	if l.windows == nil {
		l.windows = map[string]deviceClaimWindow{}
	}
	for k, v := range l.windows {
		if now.After(v.until) {
			delete(l.windows, k)
		}
	}
	if len(l.windows) >= 10000 {
		return false
	}
	v := l.windows[key]
	if v.until.IsZero() {
		v.until = now.Add(time.Minute)
	}
	if v.count >= max {
		return false
	}
	v.count++
	l.windows[key] = v
	return true
}

func (s *Server) registerDeviceBusinessRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /internal/v1/xiaozhi/voice", s.deviceVoice)
	mux.HandleFunc("POST /internal/v1/xiaozhi/command-understand", s.deviceCommandUnderstand)
	mux.HandleFunc("POST /internal/v1/xiaozhi/capture", s.deviceCapture)
	mux.HandleFunc("POST /internal/v1/xiaozhi/command-events", s.deviceCommandEvent)
	mux.HandleFunc("POST /internal/v1/xiaozhi/command-dispatch", s.deviceCommandDispatch)
	mux.HandleFunc("GET /internal/v1/xiaozhi/business-events", s.deviceBusinessQueue)
	mux.HandleFunc("POST /internal/v1/xiaozhi/business-results", s.deviceBusinessResult)
	mux.HandleFunc("GET /internal/v1/xiaozhi/business-events/{eventID}/image", s.internalDeviceBusinessImage)
	mux.HandleFunc("GET /api/v1/live/devices/{deviceID}/business-events", s.liveDeviceBusinessEvents)
	mux.HandleFunc("GET /api/v1/live/devices/{deviceID}/business-events/{eventID}/image", s.liveDeviceBusinessImage)
	mux.HandleFunc("GET /api/v1/live/devices/{deviceID}/addressing", s.liveDeviceAddressing)
	mux.HandleFunc("PUT /api/v1/live/devices/{deviceID}/addressing", s.liveSetDeviceAddressing)
}

func (s *Server) deviceBusinessHeaders(w http.ResponseWriter, r *http.Request) (string, string, bool) {
	if !s.authorizeHardware(w, r) {
		return "", "", false
	}
	mac, err := model.NormalizeHardwareMAC(r.Header.Get("X-Device-MAC"))
	if err != nil {
		writeError(w, 400, "X-Device-MAC 格式错误")
		return "", "", false
	}
	id := strings.TrimSpace(r.Header.Get("X-Request-ID"))
	if !deviceRequestIDPattern.MatchString(id) {
		writeError(w, 400, "X-Request-ID 必须为 8-96 位字母、数字或 . _ : -")
		return "", "", false
	}
	return mac, id, true
}

func readDeviceBusinessFile(w http.ResponseWriter, r *http.Request) ([]byte, string, error) {
	r.Body = http.MaxBytesReader(w, r.Body, maxDeviceBusinessBytes+(64<<10))
	if err := r.ParseMultipartForm(256 << 10); err != nil {
		return nil, "", errors.New("文件过大或上传格式无效")
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	if r.MultipartForm == nil || len(r.MultipartForm.File["file"]) != 1 {
		return nil, "", errors.New("请上传一份 file 文件")
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		return nil, "", errors.New("未收到文件")
	}
	defer file.Close()
	if header.Size <= 0 || header.Size > maxDeviceBusinessBytes {
		return nil, "", errors.New("文件不能为空且不得超过 2MB")
	}
	data, err := io.ReadAll(io.LimitReader(file, maxDeviceBusinessBytes+1))
	if err != nil || len(data) == 0 || int64(len(data)) > maxDeviceBusinessBytes {
		return nil, "", errors.New("文件不能为空且不得超过 2MB")
	}
	return data, header.Filename, nil
}

func deviceAudioFormat(data []byte) (string, string) {
	if len(data) >= 44 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WAVE" {
		return ".wav", "audio/wav"
	}
	if len(data) >= 27 && string(data[:4]) == "OggS" && bytes.Contains(data, []byte("OpusHead")) {
		return ".ogg", "audio/ogg"
	}
	return "", ""
}

func validateDeviceJPEG(data []byte) (int, int, error) {
	if len(data) < 4 || data[0] != 0xff || data[1] != 0xd8 {
		return 0, 0, errors.New("只支持 JPEG 图片")
	}
	cfg, err := jpeg.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width < 1 || cfg.Height < 1 || cfg.Width > 2048 || cfg.Height > 2048 {
		return 0, 0, errors.New("JPEG 图片无效或宽高超过 2048")
	}
	if _, err := jpeg.Decode(bytes.NewReader(data)); err != nil {
		return 0, 0, errors.New("JPEG 图片内容损坏")
	}
	return cfg.Width, cfg.Height, nil
}

type privateObjectWriter interface {
	PutPrivate(context.Context, string, io.Reader, string) error
}

func putPrivateDeviceObject(ctx context.Context, st storage.Store, key string, data []byte, mime string) error {
	if private, ok := st.(privateObjectWriter); ok {
		return private.PutPrivate(ctx, key, bytes.NewReader(data), mime)
	}
	if st.Driver() == "local" {
		return st.Put(ctx, key, bytes.NewReader(data), mime)
	}
	return errors.New("存储未提供私有对象写入")
}

func (s *Server) deviceBusinessError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, sql.ErrNoRows):
		writeError(w, 404, "设备、业务事件不存在或不属于当前用户")
	case errors.Is(err, appdb.ErrDeviceClaimUnavailable):
		writeError(w, 403, "设备未认领、归属已变更或不可用")
	case errors.Is(err, appdb.ErrDeviceBusinessConflict):
		writeError(w, 409, "请求正在处理、编号冲突或已结束，请勿重复执行")
	case errors.Is(err, appdb.ErrListenerCommandForbidden):
		writeError(w, 403, "监听设备仅支持收听及调整本机音量、字体")
	default:
		writeError(w, 500, "设备业务处理失败")
	}
}

func deviceBusinessResponse(event model.DeviceBusinessEvent) map[string]any {
	return map[string]any{"request_id": event.RequestID, "event_id": event.ID, "billing_mode": model.DeviceBillingMode, "charged_beans": 0, "status": event.Status}
}

func writeDeviceVoiceResponse(w http.ResponseWriter, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(data); err != nil {
		return err
	}
	// Small net/http JSON responses otherwise remain buffered until all deferred
	// cleanup finishes. Flush through standard Unwrap middleware, not a detached
	// goroutine, so the gateway can receive the persisted receipt immediately.
	return http.NewResponseController(w).Flush()
}

func (s *Server) deviceVoice(w http.ResponseWriter, r *http.Request) {
	voiceCtx, cancelVoice := context.WithTimeout(r.Context(), deviceVoiceResponseTimeout)
	defer cancelVoice()
	r = r.WithContext(voiceCtx)
	mac, id, ok := s.deviceBusinessHeaders(w, r)
	if !ok {
		return
	}
	data, _, err := readDeviceBusinessFile(w, r)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	ext, mime := deviceAudioFormat(data)
	if ext == "" {
		writeError(w, 400, "只支持 WAV 或 Ogg/Opus 语音")
		return
	}
	wakeText := strings.TrimSpace(r.FormValue("wake_text"))
	if wakeText != "" && wakeText != "小蓝小蓝" {
		writeError(w, 400, "wake_text 无效")
		return
	}
	hashInput := append(append([]byte(nil), data...), 0)
	hashInput = append(hashInput, wakeText...)
	hash := sha256.Sum256(hashInput)
	event, created, err := s.store.BeginDeviceBusiness(r.Context(), mac, id, "voice_control", hex.EncodeToString(hash[:]), "")
	if err != nil {
		s.deviceBusinessError(w, err)
		return
	}
	if !created {
		if event.Status == "recognized" || event.Status == "succeeded" {
			response := deviceBusinessResponse(event)
			response["text"] = event.Text
			response["speaker_addressing"] = s.cachedVoiceAddressing(r.Context(), event)
			response["kind"] = "command"
			if event.Action == "wake_greeting" {
				response["kind"] = "wake_greeting"
			}
			_ = writeDeviceVoiceResponse(w, response)
			return
		}
		s.deviceBusinessError(w, appdb.ErrDeviceBusinessConflict)
		return
	}
	finished := false
	defer func() {
		if !finished {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			status := "failed"
			if r.Context().Err() != nil {
				status = "timeout"
			}
			_ = s.store.FailDeviceBusiness(ctx, event.ID, event.TenantID, status, "语音处理失败，请重新发起指令")
		}
	}()
	if !s.deviceBusinessLimits.allow("voice:"+mac, 30) {
		writeError(w, 429, "语音请求过于频繁")
		return
	}
	if s.assetStorage == nil {
		writeError(w, 503, "语音识别存储尚未初始化")
		return
	}
	st, err := s.assetStorage.For("oss")
	if err != nil {
		writeError(w, 503, "语音识别私有存储不可用")
		return
	}
	client := speechasr.NewFromEnv()
	if client.Configured() != nil {
		writeError(w, 503, "语音识别服务尚未配置")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	key, err := newMediaObjectKey(event.TenantID, "device-control-voice", "voice"+ext)
	if err != nil {
		writeError(w, 500, "生成语音文件编号失败")
		return
	}
	if err := putPrivateDeviceObject(ctx, st, key, data, mime); err != nil {
		writeError(w, 502, "上传语音失败")
		return
	}
	defer func() {
		cleanup, done := context.WithTimeout(context.Background(), 3*time.Second)
		defer done()
		_ = st.Delete(cleanup, key)
	}()
	url, err := st.SignedURL(ctx, key, 2*time.Minute)
	if err != nil || url == "" {
		writeError(w, 502, "无法生成语音识别地址")
		return
	}
	result, err := client.Transcribe(ctx, url)
	if err != nil {
		// A locally detected wake with no command commonly contains only the
		// post-detection silence. It is still a valid wake greeting. If speech
		// recognition is unavailable, never infer or execute a command.
		if wakeText != "" {
			result.Text = ""
		} else {
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				finish, done := context.WithTimeout(context.Background(), 3*time.Second)
				_ = s.store.FailDeviceBusiness(finish, event.ID, event.TenantID, "timeout", "语音识别超时")
				done()
				finished = true
			}
			writeError(w, 502, "语音识别失败，请再说一次")
			return
		}
	}
	text := strings.TrimSpace(result.Text)
	if wakeText != "" && !strings.HasPrefix(strings.ReplaceAll(text, "，", ""), wakeText) {
		text = wakeText + text
	}
	if text == "" || utf8.RuneCountInString(text) > 1024 {
		writeError(w, 422, "未识别到有效单句指令")
		return
	}
	wakeOnly := wakeGreetingOnly(text)
	deadline, _ := voiceCtx.Deadline()
	addressingCtx, cancelAddressing := context.WithDeadline(voiceCtx, deadline.Add(-deviceVoiceCommitReserve))
	addressing := s.selectVoiceAddressing(addressingCtx, event, data, strings.TrimPrefix(ext, "."), wakeOnly)
	cancelAddressing()
	finishCtx, finishCancel := context.WithTimeout(r.Context(), time.Second)
	defer finishCancel()
	event, err = s.store.FinishDeviceVoiceAddressing(finishCtx, mac, event, text, addressing, wakeOnly)
	if err != nil {
		s.deviceBusinessError(w, err)
		return
	}
	finished = true
	out := deviceBusinessResponse(event)
	out["text"] = event.Text
	out["speaker_addressing"] = addressing.Addressing
	out["kind"] = "command"
	if wakeOnly {
		out["kind"] = "wake_greeting"
	}
	_ = writeDeviceVoiceResponse(w, out)
}

func (s *Server) deviceCapture(w http.ResponseWriter, r *http.Request) {
	mac, id, ok := s.deviceBusinessHeaders(w, r)
	if !ok {
		return
	}
	data, _, err := readDeviceBusinessFile(w, r)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	width, height, err := validateDeviceJPEG(data)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	question := strings.TrimSpace(r.FormValue("question"))
	if utf8.RuneCountInString(question) > 512 {
		writeError(w, 400, "问题最多 512 字")
		return
	}
	digest := sha256.Sum256(append(append([]byte(nil), data...), []byte("\x00"+question)...))
	imageHash := sha256.Sum256(data)
	event, created, err := s.store.BeginDeviceBusiness(r.Context(), mac, id, "environment_snapshot", hex.EncodeToString(digest[:]), question)
	if err != nil {
		s.deviceBusinessError(w, err)
		return
	}
	if !created {
		if event.AssetID != nil && (event.Status == "queued" || event.Status == "succeeded" || event.Status == "failed") {
			out := deviceBusinessResponse(event)
			out["accepted"] = true
			writeJSON(w, 200, out)
			return
		}
		s.deviceBusinessError(w, appdb.ErrDeviceBusinessConflict)
		return
	}
	finished := false
	defer func() {
		if !finished {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			_ = s.store.FailDeviceBusiness(ctx, event.ID, event.TenantID, "failed", "照片上传失败，请重新拍摄")
		}
	}()
	if !s.deviceBusinessLimits.allow("capture:"+mac, 10) {
		writeError(w, 429, "拍照请求过于频繁")
		return
	}
	if s.assetStorage == nil {
		writeError(w, 503, "照片私有存储尚未初始化")
		return
	}
	st, err := s.assetStorage.For("oss")
	if err != nil {
		writeError(w, 503, "照片私有 OSS 不可用")
		return
	}
	key, err := newMediaObjectKey(event.TenantID, "device-environment", "snapshot.jpg")
	if err != nil {
		writeError(w, 500, "生成照片编号失败")
		return
	}
	if err := putPrivateDeviceObject(r.Context(), st, key, data, "image/jpeg"); err != nil {
		writeError(w, 502, "保存照片失败")
		return
	}
	event, err = s.store.QueueDeviceSnapshot(r.Context(), mac, event, model.CreateMediaAssetInput{TenantID: event.TenantID, OriginalName: "环境快照.jpg", StorageDriver: st.Driver(), StorageBucket: st.Bucket(), ObjectKey: key, MIMEType: "image/jpeg", SizeBytes: uint64(len(data)), ChecksumSHA256: hex.EncodeToString(imageHash[:]), Metadata: map[string]any{"device_id": event.DeviceID, "request_id": event.RequestID, "room_id": event.RoomID, "width": width, "height": height, "private": true}})
	if err != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		_ = st.Delete(ctx, key)
		cancel()
		s.deviceBusinessError(w, err)
		return
	}
	finished = true
	out := deviceBusinessResponse(event)
	out["accepted"] = true
	writeJSON(w, 202, out)
}

func validateDeviceCommand(input *model.DeviceCommandEventInput) error {
	if !deviceRequestIDPattern.MatchString(input.RequestID) {
		return errors.New("request_id 格式错误")
	}
	mac, err := model.NormalizeHardwareMAC(input.HardwareMAC)
	if err != nil {
		return err
	}
	input.HardwareMAC = mac
	if input.Status == "success" {
		input.Status = "succeeded"
	}
	if input.Status == "error" {
		input.Status = "failed"
	}
	switch input.Status {
	case "succeeded", "failed", "timeout", "rejected":
	default:
		return errors.New("执行状态无效")
	}
	if utf8.RuneCountInString(input.Message) > 512 {
		return errors.New("message 最多 512 字")
	}
	allowed := false
	switch input.Action {
	case "volume":
		switch input.Operation {
		case "set", "adjust", "mute", "unmute", "query":
			allowed = true
		}
	case "font_size":
		switch input.Operation {
		case "set", "adjust", "query", "reset":
			allowed = true
		}
	case "capture":
		allowed = input.Operation == "" || input.Operation == "set"
	case "", "none":
		allowed = (input.Status == "rejected" || input.Status == "timeout" || input.Status == "failed") && input.Operation == ""
	}
	if !allowed {
		return errors.New("不支持的设备控制操作")
	}
	if len(input.Value) == 0 || string(input.Value) == "null" {
		if input.Status == "succeeded" && input.Action != "capture" {
			return errors.New("成功回执必须包含实际值")
		}
		return nil
	}
	if input.Action == "capture" || input.Action == "none" {
		return errors.New("该操作不接受 value")
	}
	var number float64
	err = json.Unmarshal(input.Value, &number)
	if err != nil && input.Action == "font_size" {
		var value string
		if json.Unmarshal(input.Value, &value) == nil {
			switch value {
			case "small":
				input.Value = json.RawMessage("0")
				return nil
			case "medium":
				input.Value = json.RawMessage("1")
				return nil
			case "large":
				input.Value = json.RawMessage("2")
				return nil
			}
		}
	}
	max := 100.0
	if input.Action == "font_size" {
		max = 2
	}
	if err != nil || number < 0 || number > max || number != float64(int(number)) {
		return errors.New("设备执行值越界或格式错误")
	}
	return nil
}

func (s *Server) deviceCommandEvent(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeHardware(w, r) {
		return
	}
	var input model.DeviceCommandEventInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, 400, "请求格式错误")
		return
	}
	if err := validateDeviceCommand(&input); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	event, err := s.store.RecordDeviceCommand(r.Context(), input)
	if err != nil {
		s.deviceBusinessError(w, err)
		return
	}
	writeJSON(w, 200, deviceBusinessResponse(event))
}

func validateDeviceDispatch(input *model.DeviceCommandDispatchInput) error {
	if input.Action != "volume" && input.Action != "font_size" && input.Action != "capture" {
		return errors.New("不支持的设备下发操作")
	}
	check := model.DeviceCommandEventInput{HardwareMAC: input.HardwareMAC, RequestID: input.RequestID, Action: input.Action, Operation: input.Operation, Status: "failed"}
	if err := validateDeviceCommand(&check); err != nil {
		return err
	}
	input.HardwareMAC = check.HardwareMAC
	return nil
}

func (s *Server) deviceCommandDispatch(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeHardware(w, r) {
		return
	}
	var input model.DeviceCommandDispatchInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, 400, "请求格式错误")
		return
	}
	if err := validateDeviceDispatch(&input); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	event, err := s.store.ClaimDeviceCommand(r.Context(), input)
	if err != nil {
		s.deviceBusinessError(w, err)
		return
	}
	out := deviceBusinessResponse(event)
	out["dispatch_allowed"] = true
	writeJSON(w, 200, out)
}

func deviceBusinessPage(r *http.Request) (int64, int, error) {
	before := int64(0)
	limit := 20
	var err error
	if v := r.URL.Query().Get("before_id"); v != "" {
		before, err = strconv.ParseInt(v, 10, 64)
		if err != nil || before < 1 {
			return 0, 0, errors.New("before_id 格式错误")
		}
	}
	if v := r.URL.Query().Get("limit"); v != "" {
		limit, err = strconv.Atoi(v)
		if err != nil || limit < 1 || limit > 100 {
			return 0, 0, errors.New("limit 需为 1-100")
		}
	}
	return before, limit, nil
}

func (s *Server) liveDeviceBusinessEvents(w http.ResponseWriter, r *http.Request) {
	_, tenant, ok := s.deviceCustomer(w, r)
	if !ok {
		return
	}
	id, ok := namedPathID(w, r, "deviceID", "设备")
	if !ok {
		return
	}
	before, limit, err := deviceBusinessPage(r)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	events, err := s.store.DeviceBusinessEvents(r.Context(), tenant, id, before, limit)
	if err != nil {
		s.deviceBusinessError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, events)
}

func (s *Server) deviceBusinessQueue(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeHardware(w, r) {
		return
	}
	tenant, err := strconv.ParseInt(r.URL.Query().Get("tenant_id"), 10, 64)
	if err != nil || tenant <= 0 {
		writeError(w, 400, "tenant_id 必填")
		return
	}
	before, limit, err := deviceBusinessPage(r)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	events, err := s.store.QueuedDeviceSnapshots(r.Context(), tenant, before, limit)
	if err != nil {
		s.deviceBusinessError(w, err)
		return
	}
	type queued struct {
		model.DeviceBusinessEvent
		HardwareMAC string `json:"hardware_mac"`
		ImageURL    string `json:"image_url"`
	}
	out := make([]queued, 0, len(events))
	for _, event := range events {
		mac, err := s.store.InternalDeviceBusinessMAC(r.Context(), event.TenantID, event.DeviceID)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			s.deviceBusinessError(w, err)
			return
		}
		out = append(out, queued{DeviceBusinessEvent: event, HardwareMAC: mac, ImageURL: fmt.Sprintf("/internal/v1/xiaozhi/business-events/%d/image", event.ID)})
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, out)
}

func (s *Server) deviceBusinessResult(w http.ResponseWriter, r *http.Request) {
	mac, id, ok := s.deviceBusinessHeaders(w, r)
	if !ok {
		return
	}
	var input struct {
		Status     string `json:"status"`
		ResultText string `json:"result_text"`
	}
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, 400, "请求格式错误")
		return
	}
	if (input.Status != "succeeded" && input.Status != "failed") || utf8.RuneCountInString(input.ResultText) > 8192 {
		writeError(w, 400, "业务结果状态或文本无效")
		return
	}
	event, err := s.store.CompleteDeviceSnapshot(r.Context(), mac, id, input.Status, strings.TrimSpace(input.ResultText))
	if err != nil {
		s.deviceBusinessError(w, err)
		return
	}
	writeJSON(w, 200, deviceBusinessResponse(event))
}

func (s *Server) liveDeviceBusinessImage(w http.ResponseWriter, r *http.Request) {
	_, tenant, ok := s.deviceCustomer(w, r)
	if !ok {
		return
	}
	device, ok := namedPathID(w, r, "deviceID", "设备")
	if !ok {
		return
	}
	event, ok := namedPathID(w, r, "eventID", "业务事件")
	if !ok {
		return
	}
	s.serveDeviceBusinessImage(w, r, tenant, device, event)
}

func (s *Server) internalDeviceBusinessImage(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeHardware(w, r) {
		return
	}
	mac, err := model.NormalizeHardwareMAC(r.Header.Get("X-Device-MAC"))
	if err != nil {
		writeError(w, 400, "X-Device-MAC 格式错误")
		return
	}
	identity, err := s.store.ResolveDeviceBusinessIdentity(r.Context(), mac)
	if err != nil {
		s.deviceBusinessError(w, err)
		return
	}
	event, ok := namedPathID(w, r, "eventID", "业务事件")
	if !ok {
		return
	}
	s.serveDeviceBusinessImage(w, r, identity.TenantID, identity.DeviceID, event)
}

func (s *Server) serveDeviceBusinessImage(w http.ResponseWriter, r *http.Request, tenant, device, event int64) {
	asset, err := s.store.DeviceBusinessAsset(r.Context(), tenant, device, event)
	if err != nil {
		s.deviceBusinessError(w, err)
		return
	}
	if s.assetStorage == nil {
		writeError(w, 503, "私有存储不可用")
		return
	}
	st, err := s.assetStorage.For(asset.StorageDriver)
	if err != nil {
		writeError(w, 503, "私有存储不可用")
		return
	}
	reader, err := st.Open(r.Context(), asset.ObjectKey)
	if err != nil {
		writeError(w, 404, "照片不存在")
		return
	}
	defer reader.Close()
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=\"snapshot-%d.jpg\"", event))
	_, _ = io.Copy(w, io.LimitReader(reader, maxDeviceBusinessBytes))
}
